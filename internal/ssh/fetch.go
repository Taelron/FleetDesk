// Package ssh manages SSH connections to fleet hosts and fetches remote
// data (accounts, services, disks, logs) over them.
package ssh

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// ErrSudoRequired is returned when a command fails because sudo requires a password.
var ErrSudoRequired = errors.New("sudo password required")

// IsSudoOutput returns true if the command output indicates sudo needs a password.
func IsSudoOutput(output string) bool {
	return strings.Contains(output, "sudo: a password is required") ||
		strings.Contains(output, "sudo: no tty present") ||
		strings.Contains(output, "[sudo] password for") ||
		strings.Contains(output, "sudo: no password supplied")
}

// IsSudoError returns true if err wraps ErrSudoRequired.
func IsSudoError(err error) bool {
	return errors.Is(err, ErrSudoRequired)
}

// EscapeSingleQuotes escapes single quotes in s for safe embedding in a single-quoted shell string.
func EscapeSingleQuotes(s string) string {
	return strings.ReplaceAll(s, "'", `'\''`)
}

// ServiceStateOrder returns a sort priority for service states.
// Lower = shown first.
func ServiceStateOrder(state string) int {
	switch state {
	case "failed":
		return 0
	case "running":
		return 1
	case "exited":
		return 2
	case "waiting":
		return 3
	case "inactive":
		return 4
	default:
		return 5
	}
}

// ContainerStateOrder returns a sort priority for container states.
func ContainerStateOrder(status string) int {
	if strings.HasPrefix(status, "Up") {
		return 0
	}
	if strings.HasPrefix(status, "Exited") {
		return 1
	}
	return 2
}

// ParseServiceLine parses a single line from systemctl list-units output.
// Format: UNIT LOAD ACTIVE SUB DESCRIPTION...
func ParseServiceLine(line string) config.Service {
	fields := strings.Fields(line)
	if len(fields) < 4 {
		return config.Service{}
	}

	name := strings.TrimSuffix(fields[0], ".service")
	state := fields[2]
	sub := fields[3]

	display := state
	if state == "active" && sub != "" {
		display = sub
	}

	desc := ""
	if len(fields) > 4 {
		desc = strings.Join(fields[4:], " ")
	}

	return config.Service{
		Name:        name,
		State:       display,
		Enabled:     "—",
		Description: desc,
	}
}

// MatchesFilter returns true if the service name matches any of the filter patterns.
// If no filters are defined, all services match.
func MatchesFilter(name string, filters []string) bool {
	if len(filters) == 0 {
		return true
	}
	for _, pattern := range filters {
		if matched, _ := filepath.Match(pattern, name); matched {
			return true
		}
	}
	return false
}

// ExtractPkgName extracts the package name from an NVRA string.
func ExtractPkgName(nvra string) string {
	if idx := strings.LastIndex(nvra, "."); idx > 0 {
		tail := nvra[idx+1:]
		if tail == "x86_64" || tail == "noarch" || tail == "i686" || tail == "aarch64" || tail == "src" {
			nvra = nvra[:idx]
		}
	}
	lastDash := strings.LastIndex(nvra, "-")
	if lastDash <= 0 {
		return nvra
	}
	secondDash := strings.LastIndex(nvra[:lastDash], "-")
	if secondDash <= 0 {
		return nvra
	}
	return nvra[:secondDash]
}

// IsAuthError checks if an error is an SSH authentication failure.
func IsAuthError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "unable to authenticate") ||
		strings.Contains(s, "no supported methods remain") ||
		strings.Contains(s, "handshake failed")
}

