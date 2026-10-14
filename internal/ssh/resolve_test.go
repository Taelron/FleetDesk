package ssh

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// isolatedHome points $HOME at a temp dir holding an ~/.ssh/config with the
// two aliases the fixture uses.
func isolatedHome(t *testing.T, extra string) string {
	t.Helper()
	if _, err := exec.LookPath("ssh"); err != nil {
		t.Skip("ssh binary not available")
	}
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := `Host alias
    Hostname 127.0.0.1
    Port 2222
    User fromconfig
    IdentityFile ~/.ssh/the_key

Host bastioned
    Hostname 127.0.0.1
    ProxyJump jump.invalid

Host cmdproxy
    ProxyCommand nc %h %p
` + extra
	if err := os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	return home
}

func TestSSHArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hargs := homeArgs()
	if len(hargs) == 0 {
		t.Skip("$HOME equals the passwd home on this machine; nothing to redirect")
	}
	cases := []struct {
		name string
		e    config.HostEntry
		want []string
	}{
		{"nothing set: ssh decides", config.HostEntry{Hostname: "h"}, []string{"--", "h"}},
		{"port only", config.HostEntry{Hostname: "h", Port: 2222}, []string{"-p", "2222", "--", "h"}},
		{"user only", config.HostEntry{Hostname: "h", User: "u"}, []string{"-o", "User=u", "--", "h"}},
		{"both", config.HostEntry{Hostname: "h", Port: 22, User: "u"}, []string{"-p", "22", "-o", "User=u", "--", "h"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := SSHArgs(c.e)
			want := append(slices.Clone(hargs), c.want...)
			if !slices.Equal(got, want) {
				t.Errorf("SSHArgs = %q, want %q", got, want)
			}
			if slices.Contains(got, "-l") {
				t.Error("must never use -l: ssh-copy-id has no -l")
			}
			if slices.Contains(got, "StrictHostKeyChecking=no") {
				t.Error("must never disable host-key checking")
			}
		})
	}
}

func TestSSHArgs_HomeRedirect(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if len(homeArgs()) == 0 {
		t.Skip("$HOME equals the passwd home on this machine")
	}
	// no ~/.ssh/config yet: only the known_hosts redirect
	got := strings.Join(homeArgs(), " ")
	if strings.Contains(got, "-F") || !strings.Contains(got, "UserKnownHostsFile="+filepath.Join(home, ".ssh", "known_hosts")) {
		t.Errorf("without a config: %q", got)
	}
	// with one, -F points at it
	if err := os.MkdirAll(filepath.Join(home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	cfg := filepath.Join(home, ".ssh", "config")
	if err := os.WriteFile(cfg, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := homeArgs(); !slices.Contains(got, "-F") || !slices.Contains(got, cfg) {
		t.Errorf("with a config: %q", got)
	}
}

func TestResolve_RewritesHostnameAndPort(t *testing.T) {
	isolatedHome(t, "")
	tg, err := Resolve(config.HostEntry{Hostname: "alias"})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Hostname != "127.0.0.1" || tg.Port != 2222 || tg.User != "fromconfig" {
		t.Errorf("alias resolved to %s:%d as %s, want 127.0.0.1:2222 as fromconfig", tg.Hostname, tg.Port, tg.User)
	}
	if tg.Alias != "alias" {
		t.Errorf("Alias = %q", tg.Alias)
	}
	if !slices.ContainsFunc(tg.IdentityFiles, func(p string) bool { return strings.HasSuffix(p, "/.ssh/the_key") && filepath.IsAbs(p) }) {
		t.Errorf("IdentityFiles = %q, want the configured key with ~ expanded", tg.IdentityFiles)
	}
	if tg.ProxyJump != "" || tg.ProxyCommand != "" {
		t.Errorf("alias has no proxy: %+v", tg)
	}
	if tg.handoverOnly() != nil {
		t.Error("alias is reachable in-app")
	}
}

func TestResolve_FleetFileWinsOverConfig(t *testing.T) {
	isolatedHome(t, "")
	tg, err := Resolve(config.HostEntry{Hostname: "alias", Port: 2200, User: "fromfleet"})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Port != 2200 || tg.User != "fromfleet" {
		t.Errorf("got %s@%d, want the fleet file's fromfleet@2200", tg.User, tg.Port)
	}
}

func TestResolve_UnknownHostIsItself(t *testing.T) {
	isolatedHome(t, "")
	tg, err := Resolve(config.HostEntry{Hostname: "somewhere.example"})
	if err != nil {
		t.Fatal(err)
	}
	if tg.Hostname != "somewhere.example" || tg.Port != 22 {
		t.Errorf("got %s:%d, want somewhere.example:22", tg.Hostname, tg.Port)
	}
}

func TestResolve_ProxyIsHandoverOnly(t *testing.T) {
	isolatedHome(t, "")
	tg, err := Resolve(config.HostEntry{Hostname: "bastioned"})
	if err != nil {
		t.Fatal(err)
	}
	var h ErrHandoverOnly
	if err := tg.handoverOnly(); !errors.As(err, &h) || h.Jump != "jump.invalid" {
		t.Errorf("ProxyJump: got %v, want ErrHandoverOnly{jump.invalid}", err)
	}
	if h.Error() != "ProxyJump via jump.invalid" {
		t.Errorf("Error() = %q", h.Error())
	}
	tg, err = Resolve(config.HostEntry{Hostname: "cmdproxy"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tg.handoverOnly(); !errors.As(err, &h) || !strings.Contains(h.Jump, "nc") {
		t.Errorf("ProxyCommand: got %v, want ErrHandoverOnly", err)
	}
}

func TestResolve_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Resolve(config.HostEntry{Hostname: "h"}); !errors.Is(err, ErrSSHBinaryMissing) {
		t.Errorf("got %v, want ErrSSHBinaryMissing", err)
	}
}

func TestParseSSHG(t *testing.T) {
	out := "host x\nhostname 10.0.0.9\nuser bob\nport 2022\nidentityfile ~/.ssh/a\nidentityfile /abs/b\nproxyjump none\nproxycommand none\n" +
		"globalknownhostsfile /etc/ssh/ssh_known_hosts /etc/ssh/ssh_known_hosts2\nuserknownhostsfile ~/.ssh/known_hosts ~/.ssh/known_hosts2\n"
	tg, err := parseSSHG([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if tg.Hostname != "10.0.0.9" || tg.User != "bob" || tg.Port != 2022 {
		t.Errorf("%+v", tg)
	}
	if tg.ProxyJump != "" || tg.ProxyCommand != "" {
		t.Errorf("'none' must read as unset: %+v", tg)
	}
	if len(tg.IdentityFiles) != 2 || tg.IdentityFiles[1] != "/abs/b" {
		t.Errorf("IdentityFiles = %q", tg.IdentityFiles)
	}
	if len(tg.UserKnownHosts) != 2 || len(tg.GlobalKnownHosts) != 2 {
		t.Errorf("known hosts: %q %q", tg.UserKnownHosts, tg.GlobalKnownHosts)
	}
	if _, err := parseSSHG([]byte("user bob\n")); err == nil {
		t.Error("output with no hostname/port must be rejected")
	}
	if _, err := parseSSHG([]byte("hostname h\nport x\n")); err == nil {
		t.Error("a non-numeric port must be rejected")
	}
}
