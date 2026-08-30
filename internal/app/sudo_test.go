package app

import (
	"errors"
	"log/slog"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

func newTestModel() Model {
	logger := slog.Default()
	return Model{
		ssh:    ssh.NewManager(logger),
		logger: logger,
		width:  80,
		height: 24,
	}
}

// TestSudoModalKeyCapture verifies keyboard handling when sudo modal is active.
func TestSudoModalKeyCapture(t *testing.T) {
	t.Run("rune accumulates in masked input", func(t *testing.T) {
		modal := NewSudoModal("alice", "host1", 0, nil)
		modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
		sic := modal.steps[0].Content.(*SecretInputContent)
		if string(sic.buf) != "a" {
			t.Errorf("value = %q, want %q", sic.buf, "a")
		}
	})

	t.Run("backspace removes last rune", func(t *testing.T) {
		modal := NewSudoModal("alice", "host1", 0, nil)
		for _, r := range "abc" {
			modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		modal.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})
		sic := modal.steps[0].Content.(*SecretInputContent)
		if string(sic.buf) != "ab" {
			t.Errorf("value = %q, want %q", sic.buf, "ab")
		}
	})

	t.Run("backspace multi-byte rune", func(t *testing.T) {
		modal := NewSudoModal("alice", "host1", 0, nil)
		for _, r := range "pàss" {
			modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		modal.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})
		sic := modal.steps[0].Content.(*SecretInputContent)
		if string(sic.buf) != "pàs" {
			t.Errorf("value = %q, want %q", sic.buf, "pàs")
		}
	})

	t.Run("enter with empty input is rejected", func(t *testing.T) {
		modal := NewSudoModal("alice", "host1", 0, nil)
		modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if modal.Done() {
			t.Error("modal should not be done on empty enter")
		}
	})

	t.Run("enter with password completes modal", func(t *testing.T) {
		modal := NewSudoModal("alice", "host1", 0, nil)
		for _, r := range "mypassword" {
			modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		cmd := modal.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if !modal.Done() {
			t.Error("modal should be done after entering password")
		}
		if cmd == nil {
			t.Error("expected non-nil cmd from OnComplete")
		}
	})

	t.Run("esc cancels modal", func(t *testing.T) {
		modal := NewSudoModal("alice", "host1", 0, nil)
		for _, r := range "typing" {
			modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		cmd := modal.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
		if !modal.Done() {
			t.Error("modal should be done after Esc")
		}
		if cmd == nil {
			t.Error("expected non-nil cmd from OnCancel")
		}
	})
}

// TestHandleSudoOrFlash verifies the sudo error detection and branching logic.
func TestHandleSudoOrFlash(t *testing.T) {
	retryCmd := func() tea.Msg { return nil }

	t.Run("non-sudo error returns false", func(t *testing.T) {
		m := newTestModel()
		err := errors.New("connection refused")
		_, cmd, ok := m.handleSudoOrFlash(err, retryCmd)
		if ok {
			t.Error("expected ok=false for non-sudo error")
		}
		if cmd != nil {
			t.Error("expected cmd=nil for non-sudo error")
		}
	})

	t.Run("sudo error with no cached passwords shows modal", func(t *testing.T) {
		m := newTestModel()
		m.hosts = []config.Host{{Entry: config.HostEntry{Name: "host1"}}}
		err := ssh.ErrSudoRequired
		m2, cmd, ok := m.handleSudoOrFlash(err, retryCmd)
		if !ok {
			t.Error("expected ok=true for sudo error")
		}
		if m2.modal == nil {
			t.Error("expected modal to be set when no passwords cached")
		}
		if cmd != nil {
			t.Error("expected cmd=nil when showing prompt directly")
		}
	})

	t.Run("sudo error with cached SSH password returns test cmd", func(t *testing.T) {
		m := newTestModel()
		m.hosts = []config.Host{{Entry: config.HostEntry{Name: "host1"}}}
		m.ssh.SetCachedPassword([]byte("sshpw"))
		err := ssh.ErrSudoRequired
		m2, cmd, ok := m.handleSudoOrFlash(err, retryCmd)
		if !ok {
			t.Error("expected ok=true for sudo error")
		}
		if m2.modal != nil {
			t.Error("expected modal=nil when silently testing SSH password")
		}
		if cmd == nil {
			t.Error("expected non-nil cmd for silent sudo test")
		}
	})

	t.Run("sudo error with wrong cached sudo password clears and shows modal", func(t *testing.T) {
		m := newTestModel()
		m.hosts = []config.Host{{Entry: config.HostEntry{Name: "host1"}}}
		m.ssh.SetSudoPassword(0, []byte("wrongpw"))
		err := ssh.ErrSudoRequired
		m2, cmd, ok := m.handleSudoOrFlash(err, retryCmd)
		if !ok {
			t.Error("expected ok=true for sudo error")
		}
		if m2.modal == nil {
			t.Error("expected modal to be set after clearing wrong sudo password")
		}
		if cmd != nil {
			t.Error("expected cmd=nil when showing prompt")
		}
		if m2.ssh.GetSudoPassword(0) {
			t.Error("expected sudo password to be cleared")
		}
	})
}
