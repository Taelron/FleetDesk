package ssh

import (
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kevinburke/ssh_config"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// Manager holds persistent SSH connections for all hosts in a fleet.
type Manager struct {
	mu             sync.Mutex
	conns          map[int]*gossh.Client
	cachedPassword string
	sudoPasswords  map[int]string // per-host sudo password cache, cleared on Close
	logger         *slog.Logger
}

// NewManager creates a new SSH manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		conns:         make(map[int]*gossh.Client),
		sudoPasswords: make(map[int]string),
		logger:        logger,
	}
}

// HostProbeResult is sent when an SSH probe completes for a host.
type HostProbeResult struct {
	Index int
	Info  ProbeInfo
	Err   error
}

// PasswordRetryResult is sent after retrying connection with a password.
type PasswordRetryResult struct {
	Index int
	Info  ProbeInfo
	Err   error
}

// ConnectAndProbe connects to a single host and runs the probe command.
// Reuses an existing connection if available.
func (sm *Manager) ConnectAndProbe(idx int, h config.Host) HostProbeResult {
	start := time.Now()
	sm.logger.Debug("probe start", "host", h.Entry.Name)

	// reuse existing connection if available
	sm.mu.Lock()
	client, ok := sm.conns[idx]
	sm.mu.Unlock()

	if ok && client != nil {
		// test the connection with a probe
		info, err := Probe(client, h.Entry.SystemdMode, h.ErrorLogSince)
		if err == nil {
			sm.logger.Debug("probe complete", "host", h.Entry.Name, "reused", true, "elapsed", time.Since(start))
			return HostProbeResult{Index: idx, Info: info}
		}
		// connection is stale, remove and reconnect
		sm.logger.Debug("probe stale connection", "host", h.Entry.Name, "err", err)
		_ = client.Close()
		sm.mu.Lock()
		delete(sm.conns, idx)
		sm.mu.Unlock()
	}

	// establish new connection
	client, err := sm.dial(h)
	if err != nil {
		sm.logger.Error("probe failed", "host", h.Entry.Name, "err", err)
		return HostProbeResult{Index: idx, Err: err}
	}

	info, err := Probe(client, h.Entry.SystemdMode, h.ErrorLogSince)
	if err != nil {
		_ = client.Close()
		sm.logger.Error("probe failed", "host", h.Entry.Name, "err", err)
		return HostProbeResult{Index: idx, Err: fmt.Errorf("probe: %w", err)}
	}

	sm.mu.Lock()
	sm.conns[idx] = client
	sm.mu.Unlock()

	sm.logger.Debug("probe complete", "host", h.Entry.Name, "reused", false, "elapsed", time.Since(start))
	return HostProbeResult{Index: idx, Info: info}
}

// GetCachedPassword returns the SSH connection password, if the user authenticated via password.
func (sm *Manager) GetCachedPassword() string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.cachedPassword
}

// SetSudoPassword caches a sudo password for a specific host index.
// Pass an empty string to clear the cached password for that host.
func (sm *Manager) SetSudoPassword(idx int, password string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if password == "" {
		delete(sm.sudoPasswords, idx)
	} else {
		sm.sudoPasswords[idx] = password
	}
}

// GetSudoPassword returns the cached sudo password for a host (empty if none).
func (sm *Manager) GetSudoPassword(idx int) string {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.sudoPasswords[idx]
}

// RunSudoCommand executes a command on the given host, wrapping sudo invocations
// with password piping if a sudo password is cached for this host.
// If no sudo password is cached, delegates to RunCommand unchanged.
func (sm *Manager) RunSudoCommand(idx int, cmd string) (string, error) {
	pw := sm.GetSudoPassword(idx)
	if pw != "" {
		cmd = rewriteSudoCmd(cmd, pw)
	}
	out, err := sm.RunCommand(idx, cmd)
	if pw != "" {
		out = stripSudoPrompt(out)
	}
	return out, err
}

