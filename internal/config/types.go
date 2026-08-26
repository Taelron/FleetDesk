package config

import "time"

// Fleet represents a parsed fleet configuration file.
type Fleet struct {
	Name             string       `yaml:"name"`
	Type             string       `yaml:"type"`
	TenantID         string       `yaml:"tenant_id"`
	ActivityLogHours int          `yaml:"activity_log_hours"`
	DisplayTags      []string     `yaml:"display_tags"`
	Path             string       `yaml:"-"`
	Defaults         HostDefaults `yaml:"defaults"`
	Groups           []HostGroup  `yaml:"groups"`
	Hosts            []HostEntry  `yaml:"hosts"`
	ProbeFleet       *ProbeFleet  `yaml:"-"` // non-nil only when Type == "probes"
}

// HostDefaults holds default values applied to all hosts in a fleet.
type HostDefaults struct {
	User            string         `yaml:"user"`
	Port            int            `yaml:"port"`
	Timeout         time.Duration  `yaml:"timeout"`
	SystemdMode     string         `yaml:"systemd_mode"`
	ServiceFilter   []string       `yaml:"service_filter"`
	Logs            []LogEntry     `yaml:"-"` // populated by parser via merge cascade
	Commands        []CommandEntry `yaml:"-"` // populated by parser via merge cascade
	ErrorLogSince   string
	RefreshInterval string
	RHOrgID         string `yaml:"rh_org_id"`
	RHActivationKey string `yaml:"rh_activation_key"`
	SatelliteURL    string `yaml:"satellite_url"`
}

// HostGroup provides visual grouping of hosts.
type HostGroup struct {
	Name  string      `yaml:"name"`
	Hosts []HostEntry `yaml:"hosts"`
}

// HostEntry represents a single host definition in a fleet file.
type HostEntry struct {
	Name            string        `yaml:"name"`
	Hostname        string        `yaml:"hostname"`
	User            string        `yaml:"user"`
	Port            int           `yaml:"port"`
	Timeout         time.Duration `yaml:"timeout"`
	SystemdMode     string        `yaml:"systemd_mode"`
	ServiceFilter   []string
	Logs            []LogEntry     // merged from defaults + group + host
	Commands        []CommandEntry // merged from defaults + group + host
	RHOrgID         string         `yaml:"rh_org_id"`
	RHActivationKey string         `yaml:"rh_activation_key"`
	SatelliteURL    string         `yaml:"satellite_url"`
}

// Host is the runtime representation of a host with connection state.
type Host struct {
	Entry         HostEntry
	Group         string
	Status        HostStatus
	NeedsPassword bool
	ErrorLogSince string

	// probe results
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
	SudoReady            bool
	SupervisorctlPresent bool
	Error                string
}

// HostStatus represents the connection state of a host.
type HostStatus int

// HostStatus values, in connection-state order.
const (
	HostConnecting HostStatus = iota
	HostOnline
	HostUnreachable
)

// HostMetrics holds live system metrics for a host.
type HostMetrics struct {
	CPUPercent  int
	MemPercent  int
	DiskPercent int
	Load        string
	Uptime      string
}

// Service represents a systemd unit on a remote host.
type Service struct {
	Name        string
	State       string
	Enabled     string
	Description string
}

// Container represents a Podman container on a remote host.
type Container struct {
	Name   string
	Image  string
	Status string
	ID     string
}

// CronJob represents a scheduled task.
type CronJob struct {
	Schedule string
	Command  string
	Source   string
}

// LogLevelEntry represents a log severity level with its count.
type LogLevelEntry struct {
	Level string
	Code  string
	Count int
}

// ErrorLog represents a journal error entry.
type ErrorLog struct {
	Time    string
	Unit    string
	Message string
}

// Update represents a pending package update.
type Update struct {
	Package string
	Version string
	Type    string
}

// Subscription represents a key-value pair from subscription-manager.
type Subscription struct {
	Field string
	Value string
}

// Disk represents a filesystem partition.
type Disk struct {
	Filesystem string
	Size       string
	Used       string
	Avail      string
	UsePercent string
	Mount      string
}

