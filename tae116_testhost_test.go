//go:build testhost

package main_test

// Acceptance tests for TAE-116: M2 -- Credentials never leave the session.
// Written from the issue description only (criteria A-H, "Test interface",
// "Stated limitations"). No plan comment, no issue comment, no
// implementation of this issue and no other session's work was read.
//
// Black-box by construction. The feature does not exist yet, so a test that
// imports its types would fail to *compile* and prove nothing. Every test
// here drives the shipped surface instead -- the built `fleetdesk` binary
// (stderr and exit code at startup, the TUI through tmux), the TAE-98
// container (`podman exec`), the Makefile, and the files on disk -- so each
// one fails by asserting on behaviour, not on a missing symbol.
//
// Shares TestMain / requireTesthost / containerName / testUser* with the
// TAE-98 harness (testhost_container_test.go). The container is one shared
// fixture; tests that mutate it (host key, a password, an account's
// authorized_keys, os-release) restore it with t.Cleanup.
//
// Assumed fixtures, taken from the issue's Test interface:
//
//	test/azstub/az           stand-in for `az` on PATH, keyed on --name /
//	                         --vault-name (stub-ok, stub-missing,
//	                         stub-forbidden, vault stub-nologin)
//	test/testhost/.home/     isolated HOME the harness generates, with
//	                         .ssh/config (aliases testhost-alias,
//	                         testhost-bastion) and .ssh/known_hosts
//	a stub RHSM D-Bus module in the container (L2)
//
// Run:
//
//	go test -tags testhost -count=1 -run TestTAE116 -v .

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	t116StubSecret = "stub-secret-value"
	t116Vault      = "stub-vault"
	t116Sub        = "00000000-0000-0000-0000-000000000000"
	t116HomeDir    = "test/testhost/.home"
	t116AzStubDir  = "test/azstub"
)

// ---------------------------------------------------------------------------
// harness: binary, isolated HOME, fleet files
// ---------------------------------------------------------------------------

var (
	t116BuildOnce sync.Once
	t116BinPath   string
	t116BuildErr  error
)

func t116Abs(t *testing.T, rel string) string {
	t.Helper()
	p, err := filepath.Abs(rel)
	if err != nil {
		t.Fatalf("abs %s: %v", rel, err)
	}
	return p
}

// t116Binary builds the shipped binary once. A compile failure of the
// *current* tree fails the test with the compiler output -- that is the only
// way these tests can ever fail to compile, and it is not a red we accept.
func t116Binary(t *testing.T) string {
	t.Helper()
	t116BuildOnce.Do(func() {
		dir := t116Abs(t, "bin/tae116")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t116BuildErr = err
			return
		}
		t116BinPath = filepath.Join(dir, "fleetdesk")
		out, err := exec.Command("go", "build", "-o", t116BinPath, ".").CombinedOutput()
		if err != nil {
			t116BuildErr = fmt.Errorf("go build: %v\n%s", err, out)
		}
	})
	if t116BuildErr != nil {
		t.Fatalf("cannot build the fleetdesk binary under test: %v", t116BuildErr)
	}
	return t116BinPath
}

type t116Home struct {
	t     *testing.T
	root  string // absolute isolated HOME
	fleet string // absolute fleet dir, inside HOME
}

// t116NewHome wipes and recreates the isolated HOME the issue names
// (test/testhost/.home) with an ssh config carrying the two aliases the
// Test interface defines, an empty known_hosts, and a config.yaml (0600)
// pointing at an empty fleet dir. Callers add fleet files.
func t116NewHome(t *testing.T) *t116Home {
	t.Helper()
	requireTesthost(t)
	root := t116Abs(t, t116HomeDir)
	if err := os.RemoveAll(root); err != nil {
		t.Fatalf("reset %s: %v", root, err)
	}
	h := &t116Home{t: t, root: root, fleet: filepath.Join(root, "fleets")}
	for _, d := range []string{filepath.Join(root, ".ssh"), h.fleet, filepath.Join(root, ".config", "fleetdesk")} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", d, err)
		}
	}
	key := t116Abs(t, clientKeyPath)
	sshCfg := fmt.Sprintf(`Host testhost-alias
    Hostname 127.0.0.1
    Port 2222

Host testhost-bastion
    Hostname 127.0.0.1
    Port 2222
    ProxyJump nowhere.invalid

Host *
    IdentityFile %s
`, key)
	h.write(".ssh/config", sshCfg, 0o600)
	// also the default key path (README: "Default keys"), so key auth does
	// not depend on how IdentityFile is parsed
	kb, err := os.ReadFile(key)
	if err != nil {
		t.Fatalf("read the generated client key %s: %v", key, err)
	}
	h.write(".ssh/id_ed25519", string(kb), 0o600)
	h.write(".ssh/known_hosts", "", 0o600)
	h.writeConfig("fleet_dir: " + h.fleet + "\n")
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	return h
}

func (h *t116Home) path(rel string) string { return filepath.Join(h.root, rel) }

func (h *t116Home) write(rel, content string, mode os.FileMode) string {
	h.t.Helper()
	p := h.path(rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		h.t.Fatalf("mkdir for %s: %v", p, err)
	}
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		h.t.Fatalf("write %s: %v", p, err)
	}
	if err := os.Chmod(p, mode); err != nil { // umask must not weaken or widen what the test set up
		h.t.Fatalf("chmod %s: %v", p, err)
	}
	return p
}

func (h *t116Home) writeConfig(content string) string {
	return h.write(".config/fleetdesk/config.yaml", content, 0o600)
}

func (h *t116Home) writeFleet(name, content string, mode os.FileMode) string {
	h.t.Helper()
	p := filepath.Join(h.fleet, name)
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		h.t.Fatalf("write fleet %s: %v", p, err)
	}
	if err := os.Chmod(p, mode); err != nil {
		h.t.Fatalf("chmod fleet %s: %v", p, err)
	}
	return p
}

// trustTesthost seeds known_hosts with the container's current host keys so
// a test that is not about host keys is not stopped by TOFU. The aliases the
// ssh config defines are seeded too, so every path is trusted whichever name
// it resolves to.
func (h *t116Home) trustTesthost() {
	h.t.Helper()
	out, err := exec.Command("ssh-keyscan", "-p", "2222", "127.0.0.1").Output()
	if err != nil {
		h.t.Fatalf("ssh-keyscan 127.0.0.1:2222: %v", err)
	}
	var kh strings.Builder
	for _, l := range strings.Split(string(out), "\n") {
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		fields := strings.SplitN(l, " ", 2)
		if len(fields) != 2 {
			continue
		}
		for _, name := range []string{"[127.0.0.1]:2222", "[testhost-alias]:2222", "testhost-alias", "[testhost-alias]:22"} {
			kh.WriteString(name + " " + fields[1] + "\n")
		}
	}
	if kh.Len() == 0 {
		h.t.Fatalf("ssh-keyscan returned no host keys for 127.0.0.1:2222")
	}
	h.write(".ssh/known_hosts", kh.String(), 0o600)
}

func (h *t116Home) env(pathDirs ...string) []string {
	return []string{
		"HOME=" + h.root,
		"PATH=" + strings.Join(append(pathDirs, "/usr/bin", "/bin"), ":"),
		"TERM=xterm-256color",
		"LANG=C.UTF-8",
	}
}

func (h *t116Home) stubPath() []string { return []string{t116Abs(h.t, t116AzStubDir)} }

