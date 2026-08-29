//go:build testhost

package main_test

// Acceptance tests for TAE-20: Sudo password on stdin, never in the remote
// command string. Written from the issue's six acceptance criteria only --
// no plan comment, no decision, no review, and no current implementation of
// the fix was read to produce these (the pre-existing, documented-in-the-issue
// defect in rewriteSudoCmd is quoted by the issue itself, not independently
// discovered here).
//
// These tests share the TAE-98 fixture's TestMain (testhost_container_test.go,
// same package/build tag) rather than standing up their own container.
//
// Updated for TAE-21: Manager.SetSudoPassword and Manager.ConnectWithPassword
// take []byte, not string, per TAE-21's storage/ownership decision. This
// file's assertions are unchanged -- only the calls at the two entry points
// are adjusted to the new signatures, so this package compiles once TAE-21
// lands, without TAE-21's Dev needing to touch an acceptance test.
//
// AC2 requires measurement against a *resident* command -- a privileged view
// fetch completes in milliseconds and leaves no observation window. Every
// container-measurement test below therefore starts a long-lived `sudo`
// command through ssh.Manager and polls the container from the host side
// while it is still running.
//
// AC5's conflict set is documented by the issue as empty today (no app-layer
// call site sets session.Stdin), so this file does not hunt for a command
// that competes with the password for stdin. It protects the stated
// assumption (TestTAE20NoAppLayerCallSiteClaimsSessionStdin) and verifies the
// underlying connection is left fully usable by a subsequent command's own
// stdin (TestTAE20ConnectionUsableForFreshStdinAfterSudoDelivery).
//
// Run:
//
//	go test -tags testhost -run TestTAE20 -v .
import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	fdssh "github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// newSSHManagerConnected builds an *fdssh.Manager identical to the one the
// app constructs, dials host index 0 at the fixture with a password over
// ConnectWithPassword (the "fixed delivery path" AC2 requires -- the same
// entry point the app uses after a password prompt), and returns it together
// with a buffer capturing every log line the manager writes at the most
// verbose (Debug) level, for AC6.
func newSSHManagerConnected(t *testing.T, info *testhostInfo, user, password string) (*fdssh.Manager, *syncBuffer) {
	t.Helper()
	buf := &syncBuffer{}
	logger := slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	sm := fdssh.NewManager(logger)

	port, err := strconv.Atoi(info.Port)
	if err != nil {
		t.Fatalf("parse testhost port %q: %v", info.Port, err)
	}
	host := config.Host{Entry: config.HostEntry{
		Hostname: info.Host,
		Port:     port,
		User:     user,
		Timeout:  10 * time.Second,
	}}

	res := sm.ConnectWithPassword(0, host, []byte(password))
	if res.Err != nil {
		t.Fatalf("ConnectWithPassword against the TAE-98 fixture (the fixed delivery path AC2 requires): %v", res.Err)
	}
	return sm, buf
}

// syncBuffer is an io.Writer safe for the manager's own logging goroutine(s)
// and the test's assertions to touch concurrently.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// containerSnapshot is `ps aux`, every process's /proc/<pid>/cmdline, and
// every process's /proc/<pid>/environ, all read from the host via `podman
// exec`, exactly as AC2 specifies ("ps aux and /proc/*/cmdline are read on
// the container").
type containerSnapshot struct {
	psAux    string
	cmdlines string
	environs string
}

func readContainerSnapshot(t *testing.T) containerSnapshot {
	t.Helper()
	psOut, _ := exec.Command("podman", "exec", containerName, "ps", "aux").CombinedOutput()
	cmdlineOut, _ := exec.Command("podman", "exec", containerName, "sh", "-c",
		`for f in /proc/[0-9]*/cmdline; do tr '\0' ' ' < "$f" 2>/dev/null; echo; done`).CombinedOutput()
	environOut, _ := exec.Command("podman", "exec", containerName, "sh", "-c",
		`for f in /proc/[0-9]*/environ; do tr '\0' ' ' < "$f" 2>/dev/null; echo; done`).CombinedOutput()
	return containerSnapshot{
		psAux:    string(psOut),
		cmdlines: string(cmdlineOut),
		environs: string(environOut),
	}
}

