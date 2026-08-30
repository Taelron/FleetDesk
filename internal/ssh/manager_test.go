package ssh

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// readByte is deliberately non-inlinable, via an index, so a test holding
// an alias to a buffer cannot have its post-erase check folded away by
// the compiler treating the buffer as dead after the call that erased it.
//
//go:noinline
func readByte(b []byte, i int) byte { return b[i] }

func allZero(t *testing.T, b []byte) bool {
	t.Helper()
	for i := range b {
		if readByte(b, i) != 0 {
			return false
		}
	}
	return true
}

func TestSameBackingIdentifiesSharedArray(t *testing.T) {
	buf := make([]byte, 4, 8)
	clone := make([]byte, 4, 8)
	copy(clone, buf)

	if !sameBacking(buf, buf) {
		t.Error("sameBacking(buf, buf) = false, want true")
	}
	if sameBacking(buf, clone) {
		t.Error("sameBacking(buf, an independent copy) = true, want false -- distinct backing arrays with equal contents must not compare equal")
	}
	if sameBacking(buf[:0], buf) {
		t.Error("sameBacking(buf[:0], buf) = true, want false -- an empty slice never aliases anything, even one sliced from the same array")
	}
	if sameBacking(nil, nil) {
		t.Error("sameBacking(nil, nil) = true, want false")
	}
}

func TestSudoPasswordCache(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	// Initially empty
	if sm.GetSudoPassword(0) {
		t.Error("GetSudoPassword(0) = true, want false")
	}

	// Set and retrieve presence
	sm.SetSudoPassword(0, []byte("secret"))
	if !sm.GetSudoPassword(0) {
		t.Error("GetSudoPassword(0) = false after Set, want true")
	}

	// Per-host isolation
	if sm.GetSudoPassword(1) {
		t.Error("GetSudoPassword(1) = true, want false (different host)")
	}
	sm.SetSudoPassword(1, []byte("other"))
	if !sm.GetSudoPassword(0) {
		t.Error("GetSudoPassword(0) = false after setting host 1, want true")
	}

	// Clear
	sm.ClearSudoPassword(0)
	if sm.GetSudoPassword(0) {
		t.Error("GetSudoPassword(0) = true after ClearSudoPassword, want false")
	}

	// Close clears all
	sm.SetSudoPassword(1, []byte("stillset"))
	sm.Close()
	if sm.GetSudoPassword(1) {
		t.Error("GetSudoPassword(1) = true after Close(), want false")
	}
}

func TestSetSudoPasswordErasesPreviousBufferBeforeStoringNext(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	first := []byte("wrong-sudo-password")
	sm.SetSudoPassword(0, first)
	second := []byte("second-attempt-password")
	sm.SetSudoPassword(0, second)

	if !allZero(t, first) {
		t.Errorf("expected the first sudo password buffer to be erased once a second is stored for the same host; alias still reads %q", first)
	}
}

func TestSudoPasswordSharingDoesNotAllocateAndReleaseHonorsRefcount(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	shared := []byte("shared-sudo-password")
	sm.SetSudoPassword(0, shared)
	if !sm.ShareSudoPassword(0, 1) {
		t.Fatalf("ShareSudoPassword(0, 1) = false, want true")
	}

	// Sharing does not allocate — both host indices reference the same
	// backing array. Clearing one must not erase what the other still
	// references.
	sm.ClearSudoPassword(0)
	if allZero(t, shared) {
		t.Errorf("expected clearing host 0 not to erase a value still shared by host 1; alias is zero: %q", shared)
	}
	if !sm.GetSudoPassword(1) {
		t.Error("expected host 1 to still have its sudo password after host 0 released the shared entry")
	}

	// Clearing the last reference does erase.
	sm.ClearSudoPassword(1)
	if !allZero(t, shared) {
		t.Errorf("expected clearing the last reference to erase the shared buffer; alias still reads %q", shared)
	}
}

func TestSudoPasswordNewCandidateOnSharingHostLeavesOtherIntact(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	shared := []byte("shared-sudo-password")
	sm.SetSudoPassword(0, shared)
	sm.ShareSudoPassword(0, 1)

	// Host 0 gets a new candidate — host 1 must keep the original value.
	sm.SetSudoPassword(0, []byte("new-candidate"))
	if allZero(t, shared) {
		t.Errorf("expected host 1's shared value to survive host 0 storing a new candidate; alias is zero: %q", shared)
	}
	if !sm.GetSudoPassword(1) {
		t.Error("expected host 1 to still have a sudo password")
	}
}