// runStartup runs the binary on a pty (tmux) and returns what it wrote and
// its exit code once it has exited. A binary without a controlling terminal
// dies on "could not open a new TTY" before it does anything else, so a pty
// is the only honest way to see its startup errors. If it is still running
// after 12s it is killed and the code is -1 (it started the TUI).
func (h *t116Home) runStartup(env []string) (string, int) {
	h.t.Helper()
	u := h.startTUI(env)
	deadline := time.Now().Add(12 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("tmux", "-L", u.sock, "list-panes", "-t", u.sess, "-F", "#{pane_dead} #{pane_dead_status}").Output()
		if err == nil {
			f := strings.Fields(string(out))
			if len(f) >= 1 && f[0] == "1" {
				code := 0
				if len(f) == 2 {
					fmt.Sscanf(f[1], "%d", &code)
				}
				// joined so a message wrapped at the pane width still matches
				return strings.ReplaceAll(u.paneJoined(), "\n", "\n"), code
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return u.pane(), -1
}

// ---------------------------------------------------------------------------
// harness: tmux-driven TUI
// ---------------------------------------------------------------------------

type t116TUI struct {
	t    *testing.T
	sock string
	sess string
}

func (h *t116Home) startTUI(env []string, args ...string) *t116TUI {
	h.t.Helper()
	bin := t116Binary(h.t)
	u := &t116TUI{t: h.t, sock: fmt.Sprintf("fd116-%d-%d", os.Getpid(), time.Now().UnixNano()%1e9), sess: "fd"}
	argv := []string{"-L", u.sock, "new-session", "-d", "-s", u.sess, "-x", "200", "-y", "50", "-c", h.root, "--", "env", "-i"}
	argv = append(argv, env...)
	argv = append(argv, bin)
	argv = append(argv, args...)
	// remain-on-exit is set in the same tmux command queue, so a binary that
	// exits at once (a startup error) still leaves its pane to be read
	argv = append(argv, ";", "set-option", "-t", u.sess, "remain-on-exit", "on")
	if out, err := exec.Command("tmux", argv...).CombinedOutput(); err != nil {
		h.t.Fatalf("tmux new-session: %v\n%s", err, out)
	}
	h.t.Cleanup(func() { _ = exec.Command("tmux", "-L", u.sock, "kill-server").Run() })
	return u
}

func (u *t116TUI) pane() string {
	out, err := exec.Command("tmux", "-L", u.sock, "capture-pane", "-p", "-t", u.sess).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

// paneJoined is the pane with soft-wrapped lines re-joined (capture-pane -J).
func (u *t116TUI) paneJoined() string {
	out, err := exec.Command("tmux", "-L", u.sock, "capture-pane", "-p", "-J", "-t", u.sess).Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func (u *t116TUI) alive() bool {
	out, err := exec.Command("tmux", "-L", u.sock, "list-panes", "-t", u.sess, "-F", "#{pane_dead}").Output()
	return err == nil && strings.TrimSpace(string(out)) == "0"
}

func (u *t116TUI) text(s string) {
	u.t.Helper()
	if out, err := exec.Command("tmux", "-L", u.sock, "send-keys", "-t", u.sess, "-l", s).CombinedOutput(); err != nil {
		u.t.Fatalf("tmux send-keys: %v\n%s", err, out)
	}
}

func (u *t116TUI) key(k ...string) {
	u.t.Helper()
	args := append([]string{"-L", u.sock, "send-keys", "-t", u.sess}, k...)
	if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
		u.t.Fatalf("tmux send-keys %v: %v\n%s", k, err, out)
	}
}

// waitAny polls the pane until one of subs is present (case-insensitive) and
// returns the pane and which one matched; it fails the test with the last
// pane on timeout.
func (u *t116TUI) waitAny(d time.Duration, what string, subs ...string) (string, string) {
	u.t.Helper()
	deadline := time.Now().Add(d)
	var last string
	for time.Now().Before(deadline) {
		last = u.pane()
		low := strings.ToLower(last)
		for _, s := range subs {
			if strings.Contains(low, strings.ToLower(s)) {
				return last, s
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	u.t.Fatalf("timed out after %s waiting for %s (any of %q); last screen:\n%s", d, what, subs, last)
	return "", ""
}

func (u *t116TUI) wait(d time.Duration, what string, sub string) string {
	u.t.Helper()
	p, _ := u.waitAny(d, what, sub)
	return p
}

// tryWait is waitAny without the fatal: for "this must NOT appear" checks and
// optional branches.
func (u *t116TUI) tryWait(d time.Duration, subs ...string) (string, string, bool) {
	deadline := time.Now().Add(d)
	var last string
	for time.Now().Before(deadline) {
		last = u.pane()
		low := strings.ToLower(last)
		for _, s := range subs {
			if strings.Contains(low, strings.ToLower(s)) {
				return last, s, true
			}
		}
		time.Sleep(120 * time.Millisecond)
	}
	return last, "", false
}

// settle waits until the screen stops changing and shows no "loading".
func (u *t116TUI) settle(max time.Duration) string {
	u.t.Helper()
	deadline := time.Now().Add(max)
	prev, stable := "", 0
	for time.Now().Before(deadline) {
		cur := u.pane()
		if cur == prev && !strings.Contains(strings.ToLower(cur), "loading") && !strings.Contains(strings.ToLower(cur), "testing sudo") {
			stable++
			if stable >= 6 {
				return cur
			}
		} else {
			stable = 0
		}
		prev = cur
		time.Sleep(200 * time.Millisecond)
	}
	return prev
}

func (u *t116TUI) openFleet(fleetName string) string {
	u.t.Helper()
	u.wait(20*time.Second, "the Fleet Picker listing "+fleetName, fleetName)
	u.key("Enter")
	return u.settle(25 * time.Second)
}

// selectRow moves the cursor to the idx-th row of a list (0-based) without
// assuming where it starts: up far enough to hit the top, then down.
func (u *t116TUI) selectRow(idx int) {
	u.t.Helper()
	for range 12 {
		u.key("Up")
	}
	for range idx {
		u.key("Down")
	}
	time.Sleep(200 * time.Millisecond)
}

var t116PickerOrder = []string{"Services", "Processes", "Containers", "Cron Jobs", "System Logs", "Updates", "Disk", "Subscription", "Accounts", "Network"}

// gotoPickerRow moves from the top of the Resource Picker to the named row,
// counting only the rows actually on screen (Processes is conditional).
func (u *t116TUI) gotoPickerRow(name string) {
	u.t.Helper()
	p := u.pane()
	idx, found := 0, false
	for _, line := range strings.Split(p, "\n") {
		for _, row := range t116PickerOrder {
			if strings.Contains(line, row) {
				if row == name {
					found = true
				} else if !found {
					idx++
				}
				break
			}
		}
		if found {
			break
		}
	}
	if !found {
		u.t.Fatalf("Resource Picker row %q is not on screen:\n%s", name, p)
	}
	u.selectRow(idx)
}

// enterHost presses Enter on host row idx. If the sudo wizard opens and a
// password is supplied it is typed. It returns once the Resource Picker is
// up (System Logs is a row only the picker has).
func (u *t116TUI) enterHost(idx int, sudoPW string) string {
	u.t.Helper()
	u.selectRow(idx)
	for i := 0; i < 100 && strings.Contains(u.pane(), "connecting..."); i++ { // the probe must have finished
		time.Sleep(500 * time.Millisecond)
	}
	u.key("Enter")
	p, hit := u.waitAny(25*time.Second, "the sudo wizard or the Resource Picker", "Sudo password for", "System Logs")
	if hit == "Sudo password for" {
		if sudoPW == "" {
			u.t.Fatalf("the sudo wizard opened but the test has no password for it:\n%s", p)
		}
		u.text(sudoPW)
		u.key("Enter")
		p = u.wait(25*time.Second, "the Resource Picker after the sudo password", "System Logs")
	}
	return u.settle(15 * time.Second)
}

// ---------------------------------------------------------------------------
// harness: container, process-list sampling
// ---------------------------------------------------------------------------

func t116Podman(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("podman", append([]string{"exec", containerName}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("podman exec %s %v: %v\n%s", containerName, args, err, out)
	}
	return string(out)
}

const t116SnapScript = `ps aux; for f in /proc/[0-9]*/cmdline; do tr '\0' ' ' < "$f" 2>/dev/null; echo; done`

func t116Snapshot() string {
	out, _ := exec.Command("podman", "exec", containerName, "sh", "-c", t116SnapScript).CombinedOutput()
	return string(out)
}

func t116Windows(s string, n int) []string {
	var w []string
	for i := 0; i+n <= len(s); i++ {
		w = append(w, s[i:i+n])
	}
	return w
}

type t116Sampler struct {
	stop    chan struct{}
	done    chan struct{}
	needles []string
	marker  *regexp.Regexp

	mu        sync.Mutex
	samples   int
	sawMarker bool
	leak      string
}

// t116KeyNeedles is the whole value plus every 8-byte window of it: "no part
// of the key" (B4).
func t116KeyNeedles(v string) []string { return append([]string{v}, t116Windows(v, 8)...) }

func t116StartSampler(needles []string, marker *regexp.Regexp) *t116Sampler {
	s := &t116Sampler{stop: make(chan struct{}), done: make(chan struct{}), needles: needles, marker: marker}
	go func() {
		defer close(s.done)
		for {
			snap := t116Snapshot()
			s.mu.Lock()
			s.samples++
			if s.marker != nil && s.marker.MatchString(snap) {
				s.sawMarker = true
			}
			if s.leak == "" {
				for _, n := range s.needles {
					if i := strings.Index(snap, n); i >= 0 {
						lo, hi := max(0, i-60), min(len(snap), i+len(n)+60)
						s.leak = fmt.Sprintf("%q found in: ...%s...", n, snap[lo:hi])
						break
					}
				}
			}
			s.mu.Unlock()
			select {
			case <-s.stop:
				return
			case <-time.After(60 * time.Millisecond):
			}
		}
	}()
	return s
}

func (s *t116Sampler) finish() (samples int, sawMarker bool, leak string) {
	close(s.stop)
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.samples, s.sawMarker, s.leak
}

// A registration shows up in the container as one of these (RHSM's D-Bus
// call, the helper, the stub module, or -- forbidden by B3 -- the CLI).
var t116RegMarker = regexp.MustCompile(`(?i)RegisterWithActivationKeys|rhsm|subscription-manager|gdbus|busctl|dbus|register`)

// ---------------------------------------------------------------------------
// harness: fleet file builders
// ---------------------------------------------------------------------------

const t116SecretsBlock = "secrets:\n  provider: azure-keyvault\n  vault: " + t116Vault + "\n  subscription: " + t116Sub + "\n"

func t116SecretsBlockFor(vault string) string {
	return "secrets:\n  provider: azure-keyvault\n  vault: " + vault + "\n  subscription: " + t116Sub + "\n"
}

func t116RegFleet(secrets, secretName string) string {
	return "name: tae116\ntype: vm\n" + secrets + `defaults:
  port: 2222
hosts:
  - name: th-reg
    hostname: 127.0.0.1
    user: testuser1
    rh_org_id: org-tae116
    rh_activation_key_secret_name: ` + secretName + "\n"
}

// t116OneHost is a fleet of one key-auth host, no secrets.
func t116OneHost(name, hostname, user string) string {
	return fmt.Sprintf("name: tae116\ntype: vm\ndefaults:\n  port: 2222\nhosts:\n  - name: %s\n    hostname: %s\n    user: %s\n", name, hostname, user)
}

// ---------------------------------------------------------------------------
// registration scenario (A, B, H1, H2)
// ---------------------------------------------------------------------------

type t116Reg struct {
	home      *t116Home
	ui        *t116TUI
	fleetPath string
	pane      string // screen after the confirm key
	samples   int
	sawMarker bool
	leak      string
}

// t116RunRegistration drives: fleet -> host (sudo wizard) -> Resource Picker
// -> Subscription -> g -> confirm -> the outcome, optionally sampling the
// container's process list from the moment the confirm key is sent. It
// stops once the streamed view reports it has finished, or a red flash
// shows, or 40s pass.
func t116RunRegistration(t *testing.T, secrets, secretName string, pathDirs []string, needles []string, extraArgs ...string) *t116Reg {
	t.Helper()
	h := t116NewHome(t)
	h.trustTesthost()
	fp := h.writeFleet("tae116.yaml", t116RegFleet(secrets, secretName), 0o644)
	u := h.startTUI(h.env(pathDirs...), extraArgs...)
	u.openFleet("tae116")
	u.enterHost(0, testUser1Password)
	u.gotoPickerRow("Subscription")
	u.key("Enter")
	u.settle(30 * time.Second)

	var samp *t116Sampler
	if needles != nil {
		samp = t116StartSampler(needles, t116RegMarker)
	}
	u.key("g")
	if p, _, ok := u.tryWait(10*time.Second, "Register to"); !ok {
		if samp != nil {
			samp.finish()
		}
		return &t116Reg{home: h, ui: u, fleetPath: fp, pane: p}
	}
	u.key("y")
	pane, _, _ := u.tryWait(40*time.Second, "STOPPED", "az CLI not found", "not logged in to Azure", "unreachable or access denied", "not found in vault", "requires RHEL 8 or later")
	r := &t116Reg{home: h, ui: u, fleetPath: fp, pane: pane}
	if samp != nil {
		time.Sleep(1500 * time.Millisecond) // a short tail: the process may outlive the view
		r.samples, r.sawMarker, r.leak = samp.finish()
	}
	return r
}

// ===========================================================================
// A -- secret resolution
// ===========================================================================

// the stand-in az is a fixture the Test interface names; check it is the
// stand-in the other tests assume, so their failures mean what they say.
func TestTAE116AzStubBehaviour(t *testing.T) {
	stub := t116Abs(t, filepath.Join(t116AzStubDir, "az"))
	if fi, err := os.Stat(stub); err != nil || fi.Mode()&0o111 == 0 {
		t.Fatalf("test/azstub/az must exist and be executable (Test interface): %v", err)
	}
	run := func(args ...string) (string, string, int) {
		cmd := exec.Command(stub, args...)
		var so, se bytes.Buffer
		cmd.Stdout, cmd.Stderr = &so, &se
		err := cmd.Run()
		code := 0
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
		return so.String(), se.String(), code
	}
	base := []string{"keyvault", "secret", "show", "--vault-name", t116Vault}
	if so, _, code := run(append(base, "--name", "stub-ok", "--query", "value", "-o", "tsv")...); code != 0 || strings.TrimSpace(so) != t116StubSecret {
		t.Errorf("--name stub-ok: want stdout %q exit 0, got %q exit %d", t116StubSecret, so, code)
	}
	if _, se, code := run(append(base, "--name", "stub-missing")...); code != 1 || !strings.Contains(se, "(SecretNotFound)") {
		t.Errorf("--name stub-missing: want exit 1 stderr (SecretNotFound), got exit %d stderr %q", code, se)
	}
	if _, se, code := run(append(base, "--name", "stub-forbidden")...); code != 1 || !strings.Contains(se, "(Forbidden)") {
		t.Errorf("--name stub-forbidden: want exit 1 stderr (Forbidden), got exit %d stderr %q", code, se)
	}
	if _, se, code := run("keyvault", "secret", "show", "--vault-name", "stub-nologin", "--name", "stub-ok"); code != 1 || !strings.Contains(se, "Please run 'az login'") {
		t.Errorf("--vault-name stub-nologin: want exit 1 stderr \"Please run 'az login'\", got exit %d stderr %q", code, se)
	}
}

// A5
func TestTAE116SecretsUnsupportedProviderRejectedAtLoad(t *testing.T) {
	h := t116NewHome(t)
	p := h.writeFleet("bad-provider.yaml", "name: bad\ntype: vm\nsecrets:\n  provider: hashicorp-vault\n  vault: v\nhosts:\n  - name: h\n    hostname: 127.0.0.1\n", 0o644)
	out, code := h.runStartup(h.env())
	if code != 1 {
		t.Errorf("A5: want exit 1 for an unsupported secrets provider, got %d\n%s", code, out)
	}
	for _, want := range []string{"error scanning fleets: parsing", filepath.Base(p), `secrets.provider "hashicorp-vault" is not supported (supported: azure-keyvault)`} {
		if !strings.Contains(out, want) {
			t.Errorf("A5: stderr should contain %q; got:\n%s", want, out)
		}
	}
}

// A6
func TestTAE116SecretNameWithoutSecretsBlockRejectedAtLoad(t *testing.T) {
	h := t116NewHome(t)
	h.writeFleet("no-block.yaml", t116RegFleet("", "stub-ok"), 0o644)
	out, code := h.runStartup(h.env())
	if code != 1 {
		t.Errorf("A6: want exit 1 for a _secret_name field with no secrets: block, got %d\n%s", code, out)
	}
	for _, want := range []string{`rh_activation_key_secret_name`, "has no secrets: block", "error scanning fleets: parsing"} {
		if !strings.Contains(out, want) {
			t.Errorf("A6: stderr should contain %q; got:\n%s", want, out)
		}
	}
}

// A7: a fleet with no _secret_name field is untouched, with or without a
// secrets: block. (Characterisation: this guards behaviour that exists today
// and must survive; it is expected to be green at red.)
func TestTAE116FleetWithoutSecretNameBehavesAsToday(t *testing.T) {
	for name, secrets := range map[string]string{"no-secrets-block": "", "with-secrets-block": t116SecretsBlock} {
		t.Run(name, func(t *testing.T) {
			h := t116NewHome(t)
			h.trustTesthost()
			h.writeFleet("plain.yaml", "name: tae116\ntype: vm\n"+secrets+"defaults:\n  port: 2222\nhosts:\n  - name: th-plain\n    hostname: 127.0.0.1\n    user: testuser1\n", 0o644)
			u := h.startTUI(h.env())
			u.openFleet("tae116")
			p := u.wait(20*time.Second, "the host list", "th-plain")
			if strings.Contains(strings.ToLower(p), "secret") {
				t.Errorf("A7: a fleet with no _secret_name field must not mention secrets; screen:\n%s", p)
			}
		})
	}
}

// A1 + A4: four failure modes, four distinct messages, nothing proceeds, and
// the fleet still opens and lists its hosts when the secret cannot resolve.
func TestTAE116SecretResolutionFailureModes(t *testing.T) {
	cases := []struct {
		name, secret, vault, want string
		noAz                      bool
	}{
		{"az-missing", "stub-ok", t116Vault, "az CLI not found", true},
		{"not-logged-in", "stub-ok", "stub-nologin", "not logged in to Azure", false},
		{"forbidden", "stub-forbidden", t116Vault, "unreachable or access denied", false},
		{"not-found", "stub-missing", t116Vault, "not found in vault", false},
	}
	seen := map[string]string{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.noAz {
				if p, err := exec.LookPath("az"); err == nil {
					// the test controls PATH for the child, but make sure the
					// directories it will use really have no az
					for _, d := range []string{"/usr/bin", "/bin"} {
						if _, e := os.Stat(filepath.Join(d, "az")); e == nil {
							t.Fatalf("precondition: %s/az exists, so a PATH without az cannot be built (host az at %s)", d, p)
						}
					}
				}
			}
			var dirs []string
			if !c.noAz {
				dirs = []string{t116Abs(t, t116AzStubDir)}
			}
			r := t116RunRegistration(t, t116SecretsBlockFor(c.vault), c.secret, dirs, nil)
			// A1: the fleet opened and listed its host despite the unresolvable secret
			if !strings.Contains(r.pane, c.want) {
				t.Errorf("A4: want a message containing %q after registering with secret %q (vault %q); screen:\n%s", c.want, c.secret, c.vault, r.pane)
			}
			if !strings.Contains(r.pane, fmt.Sprintf("secret %q", c.secret)) {
				t.Errorf("A4: the message should name the secret as secret %q; screen:\n%s", c.secret, r.pane)
			}
			if strings.Contains(r.pane, t116StubSecret) {
				t.Errorf("A4: no failure message may contain any part of the secret; screen:\n%s", r.pane)
			}
			if strings.Contains(strings.ToLower(r.pane), "live") {
				t.Errorf("A4: a resolution failure must never let the registration proceed (streamed view started); screen:\n%s", r.pane)
			}
			seen[c.name] = c.want
		})
	}
	if len(seen) == len(cases) {
		uniq := map[string]bool{}
		for _, w := range seen {
			uniq[w] = true
		}
		if len(uniq) != 4 {
			t.Errorf("A4: want four distinct messages, got %d", len(uniq))
		}
	}
}

// A1: opening the fleet and listing hosts must not need the vault at all.
func TestTAE116UnreachableVaultDoesNotStopFleetFromOpening(t *testing.T) {
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", t116RegFleet(t116SecretsBlockFor("stub-nologin"), "stub-forbidden"), 0o644)
	// PATH has no az at all: any resolution at load or at listing would fail loudly
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	p := u.wait(20*time.Second, "the host list of a fleet whose vault is unreachable", "th-reg")
	for _, bad := range []string{"az CLI not found", "not logged in", "unreachable or access denied", "not found in vault", "error scanning"} {
		if strings.Contains(p, bad) {
			t.Errorf("A1: listing the hosts must not resolve any secret; screen shows %q:\n%s", bad, p)
		}
	}
}

// ===========================================================================
// B -- activation key
// ===========================================================================

// B1
func TestTAE116ActivationKeyFieldRemovedFromSchema(t *testing.T) {
	cases := map[string]string{
		"on-defaults": "name: old\ntype: vm\ndefaults:\n  rh_activation_key: not-a-real-key\nhosts:\n  - name: h\n    hostname: 127.0.0.1\n",
		"on-host":     "name: old\ntype: vm\nhosts:\n  - name: h\n    hostname: 127.0.0.1\n    rh_activation_key: not-a-real-key\n",
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			h := t116NewHome(t)
			h.writeFleet("old.yaml", body, 0o644)
			out, code := h.runStartup(h.env())
			if code != 1 {
				t.Errorf("B1: want exit 1 for a fleet still carrying rh_activation_key, got %d\n%s", code, out)
			}
			for _, want := range []string{"rh_activation_key is no longer accepted", "rh_activation_key_secret_name"} {
				if !strings.Contains(out, want) {
					t.Errorf("B1: the error must name the field and its replacement; want %q in:\n%s", want, out)
				}
			}
			if strings.Contains(out, "not-a-real-key") {
				t.Errorf("B1: the rejected value must not be echoed:\n%s", out)
			}
		})
	}
}

// B2, B6 (and the ADR-F005 confirmation): registration confirms, then runs in
// the in-TUI streamed view, not the terminal handover.
func TestTAE116RegistrationRunsInStreamedViewAfterConfirmation(t *testing.T) {
	r := t116RunRegistration(t, t116SecretsBlock, "stub-ok", []string{t116Abs(t, t116AzStubDir)}, nil)
	low := strings.ToLower(r.pane)
	if !strings.Contains(low, "live") && !strings.Contains(low, "stopped") {
		t.Fatalf("B2/B6: after confirming, registration must run in the SSH Stream view (status bar LIVE / STOPPED); screen:\n%s", r.pane)
	}
	if strings.Contains(r.pane, "rh_org_id and rh_activation_key required") {
		t.Errorf("B2: the activation key must come from rh_activation_key_secret_name, not the removed field; screen:\n%s", r.pane)
	}
}

// B6: the ADR-F005 confirmation is kept -- nothing starts before it.
func TestTAE116RegistrationKeepsItsConfirmation(t *testing.T) {
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", t116RegFleet(t116SecretsBlock, "stub-ok"), 0o644)
	u := h.startTUI(h.env(t116Abs(t, t116AzStubDir)))
	u.openFleet("tae116")
	u.enterHost(0, testUser1Password)
	u.gotoPickerRow("Subscription")
	u.key("Enter")
	u.settle(30 * time.Second)
	u.key("g")
	p := u.wait(10*time.Second, "the register confirmation", "Register to")
	if low := strings.ToLower(p); strings.Contains(low, "live") {
		t.Errorf("B6: the streamed view must not start before the user confirms; screen:\n%s", p)
	}
	u.key("n")
	time.Sleep(800 * time.Millisecond)
	if after := strings.ToLower(u.pane()); strings.Contains(after, "live") {
		t.Errorf("B6: declining the confirmation must not start a registration; screen:\n%s", u.pane())
	}
}

// B3: older than RHEL 8 -> a message saying so, never the CLI fallback.
func TestTAE116RegistrationRefusedBelowRHEL8(t *testing.T) {
	requireTesthost(t)
	for _, f := range []string{"/etc/os-release", "/etc/redhat-release"} {
		t116Podman(t, "cp", "-a", f, f+".tae116bak")
		f := f
		t.Cleanup(func() {
			_ = exec.Command("podman", "exec", containerName, "sh", "-c", "rm -f "+f+" && mv -f "+f+".tae116bak "+f).Run()
		})
	}
	// /etc/os-release is a symlink to /usr/lib/os-release: replace the link
	// (never write through it), and restore the link itself afterwards
	t116Podman(t, "sh", "-c", `rm -f /etc/os-release /etc/redhat-release; printf 'NAME="Red Hat Enterprise Linux Server"\nVERSION="7.9 (Maipo)"\nID="rhel"\nVERSION_ID="7.9"\nPRETTY_NAME="Red Hat Enterprise Linux Server 7.9 (Maipo)"\n' > /etc/os-release; echo 'Red Hat Enterprise Linux Server release 7.9 (Maipo)' > /etc/redhat-release`)

	samp := t116StartSampler([]string{"--activationkey", "activationkey="}, nil)
	r := t116RunRegistration(t, t116SecretsBlock, "stub-ok", []string{t116Abs(t, t116AzStubDir)}, nil)
	_, _, cliLeak := samp.finish()

	if !strings.Contains(r.pane, "requires RHEL 8 or later") {
		t.Errorf("B3: want a message containing %q for a RHEL 7 target; screen:\n%s", "requires RHEL 8 or later", r.pane)
	}
	if cliLeak != "" {
		t.Errorf("B3: registration must never fall back to the subscription-manager CLI (key as an argument): %s", cliLeak)
	}
}

// B4 -- the evidence target `make testhost-register-ps` runs this test by
// name. Neither `ps aux` nor /proc/*/cmdline may show any part of the key at
// any sampled instant while a registration runs, and the measurement only
// counts if a registration process was actually observed.
func TestTAE116RegisterKeyNotInProcessList(t *testing.T) {
	r := t116RunRegistration(t, t116SecretsBlock, "stub-ok", []string{t116Abs(t, t116AzStubDir)}, t116KeyNeedles(t116StubSecret))
	if r.samples == 0 {
		t.Fatalf("B4: registration never reached the confirmation, so nothing was measured; screen:\n%s", r.pane)
	}
	if !r.sawMarker {
		t.Fatalf("B4: %d process-list samples were taken but none showed a registration process (RegisterWithActivationKeys / rhsm / dbus / register); a window with no registration in it proves nothing. Screen:\n%s", r.samples, r.pane)
	}
	if r.leak != "" {
		t.Errorf("B4: the activation key appeared in the container's process list while a registration ran: %s", r.leak)
	}
}

// B5 / A2: the key reaches no local file, log or screen.
func TestTAE116ResolvedKeyNeverWrittenLocally(t *testing.T) {
	r := t116RunRegistration(t, t116SecretsBlock, "stub-ok", []string{t116Abs(t, t116AzStubDir)}, nil, "--debug")
	low := strings.ToLower(r.pane)
	if !strings.Contains(low, "live") && !strings.Contains(low, "stopped") {
		t.Fatalf("B5: registration did not run, so there is nothing to scan for; screen:\n%s", r.pane)
	}
	if strings.Contains(r.pane, t116StubSecret) {
		t.Errorf("B5: the resolved key is on screen:\n%s", r.pane)
	}
	var leaks []string
	_ = filepath.Walk(r.home.root, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		if b, e := os.ReadFile(p); e == nil && bytes.Contains(b, []byte(t116StubSecret)) {
			leaks = append(leaks, p)
		}
		return nil
	})
	if len(leaks) > 0 {
		t.Errorf("A2/B5: the resolved key was written to local file(s): %v", leaks)
	}
	if _, err := os.Stat(r.home.path(".local/share/fleetdesk/debug.log")); err != nil {
		t.Errorf("B5: --debug should have produced a debug log to scan (so the scan above is meaningful): %v", err)
	}
}

// ===========================================================================
// C -- host-key verification
// ===========================================================================

func t116KnownHosts(h *t116Home) string {
	b, _ := os.ReadFile(h.path(".ssh/known_hosts"))
	return string(b)
}

func t116Fingerprint(t *testing.T) (keytype, fp string) {
	t.Helper()
	out, err := exec.Command("sh", "-c", "ssh-keyscan -t ed25519 -p 2222 127.0.0.1 2>/dev/null | ssh-keygen -lf -").Output()
	if err != nil {
		t.Fatalf("fingerprint of the container's ed25519 host key: %v", err)
	}
	f := strings.Fields(string(out))
	for _, x := range f {
		if strings.HasPrefix(x, "SHA256:") {
			fp = x
		}
	}
	return "ssh-ed25519", fp
}

// C2 + C5: first contact asks, accepting records the key, the next connect
// is silent.
func TestTAE116HostKeyTOFUAcceptThenSilentReconnect(t *testing.T) {
	h := t116NewHome(t) // known_hosts empty: the host is unknown
	h.writeFleet("tae116.yaml", t116OneHost("th-tofu", "127.0.0.1", testUser1), 0o644)
	_, fp := t116Fingerprint(t)

	u := h.startTUI(h.env())
	u.openFleet("tae116")
	p := u.wait(25*time.Second, "the Unknown host key modal", "Unknown host key")
	for _, want := range []string{"127.0.0.1", "SHA256:", "Trust this host and add it to known_hosts? [y/N]"} {
		if !strings.Contains(p, want) {
			t.Errorf("C2: modal should contain %q; screen:\n%s", want, p)
		}
	}
	if fp != "" && !strings.Contains(p, fp) {
		t.Errorf("C2: modal should show the fingerprint the host really presents (%s); screen:\n%s", fp, p)
	}
	if kh := t116KnownHosts(h); strings.TrimSpace(kh) != "" {
		t.Errorf("C2: nothing may be written to known_hosts before the user accepts; it holds:\n%s", kh)
	}
	u.key("y")
	u.settle(25 * time.Second)
	if out, err := exec.Command("ssh-keygen", "-F", "[127.0.0.1]:2222", "-f", h.path(".ssh/known_hosts")).CombinedOutput(); err != nil {
		t.Errorf("C2: accepting must append the key to known_hosts: ssh-keygen -F: %v\n%s\nknown_hosts:\n%s", err, out, t116KnownHosts(h))
	}

	// leave and come back: no prompt
	u.key("Escape")
	u.settle(5 * time.Second)
	u.key("Enter")
	p2, hit, prompted := u.tryWait(12*time.Second, "Unknown host key", "host key mismatch")
	if prompted {
		t.Errorf("C5: a trusted host must reconnect without a prompt or an error; saw %q:\n%s", hit, p2)
	}
}

// C2: declining aborts, and records nothing.
func TestTAE116HostKeyTOFUDeclineAborts(t *testing.T) {
	h := t116NewHome(t)
	h.writeFleet("tae116.yaml", t116OneHost("th-decline", "127.0.0.1", testUser1), 0o644)
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	u.wait(25*time.Second, "the Unknown host key modal", "Unknown host key")
	u.key("n")
	p := u.settle(20 * time.Second)
	if kh := t116KnownHosts(h); strings.TrimSpace(kh) != "" {
		t.Errorf("C2: declining must not write known_hosts; it holds:\n%s", kh)
	}
	if strings.Contains(strings.ToLower(p), "unknown host key") {
		t.Errorf("C2: after declining the modal should be gone; screen:\n%s", p)
	}
	if !strings.Contains(strings.ToLower(p), "unreachable") && !strings.Contains(strings.ToLower(p), "declined") && !strings.Contains(strings.ToLower(p), "not trusted") {
		t.Errorf("C2: a declined host is not connected; the row should say so; screen:\n%s", p)
	}
}

// C3 + C5: the key changes, the connection stops and nothing is accepted
// silently.
func TestTAE116HostKeyMismatchStopsConnection(t *testing.T) {
	h := t116NewHome(t)
	h.trustTesthost()
	before := t116KnownHosts(h)
	h.writeFleet("tae116.yaml", t116OneHost("th-mismatch", "127.0.0.1", testUser1), 0o644)
	if out, err := runMake("testhost-regen-host-key"); err != nil {
		t.Fatalf("regenerate the host key: %v\n%s", err, out)
	}
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	p := u.settle(25 * time.Second)
	if !strings.Contains(p, "host key mismatch") {
		t.Fatalf("C3: a changed host key must stop the connection with an error naming the mismatch; screen:\n%s", p)
	}
	row := ""
	for _, l := range strings.Split(p, "\n") {
		if strings.Contains(l, "th-mismatch") {
			row = l
		}
	}
	if !strings.Contains(row, "unreachable (host key mismatch") {
		t.Errorf("C3: the host row should read `th-mismatch  unreachable (host key mismatch: ...)`; row: %q", row)
	}
	if strings.Contains(p, "Unknown host key") {
		t.Errorf("C3: a changed key is a mismatch, not a first contact; it must never be offered for trust:\n%s", p)
	}
	if after := t116KnownHosts(h); after != before {
		t.Errorf("C3: a changed key must never be accepted silently; known_hosts changed.\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

// C1: the password-auth dial verifies too. The key-auth dial trusts the host;
// the host key then changes while the SSH Password prompt is open; the
// password dial must refuse.
func TestTAE116PasswordDialVerifiesHostKey(t *testing.T) {
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", t116OneHost("th-pwdial", "127.0.0.1", testUser2), 0o644) // testuser2: password only
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	u.wait(25*time.Second, "the SSH Password prompt", "SSH Password")
	if out, err := runMake("testhost-regen-host-key"); err != nil {
		t.Fatalf("regenerate the host key: %v\n%s", err, out)
	}
	before := t116KnownHosts(h)
	u.text(testUser2Password)
	u.key("Enter")
	p := u.settle(25 * time.Second)
	if !strings.Contains(p, "host key mismatch") {
		t.Errorf("C1: the password-auth dial must verify the host key against known_hosts; after the key changed, screen:\n%s", p)
	}
	if t116KnownHosts(h) != before {
		t.Errorf("C1: known_hosts must not change on a mismatch")
	}
}

func t116GitGrep(t *testing.T, args ...string) []string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"grep", "-n", "-I"}, args...)...).CombinedOutput()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
		t.Fatalf("git grep %v: %v\n%s", args, err, out)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			lines = append(lines, l)
		}
	}
	return lines
}

