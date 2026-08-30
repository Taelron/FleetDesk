package ssh

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

func TestServiceStateOrder(t *testing.T) {
	tests := []struct {
		state string
		want  int
	}{
		{"failed", 0},
		{"running", 1},
		{"exited", 2},
		{"waiting", 3},
		{"inactive", 4},
		{"unknown", 5},
		{"", 5},
	}

	for _, tt := range tests {
		t.Run(tt.state, func(t *testing.T) {
			got := ServiceStateOrder(tt.state)
			if got != tt.want {
				t.Errorf("ServiceStateOrder(%q) = %d, want %d", tt.state, got, tt.want)
			}
		})
	}

	// verify ordering: failed < running < exited < waiting < inactive
	if ServiceStateOrder("failed") >= ServiceStateOrder("running") {
		t.Error("failed should sort before running")
	}
	if ServiceStateOrder("running") >= ServiceStateOrder("inactive") {
		t.Error("running should sort before inactive")
	}
}

func TestContainerStateOrder(t *testing.T) {
	tests := []struct {
		status string
		want   int
	}{
		{"Up 3 hours", 0},
		{"Up 5 minutes", 0},
		{"Exited (0) 2 hours ago", 1},
		{"Exited (1) 5 minutes ago", 1},
		{"Created", 2},
		{"", 2},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := ContainerStateOrder(tt.status)
			if got != tt.want {
				t.Errorf("ContainerStateOrder(%q) = %d, want %d", tt.status, got, tt.want)
			}
		})
	}
}

func TestParseServiceLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.Service
	}{
		{
			name: "running service",
			line: "nginx.service loaded active running A high performance web server",
			want: config.Service{Name: "nginx", State: "running", Enabled: "—", Description: "A high performance web server"},
		},
		{
			name: "failed service",
			line: "postgresql.service loaded failed failed PostgreSQL database server",
			want: config.Service{Name: "postgresql", State: "failed", Enabled: "—", Description: "PostgreSQL database server"},
		},
		{
			name: "inactive service",
			line: "sshd-keygen@.service loaded inactive dead OpenSSH per-connection server daemon",
			want: config.Service{Name: "sshd-keygen@", State: "inactive", Enabled: "—", Description: "OpenSSH per-connection server daemon"},
		},
		{
			name: "too few fields",
			line: "incomplete",
			want: config.Service{},
		},
		{
			name: "no description",
			line: "test.service loaded active running",
			want: config.Service{Name: "test", State: "running", Enabled: "—", Description: ""},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseServiceLine(tt.line)
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.State != tt.want.State {
				t.Errorf("State = %q, want %q", got.State, tt.want.State)
			}
			if got.Enabled != tt.want.Enabled {
				t.Errorf("Enabled = %q, want %q", got.Enabled, tt.want.Enabled)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
		})
	}
}

func TestMatchesFilter(t *testing.T) {
	tests := []struct {
		name    string
		svcName string
		filters []string
		want    bool
	}{
		{"empty filter matches all", "anything", nil, true},
		{"empty filter slice matches all", "anything", []string{}, true},
		{"exact glob match", "nginx", []string{"nginx"}, true},
		{"wildcard match", "automation-controller", []string{"automation-*"}, true},
		{"no match", "postgresql", []string{"automation-*"}, false},
		{"multiple filters, one matches", "nginx", []string{"automation-*", "nginx*"}, true},
		{"multiple filters, none match", "redis", []string{"automation-*", "nginx*"}, false},
		{"star matches all", "anything", []string{"*"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchesFilter(tt.svcName, tt.filters)
			if got != tt.want {
				t.Errorf("MatchesFilter(%q, %v) = %v, want %v", tt.svcName, tt.filters, got, tt.want)
			}
		})
	}
}