// ParseAccountLine parses a pipe-delimited account line:
// user|uid|groups|shell|last_login|pw_status|expiry
func ParseAccountLine(line string) config.Account {
	if line == "" {
		return config.Account{}
	}
	parts := strings.SplitN(line, "|", 7)
	if len(parts) < 2 {
		return config.Account{}
	}

	a := config.Account{
		User: parts[0],
	}
	fmt.Sscanf(parts[1], "%d", &a.UID) //nolint:errcheck,gosec // TAE-82: a failed parse renders UID as 0, which sorts first

	if len(parts) > 2 {
		// filter out the primary group (same name as user)
		var filtered []string
		for _, g := range strings.Fields(parts[2]) {
			if g != a.User {
				filtered = append(filtered, g)
			}
		}
		a.Groups = strings.Join(filtered, " ")
	}
	if len(parts) > 3 {
		a.Shell = parts[3]
	}
	if len(parts) > 4 {
		a.LastLogin = parts[4]
	}
	if len(parts) > 5 {
		a.PasswordStatus = parts[5]
	}
	if len(parts) > 6 {
		a.Expiry = parts[6]
	}

	// detect sudo: wheel or sudo group
	groups := strings.Fields(a.Groups)
	for _, g := range groups {
		if g == "wheel" || g == "sudo" {
			a.IsSudo = true
			break
		}
	}

	// detect locked status
	a.IsLocked = a.PasswordStatus == "LK" || a.PasswordStatus == "L"

	return a
}

// AccountStateOrder returns sort priority for account states.
// Lower = shown first.
func AccountStateOrder(a config.Account) int {
	if a.IsLocked {
		return 0
	}
	return 1
}

// ParseInterfaceLine parses a line from `ip -br addr` output.
// Format: INTERFACE STATE [IP/PREFIX ...]
func ParseInterfaceLine(line string) config.NetInterface {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return config.NetInterface{}
	}
	iface := config.NetInterface{
		Name:  fields[0],
		State: fields[1],
	}
	// strip CIDR prefix from IPs
	var ips []string
	for _, f := range fields[2:] {
		if idx := strings.Index(f, "/"); idx > 0 {
			ips = append(ips, f[:idx])
		} else {
			ips = append(ips, f)
		}
	}
	iface.IPs = strings.Join(ips, " ")
	return iface
}

// InterfaceStateOrder returns sort priority for interface states.
// Lower = shown first.
func InterfaceStateOrder(state string) int {
	switch state {
	case "UP":
		return 0
	case "UNKNOWN":
		return 1
	case "DOWN":
		return 2
	default:
		return 3
	}
}

// ParsePortLine parses a line from `ss -tlnp` output.
// Format: State Recv-Q Send-Q Local_Address:Port Peer_Address:Port [Process]
func ParsePortLine(line string) config.ListeningPort {
	fields := strings.Fields(line)
	if len(fields) < 5 {
		return config.ListeningPort{}
	}

	local := fields[3] // e.g. "0.0.0.0:22", "[::]:80", "127.0.0.1:5432"
	var bindAddr string
	var port int

	if strings.HasPrefix(local, "[") {
		// IPv6: [::]:80 or [::1]:9090
		bracket := strings.LastIndex(local, "]:")
		if bracket > 0 {
			bindAddr = local[1:bracket]
			fmt.Sscanf(local[bracket+2:], "%d", &port) //nolint:errcheck,gosec // TAE-82: a failed parse renders the listening port as 0
		}
	} else {
		// IPv4: 0.0.0.0:22
		lastColon := strings.LastIndex(local, ":")
		if lastColon > 0 {
			bindAddr = local[:lastColon]
			fmt.Sscanf(local[lastColon+1:], "%d", &port) //nolint:errcheck,gosec // TAE-82: a failed parse renders the listening port as 0
		}
	}

	// extract process name from users:(("name",pid=X,fd=Y))
	process := "—"
	for _, f := range fields[5:] {
		if strings.HasPrefix(f, "users:") {
			if start := strings.Index(f, "((\""); start >= 0 {
				end := strings.Index(f[start+3:], "\"")
				if end > 0 {
					process = f[start+3 : start+3+end]
				}
			}
			break
		}
	}

	return config.ListeningPort{
		Port:        port,
		Protocol:    "tcp",
		Process:     process,
		BindAddress: bindAddr,
	}
}

