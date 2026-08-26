package probes

import (
	"bytes"
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestClassifyError_DNS(t *testing.T) {
	err := &net.DNSError{Err: "no such host", Name: "bad.example.com"}
	if got := classifyError(err); got != ErrorClassDNS {
		t.Errorf("classifyError(DNSError) = %q, want %q", got, ErrorClassDNS)
	}
}

func TestClassifyError_Connect(t *testing.T) {
	err := &net.OpError{Op: "connect", Err: fmt.Errorf("connection refused")}
	if got := classifyError(err); got != ErrorClassConnect {
		t.Errorf("classifyError(OpError) = %q, want %q", got, ErrorClassConnect)
	}
}

func TestClassifyError_Timeout(t *testing.T) {
	err := context.DeadlineExceeded
	if got := classifyError(err); got != ErrorClassTimeout {
		t.Errorf("classifyError(DeadlineExceeded) = %q, want %q", got, ErrorClassTimeout)
	}
}

func TestClassifyError_TLS_UnknownAuthority(t *testing.T) {
	err := x509.UnknownAuthorityError{Cert: &x509.Certificate{}}
	wrapped := fmt.Errorf("tls: %w", &err)
	if got := classifyError(wrapped); got != ErrorClassTLS {
		t.Errorf("classifyError(UnknownAuthority) = %q, want %q", got, ErrorClassTLS)
	}
}

func TestClassifyError_TLS_HostnameError(t *testing.T) {
	err := x509.HostnameError{Host: "wrong.com", Certificate: &x509.Certificate{}}
	wrapped := fmt.Errorf("tls: %w", &err)
	if got := classifyError(wrapped); got != ErrorClassTLS {
		t.Errorf("classifyError(HostnameError) = %q, want %q", got, ErrorClassTLS)
	}
}

func TestClassifyError_TLS_CertificateInvalid(t *testing.T) {
	err := x509.CertificateInvalidError{Reason: x509.Expired}
	wrapped := fmt.Errorf("tls: %w", &err)
	if got := classifyError(wrapped); got != ErrorClassTLS {
		t.Errorf("classifyError(CertificateInvalid) = %q, want %q", got, ErrorClassTLS)
	}
}

func TestClassifyError_Nil(t *testing.T) {
	if got := classifyError(nil); got != ErrorClassNone {
		t.Errorf("classifyError(nil) = %q, want %q", got, ErrorClassNone)
	}
}

func TestDeriveStatus(t *testing.T) {
	tests := []struct {
		name         string
		expectedCode int
		actualCode   int
		tls          *TLSInfo
		want         ProbeStatus
	}{
		{"code match no TLS", 200, 200, nil, ProbeStatusUp},
		{"code match cert OK", 200, 200, &TLSInfo{DaysToExpiry: 30}, ProbeStatusUp},
		{"code match cert expiring", 200, 200, &TLSInfo{DaysToExpiry: 6}, ProbeStatusDegraded},
		{"code match cert at boundary", 200, 200, &TLSInfo{DaysToExpiry: 7}, ProbeStatusUp},
		{"code mismatch", 200, 500, nil, ProbeStatusDown},
		{"code mismatch with TLS", 200, 404, &TLSInfo{DaysToExpiry: 30}, ProbeStatusDown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deriveStatus(tt.expectedCode, tt.actualCode, tt.tls, false)
			if got != tt.want {
				t.Errorf("deriveStatus(%d, %d, tls) = %v, want %v", tt.expectedCode, tt.actualCode, got, tt.want)
			}
		})
	}
}

func TestReadBodyPreview_JSON(t *testing.T) {
	data := `{"status":"ok","version":"1.2.3"}`
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(data)),
	}
	got := readBodyPreview(resp)
	// Should be pretty-printed
	var buf bytes.Buffer
	_ = json.Indent(&buf, []byte(data), "", "  ")
	want := buf.String()
	if got != want {
		t.Errorf("readBodyPreview(json) =\n%s\nwant\n%s", got, want)
	}
}

func TestReadBodyPreview_Text(t *testing.T) {
	data := "Hello, World!"
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/plain"}},
		Body:   io.NopCloser(strings.NewReader(data)),
	}
	got := readBodyPreview(resp)
	if got != data {
		t.Errorf("readBodyPreview(text) = %q, want %q", got, data)
	}
}

func TestReadBodyPreview_Binary(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"image/png"}},
		Body:   io.NopCloser(strings.NewReader("PNG binary data")),
	}
	got := readBodyPreview(resp)
	if got != "(binary — skipped)" {
		t.Errorf("readBodyPreview(binary) = %q, want %q", got, "(binary — skipped)")
	}
}