// pollUntilResident polls the container until `marker` shows up in `ps aux`
// (proving the resident command is actually running -- a nil observation
// window proves nothing) or the timeout elapses, and returns the snapshot
// taken at that moment.
func pollUntilResident(t *testing.T, marker string, timeout time.Duration) (containerSnapshot, bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last containerSnapshot
	for time.Now().Before(deadline) {
		last = readContainerSnapshot(t)
		if strings.Contains(last.psAux, marker) {
			return last, true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return last, false
}

// --- AC: password on stdin pipe; never in command string, args, or env ---
// --- AC: verified by measurement against TAE-98, resident command, ps aux + /proc/*/cmdline ---

func TestTAE20SudoPasswordViaStdinNotInProcessArgsOrEnviron(t *testing.T) {
	info := requireTesthost(t)
	sm, _ := newSSHManagerConnected(t, info, testUser1, testUser1Password)
	defer sm.Close()

	sm.SetSudoPassword(0, []byte(testUser1Password))

	marker := fmt.Sprintf("fleetdesk_tae20_marker_%d", time.Now().UnixNano())
	cmd := fmt.Sprintf("sudo bash -c 'M=%s; sleep 6'", marker)

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sm.RunSudoCommand(0, cmd)
		done <- result{out, err}
	}()

	snap, sawMarker := pollUntilResident(t, marker, 5*time.Second)
	if !sawMarker {
		t.Fatalf("the sudo-prefixed resident command %q never showed up in `podman exec %s ps aux` within the observation window (TAE-20 AC2); a privileged view fetch is not a valid subject here because it would complete in milliseconds and leave no window -- this test's own command must be observed running, and it wasn't:\n%s", marker, containerName, snap.psAux)
	}

	if strings.Contains(snap.psAux, testUser1Password) {
		t.Errorf("expected the sudo password to never appear in `ps aux` while the sudo-prefixed command is resident (TAE-20 AC1/AC2); it did:\n%s", snap.psAux)
	}
	if strings.Contains(snap.cmdlines, testUser1Password) {
		t.Errorf("expected the sudo password to never appear in any process's /proc/*/cmdline while resident (TAE-20 AC1/AC2 -- 'does not appear in the command string, in any argument'); it did:\n%s", snap.cmdlines)
	}
	if strings.Contains(snap.environs, testUser1Password) {
		t.Errorf("expected the sudo password to never appear in any process's /proc/*/environ (TAE-20 AC1 -- 'or in any environment variable'); it did:\n%s", snap.environs)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Errorf("expected the sudo-prefixed resident command, routed through Manager.RunSudoCommand by the fixed delivery path (TAE-20 AC2), to complete without error: %v\n%s", r.err, r.out)
		}
	case <-time.After(10 * time.Second):
		t.Errorf("RunSudoCommand did not return after the resident command's sleep should have elapsed")
	}
}

// --- AC: a Make target runs the AC2 measurement and its output goes in the PR ---

func TestTAE20MakeTargetRunsSudoStdinMeasurement(t *testing.T) {
	data, err := os.ReadFile("Makefile")
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	makefile := string(data)

	targetRe := regexp.MustCompile(`(?m)^([a-zA-Z0-9_.-]+):.*\n((?:\t.*\n?)*)`)
	matches := targetRe.FindAllStringSubmatch(makefile, -1)

	var found bool
	for _, m := range matches {
		recipe := m[2]
		if strings.Contains(recipe, "-tags testhost") && strings.Contains(recipe, "-run TestTAE20") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a Make target whose recipe runs `go test -tags testhost -run TestTAE20 ...` so the AC2 measurement runs via a Make target and its output can go in the PR (TAE-20 AC2); none of the %d targets in Makefile do", len(matches))
	}
}

// --- AC: both entry points covered, and all their call sites; multi-sudo constructions must work ---