// Account represents a local user account on a remote host.
type Account struct {
	User           string
	UID            int
	Groups         string
	Shell          string
	LastLogin      string
	PasswordStatus string // PS (set), LK (locked), NP (no password)
	Expiry         string
	IsSudo         bool // true if groups contains "wheel" or "sudo"
	IsLocked       bool // true if PasswordStatus == "LK" or "L"
}

// NetInterface represents a network interface on a remote host.
type NetInterface struct {
	Name  string
	State string // UP, DOWN, UNKNOWN
	IPs   string // space-separated, CIDR stripped
	MTU   string
}

// ListeningPort represents a listening TCP port on a remote host.
type ListeningPort struct {
	Port        int
	Protocol    string
	Process     string
	BindAddress string
}

// Route represents a network route on a remote host.
type Route struct {
	Destination string
	Gateway     string
	Interface   string
	Metric      string
	IsDefault   bool
}

// FirewallRule represents a firewall rule from any backend.
type FirewallRule struct {
	Zone     string // zone name (firewalld) or chain name (iptables/nft)
	Service  string // service name or port/proto
	Protocol string // tcp, udp, or —
	Source   string // source IP or —
	Action   string // allow, drop, reject
	Backend  string // firewalld, nftables, iptables
}

// FailedLogin represents a failed SSH login attempt.
type FailedLogin struct {
	Time   string
	User   string
	Source string // source IP
	Method string // password, publickey, etc.
}

// SudoEntry represents a sudo usage event.
type SudoEntry struct {
	Time    string
	User    string
	Command string
	Result  string // success, failed
}

// SELinuxDenial represents an SELinux AVC denial.
type SELinuxDenial struct {
	Time   string
	Action string // e.g. "open", "read", "write"
	Source string // scontext type (e.g. httpd_t)
	Target string // tcontext type
	Class  string // e.g. "file", "dir", "process"
}

// AuditEvent represents an authentication event from aureport.
type AuditEvent struct {
	Time    string
	Type    string // auth, login, anomaly
	User    string
	Result  string // success, failed
	Message string
}

// ContainerDetail holds parsed podman inspect output.
type ContainerDetail struct {
	ID      string
	Image   string
	Created string
	Status  string
	Command string
	Env     []string
	Mounts  []string // "source:destination" format
	Ports   []string // "hostPort:containerPort/proto" format
}

// LogEntry is a named log file path on a remote host.
type LogEntry struct {
	Name string
	Path string
	Sudo bool
}

// Process represents a supervisord-managed process.
type Process struct {
	Name   string // full group:process name
	State  string // RUNNING, FATAL, STOPPED, BACKOFF, STARTING, EXITED
	Uptime string
	PID    string
}

// ProbeFleet holds the probes-fleet-specific config parsed from YAML.
type ProbeFleet struct {
	Defaults ProbeDefaults
	Groups   []ProbeGroup
	Probes   []ProbeEntry // ungrouped probes
}

// ProbeDefaults holds fleet-wide probe settings.
type ProbeDefaults struct {
	Interval           time.Duration // default probe interval (minimum 5s, default 30s)
	ProxyURL           string        // raw proxy URL — only passed into http.Transport closure, never logged
	Timeout            time.Duration // HTTP client timeout (default 10s)
	InsecureSkipVerify bool          // skip TLS certificate verification
}

// ProbeGroup provides visual grouping of probes.
type ProbeGroup struct {
	Name   string
	Probes []ProbeEntry
}

// ProbeEntry represents a single probe configuration.
type ProbeEntry struct {
	Name               string
	URL                string
	Protocol           string        // "http" only in v1
	ExpectedCode       int           // default 200
	Interval           time.Duration // 0 = use fleet default
	InsecureSkipVerify *bool         // nil = use fleet default, true/false = per-probe override
}

// ServiceStatus holds parsed systemctl show output for a service.
type ServiceStatus struct {
	Name        string
	Description string
	LoadState   string // loaded, not-found, masked
	ActiveState string // active, inactive, failed
	SubState    string // running, dead, exited, waiting
	PID         string
	Memory      string
	Tasks       string
	Since       string
	Enabled     string // enabled, disabled, static
}