func TestIsAuthError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil error", nil, false},
		{"auth error", errString("unable to authenticate"), true},
		{"no methods", errString("no supported methods remain"), true},
		{"handshake", errString("handshake failed"), true},
		{"connection refused", errString("connection refused"), false},
		{"timeout", errString("i/o timeout"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsAuthError(tt.err)
			if got != tt.want {
				t.Errorf("IsAuthError(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestExpandPath(t *testing.T) {
	tests := []struct {
		name string
		path string
	}{
		{"tilde path", "~/.ssh/id_rsa"},
		{"absolute path", "/home/user/.ssh/id_rsa"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExpandPath(tt.path)
			if got == "" {
				t.Error("ExpandPath() returned empty string")
			}
			if tt.path[0] == '~' && got[0] == '~' {
				t.Error("tilde was not expanded")
			}
			if tt.path[0] == '/' && got != tt.path {
				t.Errorf("absolute path changed: %q → %q", tt.path, got)
			}
		})
	}
}

func TestExtractPkgName(t *testing.T) {
	tests := []struct {
		name string
		nvra string
		want string
	}{
		{"simple", "nginx-1.20.1-1.el9.x86_64", "nginx"},
		{"with epoch", "ansible-core-1:2.16.17-1.el9ap.noarch", "ansible-core"},
		{"complex name", "python3-dnf-plugins-core-4.3.0-5.el9.noarch", "python3-dnf-plugins-core"},
		{"no arch suffix", "simple-1.0-1", "simple"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractPkgName(tt.nvra)
			if got != tt.want {
				t.Errorf("ExtractPkgName(%q) = %q, want %q", tt.nvra, got, tt.want)
			}
		})
	}
}

func TestParseAccountLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.Account
	}{
		{
			name: "normal user",
			line: "jaminong|1000|jaminong wheel docker|/bin/bash|Apr 06 2026|PS|never",
			want: config.Account{
				User: "jaminong", UID: 1000, Groups: "wheel docker",
				Shell: "/bin/bash", LastLogin: "Apr 06 2026", PasswordStatus: "PS",
				Expiry: "never", IsSudo: true, IsLocked: false,
			},
		},
		{
			name: "locked user",
			line: "svcaccount|1001|svcaccount|/sbin/nologin|Never|LK|never",
			want: config.Account{
				User: "svcaccount", UID: 1001, Groups: "",
				Shell: "/sbin/nologin", LastLogin: "Never", PasswordStatus: "LK",
				Expiry: "never", IsSudo: false, IsLocked: true,
			},
		},
		{
			name: "sudo group member",
			line: "admin|1002|admin sudo|/bin/zsh|Mar 15 2026|PS|never",
			want: config.Account{
				User: "admin", UID: 1002, Groups: "sudo",
				Shell: "/bin/zsh", LastLogin: "Mar 15 2026", PasswordStatus: "PS",
				Expiry: "never", IsSudo: true, IsLocked: false,
			},
		},
		{
			name: "L status (locked alternate)",
			line: "olduser|1003|olduser|/bin/bash|Never|L|2025-12-31",
			want: config.Account{
				User: "olduser", UID: 1003, Groups: "",
				Shell: "/bin/bash", LastLogin: "Never", PasswordStatus: "L",
				Expiry: "2025-12-31", IsSudo: false, IsLocked: true,
			},
		},
		{
			name: "missing fields",
			line: "partial|1004",
			want: config.Account{User: "partial", UID: 1004},
		},
		{
			name: "empty line",
			line: "",
			want: config.Account{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAccountLine(tt.line)
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
			if got.UID != tt.want.UID {
				t.Errorf("UID = %d, want %d", got.UID, tt.want.UID)
			}
			if got.Groups != tt.want.Groups {
				t.Errorf("Groups = %q, want %q", got.Groups, tt.want.Groups)
			}
			if got.Shell != tt.want.Shell {
				t.Errorf("Shell = %q, want %q", got.Shell, tt.want.Shell)
			}
			if got.LastLogin != tt.want.LastLogin {
				t.Errorf("LastLogin = %q, want %q", got.LastLogin, tt.want.LastLogin)
			}
			if got.PasswordStatus != tt.want.PasswordStatus {
				t.Errorf("PasswordStatus = %q, want %q", got.PasswordStatus, tt.want.PasswordStatus)
			}
			if got.Expiry != tt.want.Expiry {
				t.Errorf("Expiry = %q, want %q", got.Expiry, tt.want.Expiry)
			}
			if got.IsSudo != tt.want.IsSudo {
				t.Errorf("IsSudo = %v, want %v", got.IsSudo, tt.want.IsSudo)
			}
			if got.IsLocked != tt.want.IsLocked {
				t.Errorf("IsLocked = %v, want %v", got.IsLocked, tt.want.IsLocked)
			}
		})
	}
}

func TestAccountStateOrder(t *testing.T) {
	locked := config.Account{IsLocked: true}
	normal := config.Account{}

	if AccountStateOrder(locked) >= AccountStateOrder(normal) {
		t.Error("locked should sort before normal")
	}
}

func TestParseInterfaceLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.NetInterface
	}{
		{
			name: "UP with single IP",
			line: "eth0             UP             10.0.0.1/24",
			want: config.NetInterface{Name: "eth0", State: "UP", IPs: "10.0.0.1"},
		},
		{
			name: "UNKNOWN with multiple IPs",
			line: "lo               UNKNOWN        127.0.0.1/8 ::1/128",
			want: config.NetInterface{Name: "lo", State: "UNKNOWN", IPs: "127.0.0.1 ::1"},
		},
		{
			name: "DOWN no IPs",
			line: "eth1             DOWN",
			want: config.NetInterface{Name: "eth1", State: "DOWN", IPs: ""},
		},
		{
			name: "UP with IPv6",
			line: "eth0             UP             10.138.1.132/24 fe80::7e1e:52ff:fe60:268d/64",
			want: config.NetInterface{Name: "eth0", State: "UP", IPs: "10.138.1.132 fe80::7e1e:52ff:fe60:268d"},
		},
		{
			name: "empty line",
			line: "",
			want: config.NetInterface{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseInterfaceLine(tt.line)
			if got.Name != tt.want.Name {
				t.Errorf("Name = %q, want %q", got.Name, tt.want.Name)
			}
			if got.State != tt.want.State {
				t.Errorf("State = %q, want %q", got.State, tt.want.State)
			}
			if got.IPs != tt.want.IPs {
				t.Errorf("IPs = %q, want %q", got.IPs, tt.want.IPs)
			}
		})
	}
}

func TestInterfaceStateOrder(t *testing.T) {
	if InterfaceStateOrder("UP") >= InterfaceStateOrder("UNKNOWN") {
		t.Error("UP should sort before UNKNOWN")
	}
	if InterfaceStateOrder("UNKNOWN") >= InterfaceStateOrder("DOWN") {
		t.Error("UNKNOWN should sort before DOWN")
	}
}

func TestParsePortLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.ListeningPort
	}{
		{
			name: "sshd with process",
			line: `LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:(("sshd",pid=1234,fd=3))`,
			want: config.ListeningPort{Port: 22, Protocol: "tcp", Process: "sshd", BindAddress: "0.0.0.0"},
		},
		{
			name: "postgres no process",
			line: `LISTEN 0 128 127.0.0.1:5432 0.0.0.0:*`,
			want: config.ListeningPort{Port: 5432, Protocol: "tcp", Process: "—", BindAddress: "127.0.0.1"},
		},
		{
			name: "IPv6 with process",
			line: `LISTEN 0 128 [::]:80 [::]:* users:(("nginx",pid=567,fd=6))`,
			want: config.ListeningPort{Port: 80, Protocol: "tcp", Process: "nginx", BindAddress: "::"},
		},
		{
			name: "IPv6 localhost",
			line: `LISTEN 0 128 [::1]:9090 [::]:*`,
			want: config.ListeningPort{Port: 9090, Protocol: "tcp", Process: "—", BindAddress: "::1"},
		},
		{
			name: "empty line",
			line: "",
			want: config.ListeningPort{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParsePortLine(tt.line)
			if got.Port != tt.want.Port {
				t.Errorf("Port = %d, want %d", got.Port, tt.want.Port)
			}
			if got.Process != tt.want.Process {
				t.Errorf("Process = %q, want %q", got.Process, tt.want.Process)
			}
			if got.BindAddress != tt.want.BindAddress {
				t.Errorf("BindAddress = %q, want %q", got.BindAddress, tt.want.BindAddress)
			}
		})
	}
}

func TestParseRouteLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.Route
	}{
		{
			name: "default route",
			line: "default via 10.0.0.1 dev eth0 proto static metric 100",
			want: config.Route{Destination: "default", Gateway: "10.0.0.1", Interface: "eth0", Metric: "100", IsDefault: true},
		},
		{
			name: "connected route",
			line: "10.0.0.0/24 dev eth0 proto kernel scope link src 10.0.0.5",
			want: config.Route{Destination: "10.0.0.0/24", Gateway: "direct", Interface: "eth0", Metric: "—", IsDefault: false},
		},
		{
			name: "route with metric",
			line: "172.16.0.0/16 via 10.0.0.254 dev eth1 metric 200",
			want: config.Route{Destination: "172.16.0.0/16", Gateway: "10.0.0.254", Interface: "eth1", Metric: "200", IsDefault: false},
		},
		{
			name: "empty line",
			line: "",
			want: config.Route{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseRouteLine(tt.line)
			if got.Destination != tt.want.Destination {
				t.Errorf("Destination = %q, want %q", got.Destination, tt.want.Destination)
			}
			if got.Gateway != tt.want.Gateway {
				t.Errorf("Gateway = %q, want %q", got.Gateway, tt.want.Gateway)
			}
			if got.Interface != tt.want.Interface {
				t.Errorf("Interface = %q, want %q", got.Interface, tt.want.Interface)
			}
			if got.Metric != tt.want.Metric {
				t.Errorf("Metric = %q, want %q", got.Metric, tt.want.Metric)
			}
			if got.IsDefault != tt.want.IsDefault {
				t.Errorf("IsDefault = %v, want %v", got.IsDefault, tt.want.IsDefault)
			}
		})
	}
}