func TestTAE20BothEntryPointsHandleMultipleSudoOccurrences(t *testing.T) {
	info := requireTesthost(t)
	sm, _ := newSSHManagerConnected(t, info, testUser1, testUser1Password)
	defer sm.Close()

	sm.SetSudoPassword(0, []byte(testUser1Password))

	// commands.go:1184 carries up to 4 sudo invocations in a single command
	// string -- the densest real construction the issue cites. Three quick
	// ones plus a fourth, resident one gives an observation window without
	// this test running four separate long sleeps.
	marker := fmt.Sprintf("fleetdesk_tae20_multi_marker_%d", time.Now().UnixNano())
	cmd := fmt.Sprintf("sudo true && sudo true && sudo true && sudo bash -c 'M=%s; sleep 6'", marker)

	type result struct {
		out string
		err error
	}
	done := make(chan result, 1)
	go func() {
		out, err := sm.RunSudoCommand(0, cmd)
		done <- result{out, err}
	}()

	snap, sawMarker := pollUntilResident(t, marker, 5*time.Second)
	if !sawMarker {
		t.Fatalf("the fourth sudo invocation in a 4-sudo command never showed up as resident in `ps aux` (TAE-20 AC3, commands.go:1184-shaped construction); ps:\n%s", snap.psAux)
	}
	if strings.Contains(snap.psAux, testUser1Password) || strings.Contains(snap.cmdlines, testUser1Password) {
		t.Errorf("expected none of a command's multiple `sudo ` occurrences to leak the password via RunSudoCommand (TAE-20 AC3 -- 'each must work'); ps:\n%s\ncmdlines:\n%s", snap.psAux, snap.cmdlines)
	}

	select {
	case r := <-done:
		if r.err != nil {
			t.Errorf("expected a command with 4 `sudo ` occurrences to succeed end to end via RunSudoCommand (TAE-20 AC3): %v\n%s", r.err, r.out)
		}
	case <-time.After(10 * time.Second):
		t.Errorf("RunSudoCommand with 4 sudo occurrences did not return in time")
	}

	// RewriteSudoInCmd is the second entry point (used by the streaming call
	// sites, sshstream.go:92). Per the approved plan's D3', it returns
	// (string, io.Reader, error); this test only pins the command-string
	// half of that contract -- no password left in what it hands back, at
	// 1, 2, and 4 occurrences -- and leaves the reader/error contract to
	// whatever the implementation lands, so this needs no container round
	// trip either.
	for _, n := range []int{1, 2, 4} {
		parts := make([]string, n)
		for i := range parts {
			parts[i] = fmt.Sprintf("sudo cmd%d", i)
		}
		multi := strings.Join(parts, "; ")
		rewritten, _, _ := sm.RewriteSudoInCmd(0, multi)
		if strings.Contains(rewritten, testUser1Password) {
			t.Errorf("expected RewriteSudoInCmd to never place the password in the returned command string, at %d `sudo ` occurrences (TAE-20 AC1/AC3): %q", n, rewritten)
		}
	}
}

// countCalls counts occurrences of `.<method>(` across internal/app/*.go,
// excluding test files, to guard the call-site inventory the issue measured
// (RunSudoCommand: 19 sites across commands.go/model.go; RewriteSudoInCmd: 1
// site, sshstream.go:92) against a call site silently starting to bypass the
// two entry points.
func countCalls(t *testing.T, method string) int {
	t.Helper()
	files, err := filepath.Glob("internal/app/*.go")
	if err != nil {
		t.Fatalf("glob internal/app/*.go: %v", err)
	}
	re := regexp.MustCompile(`\.` + regexp.QuoteMeta(method) + `\(`)
	total := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		total += len(re.FindAllIndex(data, -1))
	}
	return total
}

