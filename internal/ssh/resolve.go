package ssh

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// ErrSSHBinaryMissing is returned when the ssh binary cannot be found on
// PATH. Every path to a host resolves its target through `ssh -G`, so
// without the binary FleetDesk cannot dial anything.
var ErrSSHBinaryMissing = errors.New("ssh binary not found on PATH")

// ErrHandoverOnly is returned when a host's ssh configuration requires a
// ProxyJump or ProxyCommand. The library dial cannot honour either, so the
// host is reachable only by handing the terminal over to ssh.
type ErrHandoverOnly struct {
	// Jump is the ProxyJump host. Empty when the host uses a ProxyCommand,
	// whose text is deliberately not carried: it can hold anything.
	Jump string
}

func (e ErrHandoverOnly) Error() string {
	if e.Jump == "" {
		return "ProxyCommand"
	}
	return "ProxyJump via " + e.Jump
}

// Target is where an ssh invocation for one fleet entry ends up, as
// `ssh -G` resolves it. Every path — both library dials and the terminal
// handover — reaches the same machine because they all start from this.
type Target struct {
	Alias         string
	Hostname      string
	User          string
	Port          int
	IdentityFiles []string
	ProxyJump     string
	ProxyCommand  string
	// HostKeyAlias, when set, is the name ssh looks the host key up and
	// records it under instead of the host name.
	HostKeyAlias     string
	UserKnownHosts   []string
	GlobalKnownHosts []string
}

// Addr is the host:port the library dials.
func (t Target) Addr() string { return fmt.Sprintf("%s:%d", t.Hostname, t.Port) }

// KeyAddr is the host:port a host key is looked up and recorded under in
// known_hosts: the address, or HostKeyAlias:port when ssh would use an alias.
func (t Target) KeyAddr() string {
	if t.HostKeyAlias != "" {
		return fmt.Sprintf("%s:%d", t.HostKeyAlias, t.Port)
	}
	return t.Addr()
}

// SSHArgs is the single source of the argument list for every ssh
// invocation FleetDesk makes for a fleet entry: the handover, the
// deploy-key script (ssh and ssh-copy-id) and the `ssh -G` resolver. It
// ends with the host, after "--", so a host or user that begins with "-"
// can never be read as an option.
//
//	[-F <cfg>] [-o UserKnownHostsFile=<kh>] [-p <port>] [-o User=<user>] -- <host>
//
// -p and -o User= appear only when the fleet file sets them; otherwise ssh
// decides, from ~/.ssh/config. -o User= rather than -l, because ssh-copy-id
// has no -l.
//
// OpenSSH reads its per-user files from the passwd database's home
// directory and ignores $HOME. When the two differ — a test harness, or a
// user who points $HOME elsewhere on purpose — FleetDesk redirects the
// per-user ssh config and known_hosts to $HOME/.ssh, so what FleetDesk
// reads for itself and what ssh reads are the same files. When they are
// the same (the normal case) nothing is added.
func SSHArgs(e config.HostEntry) []string {
	var args []string
	args = append(args, homeArgs()...)
	if e.Port > 0 {
		args = append(args, "-p", strconv.Itoa(e.Port))
	}
	if e.User != "" {
		args = append(args, "-o", "User="+e.User)
	}
	return append(args, "--", e.Hostname)
}

// homeArgs redirects ssh's per-user files to $HOME/.ssh when $HOME is not
// the passwd home directory.
func homeArgs() []string {
	home := os.Getenv("HOME")
	if home == "" {
		return nil
	}
	if u, err := user.Current(); err == nil && samePath(u.HomeDir, home) {
		return nil
	}
	var args []string
	if cfg := filepath.Join(home, ".ssh", "config"); fileExists(cfg) {
		args = append(args, "-F", cfg)
	}
	kh := filepath.Join(home, ".ssh", "known_hosts")
	return append(args, "-o", "UserKnownHostsFile="+kh)
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
}

func fileExists(p string) bool {
	_, err := os.Stat(p) //nolint:gosec // G703: p is an ssh config path (ssh -G output or $HOME/.ssh); existence check only
	return err == nil
}

// Resolve asks ssh itself where an entry leads: `ssh -G` prints the
// effective configuration for the host without connecting.
func Resolve(e config.HostEntry) (Target, error) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return Target{}, ErrSSHBinaryMissing
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	args := append([]string{"-G"}, SSHArgs(e)...)
	cmd := exec.CommandContext(ctx, "ssh", args...) //nolint:gosec // G204: ssh -G, argv, host/user/port from the user's own fleet YAML
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return Target{}, ErrSSHBinaryMissing
		}
		return Target{}, fmt.Errorf("ssh -G %s: %w: %s", e.Hostname, err, strings.TrimSpace(stderr.String()))
	}
	t, err := parseSSHG(stdout.Bytes())
	if err != nil {
		return Target{}, fmt.Errorf("ssh -G %s: %w", e.Hostname, err)
	}
	t.Alias = e.Hostname
	return t, nil
}

// parseSSHG reads the "key value" lines `ssh -G` prints.
func parseSSHG(out []byte) (Target, error) {
	var t Target
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		key, val, ok := strings.Cut(sc.Text(), " ")
		if !ok {
			continue
		}
		val = strings.TrimSpace(val)
		switch key {
		case "hostname":
			t.Hostname = val
		case "user":
			t.User = val
		case "port":
			p, err := strconv.Atoi(val)
			if err != nil {
				return Target{}, fmt.Errorf("port %q: %w", val, err)
			}
			t.Port = p
		case "identityfile":
			t.IdentityFiles = append(t.IdentityFiles, ExpandPath(val))
		case "proxyjump":
			if val != "none" {
				t.ProxyJump = val
			}
		case "proxycommand":
			if val != "none" {
				t.ProxyCommand = val
			}
		case "hostkeyalias":
			if val != "none" {
				t.HostKeyAlias = val
			}
		case "userknownhostsfile":
			t.UserKnownHosts = append(t.UserKnownHosts, splitPaths(val)...)
		case "globalknownhostsfile":
			t.GlobalKnownHosts = append(t.GlobalKnownHosts, splitPaths(val)...)
		}
	}
	if t.Hostname == "" || t.Port == 0 {
		return Target{}, errors.New("no hostname or port in output")
	}
	return t, nil
}

// splitPaths splits a space-separated list of paths and expands "~".
func splitPaths(v string) []string {
	var out []string
	for _, p := range strings.Fields(v) {
		out = append(out, ExpandPath(p))
	}
	return out
}

// handoverOnly returns the error for a target the library dial cannot
// reach, or nil.
func (t Target) handoverOnly() error {
	switch {
	case t.ProxyJump != "":
		return ErrHandoverOnly{Jump: t.ProxyJump}
	case t.ProxyCommand != "":
		return ErrHandoverOnly{}
	}
	return nil
}