func TestDetectFirewallBackend(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{"firewalld", "---FIREWALLD---\npublic (active)\n  services: ssh", "firewalld"},
		{"nftables", "---NFTABLES---\ntable inet filter {", "nftables"},
		{"iptables", "---IPTABLES---\nChain INPUT (policy ACCEPT)", "iptables"},
		{"empty", "", ""},
		{"no marker", "some random output", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFirewallBackend(tt.output)
			if got != tt.want {
				t.Errorf("DetectFirewallBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseFirewalldOutput(t *testing.T) {
	output := `public (active)
  target: default
  icmp-block-inversion: no
  interfaces: eth0
  sources:
  services: cockpit dhcpv6-client ssh
  ports: 8080/tcp 9090/tcp
  protocols:
  forward: yes
  masquerade: no
  forward-ports:
  source-ports:
  icmp-blocks:
  rich rules:

block
  target: %%REJECT%%
  icmp-block-inversion: no
  interfaces:
  sources:
  services:
  ports:
  protocols:
  forward: yes
  masquerade: no
  forward-ports:
  source-ports:
  icmp-blocks:
  rich rules:
`

	rules := ParseFirewalldOutput(output)

	// public zone has 3 services + 2 ports = 5 rules
	if len(rules) != 5 {
		t.Fatalf("got %d rules, want 5", len(rules))
	}

	// check first rule
	if rules[0].Zone != "public" {
		t.Errorf("rules[0].Zone = %q, want %q", rules[0].Zone, "public")
	}
	if rules[0].Service != "cockpit" {
		t.Errorf("rules[0].Service = %q, want %q", rules[0].Service, "cockpit")
	}
	if rules[0].Action != "allow" {
		t.Errorf("rules[0].Action = %q, want %q", rules[0].Action, "allow")
	}
	if rules[0].Backend != "firewalld" {
		t.Errorf("rules[0].Backend = %q, want %q", rules[0].Backend, "firewalld")
	}

	// check port rule
	found := false
	for _, r := range rules {
		if r.Service == "8080/tcp" {
			found = true
			if r.Zone != "public" {
				t.Errorf("port rule Zone = %q, want %q", r.Zone, "public")
			}
		}
	}
	if !found {
		t.Error("expected port rule 8080/tcp not found")
	}

	// block zone should be skipped (no services, no ports)
}

func TestParseFirewalldOutput_Empty(t *testing.T) {
	rules := ParseFirewalldOutput("")
	if len(rules) != 0 {
		t.Errorf("got %d rules for empty input, want 0", len(rules))
	}
}

func TestParseIptablesOutput(t *testing.T) {
	output := `Chain INPUT (policy ACCEPT)
num  target     prot opt source               destination
1    ACCEPT     tcp  --  0.0.0.0/0            0.0.0.0/0            tcp dpt:22
2    DROP       all  --  10.0.0.0/8           0.0.0.0/0

Chain FORWARD (policy DROP)
num  target     prot opt source               destination

Chain OUTPUT (policy ACCEPT)
num  target     prot opt source               destination
`

	rules := ParseIptablesOutput(output)
	if len(rules) < 2 {
		t.Fatalf("got %d rules, want at least 2", len(rules))
	}

	if rules[0].Zone != "INPUT" {
		t.Errorf("rules[0].Zone = %q, want %q", rules[0].Zone, "INPUT")
	}
	if rules[0].Action != "ACCEPT" {
		t.Errorf("rules[0].Action = %q, want %q", rules[0].Action, "ACCEPT")
	}
}

func TestParseNftablesOutput(t *testing.T) {
	output := `table inet filter {
	chain input {
		type filter hook input priority filter; policy accept;
		ct state established,related accept
		iif "lo" accept
		tcp dport 22 accept
		ip saddr 10.0.0.0/8 tcp dport { 80, 443 } accept
		counter drop
	}
	chain output {
		type filter hook output priority filter; policy accept;
	}
}`

	rules := ParseNftablesOutput(output)
	if len(rules) < 3 {
		t.Fatalf("got %d rules, want at least 3", len(rules))
	}

	// check we got accept and drop actions
	actions := map[string]bool{}
	for _, r := range rules {
		actions[r.Action] = true
		if r.Backend != "nftables" {
			t.Errorf("Backend = %q, want %q", r.Backend, "nftables")
		}
	}
	if !actions["accept"] {
		t.Error("expected at least one accept rule")
	}
	if !actions["drop"] {
		t.Error("expected at least one drop rule")
	}
}

func TestParseServiceStatus(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   config.ServiceStatus
	}{
		{
			name: "active running",
			output: `MainPID=1617
MemoryCurrent=10960896
TasksCurrent=1
Id=sshd.service
Description=OpenSSH server daemon
LoadState=loaded
ActiveState=active
SubState=running
UnitFileState=enabled
ActiveEnterTimestamp=Tue 2026-04-07 00:40:03 UTC`,
			want: config.ServiceStatus{
				Name: "sshd.service", Description: "OpenSSH server daemon",
				LoadState: "loaded", ActiveState: "active", SubState: "running",
				PID: "1617", Memory: "10.5M", Tasks: "1",
				Since: "Tue 2026-04-07 00:40:03 UTC", Enabled: "enabled",
			},
		},
		{
			name: "inactive dead",
			output: `MainPID=0
MemoryCurrent=[not set]
TasksCurrent=[not set]
Id=cups.service
Description=CUPS Printing Service
LoadState=loaded
ActiveState=inactive
SubState=dead
UnitFileState=disabled
ActiveEnterTimestamp=`,
			want: config.ServiceStatus{
				Name: "cups.service", Description: "CUPS Printing Service",
				LoadState: "loaded", ActiveState: "inactive", SubState: "dead",
				PID: "—", Memory: "—", Tasks: "—",
				Since: "—", Enabled: "disabled",
			},
		},
		{
			name: "failed",
			output: `MainPID=0
MemoryCurrent=0
TasksCurrent=0
Id=bad.service
Description=Bad Service
LoadState=loaded
ActiveState=failed
SubState=failed
UnitFileState=enabled
ActiveEnterTimestamp=Mon 2026-04-06 12:00:00 UTC`,
			want: config.ServiceStatus{
				Name: "bad.service", Description: "Bad Service",
				LoadState: "loaded", ActiveState: "failed", SubState: "failed",
				PID: "—", Memory: "0B", Tasks: "0",
				Since: "Mon 2026-04-06 12:00:00 UTC", Enabled: "enabled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseServiceStatus(tt.output)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestParseFailedLoginLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.FailedLogin
	}{
		{
			name: "failed password",
			line: "Apr 06 14:32:01 sshd[12345]: Failed password for user1 from 192.168.1.1 port 22 ssh2",
			want: config.FailedLogin{Time: "Apr 06 14:32:01", User: "user1", Source: "192.168.1.1", Method: "password"},
		},
		{
			name: "invalid user",
			line: "Apr 06 14:32:01 sshd[12345]: Invalid user admin from 10.0.0.5 port 55123",
			want: config.FailedLogin{Time: "Apr 06 14:32:01", User: "admin", Source: "10.0.0.5", Method: "invalid user"},
		},
		{
			name: "failed publickey",
			line: "Apr 06 14:32:01 sshd[12345]: Failed publickey for root from 172.16.0.1 port 22 ssh2",
			want: config.FailedLogin{Time: "Apr 06 14:32:01", User: "root", Source: "172.16.0.1", Method: "publickey"},
		},
		{
			name: "empty line",
			line: "",
			want: config.FailedLogin{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseFailedLoginLine(tt.line)
			if got.Time != tt.want.Time {
				t.Errorf("Time = %q, want %q", got.Time, tt.want.Time)
			}
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
			if got.Source != tt.want.Source {
				t.Errorf("Source = %q, want %q", got.Source, tt.want.Source)
			}
			if got.Method != tt.want.Method {
				t.Errorf("Method = %q, want %q", got.Method, tt.want.Method)
			}
		})
	}
}

func TestParseSudoLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.SudoEntry
	}{
		{
			name: "successful sudo",
			line: "Apr 06 14:32:01 sudo[12345]: user1 : TTY=pts/0 ; PWD=/home/user1 ; USER=root ; COMMAND=/bin/ls",
			want: config.SudoEntry{Time: "Apr 06 14:32:01", User: "user1", Command: "/bin/ls", Result: "success"},
		},
		{
			name: "failed sudo",
			line: "Apr 06 14:32:01 sudo[12345]: baduser : authentication failure ; TTY=pts/0 ; PWD=/home/baduser ; USER=root ; COMMAND=/bin/su",
			want: config.SudoEntry{Time: "Apr 06 14:32:01", User: "baduser", Command: "/bin/su", Result: "failed"},
		},
		{
			name: "not in sudoers",
			line: "Apr 06 14:32:01 sudo[12345]: nope : NOT in sudoers ; TTY=pts/0 ; PWD=/tmp ; USER=root ; COMMAND=/usr/bin/cat /etc/shadow",
			want: config.SudoEntry{Time: "Apr 06 14:32:01", User: "nope", Command: "/usr/bin/cat /etc/shadow", Result: "failed"},
		},
		{
			name: "empty line",
			line: "",
			want: config.SudoEntry{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSudoLine(tt.line)
			if got.Time != tt.want.Time {
				t.Errorf("Time = %q, want %q", got.Time, tt.want.Time)
			}
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
			if got.Command != tt.want.Command {
				t.Errorf("Command = %q, want %q", got.Command, tt.want.Command)
			}
			if got.Result != tt.want.Result {
				t.Errorf("Result = %q, want %q", got.Result, tt.want.Result)
			}
		})
	}
}

func TestParseSELinuxDenialLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.SELinuxDenial
	}{
		{
			name: "avc denial",
			line: `Apr 06 14:32:01 audit[1234]: avc:  denied  { open } for  pid=5678 comm="httpd" path="/var/www/html/index.html" scontext=system_u:system_r:httpd_t:s0 tcontext=unconfined_u:object_r:default_t:s0 tclass=file permissive=0`,
			want: config.SELinuxDenial{Time: "Apr 06 14:32:01", Action: "open", Source: "httpd_t", Target: "default_t", Class: "file"},
		},
		{
			name: "read denial",
			line: `Apr 06 15:00:00 audit[999]: avc:  denied  { read } for  pid=100 comm="nginx" scontext=system_u:system_r:nginx_t:s0 tcontext=system_u:object_r:var_log_t:s0 tclass=dir permissive=1`,
			want: config.SELinuxDenial{Time: "Apr 06 15:00:00", Action: "read", Source: "nginx_t", Target: "var_log_t", Class: "dir"},
		},
		{
			name: "empty line",
			line: "",
			want: config.SELinuxDenial{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSELinuxDenialLine(tt.line)
			if got.Time != tt.want.Time {
				t.Errorf("Time = %q, want %q", got.Time, tt.want.Time)
			}
			if got.Action != tt.want.Action {
				t.Errorf("Action = %q, want %q", got.Action, tt.want.Action)
			}
			if got.Source != tt.want.Source {
				t.Errorf("Source = %q, want %q", got.Source, tt.want.Source)
			}
			if got.Target != tt.want.Target {
				t.Errorf("Target = %q, want %q", got.Target, tt.want.Target)
			}
			if got.Class != tt.want.Class {
				t.Errorf("Class = %q, want %q", got.Class, tt.want.Class)
			}
		})
	}
}