// ParseRouteLine parses a line from `ip route` output.
func ParseRouteLine(line string) config.Route {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return config.Route{}
	}

	r := config.Route{
		Destination: fields[0],
		Gateway:     "direct",
		Metric:      "—",
		IsDefault:   fields[0] == "default",
	}

	for i := 1; i < len(fields)-1; i++ {
		switch fields[i] {
		case "via":
			r.Gateway = fields[i+1]
		case "dev":
			r.Interface = fields[i+1]
		case "metric":
			r.Metric = fields[i+1]
		}
	}

	return r
}

// DetectFirewallBackend returns the firewall backend from probe output markers.
func DetectFirewallBackend(output string) string {
	if strings.Contains(output, "---FIREWALLD---") {
		return "firewalld"
	}
	if strings.Contains(output, "---NFTABLES---") {
		return "nftables"
	}
	if strings.Contains(output, "---IPTABLES---") {
		return "iptables"
	}
	return ""
}

// ParseFirewalldOutput parses `firewall-cmd --list-all-zones` output into rules.
// Skips zones with no services and no ports.
func ParseFirewalldOutput(output string) []config.FirewallRule {
	var rules []config.FirewallRule
	var zoneName string
	var services, ports []string

	flush := func() {
		if zoneName == "" {
			return
		}
		for _, svc := range services {
			rules = append(rules, config.FirewallRule{
				Zone: zoneName, Service: svc, Protocol: "—", Source: "—",
				Action: "allow", Backend: "firewalld",
			})
		}
		for _, port := range ports {
			rules = append(rules, config.FirewallRule{
				Zone: zoneName, Service: port, Protocol: "—", Source: "—",
				Action: "allow", Backend: "firewalld",
			})
		}
		zoneName = ""
		services = nil
		ports = nil
	}

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)

		// zone header: starts at column 0 (not indented)
		if line != "" && line[0] != ' ' && line[0] != '\t' {
			flush()
			// strip "(active)" suffix
			name := strings.TrimSpace(strings.Replace(trimmed, "(active)", "", 1))
			if name != "" {
				zoneName = name
			}
			continue
		}

		if strings.HasPrefix(trimmed, "services:") {
			svcLine := strings.TrimPrefix(trimmed, "services:")
			services = append(services, strings.Fields(svcLine)...)
		} else if strings.HasPrefix(trimmed, "ports:") {
			portLine := strings.TrimPrefix(trimmed, "ports:")
			ports = append(ports, strings.Fields(portLine)...)
		}
	}
	flush()

	return rules
}

// ParseIptablesOutput parses `iptables -L -n --line-numbers` output into rules.
func ParseIptablesOutput(output string) []config.FirewallRule {
	var rules []config.FirewallRule
	var chain string

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		// chain header: "Chain INPUT (policy ACCEPT)"
		if strings.HasPrefix(trimmed, "Chain ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				chain = parts[1]
			}
			continue
		}

		// skip column header line
		if strings.HasPrefix(trimmed, "num") || strings.HasPrefix(trimmed, "target") {
			continue
		}

		fields := strings.Fields(trimmed)
		if len(fields) < 5 {
			continue
		}

		// fields: num target prot opt source destination [extras]
		action := fields[1]
		protocol := fields[2]
		source := fields[4]
		if source == "0.0.0.0/0" {
			source = "—"
		}

		// extract dpt:PORT if present
		service := "—"
		for _, f := range fields[6:] {
			if strings.HasPrefix(f, "dpt:") {
				service = strings.TrimPrefix(f, "dpt:")
				break
			}
		}

		rules = append(rules, config.FirewallRule{
			Zone:     chain,
			Service:  service,
			Protocol: protocol,
			Source:   source,
			Action:   action,
			Backend:  "iptables",
		})
	}

	return rules
}

// ParseNftablesOutput provides basic parsing of `nft list ruleset` output.
func ParseNftablesOutput(output string) []config.FirewallRule {
	var rules []config.FirewallRule
	var chain string

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)

		if strings.HasPrefix(trimmed, "chain ") {
			parts := strings.Fields(trimmed)
			if len(parts) >= 2 {
				chain = parts[1]
			}
			continue
		}

		// rule lines contain keywords like "accept", "drop", "reject"
		if chain != "" && (strings.Contains(trimmed, "accept") || strings.Contains(trimmed, "drop") || strings.Contains(trimmed, "reject")) {
			action := "—"
			if strings.Contains(trimmed, "accept") {
				action = "accept"
			} else if strings.Contains(trimmed, "drop") {
				action = "drop"
			} else if strings.Contains(trimmed, "reject") {
				action = "reject"
			}

			rules = append(rules, config.FirewallRule{
				Zone:    chain,
				Service: trimmed,
				Action:  action,
				Backend: "nftables",
			})
		}
	}

	return rules
}