func TestEraseCredentialsZeroesCachedAndSudoPasswords(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	sshPw := []byte("session-ssh-password")
	sm.SetCachedPassword(sshPw)
	sudoPw := []byte("session-sudo-password")
	sm.SetSudoPassword(0, sudoPw)

	sm.EraseCredentials()

	if !allZero(t, sshPw) {
		t.Errorf("expected EraseCredentials to erase the cached SSH password; alias still reads %q", sshPw)
	}
	if !allZero(t, sudoPw) {
		t.Errorf("expected EraseCredentials to erase the cached sudo password; alias still reads %q", sudoPw)
	}
	if sm.HasCachedPassword() {
		t.Error("expected HasCachedPassword() = false after EraseCredentials")
	}
	if sm.GetSudoPassword(0) {
		t.Error("expected GetSudoPassword(0) = false after EraseCredentials")
	}
}

func TestSeedSudoFromCachedPasswordCopiesAndRefusesUndeliverable(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	if sm.SeedSudoFromCachedPassword(0) {
		t.Error("expected SeedSudoFromCachedPassword to refuse when nothing is cached")
	}

	cached := []byte("ssh-password")
	sm.SetCachedPassword(cached)
	if !sm.SeedSudoFromCachedPassword(1) {
		t.Fatal("expected SeedSudoFromCachedPassword(1) to succeed with a deliverable cached password")
	}
	if !sm.GetSudoPassword(1) {
		t.Error("expected host 1 to have a sudo password after seeding")
	}
	// Independent copy: erasing the cached SSH password must not affect it.
	sm.ClearPassword()
	if sm.GetSudoPassword(1) != true {
		t.Error("expected the seeded sudo password to survive clearing the cached SSH password (independent copy)")
	}

	sm.SetCachedPassword([]byte("bad\npassword"))
	if sm.SeedSudoFromCachedPassword(2) {
		t.Error("expected SeedSudoFromCachedPassword to refuse an SSH password sudo -S cannot accept")
	}
	if sm.GetSudoPassword(2) {
		t.Error("expected nothing stored for host 2 after a refused seed")
	}
}

func TestIdleTimeoutErasesSudoPasswordAtDeadline(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	sm.SetCredentialTimeout(50 * time.Millisecond)
	buf := []byte("idle-timeout-test-password")
	sm.SetSudoPassword(0, buf)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if !sm.GetSudoPassword(0) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if sm.GetSudoPassword(0) {
		t.Fatal("expected the sudo password to expire within 2s of a 50ms idle timeout")
	}
	if !allZero(t, buf) {
		t.Errorf("expected the expired sudo password's buffer to be zero; alias still reads %q", buf)
	}
}

func TestIdleTimeoutTouchWithinWindowDefersExpiry(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	sm.SetCredentialTimeout(150 * time.Millisecond)
	sm.SetSudoPassword(0, []byte("touched-password"))

	// Touch repeatedly within the window via RunSudoCommand (no live
	// connection, so it errors, but touch happens before the run attempt).
	until := time.Now().Add(400 * time.Millisecond)
	for time.Now().Before(until) {
		_, _ = sm.RunSudoCommand(0, "sudo true")
		time.Sleep(50 * time.Millisecond)
	}
	if !sm.GetSudoPassword(0) {
		t.Error("expected a repeatedly-touched credential not to expire while still in use")
	}
}

func TestIdleTimeoutZeroNeverExpires(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	sm.SetSudoPassword(0, []byte("never-expires"))
	time.Sleep(50 * time.Millisecond)
	if !sm.GetSudoPassword(0) {
		t.Error("expected a credential to never expire when SetCredentialTimeout was never called (0 = disabled)")
	}
}

// TestRewriteSudoCmd checks the shape every rewritten command must have:
// exactly one preamble, one relay per "sudo " occurrence, and — since the
// function takes no password at all — no way for a password to appear in
// the result.
func TestRewriteSudoCmd(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{
			name: "no sudo in command is byte-identical",
			cmd:  "systemctl status",
			want: "systemctl status",
		},
		{
			name: "single sudo",
			cmd:  "sudo journalctl -u sshd",
			want: sudoPreamble + `printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null journalctl -u sshd`,
		},
		{
			name: "multiple sudo",
			cmd:  "sudo systemctl status && sudo journalctl",
			want: sudoPreamble + `printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null systemctl status && printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null journalctl`,
		},
		{
			name: "inside a command substitution",
			cmd:  "count=$(sudo wc -l /etc/shadow)",
			want: sudoPreamble + `count=$(printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null wc -l /etc/shadow)`,
		},
		{
			name: "inside a backgrounded subshell",
			cmd:  "(sudo nft list ruleset) &",
			want: sudoPreamble + `(printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null nft list ruleset) &`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rewriteSudoCmd(tt.cmd)
			if got != tt.want {
				t.Errorf("rewriteSudoCmd(%q)\n  got  %q\n  want %q", tt.cmd, got, tt.want)
			}
			if !strings.Contains(tt.cmd, "sudo ") {
				return
			}
			if strings.Count(got, sudoPreamble) != 1 {
				t.Errorf("rewriteSudoCmd(%q) = %q: expected the preamble exactly once", tt.cmd, got)
			}
			if !strings.Contains(got, "| sudo -S") {
				t.Errorf("rewriteSudoCmd(%q) = %q: expected the `[sudo-rewritten]` masking marker `| sudo -S` to still be present", tt.cmd, got)
			}
		})
	}
}