// C4
func TestTAE116HostKeyChecksNeverDisabledAnywhere(t *testing.T) {
	// the needles are built from pieces so this file does not match itself
	insecure := "Insecure" + "IgnoreHostKey"
	strict := "StrictHostKey" + "Checking=no"
	const self = ":!tae116_testhost_test.go"
	if hits := t116GitGrep(t, insecure, "--", ".", self); len(hits) > 0 {
		t.Errorf("C4: %s must appear nowhere in the codebase, test harness included:\n%s", insecure, strings.Join(hits, "\n"))
	}
	if hits := t116GitGrep(t, strict, "--", ".", self); len(hits) > 0 {
		t.Errorf("C4: %s must be gone from every ssh and ssh-copy-id invocation, the deploy-key script included:\n%s", strict, strings.Join(hits, "\n"))
	}
	if hits := t116GitGrep(t, "-E", `nolint:[a-z]+.*TAE-22`, "--", "*.go", self); len(hits) > 0 {
		t.Errorf("C4: every //nolint tagged TAE-22 must be removed:\n%s", strings.Join(hits, "\n"))
	}
	// the test that asserts those suppressions exist is removed too
	if hits := t116GitGrep(t, "-E", `nolint:gosec.*TAE-22|naming TAE-22`, "--", "*_test.go", self); len(hits) > 0 {
		t.Errorf("C4: the test asserting the TAE-22 suppressions exist must be removed:\n%s", strings.Join(hits, "\n"))
	}
	mk, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^NOLINT_GOSEC_TAE22_WANT\s*:=\s*0\s*$`).Match(mk) {
		t.Errorf("C4: verify-nolint's expected TAE-22 count must be updated to 0")
	}
	if out, err := exec.Command("make", "verify-nolint").CombinedOutput(); err != nil {
		t.Errorf("C4: make verify-nolint must pass with the updated counts: %v\n%s", err, out)
	}
}

// ===========================================================================
// D -- every SSH path reaches the same host
// ===========================================================================

// D1 + D2: the ssh config rewrites Hostname/Port. The host list (key auth),
// the password-auth fetch and the handover all reach the container.
func TestTAE116EverySSHPathReachesTheSameHost(t *testing.T) {
	want := strings.TrimSpace(t116Podman(t, "cat", "/proc/sys/kernel/hostname"))
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", `name: tae116
type: vm
hosts:
  - name: th-alias-key
    hostname: testhost-alias
    user: testuser1
  - name: th-alias-pw
    hostname: testhost-alias
    user: testuser2
`, 0o644)
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	// password-auth path: testuser2 has no key
	u.wait(25*time.Second, "the SSH Password prompt for the password-only account", "SSH Password")
	u.text(testUser2Password)
	u.key("Enter")
	p := u.settle(25 * time.Second)
	for _, l := range strings.Split(p, "\n") {
		if (strings.Contains(l, "th-alias-key") || strings.Contains(l, "th-alias-pw")) && strings.Contains(strings.ToLower(l), "unreachable") {
			t.Errorf("D2: both hosts must be reachable through the alias on every in-app path; row: %q", l)
		}
	}
	// key-auth metrics fetch
	u.selectRow(0)
	u.key("d")
	mp := u.settle(20 * time.Second)
	if !strings.Contains(mp, "th-alias-key") || strings.Contains(strings.ToLower(mp), "unreachable") {
		t.Errorf("D2: the key-auth metrics fetch must reach the aliased host; screen:\n%s", mp)
	}
	u.key("Escape")
	u.settle(5 * time.Second)
	// handover: the shell lands on the same machine
	u.selectRow(0)
	u.key("x")
	time.Sleep(2500 * time.Millisecond)
	u.text("hostname\n")
	hp, _, ok := u.tryWait(15*time.Second, want)
	if !ok {
		t.Errorf("D2: the handover shell must land on the same machine as the in-app paths (container hostname %q); screen:\n%s", want, hp)
	}
	u.text("exit\n")
}

// D3: ProxyJump host -> a specific state, handover still offered.
func TestTAE116ProxyJumpHostIsHandoverOnly(t *testing.T) {
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", t116OneHost("th-bastion", "testhost-bastion", testUser1), 0o644)
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	p := u.settle(25 * time.Second)
	row := ""
	for _, l := range strings.Split(p, "\n") {
		if strings.Contains(l, "th-bastion") {
			row = l
		}
	}
	for _, want := range []string{"handover only", "ProxyJump via nowhere.invalid", "in-app views unavailable"} {
		if !strings.Contains(row, want) {
			t.Errorf("D3: the host row should contain %q; row: %q\nscreen:\n%s", want, row, p)
		}
	}
	u.selectRow(0)
	u.key("Enter")
	fp, _, ok := u.tryWait(8*time.Second, "Needs ProxyJump")
	if !ok || !strings.Contains(fp, "only shell, reboot and deploy-key work on this host") {
		t.Errorf("D3: Enter on it should flash `Needs ProxyJump — only shell, reboot and deploy-key work on this host`; screen:\n%s", fp)
	}
	// the handover action is really attempted: ssh runs and names the jump host
	u.key("x")
	sp, _, ok := u.tryWait(15*time.Second, "nowhere.invalid")
	if !ok {
		t.Errorf("D3: a handover action on a ProxyJump host must still run (ssh should have tried the jump host); screen:\n%s", sp)
	}
}

// D4
func TestTAE116ReadmeDescribesEverySSHPath(t *testing.T) {
	b, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)
	i := strings.Index(doc, "## SSH Authentication")
	if i < 0 {
		t.Fatalf("D4: README has no `## SSH Authentication` section")
	}
	sec := doc[i+len("## SSH Authentication"):]
	if j := strings.Index(sec, "\n## "); j >= 0 {
		sec = sec[:j]
	}
	low := strings.ToLower(sec)
	for _, want := range []string{"hostname", "port", "identityfile", "proxyjump", "known_hosts", "handover", "password"} {
		if !strings.Contains(low, want) {
			t.Errorf("D4: the SSH Authentication section should cover %q (which ~/.ssh/config directives apply to which path); section:\n%s", want, sec)
		}
	}
}