// ParseFailedLoginLine parses a journalctl sshd line for failed/invalid login attempts.
// Expected formats:
//
//	Apr 06 14:32:01 sshd[12345]: Failed password for user1 from 192.168.1.1 port 22 ssh2
//	Apr 06 14:32:01 sshd[12345]: Invalid user admin from 192.168.1.1 port 22
func ParseFailedLoginLine(line string) config.FailedLogin {
	if line == "" {
		return config.FailedLogin{}
	}

	fl := config.FailedLogin{}

	// extract timestamp: first 15 characters (e.g. "Apr 06 14:32:01")
	if len(line) >= 15 {
		fl.Time = line[:15]
	}

	// detect method
	if strings.Contains(line, "Failed password") {
		fl.Method = "password"
	} else if strings.Contains(line, "Failed publickey") {
		fl.Method = "publickey"
	} else if strings.Contains(line, "Invalid user") {
		fl.Method = "invalid user"
	} else {
		fl.Method = "—"
	}

	// extract user: "for <user> from" or "Invalid user <user> from"
	if idx := strings.Index(line, "Invalid user "); idx >= 0 {
		rest := line[idx+len("Invalid user "):]
		if fromIdx := strings.Index(rest, " from "); fromIdx >= 0 {
			fl.User = rest[:fromIdx]
		}
	} else if idx := strings.Index(line, " for "); idx >= 0 {
		rest := line[idx+5:]
		if fromIdx := strings.Index(rest, " from "); fromIdx >= 0 {
			fl.User = rest[:fromIdx]
		}
	}

	// extract source IP: "from <IP> port"
	if idx := strings.Index(line, " from "); idx >= 0 {
		rest := line[idx+6:]
		if spIdx := strings.Index(rest, " "); spIdx >= 0 {
			fl.Source = rest[:spIdx]
		} else {
			fl.Source = rest
		}
	}

	return fl
}

// ParseSudoLine parses a journalctl sudo line.
// Expected format:
//
//	Apr 06 14:32:01 sudo[12345]: user1 : TTY=pts/0 ; PWD=/home/user1 ; USER=root ; COMMAND=/bin/ls
func ParseSudoLine(line string) config.SudoEntry {
	if line == "" {
		return config.SudoEntry{}
	}

	se := config.SudoEntry{}

	// timestamp
	if len(line) >= 15 {
		se.Time = line[:15]
	}

	// find the content after "sudo[...]: "
	sudoIdx := strings.Index(line, "]: ")
	if sudoIdx < 0 {
		return se
	}
	content := line[sudoIdx+3:]

	// user is first token before " : "
	if colonIdx := strings.Index(content, " : "); colonIdx >= 0 {
		se.User = content[:colonIdx]
		rest := content[colonIdx+3:]

		// check result
		if strings.Contains(rest, "authentication failure") || strings.Contains(rest, "NOT in sudoers") || strings.Contains(rest, "command not allowed") {
			se.Result = "failed"
		} else {
			se.Result = "success"
		}

		// extract COMMAND=
		if cmdIdx := strings.Index(rest, "COMMAND="); cmdIdx >= 0 {
			se.Command = rest[cmdIdx+8:]
		}
	}

	return se
}

