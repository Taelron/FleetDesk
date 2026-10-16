package ssh

import (
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

func TestParseProbeOutput_ErrorCountKnown(t *testing.T) {
	base := "---PROBE---\nhost.example\n2026-01-01 00:00:00\nRHEL 9\n3\n%s\n4\n0\n7\n2\n2\n5\n11\n0\n"
	cases := []struct {
		line      string
		wantCount int
		wantKnown bool
	}{
		{"0", 0, true}, // a genuine zero is known
		{"42", 42, true},
		{"-", 0, false}, // the read could not run
		{"", 0, false},
		{"sudo: a password is required", 0, false},
	}
	for _, c := range cases {
		info, err := ParseProbeOutput(strings.Replace(base, "%s", c.line, 1), "system")
		if err != nil {
			t.Fatalf("%q: %v", c.line, err)
		}
		if info.ErrorCount != c.wantCount || info.ErrorCountKnown != c.wantKnown {
			t.Errorf("line %q: count %d known %v, want %d %v", c.line, info.ErrorCount, info.ErrorCountKnown, c.wantCount, c.wantKnown)
		}
		if info.DiskCount != 4 {
			t.Errorf("line %q: the lines after it must still parse in place, DiskCount = %d", c.line, info.DiskCount)
		}
	}
	// the short fallback output never reached the read
	info, err := ParseProbeOutput("---PROBE---\nh\nup\nos\n", "user")
	if err != nil || info.ErrorCountKnown {
		t.Errorf("fallback output: %+v %v, want ErrorCountKnown false", info, err)
	}
}

func TestProbeCommand_Forms(t *testing.T) {
	unverified := probeCommand("sudo -n ", "1 hour ago")
	if !strings.Contains(unverified, "$(sudo -n journalctl -p err --since '1 hour ago'") {
		t.Errorf("unverified form must use sudo -n:\n%s", unverified)
	}
	if !strings.Contains(unverified, "else echo -; fi") {
		t.Errorf("a failed read must yield '-':\n%s", unverified)
	}
	rewritten := rewriteSudoCmd(probeCommand("sudo ", "1 hour ago"))
	if !strings.HasPrefix(rewritten, sudoPreamble) {
		t.Errorf("verified form must start with the relay preamble:\n%s", rewritten)
	}
	if strings.Count(rewritten, sudoPreamble) != 1 {
		t.Errorf("exactly one preamble, got %d", strings.Count(rewritten, sudoPreamble))
	}
	if !strings.Contains(rewritten, `printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null journalctl`) {
		t.Errorf("verified form must deliver the password through the relay:\n%s", rewritten)
	}
	if strings.Contains(rewritten, "sudo -n") || strings.Contains(rewritten, "sudo journalctl") {
		t.Errorf("no bare sudo may remain:\n%s", rewritten)
	}
}

// fakeSudoDir writes stub sudo and journalctl executables. The sudo stub
// accepts -n (fails unless NOPASSWD=1) and -S (reads one password line from
// stdin and runs the command only if it equals "right").
func fakeSudoDir(t *testing.T, journal string) string {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/bash\n"+body), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	write("sudo", `
case "$1" in
-n) [ "$NOPASSWD" = 1 ] || { echo "sudo: a password is required" >&2; exit 1; }; shift ;;
-S) read -r pw; [ "$pw" = right ] || { echo "sudo: incorrect password" >&2; exit 1; }; shift ;;
esac
exec "$@"
`)
	write("journalctl", "printf '%s' "+shellSingle(journal)+"\n")
	write("dnf", "exit 0\n") // the real one takes tens of seconds
	return dir
}

func shellSingle(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

func runLocal(t *testing.T, dir, cmd, stdin string, env ...string) []string {
	t.Helper()
	c := exec.Command("bash", "-c", cmd) //nolint:gosec // G204: the test runs the probe command against stub binaries
	c.Env = append([]string{"PATH=" + dir + ":" + os.Getenv("PATH"), "HOME=" + t.TempDir()}, env...)
	c.Stdin = strings.NewReader(stdin)
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("running the probe: %v\n%s", err, out)
	}
	s := string(out)
	_, after, _ := strings.Cut(s, "---PROBE---\n")
	return strings.Split(strings.TrimRight(after, "\n"), "\n")
}

// Line four of the real probe command, executed.
func TestProbeCommand_LineFour(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the probe command targets Linux hosts")
	}
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not available")
	}
	three := "one\ntwo\nthree\n"
	cases := []struct {
		name    string
		journal string
		cmd     string
		stdin   string
		env     []string
		want    string
	}{
		{"unverified, password required: could not read", three, probeCommand("sudo -n ", "1 hour ago"), "", nil, "-"},
		{"unverified, NOPASSWD, three errors", three, probeCommand("sudo -n ", "1 hour ago"), "", []string{"NOPASSWD=1"}, "3"},
		{"unverified, NOPASSWD, empty journal: a real zero", "", probeCommand("sudo -n ", "1 hour ago"), "", []string{"NOPASSWD=1"}, "0"},
		{"verified, right password", three, rewriteSudoCmd(probeCommand("sudo ", "1 hour ago")), "right\n", nil, "3"},
		{"verified, right password, empty journal", "", rewriteSudoCmd(probeCommand("sudo ", "1 hour ago")), "right\n", nil, "0"},
		{"verified, stale password: could not read", three, rewriteSudoCmd(probeCommand("sudo ", "1 hour ago")), "wrong\n", nil, "-"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			lines := runLocal(t, fakeSudoDir(t, c.journal), c.cmd, c.stdin, c.env...)
			if len(lines) < 13 {
				t.Fatalf("a failed or empty read must not cut the rest of the probe short: %d lines:\n%s", len(lines), strings.Join(lines, "\n"))
			}
			if got := strings.TrimSpace(lines[4]); got != c.want {
				t.Errorf("line four = %q, want %q", got, c.want)
			}
			if strings.Contains(strings.Join(lines, "\n"), "right") {
				t.Error("the password must not appear in the output")
			}
		})
	}
}

func TestVerifiedSudoTracking(t *testing.T) {
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	set := func(idx int, pw string) { sm.SetSudoPassword(idx, mustClone(t, pw)) }

	set(0, "pw0")
	if sm.HasVerifiedSudo(0) {
		t.Error("a password just stored is a candidate, not verified")
	}
	sm.MarkSudoVerified(0)
	if !sm.HasVerifiedSudo(0) {
		t.Error("MarkSudoVerified then HasVerifiedSudo")
	}
	sm.MarkSudoVerified(5) // holds nothing: no-op
	if sm.HasVerifiedSudo(5) {
		t.Error("a host with no password cannot be verified")
	}

	// newest wins, the caller is excluded
	set(1, "pw1")
	sm.MarkSudoVerified(1)
	set(2, "pw2")
	sm.MarkSudoVerified(2)
	if got, ok := sm.MostRecentVerifiedSudo(3); !ok || got != 2 {
		t.Errorf("newest = %d %v, want 2", got, ok)
	}
	if got, ok := sm.MostRecentVerifiedSudo(2); !ok || got != 1 {
		t.Errorf("newest excluding the caller = %d %v, want 1", got, ok)
	}
	sm.MarkSudoVerified(0) // re-verifying makes it the newest
	if got, _ := sm.MostRecentVerifiedSudo(3); got != 0 {
		t.Errorf("after re-verifying 0: newest = %d, want 0", got)
	}
	if _, ok := sm.MostRecentVerifiedSudo(0); !ok {
		t.Error("others remain")
	}

	// sharing makes the target an untested holder
	if !sm.ShareSudoPassword(0, 4) {
		t.Fatal("share")
	}
	if sm.HasVerifiedSudo(4) {
		t.Error("a shared password is not verified on the host it was shared to")
	}
	if !sm.HasVerifiedSudo(0) {
		t.Error("sharing leaves the source verified")
	}
	// re-sharing over a verified target clears its mark
	sm.MarkSudoVerified(4)
	sm.ShareSudoPassword(1, 4)
	if sm.HasVerifiedSudo(4) {
		t.Error("ShareSudoPassword must clear the verified mark of its target")
	}

	// a failure on one host leaves the other's alone
	sm.ClearSudoPassword(4)
	if !sm.HasVerifiedSudo(1) || !sm.GetSudoPassword(1) {
		t.Error("clearing the target must not touch the source")
	}
	sm.ClearSudoPassword(1)
	if sm.HasVerifiedSudo(1) {
		t.Error("clearing drops the mark")
	}
	// a replaced candidate is unverified again
	sm.SetSudoPassword(2, mustClone(t, "new"))
	if sm.HasVerifiedSudo(2) {
		t.Error("SetSudoPassword stores a new candidate")
	}
	// erasing everything leaves nothing to offer
	sm.MarkSudoVerified(2)
	sm.EraseCredentials()
	if _, ok := sm.MostRecentVerifiedSudo(9); ok || sm.HasVerifiedSudo(0) {
		t.Error("erasure drops every mark")
	}
}

func mustClone(t *testing.T, s string) []byte {
	t.Helper()
	b, err := credential.Clone([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// A host that rejected a verification of another host's password is not
// offered that same verification again; a fresh verification is offered once more.
func TestSudoCandidateRejectionMemory(t *testing.T) {
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	sm.SetSudoPassword(0, mustClone(t, "pw0"))
	sm.MarkSudoVerified(0)
	if from, ok := sm.SudoCandidateFor(1); !ok || from != 0 {
		t.Fatalf("candidate = %d %v, want host 0", from, ok)
	}
	sm.RecordSudoRejected(1, 0)
	if _, ok := sm.SudoCandidateFor(1); ok {
		t.Error("host 1 already rejected this verification of host 0's password")
	}
	if from, ok := sm.SudoCandidateFor(2); !ok || from != 0 {
		t.Error("another host is still offered it")
	}
	sm.MarkSudoVerified(0) // the same password verified again: still the one 1 rejected
	if _, ok := sm.SudoCandidateFor(1); ok {
		t.Error("the same password is not offered again")
	}
	// a password typed afresh is a new password
	sm.SetSudoPassword(0, mustClone(t, "typed-afresh"))
	sm.MarkSudoVerified(0)
	if from, ok := sm.SudoCandidateFor(1); !ok || from != 0 {
		t.Errorf("a new password may be offered: %d %v", from, ok)
	}
	sm.RecordSudoRejected(1, 5) // nothing held on 5: no-op, no panic
	sm.EraseCredentials()
	if _, ok := sm.SudoCandidateFor(1); ok {
		t.Error("erasure leaves nothing to offer")
	}
}

// Hosts that reused a password share it; one rejection covers every route to it.
func TestSudoRejectionFollowsThePasswordNotTheHost(t *testing.T) {
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	sm.SetSudoPassword(0, mustClone(t, "pw"))
	sm.MarkSudoVerified(0)
	sm.ShareSudoPassword(0, 1) // B reused A's password...
	sm.MarkSudoVerified(1)     // ...and it worked there (newest verification)
	if from, _ := sm.SudoCandidateFor(2); from != 1 {
		t.Fatalf("the newest verification is offered first: %d", from)
	}
	sm.RecordSudoRejected(2, 1) // C rejects it
	if from, ok := sm.SudoCandidateFor(2); ok {
		t.Errorf("C rejected this password; host %d holds the same one and must not be offered either", from)
	}
}