func TestParseAuditEventLine(t *testing.T) {
	tests := []struct {
		name string
		line string
		want config.AuditEvent
	}{
		{
			name: "success with line number",
			line: "42. 04/06/2026 14:32:01 user1 pts/0 192.168.1.1 /usr/sbin/sshd yes 12345",
			want: config.AuditEvent{Time: "04/06/2026 14:32:01", Type: "auth", User: "user1", Result: "success", Message: "pts/0 192.168.1.1 /usr/sbin/sshd yes 12345"},
		},
		{
			name: "failure",
			line: "43. 04/06/2026 14:33:00 root pts/1 10.0.0.1 /usr/sbin/sshd no 12346",
			want: config.AuditEvent{Time: "04/06/2026 14:33:00", Type: "auth", User: "root", Result: "failed", Message: "pts/1 10.0.0.1 /usr/sbin/sshd no 12346"},
		},
		{
			name: "empty line",
			line: "",
			want: config.AuditEvent{},
		},
		{
			name: "too few fields",
			line: "one two",
			want: config.AuditEvent{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAuditEventLine(tt.line)
			if got.Time != tt.want.Time {
				t.Errorf("Time = %q, want %q", got.Time, tt.want.Time)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if got.User != tt.want.User {
				t.Errorf("User = %q, want %q", got.User, tt.want.User)
			}
			if got.Result != tt.want.Result {
				t.Errorf("Result = %q, want %q", got.Result, tt.want.Result)
			}
			if got.Message != tt.want.Message {
				t.Errorf("Message = %q, want %q", got.Message, tt.want.Message)
			}
		})
	}
}

func TestParseContainerInspect(t *testing.T) {
	input := `[
  {
    "Id": "abc123def456",
    "Created": "2026-04-01T10:00:00.000000000Z",
    "State": {
      "Status": "running",
      "Running": true,
      "StartedAt": "2026-04-01T10:00:01.000000000Z"
    },
    "ImageName": "nginx:latest",
    "Config": {
      "Env": ["PATH=/usr/local/sbin:/usr/local/bin", "NGINX_VERSION=1.25.0"],
      "Cmd": ["nginx", "-g", "daemon off;"]
    },
    "Mounts": [
      {"Source": "/data/nginx", "Destination": "/usr/share/nginx/html", "Type": "bind"}
    ],
    "HostConfig": {
      "PortBindings": {
        "80/tcp": [{"HostIp": "", "HostPort": "8080"}],
        "443/tcp": [{"HostIp": "", "HostPort": "8443"}]
      }
    }
  }
]`
	detail := ParseContainerInspect(input)
	if detail.ID == "" {
		t.Fatal("ID should not be empty")
	}
	if detail.ID != "abc123def456" {
		t.Errorf("ID = %q, want %q", detail.ID, "abc123def456")
	}
	if detail.Image != "nginx:latest" {
		t.Errorf("Image = %q, want %q", detail.Image, "nginx:latest")
	}
	if detail.Status != "running" {
		t.Errorf("Status = %q, want %q", detail.Status, "running")
	}
	if len(detail.Env) != 2 {
		t.Errorf("Env count = %d, want 2", len(detail.Env))
	}
	if len(detail.Mounts) != 1 {
		t.Errorf("Mounts count = %d, want 1", len(detail.Mounts))
	} else if detail.Mounts[0] != "/data/nginx:/usr/share/nginx/html" {
		t.Errorf("Mount = %q, want %q", detail.Mounts[0], "/data/nginx:/usr/share/nginx/html")
	}
	if len(detail.Ports) != 2 {
		t.Errorf("Ports count = %d, want 2", len(detail.Ports))
	}
	if detail.Command != "nginx -g daemon off;" {
		t.Errorf("Command = %q, want %q", detail.Command, "nginx -g daemon off;")
	}
}

func TestParseContainerInspect_Empty(t *testing.T) {
	detail := ParseContainerInspect("")
	if detail.ID != "" {
		t.Errorf("expected empty detail for empty input, got ID=%q", detail.ID)
	}
}

func TestParseContainerInspect_InvalidJSON(t *testing.T) {
	detail := ParseContainerInspect("not json")
	if detail.ID != "" {
		t.Errorf("expected empty detail for invalid JSON, got ID=%q", detail.ID)
	}
}

func TestParseContainerInspect_NoPorts(t *testing.T) {
	input := `[{"Id": "minimal123", "ImageName": "alpine:latest", "State": {"Status": "exited"}, "Config": {"Env": [], "Cmd": ["sh"]}, "Mounts": [], "HostConfig": {"PortBindings": {}}}]`
	detail := ParseContainerInspect(input)
	if detail.ID != "minimal123" {
		t.Errorf("ID = %q, want %q", detail.ID, "minimal123")
	}
	if len(detail.Ports) != 0 {
		t.Errorf("Ports = %d, want 0", len(detail.Ports))
	}
	if len(detail.Mounts) != 0 {
		t.Errorf("Mounts = %d, want 0", len(detail.Mounts))
	}
}

func TestParseMetricsOutput(t *testing.T) {
	tests := []struct {
		name   string
		output string
		want   config.HostMetrics
	}{
		{
			name:   "full output",
			output: "45\n72\n60%\n1.23\n2026-04-01 10:00:00",
			want:   config.HostMetrics{CPUPercent: 45, MemPercent: 72, DiskPercent: 60, Load: "1.23", Uptime: "01/04/2026"},
		},
		{
			name:   "high values",
			output: "99\n95\n92%\n8.50\n2026-03-15 08:30:00",
			want:   config.HostMetrics{CPUPercent: 99, MemPercent: 95, DiskPercent: 92, Load: "8.50", Uptime: "15/03/2026"},
		},
		{
			name:   "no percent sign on disk",
			output: "10\n20\n30\n0.05\n2026-01-01 00:00:00",
			want:   config.HostMetrics{CPUPercent: 10, MemPercent: 20, DiskPercent: 30, Load: "0.05", Uptime: "01/01/2026"},
		},
		{
			name:   "empty output",
			output: "",
			want:   config.HostMetrics{},
		},
		{
			name:   "partial output",
			output: "55\n40",
			want:   config.HostMetrics{CPUPercent: 55, MemPercent: 40},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseMetricsOutput(tt.output)
			if got.CPUPercent != tt.want.CPUPercent {
				t.Errorf("CPUPercent = %d, want %d", got.CPUPercent, tt.want.CPUPercent)
			}
			if got.MemPercent != tt.want.MemPercent {
				t.Errorf("MemPercent = %d, want %d", got.MemPercent, tt.want.MemPercent)
			}
			if got.DiskPercent != tt.want.DiskPercent {
				t.Errorf("DiskPercent = %d, want %d", got.DiskPercent, tt.want.DiskPercent)
			}
			if got.Load != tt.want.Load {
				t.Errorf("Load = %q, want %q", got.Load, tt.want.Load)
			}
			if got.Uptime != tt.want.Uptime {
				t.Errorf("Uptime = %q, want %q", got.Uptime, tt.want.Uptime)
			}
		})
	}
}

