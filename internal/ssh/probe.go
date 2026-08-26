package ssh

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ProbeInfo holds the results of a host probe.
type ProbeInfo struct {
	FQDN                 string
	OS                   string
	UpSince              string
	CronCount            int
	ErrorCount           int
	DiskCount            int
	DiskHighCount        int
	UserCount            int
	InterfacesUp         int
	InterfacesTotal      int
	ListeningPorts       int
	UpdateCount          int
	SupervisorctlPresent bool
	SystemdMode          string
}

// FormatDateEU converts a date string to DD/MM/YYYY format.
func FormatDateEU(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || s == "—" || s == "unknown" {
		return s
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Format("02/01/2006")
		}
	}
	return s
}

// ParseProbeOutput parses the probe command output.
// Expected lines:
//
//	0: hostname
//	1: uptime -s
//	2: OS pretty name
//	3: cron count
//	4: error count (journalctl -p err)
//	5: disk count
//	6: disk high count
//	7: user count
//	8: interfaces up
//	9: interfaces total
//	10: listening ports
//	11: update count (dnf check-update)
//	12: supervisorctl present (1 or 0)
func ParseProbeOutput(output string, systemdMode string) (ProbeInfo, error) {
	// Strip any shell warnings (e.g., "Could not chdir to home directory")
	// that appear before the probe sentinel marker.
	if idx := strings.Index(output, "---PROBE---\n"); idx >= 0 {
		output = output[idx+len("---PROBE---\n"):]
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 3 {
		return ProbeInfo{}, fmt.Errorf("unexpected probe output (%d lines): %s", len(lines), output)
	}

	getInt := func(idx int) int {
		if idx < len(lines) {
			v, _ := strconv.Atoi(strings.TrimSpace(lines[idx]))
			return v
		}
		return 0
	}

	return ProbeInfo{
		FQDN:                 strings.TrimSpace(lines[0]),
		UpSince:              FormatDateEU(strings.TrimSpace(lines[1])),
		OS:                   strings.TrimSpace(lines[2]),
		CronCount:            getInt(3),
		ErrorCount:           getInt(4),
		DiskCount:            getInt(5),
		DiskHighCount:        getInt(6),
		UserCount:            getInt(7),
		InterfacesUp:         getInt(8),
		InterfacesTotal:      getInt(9),
		ListeningPorts:       getInt(10),
		UpdateCount:          getInt(11),
		SupervisorctlPresent: getInt(12) == 1,
		SystemdMode:          systemdMode,
	}, nil
}