// ParseSELinuxDenialLine parses an AVC denial line from journalctl or ausearch.
// Expected format:
//
//	... avc:  denied  { open } for ... scontext=...:httpd_t:... tcontext=...:default_t:... tclass=file ...
func ParseSELinuxDenialLine(line string) config.SELinuxDenial {
	if line == "" {
		return config.SELinuxDenial{}
	}

	d := config.SELinuxDenial{}

	// timestamp
	if len(line) >= 15 {
		d.Time = line[:15]
	}

	// action: { verb }
	if braceStart := strings.Index(line, "{ "); braceStart >= 0 {
		braceEnd := strings.Index(line[braceStart:], " }")
		if braceEnd > 0 {
			d.Action = line[braceStart+2 : braceStart+braceEnd]
		}
	}

	// scontext type: third colon-delimited field
	if idx := strings.Index(line, "scontext="); idx >= 0 {
		sctx := line[idx+9:]
		if spIdx := strings.Index(sctx, " "); spIdx >= 0 {
			sctx = sctx[:spIdx]
		}
		parts := strings.Split(sctx, ":")
		if len(parts) >= 3 {
			d.Source = parts[2]
		}
	}

	// tcontext type
	if idx := strings.Index(line, "tcontext="); idx >= 0 {
		tctx := line[idx+9:]
		if spIdx := strings.Index(tctx, " "); spIdx >= 0 {
			tctx = tctx[:spIdx]
		}
		parts := strings.Split(tctx, ":")
		if len(parts) >= 3 {
			d.Target = parts[2]
		}
	}

	// tclass
	if idx := strings.Index(line, "tclass="); idx >= 0 {
		cls := line[idx+7:]
		if spIdx := strings.Index(cls, " "); spIdx >= 0 {
			cls = cls[:spIdx]
		}
		d.Class = cls
	}

	return d
}

// ParseAuditEventLine parses an aureport --auth line.
// Expected format (aureport -i):
//
//  42. 04/06/2026 14:32:01 user1 pts/0 192.168.1.1 /usr/sbin/sshd yes 12345
func ParseAuditEventLine(line string) config.AuditEvent {
	if line == "" {
		return config.AuditEvent{}
	}

	fields := strings.Fields(line)
	if len(fields) < 5 {
		return config.AuditEvent{}
	}

	ae := config.AuditEvent{
		Type: "auth",
	}

	// skip line number (e.g. "42.")
	offset := 0
	if strings.HasSuffix(fields[0], ".") {
		offset = 1
	}

	if offset+1 < len(fields) {
		ae.Time = fields[offset]
		if offset+2 <= len(fields) {
			ae.Time += " " + fields[offset+1]
		}
	}
	if offset+2 < len(fields) {
		ae.User = fields[offset+2]
	}

	// result is the second-to-last field in aureport -i output
	if len(fields) > 1 {
		switch fields[len(fields)-2] {
		case "yes":
			ae.Result = "success"
		case "no":
			ae.Result = "failed"
		}
	}

	// message: remaining fields for context
	if offset+3 < len(fields) {
		ae.Message = strings.Join(fields[offset+3:], " ")
	}

	return ae
}

// ParseMetricsOutput parses the metrics command output.
// Expected: 5 lines — CPU%, MEM%, DISK%, LOAD, UP_SINCE
func ParseMetricsOutput(output string) config.HostMetrics {
	if output == "" {
		return config.HostMetrics{}
	}
	lines := strings.Split(strings.TrimSpace(output), "\n")
	m := config.HostMetrics{}
	if len(lines) >= 1 {
		fmt.Sscanf(strings.TrimSpace(lines[0]), "%d", &m.CPUPercent) //nolint:errcheck,gosec // TAE-82: a failed parse renders CPU% as 0 and silently skips the ≥80/≥90 alert colouring
	}
	if len(lines) >= 2 {
		fmt.Sscanf(strings.TrimSpace(lines[1]), "%d", &m.MemPercent) //nolint:errcheck,gosec // TAE-82: a failed parse renders MEM% as 0 and silently skips the ≥80/≥90 alert colouring
	}
	if len(lines) >= 3 {
		pct := strings.TrimSuffix(strings.TrimSpace(lines[2]), "%")
		fmt.Sscanf(pct, "%d", &m.DiskPercent) //nolint:errcheck,gosec // TAE-82: a failed parse renders DISK% as 0 and silently skips the ≥80/≥90 alert colouring
	}
	if len(lines) >= 4 {
		m.Load = strings.TrimSpace(lines[3])
	}
	if len(lines) >= 5 {
		m.Uptime = FormatDateEU(strings.TrimSpace(lines[4]))
	}
	return m
}