// errString is a simple error type for testing.
type errString string

func (e errString) Error() string { return string(e) }

func TestIsSudoOutput(t *testing.T) {
	tests := []struct {
		output string
		want   bool
	}{
		{"sudo: a password is required", true},
		{"sudo: no tty present and no askpass program specified", true},
		{"[sudo] password for gaetan:", true},
		{"sudo: no password supplied", true},
		{"", false},
		{"permission denied", false},
		{"exit status 1", false},
		// partial match
		{"some output\nsudo: a password is required\nmore output", true},
	}
	for _, tt := range tests {
		t.Run(tt.output, func(t *testing.T) {
			got := IsSudoOutput(tt.output)
			if got != tt.want {
				t.Errorf("IsSudoOutput(%q) = %v, want %v", tt.output, got, tt.want)
			}
		})
	}
}

func TestIsSudoError(t *testing.T) {
	if IsSudoError(nil) {
		t.Error("IsSudoError(nil) = true, want false")
	}
	if IsSudoError(errString("some other error")) {
		t.Error("IsSudoError(non-sudo error) = true, want false")
	}
	if !IsSudoError(ErrSudoRequired) {
		t.Error("IsSudoError(ErrSudoRequired) = false, want true")
	}
	// wrapped
	wrapped := fmt.Errorf("fetch failed: %w", ErrSudoRequired)
	if !IsSudoError(wrapped) {
		t.Error("IsSudoError(wrapped ErrSudoRequired) = false, want true")
	}
}

