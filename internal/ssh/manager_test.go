package ssh

import (
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
)

func TestSudoPasswordCache(t *testing.T) {
	sm := NewManager(slog.Default())

	// Initially empty
	if got := sm.GetSudoPassword(0); got != "" {
		t.Errorf("GetSudoPassword(0) = %q, want empty", got)
	}

	// Set and retrieve
	sm.SetSudoPassword(0, "secret")
	if got := sm.GetSudoPassword(0); got != "secret" {
		t.Errorf("GetSudoPassword(0) = %q, want %q", got, "secret")
	}

	// Per-host isolation
	if got := sm.GetSudoPassword(1); got != "" {
		t.Errorf("GetSudoPassword(1) = %q, want empty (different host)", got)
	}
	sm.SetSudoPassword(1, "other")
	if got := sm.GetSudoPassword(0); got != "secret" {
		t.Errorf("GetSudoPassword(0) = %q, want %q after setting host 1", got, "secret")
	}

	// Clear with empty string
	sm.SetSudoPassword(0, "")
	if got := sm.GetSudoPassword(0); got != "" {
		t.Errorf("GetSudoPassword(0) = %q, want empty after clear", got)
	}

	// Close clears all
	sm.SetSudoPassword(1, "stillset")
	sm.Close()
	if got := sm.GetSudoPassword(1); got != "" {
		t.Errorf("GetSudoPassword(1) = %q, want empty after Close()", got)
	}
}

func TestGetCachedPassword(t *testing.T) {
	sm := NewManager(slog.Default())

	if got := sm.GetCachedPassword(); got != "" {
		t.Errorf("GetCachedPassword() = %q, want empty initially", got)
	}

	sm.SetCachedPassword("mypassword")
	if got := sm.GetCachedPassword(); got != "mypassword" {
		t.Errorf("GetCachedPassword() = %q, want %q", got, "mypassword")
	}

	sm.ClearPassword()
	if got := sm.GetCachedPassword(); got != "" {
		t.Errorf("GetCachedPassword() = %q, want empty after ClearPassword", got)
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

// TestSudoStdinContract pins D2: the sudo password relay's entire stdin
// contract is one line, then EOF.
func TestSudoStdinContract(t *testing.T) {
	r := sudoStdin("hunter2")
	data, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read sudoStdin: %v", err)
	}
	if string(data) != "hunter2\n" {
		t.Errorf("sudoStdin content = %q, want %q", string(data), "hunter2\n")
	}
	n, err := r.Read(make([]byte, 1))
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Errorf("sudoStdin after its one line: n=%d err=%v, want n=0 err=io.EOF", n, err)
	}
}

func TestValidateSudoPassword(t *testing.T) {
	rejected := []string{"has\nnewline", "has\rcarriage", "has\x00nul"}
	for _, pw := range rejected {
		if err := ValidateSudoPassword(pw); !errors.Is(err, ErrSudoDelivery) {
			t.Errorf("ValidateSudoPassword(%q) = %v, want an error wrapping ErrSudoDelivery", pw, err)
		}
	}

	accepted := []string{"", "simple", "with spaces", `with "quotes"`, `with\backslashes`, "-leading-dash"}
	for _, pw := range accepted {
		if err := ValidateSudoPassword(pw); err != nil {
			t.Errorf("ValidateSudoPassword(%q) = %v, want nil", pw, err)
		}
	}
}

func TestRewriteSudoInCmd(t *testing.T) {
	sm := NewManager(slog.Default())

	if cmd, stdin, err := sm.RewriteSudoInCmd(0, "sudo true"); cmd != "sudo true" || stdin != nil || err != nil {
		t.Errorf("RewriteSudoInCmd with no cached password = (%q, %v, %v), want (\"sudo true\", nil, nil)", cmd, stdin, err)
	}

	sm.SetSudoPassword(0, "secret")
	if cmd, stdin, err := sm.RewriteSudoInCmd(0, "systemctl status"); cmd != "systemctl status" || stdin != nil || err != nil {
		t.Errorf("RewriteSudoInCmd with no \"sudo \" in cmd = (%q, %v, %v), want (\"systemctl status\", nil, nil)", cmd, stdin, err)
	}

	sm.SetSudoPassword(0, "bad\npassword")
	if cmd, stdin, err := sm.RewriteSudoInCmd(0, "sudo true"); cmd != "" || stdin != nil || !errors.Is(err, ErrSudoDelivery) {
		t.Errorf("RewriteSudoInCmd with an undeliverable password = (%q, %v, %v), want (\"\", nil, ErrSudoDelivery)", cmd, stdin, err)
	}

	sm.SetSudoPassword(0, "secret")
	cmd, stdin, err := sm.RewriteSudoInCmd(0, "sudo true")
	if err != nil {
		t.Fatalf("RewriteSudoInCmd(0, \"sudo true\") with a valid cached password: %v", err)
	}
	if cmd == "sudo true" || !strings.Contains(cmd, "| sudo -S") {
		t.Errorf("RewriteSudoInCmd returned command %q, want it rewritten", cmd)
	}
	if stdin == nil {
		t.Errorf("RewriteSudoInCmd returned a nil stdin reader for a rewritten command")
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