// stripSudoPrompt removes "[sudo] password for ..." lines from output.
// These leak into stdout when commands use 2>&1 with sudo -S.
func stripSudoPrompt(s string) string {
	var clean []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[sudo] password for") {
			continue
		}
		clean = append(clean, line)
	}
	return strings.Join(clean, "\n")
}

// rewriteSudoCmd rewrites every "sudo " occurrence in cmd to pipe the password
// via stdin. Single quotes in the password are escaped to prevent shell injection.
func rewriteSudoCmd(cmd string, password string) string {
	escaped := EscapeSingleQuotes(password)
	return strings.ReplaceAll(cmd, "sudo ", "echo '"+escaped+"' | sudo -S 2>/dev/null ")
}

// RunCommand executes a command on the given host index and returns stdout.
func (sm *Manager) RunCommand(idx int, cmd string) (string, error) {
	logPrefix := cmd[:min(len(cmd), 60)]
	if strings.Contains(cmd, "| sudo -S") {
		logPrefix = "[sudo-rewritten]"
	}
	sm.logger.Debug("runCommand", "idx", idx, "cmd_prefix", logPrefix)

	sm.mu.Lock()
	client, ok := sm.conns[idx]
	sm.mu.Unlock()

	if !ok || client == nil {
		return "", fmt.Errorf("no connection for host")
	}

	session, err := client.NewSession()
	if err != nil {
		sm.logger.Error("runCommand failed", "idx", idx, "err", err)
		return "", fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	out, err := session.CombinedOutput(cmd)
	result := stripShellWarnings(string(out))
	if err != nil {
		sm.logger.Error("runCommand failed", "idx", idx, "err", err)
		return result, err
	}
	return result, nil
}

// stripShellWarnings removes common shell login warnings that appear before
// command output (e.g., when the user's home directory doesn't exist on the host).
func stripShellWarnings(s string) string {
	for {
		if strings.HasPrefix(s, "Could not chdir to home directory") ||
			strings.HasPrefix(s, "-bash: warning:") {
			if idx := strings.Index(s, "\n"); idx >= 0 {
				s = s[idx+1:]
				continue
			}
		}
		break
	}
	return s
}

// SetCachedPassword stores a password temporarily for batch retries.
func (sm *Manager) SetCachedPassword(password string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cachedPassword = password
}

// ClearPassword zeroes out the cached password.
func (sm *Manager) ClearPassword() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.cachedPassword = ""
}

// RetryWithCachedPassword uses the temporarily cached password to connect a host.
func (sm *Manager) RetryWithCachedPassword(idx int, h config.Host) PasswordRetryResult {
	sm.mu.Lock()
	pw := sm.cachedPassword
	sm.mu.Unlock()

	if pw == "" {
		return PasswordRetryResult{Index: idx, Err: fmt.Errorf("no cached password")}
	}

	return sm.ConnectWithPassword(idx, h, pw)
}

// ConnectWithPassword connects to a host using password auth and probes it.
func (sm *Manager) ConnectWithPassword(idx int, h config.Host, password string) PasswordRetryResult {
	entry := h.Entry
	hostname := entry.Hostname
	port := entry.Port
	if port == 0 {
		port = 22
	}

	sshConfig := &gossh.ClientConfig{
		User: entry.User,
		Auth: []gossh.AuthMethod{
			gossh.Password(password),
			gossh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
				answers := make([]string, len(questions))
				for i := range questions {
					answers[i] = password
				}
				return answers, nil
			}),
		},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // G106: no host-key verification yet — TAE-22
		Timeout:         entry.Timeout,
	}

	addr := fmt.Sprintf("%s:%d", hostname, port)
	start := time.Now()
	sm.logger.Debug("password dial start", "addr", addr, "idx", idx)
	client, err := gossh.Dial("tcp", addr, sshConfig)
	if err != nil {
		sm.logger.Error("password dial failed", "addr", addr, "idx", idx, "err", err, "elapsed", time.Since(start))
		return PasswordRetryResult{Index: idx, Err: fmt.Errorf("password auth: %w", err)}
	}
	sm.logger.Debug("password dial success", "addr", addr, "idx", idx, "elapsed", time.Since(start))

	sm.mu.Lock()
	sm.conns[idx] = client
	sm.mu.Unlock()

	info, err := Probe(client, entry.SystemdMode, h.ErrorLogSince)
	if err != nil {
		sm.logger.Error("password probe failed", "addr", addr, "idx", idx, "err", err)
		return PasswordRetryResult{Index: idx, Err: fmt.Errorf("probe: %w", err)}
	}

	sm.logger.Debug("password probe success", "addr", addr, "idx", idx)
	return PasswordRetryResult{Index: idx, Info: info}
}