func TestIsPasswordRejectedExcludesTransportHandshakeFailures(t *testing.T) {
	// x/crypto/ssh wraps the whole clientHandshake (version exchange, key
	// exchange, cipher negotiation) in "ssh: handshake failed: %w", not
	// just password auth (client.go:83-85) -- so a key-exchange mismatch
	// or a version-string failure reaches here with the same prefix a
	// rejected password would carry, and must not be mistaken for one.
	rejected := fmt.Errorf("password auth: %w", errString("ssh: handshake failed: ssh: unable to authenticate, attempted methods [none password], no supported methods remain"))
	if !IsPasswordRejected(rejected) {
		t.Error("IsPasswordRejected(rejected password) = false, want true")
	}
	if !IsAuthError(rejected) {
		t.Error("IsAuthError(rejected password) = false, want true")
	}

	transport := fmt.Errorf("password auth: %w", errString("ssh: handshake failed: ssh: no common algorithm for key exchange"))
	if IsPasswordRejected(transport) {
		t.Error("IsPasswordRejected(key-exchange failure) = true, want false -- would falsely implicate a password never tried")
	}
	if !IsAuthError(transport) {
		t.Error("IsAuthError(key-exchange failure) = false, want true -- IsAuthError intentionally matches every handshake failure for the cheap show-a-prompt decision")
	}

	if IsPasswordRejected(nil) {
		t.Error("IsPasswordRejected(nil) = true, want false")
	}
}
