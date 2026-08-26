// Package probes runs background health checks (SSH reachability, cert
// expiry) against fleet hosts and reports their status.
package probes

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// CertWarnDays is how many days before TLS certificate expiry a probe
// starts warning.
const (
	maxBodyPreview   = 2048
	CertWarnDays     = 7
	defaultUserAgent = "fleetdesk-probe/1.0"
)

// Manager coordinates HTTP endpoint probing.
type Manager struct {
	logger *slog.Logger
}

// NewManager creates a new probe manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{logger: logger}
}

// Start launches one goroutine per probe entry. Results are sent to ch.
// When ctx is cancelled, all goroutines exit and ch is closed.
// The caller must allocate ch (buffered recommended).
func (m *Manager) Start(ctx context.Context, entries []config.ProbeEntry, defaults config.ProbeDefaults, ch chan<- ProbeResult) {
	var wg sync.WaitGroup

	for i, entry := range entries {
		wg.Add(1)
		go func(idx int, e config.ProbeEntry) {
			defer wg.Done()
			m.probeLoop(ctx, idx, e, defaults, ch)
		}(i, entry)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()
}

func (m *Manager) probeLoop(ctx context.Context, idx int, entry config.ProbeEntry, defaults config.ProbeDefaults, ch chan<- ProbeResult) {
	interval := entry.Interval
	if interval == 0 {
		interval = defaults.Interval
	}

	timeout := defaults.Timeout
	if timeout >= interval {
		timeout = interval - time.Second
		if timeout < time.Second {
			timeout = time.Second
		}
	}

	// Resolve per-probe insecure_skip_verify: probe override → fleet default
	skipVerify := defaults.InsecureSkipVerify
	if entry.InsecureSkipVerify != nil {
		skipVerify = *entry.InsecureSkipVerify
	}

	client := buildHTTPClient(timeout, defaults.ProxyURL, skipVerify)

	// Probe immediately on start.
	result := runProbe(ctx, client, entry, idx, defaults.ProxyURL, skipVerify)
	m.logger.Debug("probe complete", "name", entry.Name, "status", result.Status.String(), "latency", result.Latency, "code", result.Code)
	select {
	case ch <- result:
	case <-ctx.Done():
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			result := runProbe(ctx, client, entry, idx, defaults.ProxyURL, skipVerify)
			m.logger.Debug("probe complete", "name", entry.Name, "status", result.Status.String(), "latency", result.Latency, "code", result.Code)
			select {
			case ch <- result:
			case <-ctx.Done():
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

func buildHTTPClient(timeout time.Duration, proxyURL string, insecureSkipVerify bool) *http.Client {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: insecureSkipVerify, //nolint:gosec // G402: the insecure_skip_verify config field, per-probe override
		},
	}

	if proxyURL != "" {
		parsed, err := url.Parse(proxyURL)
		if err == nil {
			transport.Proxy = func(*http.Request) (*url.URL, error) {
				return parsed, nil
			}
		}
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}
}

func runProbe(ctx context.Context, client *http.Client, entry config.ProbeEntry, idx int, proxyURL string, skipTLSCheck bool) ProbeResult {
	result := ProbeResult{
		ProbeIndex: idx,
		ProbeTime:  time.Now(),
		Error:      ErrorClassNone,
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, entry.URL, nil)
	if err != nil {
		result.Status = ProbeStatusDown
		result.Error = ErrorClassConnect
		result.ErrorMsg = RedactError(err, proxyURL)
		return result
	}
	req.Header.Set("User-Agent", defaultUserAgent)

	var ttfb time.Time
	trace := &httptrace.ClientTrace{
		GotFirstResponseByte: func() {
			ttfb = time.Now()
		},
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))

	start := time.Now()
	resp, err := client.Do(req)
	result.Latency = time.Since(start)

	if !ttfb.IsZero() {
		result.TTFB = ttfb.Sub(start)
	}

	if err != nil {
		result.Status = ProbeStatusDown
		result.Error = classifyError(err)
		result.ErrorMsg = RedactError(err, proxyURL)
		return result
	}
	defer func() { _ = resp.Body.Close() }()

	result.Code = resp.StatusCode

	// Extract TLS info
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		cert := resp.TLS.PeerCertificates[0]
		daysLeft := int(math.Floor(time.Until(cert.NotAfter).Hours() / 24))
		result.TLS = &TLSInfo{
			Version:      tlsVersionString(resp.TLS.Version),
			Issuer:       cert.Issuer.CommonName,
			Subject:      cert.Subject.CommonName,
			Expiry:       cert.NotAfter,
			DaysToExpiry: daysLeft,
		}
	}

	// Read body preview
	result.BodyPreview = readBodyPreview(resp)

	// Derive status
	result.Status = deriveStatus(entry.ExpectedCode, result.Code, result.TLS, skipTLSCheck)
	if result.Status == ProbeStatusDown && result.Error == ErrorClassNone {
		result.Error = ErrorClassHTTPStatus
		result.ErrorMsg = fmt.Sprintf("expected %d, got %d", entry.ExpectedCode, result.Code)
	}

	return result
}

