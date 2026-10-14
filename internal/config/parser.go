package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// fleetFile is the raw YAML structure of a fleet configuration file.
type fleetFile struct {
	Name             string          `yaml:"name"`
	Type             string          `yaml:"type"`
	TenantID         string          `yaml:"tenant_id"`
	ActivityLogHours int             `yaml:"activity_log_hours"`
	DisplayTags      []string        `yaml:"display_tags"`
	Secrets          *secretsFile    `yaml:"secrets"`
	Defaults         defaultsFile    `yaml:"defaults"`
	Groups           []groupFile     `yaml:"groups"`
	Hosts            []hostEntryFile `yaml:"hosts"`
}

// secretsFile is the raw secrets: block (ADR-F011).
type secretsFile struct {
	Provider     string `yaml:"provider"`
	Vault        string `yaml:"vault"`
	Subscription string `yaml:"subscription"`
}

type commandEntryFile struct {
	Name        string `yaml:"name"`
	Group       string `yaml:"group"`
	Description string `yaml:"description"`
	Run         string `yaml:"run"`
}

type defaultsFile struct {
	User            string             `yaml:"user"`
	Port            int                `yaml:"port"`
	Timeout         string             `yaml:"timeout"`
	SystemdMode     string             `yaml:"systemd_mode"`
	ServiceFilter   []string           `yaml:"service_filter"`
	Logs            []logEntryFile     `yaml:"logs"`
	Commands        []commandEntryFile `yaml:"commands"`
	ErrorLogSince   string             `yaml:"error_log_since"`
	RefreshInterval string             `yaml:"refresh_interval"`
	RHOrgID         string             `yaml:"rh_org_id"`
	// RHActivationKey is a tombstone: the field was removed (ADR-F011), but
	// the YAML parser ignores unknown keys, so a file that still carries it
	// would otherwise load silently with a key nothing reads. A pointer, so
	// that its presence is detected, not just a non-empty value.
	RHActivationKey           *string `yaml:"rh_activation_key"`
	RHActivationKeySecretName string  `yaml:"rh_activation_key_secret_name"`
	SatelliteURL              string  `yaml:"satellite_url"`
}

type logEntryFile struct {
	Name string `yaml:"name"`
	Path string `yaml:"path"`
	Sudo bool   `yaml:"sudo"`
}

type groupFile struct {
	Name          string             `yaml:"name"`
	Hosts         []hostEntryFile    `yaml:"hosts"`
	ServiceFilter []string           `yaml:"service_filter"`
	Logs          []logEntryFile     `yaml:"logs"`
	Commands      []commandEntryFile `yaml:"commands"`
}

type hostEntryFile struct {
	Name          string             `yaml:"name"`
	Hostname      string             `yaml:"hostname"`
	User          string             `yaml:"user"`
	Port          int                `yaml:"port"`
	Timeout       string             `yaml:"timeout"`
	SystemdMode   string             `yaml:"systemd_mode"`
	ServiceFilter []string           `yaml:"service_filter"`
	Logs          []logEntryFile     `yaml:"logs"`
	Commands      []commandEntryFile `yaml:"commands"`
	RHOrgID       string             `yaml:"rh_org_id"`
	// RHActivationKey: tombstone, see defaultsFile.
	RHActivationKey           *string `yaml:"rh_activation_key"`
	RHActivationKeySecretName string  `yaml:"rh_activation_key_secret_name"`
	SatelliteURL              string  `yaml:"satellite_url"`
}

// probeFleetFile is the raw YAML structure for a probes fleet file.
type probeFleetFile struct {
	Name     string            `yaml:"name"`
	Type     string            `yaml:"type"`
	Defaults probeDefaultsFile `yaml:"defaults"`
	Groups   []probeGroupFile  `yaml:"groups"`
	Probes   []probeEntryFile  `yaml:"probes"`
}

