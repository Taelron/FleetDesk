package probes

import "time"

// ProbeStatus represents the health state of a probed endpoint.
type ProbeStatus int

// ProbeStatus values, in health-state order.
const (
	ProbeStatusPending ProbeStatus = iota
	ProbeStatusUp
	ProbeStatusDown
	ProbeStatusDegraded
)

func (s ProbeStatus) String() string {
	switch s {
	case ProbeStatusUp:
		return "UP"
	case ProbeStatusDown:
		return "DOWN"
	case ProbeStatusDegraded:
		return "DEGRADED"
	default:
		return "PENDING"
	}
}

// ErrorClass is a closed enum of probe failure categories.
// Raw Go error strings must never leak to the UI.
type ErrorClass string

// ErrorClass values, the closed enum ErrorClass is restricted to.
const (
	ErrorClassNone       ErrorClass = "none"
	ErrorClassDNS        ErrorClass = "dns"
	ErrorClassConnect    ErrorClass = "connect"
	ErrorClassTimeout    ErrorClass = "timeout"
	ErrorClassTLS        ErrorClass = "tls"
	ErrorClassHTTPStatus ErrorClass = "http_status"
)

// TLSInfo holds TLS certificate details for an HTTPS probe.
type TLSInfo struct {
	Version      string
	Issuer       string
	Subject      string
	Expiry       time.Time
	DaysToExpiry int
}

// ProbeResult is the per-probe result returned from the connector to the view layer.
type ProbeResult struct {
	ProbeIndex  int
	Status      ProbeStatus
	Error       ErrorClass
	ErrorMsg    string // redacted — no proxy credentials
	Code        int
	Latency     time.Duration
	TTFB        time.Duration
	TLS         *TLSInfo // nil on plain HTTP
	BodyPreview string   // up to 2KB, JSON pretty-printed if applicable
	ProbeTime   time.Time
}