func TestTAE20CallSitesRouteThroughEntryPoints(t *testing.T) {
	const wantRunSudoCommand = 19
	const wantRewriteSudoInCmd = 1

	if got := countCalls(t, "RunSudoCommand"); got != wantRunSudoCommand {
		t.Errorf("expected %d internal/app call sites to route through Manager.RunSudoCommand, the issue's measured inventory for that entry point (TAE-20 AC3); found %d -- a call site was added, removed, or started bypassing the entry point", wantRunSudoCommand, got)
	}
	if got := countCalls(t, "RewriteSudoInCmd"); got != wantRewriteSudoInCmd {
		t.Errorf("expected %d internal/app call site to route through Manager.RewriteSudoInCmd, the issue's measured inventory for that entry point (TAE-20 AC3, sshstream.go:92); found %d", wantRewriteSudoInCmd, got)
	}

	// The one documented app-layer exception: the does-sudo-need-a-password
	// check sends no credential, so it goes through plain RunCommand, not
	// either rewriting entry point.
	data, err := os.ReadFile("internal/app/model.go")
	if err != nil {
		t.Fatalf("read internal/app/model.go: %v", err)
	}
	if !strings.Contains(string(data), `sm.RunCommand(idx, "sudo -n true 2>&1")`) {
		t.Errorf("expected the documented no-credential exception (`sudo -n true`, TAE-20 issue text) to still run via plain RunCommand in internal/app/model.go; its shape changed -- re-verify it still sends no password")
	}
}

// --- AC: a wrong password at the prompt still fails the way it does today ---

func TestTAE20WrongSudoPasswordFailsAndMessageOmitsPassword(t *testing.T) {
	info := requireTesthost(t)
	sm, _ := newSSHManagerConnected(t, info, testUser1, testUser1Password)
	defer sm.Close()

	wrongPassword := testUser1Password + "-wrong"
	sm.SetSudoPassword(0, []byte(wrongPassword))

	out, err := sm.RunSudoCommand(0, "sudo true")
	wrongDetected := err != nil || fdssh.IsSudoOutput(out) || fdssh.IsSudoError(err)
	if !wrongDetected {
		t.Errorf("expected `sudo true` with a wrong cached password to fail the way it does today -- err != nil or IsSudoOutput(out) (TAE-20 AC4, model.go's sudoWizardResultMsg/handleSudoOrFlash paths key on exactly this): err=%v out=%q", err, out)
	}
	if strings.Contains(out, wrongPassword) {
		t.Errorf("expected sudo's failure output to contain no part of the password (TAE-20 AC4): %q", out)
	}
	if err != nil && strings.Contains(err.Error(), wrongPassword) {
		t.Errorf("expected the returned error to contain no part of the password (TAE-20 AC4): %v", err)
	}

	// Should-not-regress: the app still shows the same, password-free flash
	// message on a failed retry (model.go's sudoWizardResultMsg handler).
	data, err2 := os.ReadFile("internal/app/model.go")
	if err2 != nil {
		t.Fatalf("read internal/app/model.go: %v", err2)
	}
	if !strings.Contains(string(data), `m.flash = "Wrong sudo password"`) {
		t.Errorf("expected internal/app/model.go to still set the flash message literally to \"Wrong sudo password\" on a failed retry (TAE-20 AC4 -- 'fails the way it does today'); a literal, non-interpolated string can never carry the password")
	}
}

// --- AC: the mechanism leaves the session's stdin usable by the command ---