// ParseServiceStatus parses `systemctl show` key=value output into ServiceStatus.
func ParseServiceStatus(output string) config.ServiceStatus {
	props := make(map[string]string)
	for _, line := range strings.Split(output, "\n") {
		if idx := strings.Index(line, "="); idx > 0 {
			props[line[:idx]] = line[idx+1:]
		}
	}

	s := config.ServiceStatus{
		Name:        props["Id"],
		Description: props["Description"],
		LoadState:   props["LoadState"],
		ActiveState: props["ActiveState"],
		SubState:    props["SubState"],
		Enabled:     props["UnitFileState"],
		Since:       props["ActiveEnterTimestamp"],
	}

	// PID: 0 means not running
	if pid := props["MainPID"]; pid != "" && pid != "0" {
		s.PID = pid
	} else {
		s.PID = "—"
	}

	// Memory: convert bytes to human-readable
	if mem := props["MemoryCurrent"]; mem != "" && mem != "[not set]" {
		var bytes uint64
		fmt.Sscanf(mem, "%d", &bytes) //nolint:errcheck,gosec // TAE-82: a failed parse renders service memory as "0B"
		switch {
		case bytes >= 1<<30:
			s.Memory = fmt.Sprintf("%.1fG", float64(bytes)/float64(1<<30))
		case bytes >= 1<<20:
			s.Memory = fmt.Sprintf("%.1fM", float64(bytes)/float64(1<<20))
		case bytes >= 1<<10:
			s.Memory = fmt.Sprintf("%.1fK", float64(bytes)/float64(1<<10))
		default:
			s.Memory = fmt.Sprintf("%dB", bytes)
		}
	} else {
		s.Memory = "—"
	}

	// Tasks
	if tasks := props["TasksCurrent"]; tasks != "" && tasks != "[not set]" {
		s.Tasks = tasks
	} else {
		s.Tasks = "—"
	}

	// Since: empty means never started
	if s.Since == "" {
		s.Since = "—"
	}

	return s
}

// ParseContainerInspect parses podman inspect JSON output into ContainerDetail.
func ParseContainerInspect(output string) config.ContainerDetail {
	if output == "" {
		return config.ContainerDetail{}
	}

	var raw []struct {
		ID        string `json:"Id"`
		Created   string `json:"Created"`
		ImageName string `json:"ImageName"`
		State     struct {
			Status    string `json:"Status"`
			StartedAt string `json:"StartedAt"`
		} `json:"State"`
		Config struct {
			Env []string `json:"Env"`
			Cmd []string `json:"Cmd"`
		} `json:"Config"`
		Mounts []struct {
			Source      string `json:"Source"`
			Destination string `json:"Destination"`
		} `json:"Mounts"`
		HostConfig struct {
			PortBindings map[string][]struct {
				HostPort string `json:"HostPort"`
			} `json:"PortBindings"`
		} `json:"HostConfig"`
	}

	if err := json.Unmarshal([]byte(output), &raw); err != nil || len(raw) == 0 {
		return config.ContainerDetail{}
	}

	c := raw[0]
	detail := config.ContainerDetail{
		ID:      c.ID,
		Image:   c.ImageName,
		Created: c.Created,
		Status:  c.State.Status,
		Command: strings.Join(c.Config.Cmd, " "),
		Env:     c.Config.Env,
	}

	for _, m := range c.Mounts {
		detail.Mounts = append(detail.Mounts, m.Source+":"+m.Destination)
	}

	for containerPort, bindings := range c.HostConfig.PortBindings {
		for _, b := range bindings {
			detail.Ports = append(detail.Ports, b.HostPort+":"+containerPort)
		}
	}
	sort.Strings(detail.Ports)

	return detail
}

// ExpandPath expands ~ in paths.
func ExpandPath(path string) string {
	if len(path) > 1 && path[:2] == "~/" {
		home, _ := os.UserHomeDir()
		if home != "" {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