// TestSudoReaderStdinContract pins D2: the sudo password relay's entire
// stdin contract is one line, then EOF.
func TestSudoReaderStdinContract(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	sm.SetSudoPassword(0, []byte("hunter2"))
	_, r, err := sm.RewriteSudoInCmd(0, "sudo true")
	if err != nil {
		t.Fatalf("RewriteSudoInCmd: %v", err)
	}
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read sudo reader: %v", err)
	}
	if string(data) != "hunter2\n" {
		t.Errorf("sudo reader content = %q, want %q", string(data), "hunter2\n")
	}
	n, err := r.Read(make([]byte, 1))
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("sudo reader after its one line: n=%d err=%v, want n=0 err=io.EOF", n, err)
	}
}

func TestValidateSudoPassword(t *testing.T) {
	rejected := []string{"has\nnewline", "has\rcarriage", "has\x00nul"}
	for _, pw := range rejected {
		if err := ValidateSudoPassword([]byte(pw)); !errors.Is(err, ErrSudoDelivery) {
			t.Errorf("ValidateSudoPassword(%q) = %v, want an error wrapping ErrSudoDelivery", pw, err)
		}
	}

	accepted := []string{"", "simple", "with spaces", `with "quotes"`, `with\backslashes`, "-leading-dash"}
	for _, pw := range accepted {
		if err := ValidateSudoPassword([]byte(pw)); err != nil {
			t.Errorf("ValidateSudoPassword(%q) = %v, want nil", pw, err)
		}
	}
}

func TestRewriteSudoInCmd(t *testing.T) {
	sm := NewManager(slog.Default())
	defer sm.Close()

	if cmd, r, err := sm.RewriteSudoInCmd(0, "sudo true"); cmd != "sudo true" || r != nil || err != nil {
		t.Errorf("RewriteSudoInCmd with no cached password = (%q, %v, %v), want (\"sudo true\", nil, nil)", cmd, r, err)
	}

	sm.SetSudoPassword(0, []byte("secret"))
	if cmd, r, err := sm.RewriteSudoInCmd(0, "systemctl status"); cmd != "systemctl status" || r != nil || err != nil {
		t.Errorf("RewriteSudoInCmd with no \"sudo \" in cmd = (%q, %v, %v), want (\"systemctl status\", nil, nil)", cmd, r, err)
	}

	sm.SetSudoPassword(0, []byte("bad\npassword"))
	if cmd, r, err := sm.RewriteSudoInCmd(0, "sudo true"); cmd != "" || r != nil || !errors.Is(err, ErrSudoDelivery) {
		t.Errorf("RewriteSudoInCmd with an undeliverable password = (%q, %v, %v), want (\"\", nil, ErrSudoDelivery)", cmd, r, err)
	}

	sm.SetSudoPassword(0, []byte("secret"))
	cmd, r, err := sm.RewriteSudoInCmd(0, "sudo true")
	if err != nil {
		t.Fatalf("RewriteSudoInCmd(0, \"sudo true\") with a valid cached password: %v", err)
	}
	if cmd == "sudo true" || !strings.Contains(cmd, "| sudo -S") {
		t.Errorf("RewriteSudoInCmd returned command %q, want it rewritten", cmd)
	}
	if r == nil {
		t.Errorf("RewriteSudoInCmd returned a nil reader for a rewritten command")
	}
}

func TestSudoDeliveryError(t *testing.T) {
	if err := SudoDeliveryError(97); !errors.Is(err, ErrSudoDelivery) {
		t.Errorf("SudoDeliveryError(97) = %v, want an error wrapping ErrSudoDelivery", err)
	}
	if err := SudoDeliveryError(96); !errors.Is(err, ErrSudoDelivery) {
		t.Errorf("SudoDeliveryError(96) = %v, want an error wrapping ErrSudoDelivery", err)
	}
	if err := SudoDeliveryError(1); err != nil {
		t.Errorf("SudoDeliveryError(1) = %v, want nil", err)
	}
	if err := SudoDeliveryError(0); err != nil {
		t.Errorf("SudoDeliveryError(0) = %v, want nil", err)
	}
}