type probeDefaultsFile struct {
	Interval           string `yaml:"interval"`
	Proxy              string `yaml:"proxy"`
	Timeout            string `yaml:"timeout"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

type probeGroupFile struct {
	Name   string           `yaml:"name"`
	Probes []probeEntryFile `yaml:"probes"`
}

type probeEntryFile struct {
	Name               string `yaml:"name"`
	URL                string `yaml:"url"`
	Protocol           string `yaml:"protocol"`
	ExpectedCode       int    `yaml:"expected_code"`
	Interval           string `yaml:"interval"`
	InsecureSkipVerify *bool  `yaml:"insecure_skip_verify"`
}

// ParseFleetFile reads and parses a single fleet YAML file.
func ParseFleetFile(path string) (Fleet, error) {
	// FleetDesk executes what a fleet file contains, so a file another user
	// can write is refused. A merely readable one is fine: fleet files hold
	// no secrets (ADR-F011) and are meant to be shared.
	info, err := os.Stat(path)
	if err != nil {
		return Fleet{}, fmt.Errorf("reading file: %w", err)
	}
	if mode := info.Mode().Perm(); mode&0o022 != 0 {
		return Fleet{}, fmt.Errorf("fleet file %s is writable by group or others (mode %04o); run: chmod go-w %s", path, mode, path)
	}
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's fleet directory listing
	if err != nil {
		return Fleet{}, fmt.Errorf("reading file: %w", err)
	}

	var raw fleetFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Fleet{}, fmt.Errorf("parsing YAML: %w", err)
	}

	// default name from filename
	name := raw.Name
	if name == "" {
		base := filepath.Base(path)
		name = strings.TrimSuffix(base, filepath.Ext(base))
	}

	// validate type
	switch raw.Type {
	case "vm", "azure", "kubernetes":
		// valid — parsed below
	case "probes":
		return parseProbeFleetFile(data, name, path)
	case "":
		return Fleet{}, fmt.Errorf("missing required field 'type' in %s", path)
	default:
		return Fleet{}, fmt.Errorf("unknown fleet type %q in %s (must be vm, azure, kubernetes, or probes)", raw.Type, path)
	}

	secrets, err := parseSecrets(raw.Secrets)
	if err != nil {
		return Fleet{}, err
	}
	if raw.Defaults.RHActivationKey != nil {
		return Fleet{}, fmt.Errorf("defaults: %w", errActivationKeyRemoved)
	}
	if secrets == nil {
		if err := rejectSecretNamesWithoutBlock(raw); err != nil {
			return Fleet{}, err
		}
	}

	// parse defaults
	defaults := HostDefaults{
		User:            raw.Defaults.User,
		Port:            raw.Defaults.Port,
		SystemdMode:     raw.Defaults.SystemdMode,
		ServiceFilter:   raw.Defaults.ServiceFilter,
		ErrorLogSince:   raw.Defaults.ErrorLogSince,
		RefreshInterval: raw.Defaults.RefreshInterval,
		RHOrgID:         raw.Defaults.RHOrgID,
		SatelliteURL:    raw.Defaults.SatelliteURL,

		RHActivationKeySecretName: raw.Defaults.RHActivationKeySecretName,
	}
	// Port 0 and User "" mean "let ssh decide" (~/.ssh/config, then ssh's own
	// defaults); the library dial and the handover both resolve them through
	// `ssh -G`.
	if strings.HasPrefix(defaults.User, "-") {
		return Fleet{}, fmt.Errorf("defaults: user must not begin with %q", "-")
	}
	if defaults.SystemdMode == "" {
		defaults.SystemdMode = "system"
	}
	if defaults.ErrorLogSince == "" {
		defaults.ErrorLogSince = "1 hour ago"
	}
	if defaults.RefreshInterval == "" {
		defaults.RefreshInterval = "15s"
	}
	if raw.Defaults.Timeout != "" {
		d, err := time.ParseDuration(raw.Defaults.Timeout)
		if err != nil {
			return Fleet{}, fmt.Errorf("invalid timeout %q: %w", raw.Defaults.Timeout, err)
		}
		defaults.Timeout = d
	} else {
		defaults.Timeout = 10 * time.Second
	}

	// parse default logs
	for _, l := range raw.Defaults.Logs {
		if l.Name == "" {
			return Fleet{}, fmt.Errorf("log entry missing required field 'name'")
		}
		if err := ValidateLogPath(l.Name, l.Path); err != nil {
			return Fleet{}, err
		}
		defaults.Logs = append(defaults.Logs, LogEntry(l))
	}

	// parse default commands
	for _, c := range raw.Defaults.Commands {
		ce := CommandEntry(c)
		if err := ValidateCommand(ce); err != nil {
			return Fleet{}, err
		}
		defaults.Commands = append(defaults.Commands, ce)
	}

	// parse groups
	var groups []HostGroup
	for _, g := range raw.Groups {
		groupLogs := convertLogEntries(g.Logs)
		groupCmds := convertCommandEntries(g.Commands)
		hosts, err := parseHosts(g.Hosts, defaults, g.ServiceFilter, groupLogs, groupCmds)
		if err != nil {
			return Fleet{}, fmt.Errorf("group %q: %w", g.Name, err)
		}
		groups = append(groups, HostGroup{
			Name:  g.Name,
			Hosts: hosts,
		})
	}

	// parse ungrouped hosts
	hosts, err := parseHosts(raw.Hosts, defaults, nil, nil, nil)
	if err != nil {
		return Fleet{}, fmt.Errorf("hosts: %w", err)
	}

	activityLogHours := raw.ActivityLogHours
	if activityLogHours <= 0 {
		activityLogHours = 3
	}

	return Fleet{
		Name:             name,
		Type:             raw.Type,
		TenantID:         raw.TenantID,
		ActivityLogHours: activityLogHours,
		DisplayTags:      raw.DisplayTags,
		Secrets:          secrets,
		Path:             path,
		Defaults:         defaults,
		Groups:           groups,
		Hosts:            hosts,
	}, nil
}

// parseHosts converts raw host entries, applying defaults where needed.
// groupFilter is the group-level service filter (can be nil).
// groupLogs is the group-level log entries (can be nil).
// groupCmds is the group-level command entries (can be nil).
func parseHosts(raw []hostEntryFile, defaults HostDefaults, groupFilter []string, groupLogs []LogEntry, groupCmds []CommandEntry) ([]HostEntry, error) {
	var hosts []HostEntry
	for _, r := range raw {
		if r.Name == "" {
			return nil, fmt.Errorf("host missing required field 'name'")
		}
		if r.Hostname == "" {
			return nil, fmt.Errorf("host %q missing required field 'hostname'", r.Name)
		}
		// A value beginning with "-" would be read by ssh as an option.
		if strings.HasPrefix(r.Hostname, "-") {
			return nil, fmt.Errorf("host %q: hostname must not begin with %q", r.Name, "-")
		}
		if strings.HasPrefix(r.User, "-") {
			return nil, fmt.Errorf("host %q: user must not begin with %q", r.Name, "-")
		}
		if r.RHActivationKey != nil {
			return nil, fmt.Errorf("host %q: %w", r.Name, errActivationKeyRemoved)
		}

		h := HostEntry{
			Name:         r.Name,
			Hostname:     r.Hostname,
			User:         r.User,
			Port:         r.Port,
			SystemdMode:  r.SystemdMode,
			RHOrgID:      r.RHOrgID,
			SatelliteURL: r.SatelliteURL,

			RHActivationKeySecretName: r.RHActivationKeySecretName,
		}

		// service filter: host → group → defaults
		switch {
		case len(r.ServiceFilter) > 0:
			h.ServiceFilter = r.ServiceFilter
		case len(groupFilter) > 0:
			h.ServiceFilter = groupFilter
		default:
			h.ServiceFilter = defaults.ServiceFilter
		}

		// apply defaults
		if h.User == "" {
			h.User = defaults.User
		}
		if h.Port == 0 {
			h.Port = defaults.Port
		}
		if h.SystemdMode == "" {
			h.SystemdMode = defaults.SystemdMode
		}
		// RH subscription: if host defines its own org, it's a complete override — don't inherit satellite_url
		if h.RHOrgID != "" {
			if h.RHActivationKeySecretName == "" {
				h.RHActivationKeySecretName = defaults.RHActivationKeySecretName
			}
		} else {
			h.RHOrgID = defaults.RHOrgID
			h.RHActivationKeySecretName = defaults.RHActivationKeySecretName
			h.SatelliteURL = defaults.SatelliteURL
		}

		if r.Timeout != "" {
			d, err := time.ParseDuration(r.Timeout)
			if err != nil {
				return nil, fmt.Errorf("host %q invalid timeout: %w", r.Name, err)
			}
			h.Timeout = d
		} else {
			h.Timeout = defaults.Timeout
		}

		// logs: merge defaults + group + host (additive, dedupe by name)
		hostLogs := convertLogEntries(r.Logs)
		merged := MergeLogEntries(defaults.Logs, groupLogs, hostLogs)
		for _, l := range merged {
			if err := ValidateLogPath(l.Name, l.Path); err != nil {
				return nil, fmt.Errorf("host %q: %w", r.Name, err)
			}
		}
		h.Logs = merged

		// commands: merge defaults + group + host (additive, dedupe by group/name)
		hostCmds := convertCommandEntries(r.Commands)
		mergedCmds := MergeCommands(defaults.Commands, groupCmds, hostCmds)
		for _, c := range mergedCmds {
			if err := ValidateCommand(c); err != nil {
				return nil, fmt.Errorf("host %q: %w", r.Name, err)
			}
		}
		h.Commands = mergedCmds

		hosts = append(hosts, h)
	}
	return hosts, nil
}

// convertLogEntries converts raw YAML log entries to domain LogEntry.
func convertLogEntries(raw []logEntryFile) []LogEntry {
	var entries []LogEntry
	for _, r := range raw {
		entries = append(entries, LogEntry(r))
	}
	return entries
}

// convertCommandEntries converts raw YAML command entries to domain CommandEntry.
func convertCommandEntries(raw []commandEntryFile) []CommandEntry {
	var entries []CommandEntry
	for _, r := range raw {
		entries = append(entries, CommandEntry(r))
	}
	return entries
}

// parseProbeFleetFile parses a probes fleet YAML file into a Fleet with ProbeFleet set.
func parseProbeFleetFile(data []byte, name, path string) (Fleet, error) {
	var raw probeFleetFile
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return Fleet{}, fmt.Errorf("parsing probes YAML: %w", err)
	}

	if raw.Name != "" {
		name = raw.Name
	}

	// parse defaults
	defaults := ProbeDefaults{
		Interval: 30 * time.Second,
		Timeout:  10 * time.Second,
	}
	if raw.Defaults.Interval != "" {
		d, err := time.ParseDuration(raw.Defaults.Interval)
		if err != nil {
			return Fleet{}, fmt.Errorf("invalid default interval %q: %w", raw.Defaults.Interval, err)
		}
		if d < 5*time.Second {
			return Fleet{}, fmt.Errorf("default interval %v must be >= 5s", d)
		}
		defaults.Interval = d
	}
	if raw.Defaults.Timeout != "" {
		d, err := time.ParseDuration(raw.Defaults.Timeout)
		if err != nil {
			return Fleet{}, fmt.Errorf("invalid default timeout %q: %w", raw.Defaults.Timeout, err)
		}
		defaults.Timeout = d
	}
	if raw.Defaults.Proxy != "" {
		if _, err := url.Parse(raw.Defaults.Proxy); err != nil {
			return Fleet{}, fmt.Errorf("invalid proxy URL %q: %w", raw.Defaults.Proxy, err)
		}
		defaults.ProxyURL = raw.Defaults.Proxy
	}
	defaults.InsecureSkipVerify = raw.Defaults.InsecureSkipVerify

	// parse groups
	var groups []ProbeGroup
	for _, g := range raw.Groups {
		if g.Name == "" {
			return Fleet{}, fmt.Errorf("probe group missing required field 'name'")
		}
		probes, err := parseProbeEntries(g.Probes)
		if err != nil {
			return Fleet{}, fmt.Errorf("group %q: %w", g.Name, err)
		}
		groups = append(groups, ProbeGroup{
			Name:   g.Name,
			Probes: probes,
		})
	}

	// parse ungrouped probes
	ungrouped, err := parseProbeEntries(raw.Probes)
	if err != nil {
		return Fleet{}, fmt.Errorf("probes: %w", err)
	}

	pf := &ProbeFleet{
		Defaults: defaults,
		Groups:   groups,
		Probes:   ungrouped,
	}

	return Fleet{
		Name:       name,
		Type:       "probes",
		Path:       path,
		ProbeFleet: pf,
	}, nil
}

// parseProbeEntries converts raw probe entries, applying defaults where needed.
func parseProbeEntries(raw []probeEntryFile) ([]ProbeEntry, error) {
	var entries []ProbeEntry
	for _, r := range raw {
		if r.Name == "" {
			return nil, fmt.Errorf("probe missing required field 'name'")
		}
		if r.URL == "" {
			return nil, fmt.Errorf("probe %q missing required field 'url'", r.Name)
		}
		u, err := url.Parse(r.URL)
		if err != nil {
			return nil, fmt.Errorf("probe %q invalid URL: %w", r.Name, err)
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return nil, fmt.Errorf("probe %q URL scheme must be http or https, got %q", r.Name, u.Scheme)
		}

		protocol := r.Protocol
		if protocol == "" {
			protocol = "http"
		}
		if protocol != "http" {
			return nil, fmt.Errorf("probe %q unsupported protocol %q (v1 supports http only)", r.Name, protocol)
		}

		expectedCode := r.ExpectedCode
		if expectedCode == 0 {
			expectedCode = 200
		}

		var interval time.Duration
		if r.Interval != "" {
			d, err := time.ParseDuration(r.Interval)
			if err != nil {
				return nil, fmt.Errorf("probe %q invalid interval: %w", r.Name, err)
			}
			if d < 5*time.Second {
				return nil, fmt.Errorf("probe %q interval %v must be >= 5s", r.Name, d)
			}
			interval = d
		}

		entries = append(entries, ProbeEntry{
			Name:               r.Name,
			URL:                r.URL,
			Protocol:           protocol,
			ExpectedCode:       expectedCode,
			Interval:           interval,
			InsecureSkipVerify: r.InsecureSkipVerify,
		})
	}
	return entries, nil
}

// errActivationKeyRemoved is the load error for a fleet file that still
// carries rh_activation_key. It names the field and its replacement; it
// never carries the value.
var errActivationKeyRemoved = errors.New("rh_activation_key is no longer accepted; use rh_activation_key_secret_name with a secrets: block (ADR-F011)")

// parseSecrets validates a fleet's secrets: block. A fleet with no block
// returns nil, which is valid. Each fleet has one provider.
func parseSecrets(raw *secretsFile) (*SecretsConfig, error) {
	if raw == nil {
		return nil, nil
	}
	supported := strings.Join(SupportedSecretProviders, ", ")
	if raw.Provider == "" {
		return nil, fmt.Errorf("secrets.provider is required (supported: %s)", supported)
	}
	if !slices.Contains(SupportedSecretProviders, raw.Provider) {
		return nil, fmt.Errorf("secrets.provider %q is not supported (supported: %s)", raw.Provider, supported)
	}
	if raw.Vault == "" {
		return nil, fmt.Errorf("secrets.vault is required for provider %s", raw.Provider)
	}
	if raw.Subscription == "" {
		return nil, fmt.Errorf("secrets.subscription is required for provider %s", raw.Provider)
	}
	return &SecretsConfig{Provider: raw.Provider, Vault: raw.Vault, Subscription: raw.Subscription}, nil
}

// rejectSecretNamesWithoutBlock fails a fleet in which a field names a secret
// but there is no secrets: block to resolve it from. It reads the raw file,
// not the inherited entries, so a name set where inheritance would discard it
// is still caught.
func rejectSecretNamesWithoutBlock(raw fleetFile) error {
	const field = "rh_activation_key_secret_name"
	fail := func(where string) error {
		return fmt.Errorf("%sfield %q names a secret but the fleet has no secrets: block", where, field)
	}
	if raw.Defaults.RHActivationKeySecretName != "" {
		return fail("defaults: ")
	}
	for _, g := range raw.Groups {
		for _, h := range g.Hosts {
			if h.RHActivationKeySecretName != "" {
				return fail(fmt.Sprintf("host %q: ", h.Name))
			}
		}
	}
	for _, h := range raw.Hosts {
		if h.RHActivationKeySecretName != "" {
			return fail(fmt.Sprintf("host %q: ", h.Name))
		}
	}
	return nil
}