// ===========================================================================
// E -- a sudo password stays with its host
// ===========================================================================

// t116TwoSudoHosts gives testuser2 key auth (the fixture ships it
// password-only) so neither host has an SSH password cached, then restores it.
func t116TwoSudoHosts(t *testing.T) *t116Home {
	t.Helper()
	requireTesthost(t)
	t116Podman(t, "sh", "-c", `install -d -m 0700 -o testuser2 -g testuser2 /home/testuser2/.ssh && cp /home/testuser1/.ssh/authorized_keys /home/testuser2/.ssh/authorized_keys && chown testuser2:testuser2 /home/testuser2/.ssh/authorized_keys && chmod 0600 /home/testuser2/.ssh/authorized_keys`)
	t.Cleanup(func() {
		_ = exec.Command("podman", "exec", containerName, "rm", "-f", "/home/testuser2/.ssh/authorized_keys").Run()
	})
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", `name: tae116
type: vm
defaults:
  port: 2222
hosts:
  - name: th-sudo-a
    hostname: 127.0.0.1
    user: testuser1
  - name: th-sudo-b
    hostname: 127.0.0.1
    user: testuser2
`, 0o644)
	return h
}

// E1: verified on A; B needs one; it is offered, and the user is told.
func TestTAE116SudoPasswordReuseIsAnnounced(t *testing.T) {
	h := t116TwoSudoHosts(t)
	// make the two accounts share a sudo password so the reuse can succeed
	t116Podman(t, "sh", "-c", fmt.Sprintf("echo 'testuser2:%s' | chpasswd", testUser1Password))
	t.Cleanup(func() {
		_ = exec.Command("podman", "exec", containerName, "sh", "-c", fmt.Sprintf("echo 'testuser2:%s' | chpasswd", testUser2Password)).Run()
	})
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	u.enterHost(0, testUser1Password)
	u.key("Escape")
	u.settle(10 * time.Second)
	u.selectRow(1)
	u.key("Enter")
	p, _, ok := u.tryWait(25*time.Second, "reusing the password verified on th-sudo-a")
	if !ok {
		t.Fatalf("E1: entering B with A's password verified should flash `sudo: reusing the password verified on th-sudo-a`; screen:\n%s", p)
	}
	if !strings.Contains(p, "sudo: reusing the password verified on th-sudo-a") {
		t.Errorf("E1: flash wording; screen:\n%s", p)
	}
	if strings.Contains(p, "Confirm") || strings.Contains(p, "[Y/n]") || strings.Contains(p, "[y/N]") {
		t.Errorf("E1: there is no confirmation prompt for reuse; screen:\n%s", p)
	}
}

// E2 + E3 + E4: capability tested per host; a failure on B leaves A working;
// both states visible through behaviour.
func TestTAE116SudoPasswordStaysWithItsHost(t *testing.T) {
	h := t116TwoSudoHosts(t)
	u := h.startTUI(h.env())
	u.openFleet("tae116")

	u.enterHost(0, testUser1Password) // A verified
	u.key("Escape")
	u.settle(10 * time.Second)

	// E2: B must not be treated as sudo-ready because A is
	u.selectRow(1)
	u.key("Enter")
	p, _ := u.waitAny(25*time.Second, "B to ask for its own sudo password, not inherit A's readiness", "Sudo password for", "System Logs")
	if !strings.Contains(p, "Sudo password for") {
		t.Fatalf("E2: SudoReady must reflect a capability tested on that host; B went straight to the Resource Picker on A's strength:\n%s", p)
	}
	if !strings.Contains(p, "testuser2") {
		t.Errorf("E2: the prompt should be for testuser2@..., B's own account; screen:\n%s", p)
	}

	// E3: a wrong password on B ...
	u.text("not-the-password")
	u.key("Enter")
	wp := u.wait(25*time.Second, "the wrong-password message", "wrong sudo password")
	_ = wp
	u.key("Escape") // leave B's re-opened wizard
	u.settle(8 * time.Second)

	// ... leaves A's verified password working: no prompt
	u.selectRow(0)
	u.key("Enter")
	deadline := time.Now().Add(20 * time.Second)
	reached := false
	for time.Now().Before(deadline) {
		cur := u.pane()
		if strings.Contains(cur, "Sudo password for") {
			t.Fatalf("E3: a password that failed on B must not invalidate A's; A asked again:\n%s", cur)
		}
		if strings.Contains(cur, "System Logs") {
			reached = true
			break
		}
		time.Sleep(150 * time.Millisecond)
	}
	if !reached {
		t.Fatalf("E3: A did not reach the Resource Picker; screen:\n%s", u.pane())
	}
	u.key("Escape")
	u.settle(8 * time.Second)

	// E4: B's own password works on B
	u.enterHost(1, testUser2Password)
}

// ===========================================================================
// F -- the probe's privileged read
// ===========================================================================

var t116SystemLogsRe = regexp.MustCompile(`System Logs\s+(\S+)`)

func t116SystemLogsTotal(t *testing.T, pane string) string {
	t.Helper()
	m := t116SystemLogsRe.FindStringSubmatch(pane)
	if m == nil {
		t.Fatalf("no System Logs row in the Resource Picker:\n%s", pane)
	}
	return m[1]
}

// F1 + F2 + F4: a probe whose privileged read could not obtain sudo says
// n/a, never a count that reads as a successful zero. Made deterministic:
// the account's sudo password is changed on the host, so no password the
// session holds can work when the host is re-probed from the host list; the
// user then supplies the new password and the Resource Picker's System Logs
// TOTAL is judged. (The Resource Picker's own `r` refresh is not used: on
// this container it stays on "Loading resource counts..." indefinitely,
// independent of this issue.) The positive half -- an integer when sudo
// worked -- is not asserted: it needs journalctl in the fixture, which the
// container does not have (TAE-99's VM tier); see the red-run notes.
func TestTAE116ProbePrivilegedReadReportsWhenItCouldNotRun(t *testing.T) {
	requireTesthost(t)
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", t116OneHost("th-probe", "127.0.0.1", testUser1), 0o644)
	const newPW = "changed-tae116-pw"
	t.Cleanup(func() {
		_ = exec.Command("podman", "exec", containerName, "sh", "-c", fmt.Sprintf("echo 'testuser1:%s' | chpasswd", testUser1Password)).Run()
	})
	u := h.startTUI(h.env())
	u.openFleet("tae116")
	u.enterHost(0, testUser1Password) // a session that held a working sudo password
	u.key("Escape")
	u.settle(8 * time.Second)

	t116Podman(t, "sh", "-c", fmt.Sprintf("echo 'testuser1:%s' | chpasswd", newPW))
	u.key("r") // re-probe: sudo cannot be obtained now
	time.Sleep(10 * time.Second)
	u.settle(30 * time.Second)

	p := u.enterHost(0, newPW)
	if got := t116SystemLogsTotal(t, p); got != "n/a" {
		t.Errorf("F1/F2: the probe's privileged read could not obtain sudo, so the System Logs TOTAL must be `n/a`, not a count that reads as a successful zero; got %q\n%s", got, p)
	}
}

// F1: the probe carries whether the count is known.
func TestTAE116ProbeInfoCarriesErrorCountKnown(t *testing.T) {
	hits := t116GitGrep(t, "ErrorCountKnown", "--", "*.go", ":!*_test.go")
	if len(hits) == 0 {
		t.Errorf("F1: ProbeInfo must gain ErrorCountKnown (Test interface)")
	}
}

// ===========================================================================
// G -- config file permissions
// ===========================================================================

func t116Mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatalf("stat %s: %v", p, err)
	}
	return fi.Mode().Perm()
}

// G1: the first-run wizard writes config.yaml 0600 in a 0700 directory.
func TestTAE116ConfigWrittenOwnerOnly(t *testing.T) {
	h := t116NewHome(t)
	if err := os.RemoveAll(h.path(".config")); err != nil {
		t.Fatal(err)
	}
	h.writeFleet("tae116.yaml", t116OneHost("th-g1", "127.0.0.1", testUser1), 0o644)
	u := h.startTUI(h.env())
	u.wait(15*time.Second, "the first-run wizard", "fleet files directory")
	u.text(h.fleet)
	u.key("Enter")
	u.wait(15*time.Second, "the editor step", "editor")
	u.key("Enter")
	u.wait(20*time.Second, "setup to finish", "tae116")
	cfg := h.path(".config/fleetdesk/config.yaml")
	if m := t116Mode(t, cfg); m != 0o600 {
		t.Errorf("G1: config.yaml must be written 0600; got %04o", m)
	}
	if m := t116Mode(t, filepath.Dir(cfg)); m != 0o700 {
		t.Errorf("G1: the config directory must be created 0700; got %04o", m)
	}
}

// G2: config.yaml other than 0600 stops startup.
func TestTAE116ConfigModeCheckedOnLoad(t *testing.T) {
	for _, mode := range []os.FileMode{0o644, 0o660} {
		t.Run(fmt.Sprintf("%04o", mode), func(t *testing.T) {
			h := t116NewHome(t)
			h.writeFleet("ok.yaml", t116OneHost("th-g2", "127.0.0.1", testUser1), 0o644)
			cfg := h.writeConfig("fleet_dir: " + h.fleet + "\n")
			if err := os.Chmod(cfg, mode); err != nil {
				t.Fatal(err)
			}
			out, code := h.runStartup(h.env())
			if code != 1 {
				t.Errorf("G2: want exit 1 for config.yaml at %04o, got %d\n%s", mode, code, out)
			}
			for _, want := range []string{fmt.Sprintf("config.yaml is too permissive (mode %04o); expected 0600", mode), "chmod 0600 " + cfg} {
				if !strings.Contains(out, want) {
					t.Errorf("G2: stderr should contain %q; got:\n%s", want, out)
				}
			}
		})
	}
}

// G2: a fleet file writable by group or others stops startup; a merely
// readable one (0644) is accepted.
func TestTAE116FleetFileWritabilityCheckedOnLoad(t *testing.T) {
	for _, mode := range []os.FileMode{0o666, 0o664, 0o662} {
		t.Run(fmt.Sprintf("writable-%04o", mode), func(t *testing.T) {
			h := t116NewHome(t)
			p := h.writeFleet("loose.yaml", t116OneHost("th-g2w", "127.0.0.1", testUser1), mode)
			out, code := h.runStartup(h.env())
			if code != 1 {
				t.Errorf("G2: want exit 1 for a fleet file at %04o, got %d\n%s", mode, code, out)
			}
			for _, want := range []string{"fleet file " + p + " is writable by group or others", fmt.Sprintf("(mode %04o); run: chmod go-w %s", mode, p)} {
				if !strings.Contains(out, want) {
					t.Errorf("G2: stderr should contain %q; got:\n%s", want, out)
				}
			}
		})
	}
	t.Run("readable-0644-accepted", func(t *testing.T) {
		h := t116NewHome(t)
		h.trustTesthost()
		h.writeFleet("shareable.yaml", t116OneHost("th-g2r", "127.0.0.1", testUser1), 0o644)
		u := h.startTUI(h.env())
		u.wait(15*time.Second, "the Fleet Picker with a 0644 fleet file", "tae116")
	})
}

// G3
func TestTAE116XDGConfigHomeHonoured(t *testing.T) {
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", t116OneHost("th-xdg", "127.0.0.1", testUser1), 0o644)
	// the default location points nowhere useful; only $XDG_CONFIG_HOME is right
	h.writeConfig("fleet_dir: " + filepath.Join(h.root, "does-not-exist") + "\n")
	xdg := h.path("xdgdir")
	if err := os.MkdirAll(filepath.Join(xdg, "fleetdesk"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(xdg, "fleetdesk", "config.yaml"), []byte("fleet_dir: "+h.fleet+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	u := h.startTUI(append(h.env(), "XDG_CONFIG_HOME="+xdg))
	u.wait(15*time.Second, "the fleet from $XDG_CONFIG_HOME/fleetdesk/config.yaml", "tae116")
}

// G4
func TestTAE116ConfigNolintSuppressionsRemoved(t *testing.T) {
	if hits := t116GitGrep(t, "-E", `nolint:[a-z]+.*TAE-23`, "--", "*.go", ":!tae116_testhost_test.go"); len(hits) > 0 {
		t.Errorf("G4: every //nolint tagged TAE-23 must be removed:\n%s", strings.Join(hits, "\n"))
	}
	mk, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`(?m)^NOLINT_GOSEC_TAE23_WANT\s*:=\s*0\s*$`).Match(mk) {
		t.Errorf("G4: verify-nolint's expected TAE-23 count must be updated to 0")
	}
}

// ===========================================================================
// Make targets (Test interface) and the L1 human step's runnable half
// ===========================================================================

func TestTAE116MakeTargetsExist(t *testing.T) {
	want := map[string][]string{
		"testhost-m2":             {"TestTAE116", "tae116.out"},
		"testhost-exit-criterion": {"TestTAE116Exit"},
		"testhost-register-ps":    {"TestTAE116RegisterKeyNotInProcessList"},
	}
	for target, subs := range want {
		out, err := exec.Command("make", "-n", target).CombinedOutput()
		if err != nil {
			t.Errorf("Test interface: make target %s must exist: %v\n%s", target, err, out)
			continue
		}
		for _, s := range subs {
			if !strings.Contains(string(out), s) {
				t.Errorf("Test interface: `make -n %s` should run/write %q; got:\n%s", target, s, out)
			}
		}
	}
	mk, _ := os.ReadFile("Makefile")
	if m := regexp.MustCompile(`(?m)^REGRESSION_CONTROLS\s*:?=\s*(.*)$`).FindSubmatch(mk); m != nil {
		for _, bad := range []string{"testhost-m2", "testhost-exit-criterion", "testhost-register-ps", "keyvault-resolve"} {
			if strings.Contains(string(m[1]), bad) {
				t.Errorf("Test interface / H: %s must not join regression (macOS CI has no Podman)", bad)
			}
		}
	}
}

func TestTAE116KeyvaultResolveTarget(t *testing.T) {
	cmd := exec.Command("make", "keyvault-resolve", "VAULT="+t116Vault, "SUBSCRIPTION="+t116Sub, "NAME=stub-ok")
	cmd.Env = append(os.Environ(), "PATH="+t116Abs(t, t116AzStubDir)+":"+os.Getenv("PATH"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("make keyvault-resolve must succeed against the stand-in az: %v\n%s", err, out)
	}
	want := fmt.Sprintf("resolved stub-ok from %s: %d bytes, erased", t116Vault, len(t116StubSecret))
	if !strings.Contains(string(out), want) {
		t.Errorf("keyvault-resolve should print %q; got:\n%s", want, out)
	}
	if strings.Contains(string(out), t116StubSecret) {
		t.Errorf("keyvault-resolve must never print the value:\n%s", out)
	}
}

// ===========================================================================
// H -- milestone exit criterion (make testhost-exit-criterion)
// ===========================================================================

// H1: register a host and run a privileged command; no credential in the
// process list at any point, from the first connection to the last.
func TestTAE116ExitRegisterNoCredentialInProcessList(t *testing.T) {
	needles := append(t116KeyNeedles(t116StubSecret), testUser1Password, testUser2Password)
	h := t116NewHome(t)
	h.trustTesthost()
	h.writeFleet("tae116.yaml", t116RegFleet(t116SecretsBlock, "stub-ok"), 0o644)
	samp := t116StartSampler(needles, t116RegMarker)
	u := h.startTUI(h.env(t116Abs(t, t116AzStubDir)))
	u.openFleet("tae116")
	u.enterHost(0, testUser1Password) // a privileged path: the sudo wizard
	u.gotoPickerRow("Subscription")
	u.key("Enter")
	u.settle(30 * time.Second)
	u.key("g")
	u.wait(10*time.Second, "the register confirmation", "Register to")
	u.key("y")
	pane, _, _ := u.tryWait(40*time.Second, "STOPPED")
	time.Sleep(1500 * time.Millisecond)
	u.key("r") // a privileged command after the registration
	u.settle(30 * time.Second)
	samples, sawMarker, leak := samp.finish()
	if !sawMarker {
		t.Fatalf("H1: %d samples, none showed a registration process, so the exit criterion was not demonstrated; screen:\n%s", samples, pane)
	}
	if leak != "" {
		t.Errorf("H1: a credential appeared in the container's process list: %s", leak)
	}
}

// H2: the fleet file holds a name, never the value.
func TestTAE116ExitFleetFileHoldsOnlyAName(t *testing.T) {
	r := t116RunRegistration(t, t116SecretsBlock, "stub-ok", []string{t116Abs(t, t116AzStubDir)}, nil)
	low := strings.ToLower(r.pane)
	if !strings.Contains(low, "live") && !strings.Contains(low, "stopped") {
		t.Fatalf("H2: the registration did not run; screen:\n%s", r.pane)
	}
	b, err := os.ReadFile(r.fleetPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(b, []byte(t116StubSecret)) {
		t.Errorf("H2: the fleet file contains the key's value:\n%s", b)
	}
	if !bytes.Contains(b, []byte("rh_activation_key_secret_name")) {
		t.Errorf("H2: the fleet file should hold only a name, under rh_activation_key_secret_name:\n%s", b)
	}
	if hits := t116GitGrep(t, t116StubSecret, "--", ".", ":!tae116_testhost_test.go", ":!test/azstub"); len(hits) > 0 {
		t.Errorf("H2: the key value appears in tracked files:\n%s", strings.Join(hits, "\n"))
	}
}

// H3: every config file FleetDesk writes is owner-only, and it refuses to
// start on one that is not.
func TestTAE116ExitConfigFilesOwnerOnly(t *testing.T) {
	TestTAE116ConfigWrittenOwnerOnly(t)
	TestTAE116ConfigModeCheckedOnLoad(t)
}

// H4: change the container's host key and reconnect: the connection stops.
func TestTAE116ExitHostKeyChangeStopsConnection(t *testing.T) {
	TestTAE116HostKeyMismatchStopsConnection(t)
}

// H5: a host whose local ssh config rewrites its hostname -- every path
// reaches the same machine.
func TestTAE116ExitSSHPathsReachSameMachine(t *testing.T) {
	TestTAE116EverySSHPathReachesTheSameHost(t)
}