// RewriteSudoInCmd rewrites sudo commands with the cached password for the given host.
// If no password is cached, returns the command unchanged.
func (sm *Manager) RewriteSudoInCmd(idx int, cmd string) string {
	pw := sm.GetSudoPassword(idx)
	if pw == "" {
		return cmd
	}
	return rewriteSudoCmd(cmd, pw)
}

// GetConnection returns the SSH client for the given host index, or nil if not connected.
func (sm *Manager) GetConnection(idx int) *gossh.Client {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.conns[idx]
}

// HasConnection returns true if the manager has an active SSH client for the given host index.
func (sm *Manager) HasConnection(idx int) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.conns[idx] != nil
}

// Close shuts down all SSH connections and clears all cached credentials.
func (sm *Manager) Close() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, c := range sm.conns {
		if c != nil {
			_ = c.Close()
		}
	}
	sm.conns = make(map[int]*gossh.Client)
	sm.cachedPassword = ""
	sm.sudoPasswords = make(map[int]string)
}

// Probe runs a single SSH command to gather all host info in one roundtrip.
func Probe(client *gossh.Client, systemdMode string, errorLogSince string) (ProbeInfo, error) {
	session, err := client.NewSession()
	if err != nil {
		return ProbeInfo{}, fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	cmd := `echo '---PROBE---' && ` +
		`(hostname -f 2>/dev/null || hostname) | head -1 && ` +
		`uptime -s 2>/dev/null || echo unknown && ` +
		`(grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2 | tr -d '"') || echo unknown && ` +
		`echo $(( $(crontab -l 2>/dev/null | grep -v '^#' | grep -v '^$' | wc -l) + $(ls /etc/cron.d/ 2>/dev/null | wc -l) )) && ` +
		fmt.Sprintf(`sudo journalctl -p err --since '%s' --no-pager -q 2>/dev/null | wc -l && `, errorLogSince) +
		`df -h --output=pcent -x tmpfs -x devtmpfs 2>/dev/null | tail -n+2 | wc -l && ` +
		`df -h --output=pcent -x tmpfs -x devtmpfs 2>/dev/null | tail -n+2 | awk '{gsub(/%%/,""); if ($1 >= 80) print}' | wc -l && ` +
		`(getent passwd | awk -F: '$3 >= 1000 && $3 != 65534 {print $1}'; for d in /home/*/; do u=$(basename "$d"); getent passwd "$u" >/dev/null 2>&1 && echo "$u"; done) | sort -u | wc -l && ` +
		`(ip -br link | grep -c UP || echo 0) && ` +
		`ip -br link | wc -l && ` +
		`(ss -tlnp 2>/dev/null | tail -n +2 | wc -l || echo 0) && ` +
		`(dnf check-update --quiet 2>/dev/null | grep -c '^\S' || echo 0) && ` +
		`(command -v supervisorctl >/dev/null 2>&1 && echo 1 || echo 0)`

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		session2, err2 := client.NewSession()
		if err2 != nil {
			return ProbeInfo{}, fmt.Errorf("probe failed: %w", err)
		}
		defer func() { _ = session2.Close() }()

		if systemdMode == "user" {
			systemdMode = "system"
		} else {
			systemdMode = "user"
		}

		cmd = `echo '---PROBE---' && ` +
			`(hostname -f 2>/dev/null || hostname) | head -1 && ` +
			`uptime -s 2>/dev/null || echo unknown && ` +
			`(grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2 | tr -d '"') || echo unknown`

		out, err = session2.CombinedOutput(cmd)
		if err != nil {
			return ProbeInfo{}, fmt.Errorf("probe failed both modes: %w", err)
		}
	}

	return ParseProbeOutput(string(out), systemdMode)
}

