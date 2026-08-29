package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// --- Message types for modal prompt results ---

// passwordEnteredMsg is sent when the SSH password modal is completed.
type passwordEnteredMsg struct {
	password string
	hostIdx  int
}

// passwordCancelledMsg is sent when the SSH password modal is cancelled.
type passwordCancelledMsg struct {
	hostIdx int
}

// sudoEnteredMsg is sent when the sudo password modal is completed.
type sudoEnteredMsg struct {
	password string
	hostIdx  int
	retry    tea.Cmd
}

// sudoCancelledMsg is sent when the sudo password modal is cancelled.
type sudoCancelledMsg struct{}

// transitionConfirmedMsg is sent when a transition confirm modal is confirmed.
type transitionConfirmedMsg struct {
	t transition
}

// confirmCancelledMsg is sent when any confirm modal is cancelled.
type confirmCancelledMsg struct{}

// isPasswordModal returns true if the modal is an SSH password prompt.
func isPasswordModal(m *ModalOverlay) bool {
	return m != nil && m.title == "SSH Password"
}

// --- Modal constructors ---

// NewPasswordModal creates a 1-step masked input modal for SSH password.
func NewPasswordModal(user, host string, hostIdx int) *ModalOverlay {
	prompt := fmt.Sprintf("Password for %s@%s:", user, host)
	m := NewModalOverlay("SSH Password", []ModalStep{
		{Title: prompt, Content: NewMaskedTextInputContent(prompt)},
	}, func(results []any) tea.Cmd {
		pw := results[0].(string)
		idx := hostIdx
		return func() tea.Msg {
			return passwordEnteredMsg{password: pw, hostIdx: idx}
		}
	}, func() tea.Cmd {
		idx := hostIdx
		return func() tea.Msg {
			return passwordCancelledMsg{hostIdx: idx}
		}
	})
	return m
}

// NewSudoModal creates a 1-step masked input modal for sudo password.
func NewSudoModal(user, host string, hostIdx int, retry tea.Cmd) *ModalOverlay {
	prompt := fmt.Sprintf("Sudo password for %s:", user)
	m := NewModalOverlay("Sudo Password", []ModalStep{
		{Title: prompt, Content: NewMaskedTextInputContentWithValidator(prompt, ssh.ValidateSudoPassword)},
	}, func(results []any) tea.Cmd {
		pw := results[0].(string)
		idx := hostIdx
		r := retry
		return func() tea.Msg {
			return sudoEnteredMsg{password: pw, hostIdx: idx, retry: r}
		}
	}, func() tea.Cmd {
		return func() tea.Msg {
			return sudoCancelledMsg{}
		}
	})
	return m
}

// NewConfirmModal creates a 1-step Y/N confirmation modal.
// onConfirm is the tea.Cmd to execute when confirmed.
func NewConfirmModal(title, message string, onConfirm tea.Cmd) *ModalOverlay {
	m := NewModalOverlay(title, []ModalStep{
		{Title: "", Content: NewConfirmContent(message)},
	}, func(results []any) tea.Cmd {
		confirmed := results[0].(bool)
		if confirmed {
			return onConfirm
		}
		return func() tea.Msg {
			return confirmCancelledMsg{}
		}
	}, func() tea.Cmd {
		return func() tea.Msg {
			return confirmCancelledMsg{}
		}
	})
	m.FooterFn = func() string {
		return modalKeyStyle.Render("Y/Enter") + " " + modalDimStyle.Render("confirm") +
			"  " + modalKeyStyle.Render("N/Esc") + " " + modalDimStyle.Render("cancel")
	}
	return m
}

// NewTransitionConfirmModal creates a confirm modal for transition actions (Azure/K8s).
func NewTransitionConfirmModal(message string, t transition) *ModalOverlay {
	return NewConfirmModal("Confirm", message, func() tea.Msg {
		return transitionConfirmedMsg{t: t}
	})
}