func classifyError(err error) ErrorClass {
	if err == nil {
		return ErrorClassNone
	}

	// Timeout — check first because timed-out connects wrap both deadline and OpError
	if errors.Is(err, context.DeadlineExceeded) || isTimeoutError(err) {
		return ErrorClassTimeout
	}

	// DNS
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return ErrorClassDNS
	}

	// TLS
	var x509Unknown x509.UnknownAuthorityError
	if errors.As(err, &x509Unknown) {
		return ErrorClassTLS
	}
	var x509Hostname x509.HostnameError
	if errors.As(err, &x509Hostname) {
		return ErrorClassTLS
	}
	var certInvalid x509.CertificateInvalidError
	if errors.As(err, &certInvalid) {
		return ErrorClassTLS
	}
	// Check for "certificate" in error string as fallback for wrapped TLS errors
	if strings.Contains(strings.ToLower(err.Error()), "certificate") {
		return ErrorClassTLS
	}

	// Connect
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return ErrorClassConnect
	}

	return ErrorClassConnect
}

func isTimeoutError(err error) bool {
	type timeouter interface {
		Timeout() bool
	}
	var t timeouter
	if errors.As(err, &t) {
		return t.Timeout()
	}
	return false
}

func deriveStatus(expectedCode, actualCode int, tlsInfo *TLSInfo, skipTLSCheck bool) ProbeStatus {
	if actualCode != expectedCode {
		return ProbeStatusDown
	}
	if !skipTLSCheck && tlsInfo != nil && tlsInfo.DaysToExpiry < CertWarnDays {
		return ProbeStatusDegraded
	}
	return ProbeStatusUp
}

func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("TLS 0x%04x", v)
	}
}

func readBodyPreview(resp *http.Response) string {
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}
	ct = strings.ToLower(ct)

	isText := strings.HasPrefix(ct, "text/")
	isJSON := strings.Contains(ct, "application/json")

	if !isText && !isJSON {
		return "(binary — skipped)"
	}

	// Read up to 64KB for JSON pretty-printing, then truncate the formatted output.
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Sprintf("(read error: %v)", err)
	}

	s := string(body)

	if isJSON {
		var buf bytes.Buffer
		if err := json.Indent(&buf, body, "", "  "); err == nil {
			s = buf.String()
		}
	}

	if len(s) > maxBodyPreview {
		s = s[:maxBodyPreview] + "\n... (truncated)"
	}

	return sanitizeBodyPreview(s)
}

var ansiEscapeRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

// sanitizeBodyPreview strips ANSI escape sequences and non-printable characters
// from the body preview to prevent TUI rendering corruption.
func sanitizeBodyPreview(s string) string {
	s = ansiEscapeRe.ReplaceAllString(s, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || unicode.IsPrint(r) {
			return r
		}
		return -1
	}, s)
}