// dial establishes an SSH connection to a host.
func (sm *Manager) dial(h config.Host) (*gossh.Client, error) {
	entry := h.Entry
	hostname := entry.Hostname
	port := entry.Port
	user := entry.User
	timeout := entry.Timeout

	if user == "" {
		user = ssh_config.Get(hostname, "User")
	}
	if user == "" {
		user = os.Getenv("USER")
	}

	configPort := ssh_config.Get(hostname, "Port")
	if port == 0 && configPort != "" {
		_, _ = fmt.Sscanf(configPort, "%d", &port)
	}
	if port == 0 {
		port = 22
	}

	if timeout == 0 {
		timeout = 10 * time.Second
	}

	configHost := ssh_config.Get(hostname, "Hostname")
	if configHost != "" {
		hostname = configHost
	}

	var authMethods []gossh.AuthMethod
	var authNames []string

	agentAuth, agentConn := sshAgentAuth()
	if agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
		authNames = append(authNames, "agent")
	}

	identityFile := ssh_config.Get(entry.Hostname, "IdentityFile")
	if identityFile != "" {
		if key := publicKeyFile(ExpandPath(identityFile)); key != nil {
			authMethods = append(authMethods, key)
			authNames = append(authNames, "identity:"+identityFile)
		}
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			path := filepath.Join(home, ".ssh", name)
			if _, err := os.Stat(path); err != nil {
				continue // skip non-existent keys
			}
			if key := publicKeyFile(path); key != nil {
				authMethods = append(authMethods, key)
				authNames = append(authNames, "key:"+name)
			}
		}
	}

	if len(authMethods) == 0 {
		return nil, fmt.Errorf("no SSH auth methods available")
	}

	addr := fmt.Sprintf("%s:%d", hostname, port)
	start := time.Now()
	sm.logger.Debug("dial start", "addr", addr, "user", user)

	var lastErr error
	for i, auth := range authMethods {
		sm.logger.Debug("dial trying", "auth", authNames[i], "addr", addr)
		sshConfig := &gossh.ClientConfig{
			User:            user,
			Auth:            []gossh.AuthMethod{auth},
			HostKeyCallback: gossh.InsecureIgnoreHostKey(), //nolint:gosec // G106: trusted fleet network, no host-key verification yet — TAE-22
			Timeout:         timeout,
		}
		client, err := gossh.Dial("tcp", addr, sshConfig)
		if err == nil {
			if agentConn != nil {
				_ = agentConn.Close()
			}
			sm.logger.Debug("dial success", "addr", addr, "elapsed", time.Since(start))
			return client, nil
		}
		lastErr = err
	}

	// close agent socket on failure — no SSH client to use it
	if agentConn != nil {
		_ = agentConn.Close()
	}

	sm.logger.Error("dial failed", "addr", addr, "err", lastErr, "elapsed", time.Since(start))
	return nil, fmt.Errorf("dial %s: %w", addr, lastErr)
}

// sshAgentAuth returns an auth method from the SSH agent and the underlying
// connection. The caller must close agentConn when the SSH client is established
// or when all auth attempts fail.
func sshAgentAuth() (gossh.AuthMethod, net.Conn) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil
	}
	conn, err := net.Dial("unix", sock) //nolint:gosec // G704: Unix-socket dial to $SSH_AUTH_SOCK, not a network dial
	if err != nil {
		return nil, nil
	}
	return gossh.PublicKeysCallback(agent.NewClient(conn).Signers), conn
}

// publicKeyFile returns an auth method from a private key file, or nil.
func publicKeyFile(path string) gossh.AuthMethod {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's own ~/.ssh/config IdentityFile or a stat-gated default key
	if err != nil {
		return nil
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil
	}
	return gossh.PublicKeys(signer)
}
