package main_test

// Acceptance tests for TAE-21: Credential lifecycle -- []byte storage,
// erasure, and idle timeout. Written from the issue's acceptance criteria
// only -- no plan comment, no decision, no review, and no implementation
// were read to produce these.
//
// This file holds the criteria that are checkable today, against the
// current (string-based) exported API, with a real runtime failure -- not
// a compile failure. Everything that requires the []byte signatures the
// issue mandates (Set... methods taking ownership of the caller's slice,
// per-operation and idle-timeout erasure proven by reading an alias back)
// cannot compile against today's string-typed ssh.Manager and lives in
// credential_lifecycle_testhost_test.go instead, gated behind the
// `testhost` build tag alongside the TAE-20 fixture it also updates -- see
// that file's header for why a compile failure is the correct evidence
// there.
//
// Run:
//
//	go test -run TestTAE21 -v .
import (
	"os"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/app"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// --- AC (Honesty): three existing claims are false and must be corrected ---

func TestTAE21FalseZeroingCommentsAreCorrected(t *testing.T) {
	data, err := os.ReadFile("internal/ssh/manager.go")
	if err != nil {
		t.Fatalf("read internal/ssh/manager.go: %v", err)
	}
	src := string(data)

	// The issue names these three verbatim as false -- none of them
	// overwrites a byte today (TAE-21 Honesty AC). It does not ask for
	// specific replacement wording, only that the false claim is gone.
	falseClaims := []string{
		"zeroes out the cached password", // manager.go:329, ClearPassword
		"cleared on Close",               // manager.go:86, sudoPasswords field comment
		"clears all cached credentials",  // manager.go:436, Close doc comment
	}
	for _, claim := range falseClaims {
		if strings.Contains(src, claim) {
			t.Errorf("expected internal/ssh/manager.go to no longer claim %q -- the issue names this comment as false today (nothing overwrites a byte) and requires it corrected (TAE-21 Honesty AC)", claim)
		}
	}
}

// --- AC (Idle timeout): credential_timeout is a Go duration string; an
// unparseable value fails at startup, as a malformed fleet file does ---

func TestTAE21UnparseableCredentialTimeoutFailsAtStartup(t *testing.T) {
	dir := t.TempDir()
	yaml := "fleet_dir: " + dir + "\ncredential_timeout: 15min\n" // not a valid time.ParseDuration string
	if err := os.WriteFile(dir+"/config.yaml", []byte(yaml), 0o600); err != nil {
		t.Fatalf("write config.yaml: %v", err)
	}

	_, err := config.LoadAppConfig(dir)
	if err == nil {
		t.Errorf(`expected LoadAppConfig to fail startup on an unparseable credential_timeout ("15min", TAE-21 Idle timeout AC -- "defaulting silently would leave a user who wrote 15min believing they had set five minutes when they had fifteen"); it succeeded instead, which means credential_timeout is not yet parsed/validated at all`)
	}
}

// --- AC (Idle timeout): a Make target runs the measurement this issue
// requires, so its output can go in the PR, mirroring TAE-20's own AC2 ---

func TestTAE21MakeTargetRunsIdleTimeoutMeasurement(t *testing.T) {
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
		if strings.Contains(recipe, "-tags testhost") && strings.Contains(recipe, "-run TestTAE21IdleTimeout") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected a Make target whose recipe runs `go test -tags testhost -run TestTAE21IdleTimeout ...` -- TestTAE21IdleTimeoutErasesCredentialAtDeadline in credential_lifecycle_testhost_test.go is the measurement this issue requires (TAE-21 Idle timeout AC, \"verified by measurement\"); none of the %d targets in Makefile do", len(matches))
	}
}

// --- AC (Storage): a credential longer than the maximum is refused at
// entry, not truncated -- Bubble Tea delivers a bracketed paste as a
// single KeyRunes message, so one message can carry more than the cap ---

const maxCredentialLength = 1024 // the issue's stated maximum, named as such

func hugePaste(n int) tea.KeyMsg {
	runes := make([]rune, n)
	for i := range runes {
		runes[i] = 'x'
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: runes}
}

func testMaximumLengthPasteAccepted(t *testing.T, name string, modal *app.ModalOverlay) {
	t.Helper()

	// One paste at exactly the maximum must still be accepted -- this is
	// refusal of what's over the cap, not a cap so low real passphrases
	// stop working.
	modal.HandleKey(hugePaste(maxCredentialLength))
	modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !modal.Done() {
		t.Errorf("%s: expected a %d-rune paste (exactly the stated maximum) to be accepted, not refused (TAE-21 Storage AC): modal did not complete; view:\n%s", name, maxCredentialLength, modal.View("", 100, 30))
	}
}

func testOversizedPasteRejected(t *testing.T, name string, modal *app.ModalOverlay) {
	t.Helper()

	modal.HandleKey(hugePaste(maxCredentialLength + 1))
	modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if modal.Done() {
		t.Errorf("%s: expected a %d-rune paste (one over the stated 1024-byte maximum) to be refused at entry, not silently accepted (TAE-21 Storage AC -- \"refused at entry, not truncated\"); the modal completed as if the paste were valid", name, maxCredentialLength+1)
	}
}

func TestTAE21OversizedPasteRefusedAtEntrySSHPasswordPrompt(t *testing.T) {
	testOversizedPasteRejected(t, "SSH password modal", app.NewPasswordModal("alice", "host1", 0))
}

func TestTAE21OversizedPasteRefusedAtEntrySudoPrompt(t *testing.T) {
	testOversizedPasteRejected(t, "sudo password modal", app.NewSudoModal("alice", "host1", 0, nil))
}

func TestTAE21MaximumLengthPasteStillAcceptedSSHPasswordPrompt(t *testing.T) {
	testMaximumLengthPasteAccepted(t, "SSH password modal", app.NewPasswordModal("alice", "host1", 0))
}

func TestTAE21MaximumLengthPasteStillAcceptedSudoPrompt(t *testing.T) {
	testMaximumLengthPasteAccepted(t, "sudo password modal", app.NewSudoModal("alice", "host1", 0, nil))
}