func TestReadBodyPreview_InvalidJSON(t *testing.T) {
	data := `{"broken": json`
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(data)),
	}
	got := readBodyPreview(resp)
	if got != data {
		t.Errorf("readBodyPreview(invalid json) = %q, want raw %q", got, data)
	}
}

func TestReadBodyPreview_Truncation(t *testing.T) {
	data := strings.Repeat("x", maxBodyPreview+100)
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"text/plain"}},
		Body:   io.NopCloser(strings.NewReader(data)),
	}
	got := readBodyPreview(resp)
	if !strings.HasSuffix(got, "... (truncated)") {
		t.Errorf("readBodyPreview(large) should end with truncation marker, got suffix: %q", got[len(got)-30:])
	}
}

func TestManagerStartStop(t *testing.T) {
	// Start a test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = fmt.Fprintf(w, `{"status":"ok"}`)
	}))
	defer ts.Close()

	entries := []config.ProbeEntry{
		{Name: "test", URL: ts.URL, ExpectedCode: 200, Interval: 0},
	}
	defaults := config.ProbeDefaults{
		Interval: 5 * time.Second,
		Timeout:  5 * time.Second,
	}

	ch := make(chan ProbeResult, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(testLogger())
	mgr.Start(ctx, entries, defaults, ch)

	// Should receive at least one result (immediate probe)
	select {
	case result := <-ch:
		if result.Status != ProbeStatusUp {
			t.Errorf("Status = %v, want Up", result.Status)
		}
		if result.Code != 200 {
			t.Errorf("Code = %d, want 200", result.Code)
		}
		if result.Latency <= 0 {
			t.Errorf("Latency = %v, want > 0", result.Latency)
		}
		if result.ProbeIndex != 0 {
			t.Errorf("ProbeIndex = %d, want 0", result.ProbeIndex)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for probe result")
	}

	// Cancel should stop goroutines and close channel
	cancel()
	// Drain remaining results
	for range ch {
	}
}

func TestManagerHTTPStatusMismatch(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	entries := []config.ProbeEntry{
		{Name: "test", URL: ts.URL, ExpectedCode: 200, Interval: 0},
	}
	defaults := config.ProbeDefaults{
		Interval: 5 * time.Second,
		Timeout:  5 * time.Second,
	}

	ch := make(chan ProbeResult, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(testLogger())
	mgr.Start(ctx, entries, defaults, ch)

	select {
	case result := <-ch:
		if result.Status != ProbeStatusDown {
			t.Errorf("Status = %v, want Down", result.Status)
		}
		if result.Error != ErrorClassHTTPStatus {
			t.Errorf("Error = %q, want %q", result.Error, ErrorClassHTTPStatus)
		}
		if result.Code != 500 {
			t.Errorf("Code = %d, want 500", result.Code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for probe result")
	}

	cancel()
	for range ch {
	}
}

func TestManagerDNSError(t *testing.T) {
	entries := []config.ProbeEntry{
		{Name: "test", URL: "http://this-host-does-not-exist.invalid/health", ExpectedCode: 200, Interval: 0},
	}
	defaults := config.ProbeDefaults{
		Interval: 5 * time.Second,
		Timeout:  5 * time.Second,
	}

	ch := make(chan ProbeResult, 10)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	mgr := NewManager(testLogger())
	mgr.Start(ctx, entries, defaults, ch)

	select {
	case result := <-ch:
		if result.Status != ProbeStatusDown {
			t.Errorf("Status = %v, want Down", result.Status)
		}
		if result.Error != ErrorClassDNS {
			t.Errorf("Error = %q, want %q", result.Error, ErrorClassDNS)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timeout waiting for probe result")
	}

	cancel()
	for range ch {
	}
}

func TestProbeStatusString(t *testing.T) {
	tests := []struct {
		status ProbeStatus
		want   string
	}{
		{ProbeStatusPending, "PENDING"},
		{ProbeStatusUp, "UP"},
		{ProbeStatusDown, "DOWN"},
		{ProbeStatusDegraded, "DEGRADED"},
	}
	for _, tt := range tests {
		if got := tt.status.String(); got != tt.want {
			t.Errorf("ProbeStatus(%d).String() = %q, want %q", tt.status, got, tt.want)
		}
	}
}

// classifyError is not exported but is in the same package, so we can test it directly.
func TestClassifyError_WrappedTimeout(t *testing.T) {
	inner := &net.OpError{
		Op:  "connect",
		Err: errors.New("i/o timeout"),
	}
	// Wrap with context deadline
	err := fmt.Errorf("Get: %w", context.DeadlineExceeded)
	_ = inner // just to show the scenario
	if got := classifyError(err); got != ErrorClassTimeout {
		t.Errorf("classifyError(wrapped deadline) = %q, want %q", got, ErrorClassTimeout)
	}
}