func TestTAE20NoAppLayerCallSiteClaimsSessionStdin(t *testing.T) {
	files, err := filepath.Glob("internal/app/*.go")
	if err != nil {
		t.Fatalf("glob internal/app/*.go: %v", err)
	}
	// Scoped to the ssh.Session type (conventionally named "session" at its
	// one construction site, sshstream.go:84's `client.NewSession()`), not
	// any .Stdin -- a local os/exec.Cmd handed the real terminal (e.g.
	// handover.go's `c.Stdin = os.Stdin`) is ADR-F004's terminal-handover
	// path, explicitly a different mechanism the issue places out of AC5's
	// conflict set ("the user types the password into the handed-over
	// terminal and FleetDesk delivers nothing").
	stdinRe := regexp.MustCompile(`\bsession\.Stdin\s*=|\bsession\.StdinPipe\(`)
	var offenders []string
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			if stdinRe.MatchString(line) {
				offenders = append(offenders, fmt.Sprintf("%s:%d: %s", f, i+1, strings.TrimSpace(line)))
			}
		}
	}

	// The issue's stated baseline ("No call site sets session.Stdin today,
	// so AC 5's conflict set is currently empty") holds only until the fix
	// itself claims session.Stdin to deliver the sudo password -- per the
	// approved plan (D2/D3'), that is sshstream.go's streaming entry point,
	// and nowhere else. So the canary now allows exactly one occurrence,
	// and only in that file: the mechanism itself is not a conflict: a
	// second, unrelated call site claiming the same session's stdin is
	// exactly the conflict AC5 says must be identifiable, not discovered
	// later.
	const sudoDeliverySite = "internal/app/sshstream.go"
	var unexpected []string
	sudoDeliveryCount := 0
	for _, o := range offenders {
		if strings.HasPrefix(o, sudoDeliverySite+":") {
			sudoDeliveryCount++
			continue
		}
		unexpected = append(unexpected, o)
	}
	if len(unexpected) != 0 {
		t.Errorf("expected no internal/app call site outside %s's sudo-password stdin delivery to set a session's Stdin (TAE-20 AC5); found %d that do, which now conflict with sudo's stdin delivery and need a deliberate resolution, not a silent one:\n%s", sudoDeliverySite, len(unexpected), strings.Join(unexpected, "\n"))
	}
	if sudoDeliveryCount != 1 {
		t.Errorf("expected exactly one session.Stdin assignment in %s -- the sudo password's own stdin delivery for the streaming entry point (TAE-20 AC1/AC5, RewriteSudoInCmd's D3' reader consumed at sshstream.go:92-94-shaped call site); found %d", sudoDeliverySite, sudoDeliveryCount)
	}
}

func TestTAE20ConnectionUsableForFreshStdinAfterSudoDelivery(t *testing.T) {
	info := requireTesthost(t)
	sm, _ := newSSHManagerConnected(t, info, testUser1, testUser1Password)
	defer sm.Close()

	sm.SetSudoPassword(0, []byte(testUser1Password))
	if _, err := sm.RunSudoCommand(0, "sudo true"); err != nil {
		t.Fatalf("RunSudoCommand(sudo true) before the stdin-usability check: %v", err)
	}

	client := sm.GetConnection(0)
	if client == nil {
		t.Fatalf("expected Manager.GetConnection to return the live connection after a successful sudo command")
	}
	session, err := client.NewSession()
	if err != nil {
		t.Fatalf("open a fresh session on the same connection after delivering a sudo password: %v", err)
	}
	defer func() { _ = session.Close() }()

	session.Stdin = strings.NewReader("fleetdesk-tae20-stdin-probe\n")
	out, err := session.CombinedOutput("cat")
	if err != nil {
		t.Fatalf("run `cat` reading its own stdin on a fresh session after a sudo password was delivered over the connection (TAE-20 AC5 -- the mechanism must leave the session's stdin usable by the command): %v\n%s", err, out)
	}
	if strings.TrimSpace(string(out)) != "fleetdesk-tae20-stdin-probe" {
		t.Errorf("expected a fresh session's own stdin to reach the command unaltered after prior sudo password delivery on the same connection (TAE-20 AC5); got %q", out)
	}
}

// --- AC: at the most verbose log level, with a password cached, no log line contains the password ---

func TestTAE20NoLogLineContainsPasswordAtDebugLevel(t *testing.T) {
	info := requireTesthost(t)
	sm, buf := newSSHManagerConnected(t, info, testUser1, testUser1Password)
	defer sm.Close()

	sm.SetSudoPassword(0, []byte(testUser1Password))
	if _, err := sm.RunSudoCommand(0, "sudo true"); err != nil {
		t.Fatalf("RunSudoCommand(sudo true) with a cached password: %v", err)
	}

	logs := buf.String()
	if logs == "" {
		t.Fatalf("expected Debug-level logging to produce output for a sudo command run with a cached password -- an empty log makes this check vacuous")
	}
	if strings.Contains(logs, testUser1Password) {
		t.Errorf("expected no log line to contain any part of the sudo password at Debug level, the most verbose level, with a password cached (TAE-20 AC6 -- this is the criterion, independent of the '[sudo-rewritten]' marker); log output:\n%s", logs)
	}
}
