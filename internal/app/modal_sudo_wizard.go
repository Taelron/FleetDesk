package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// sudoWizardTestMsg is sent when the user enters a password in the wizard.
// The model handler shows a loading modal and fires the async test.
type sudoWizardTestMsg struct {
	hostIdx  int
	password []byte
}

// sudoWizardResultMsg is sent after the sudo wizard tests the password.
// err is set only when the password could not be delivered safely
// (ssh.ErrSudoDelivery) — never for a wrong password, which still reports
// via success=false. tested reports whether a password was actually
// tested (success or failure) as opposed to the wizard being cancelled.
type sudoWizardResultMsg struct {
	hostIdx int
	tested  bool
	success bool
	err     error
}

// NewSudoWizard creates a sudo authentication wizard.
// Step 1: Enter sudo password (masked input) — user presses Enter.
// After Enter: modal switches to "Testing sudo..." loading overlay,
// then async tests the password and sends sudoWizardResultMsg.
func NewSudoWizard(user, host string, hostIdx int, sshMgr *ssh.Manager) *ModalOverlay {
	prompt := fmt.Sprintf("Sudo password for %s@%s:", user, host)
	m := NewModalOverlay("Sudo Authentication", []ModalStep{
		{Title: prompt, Content: NewSecretInputContent(prompt, ssh.ValidateSudoPassword)},
	}, func(results []any) tea.Cmd {
		pw := results[0].([]byte)
		idx := hostIdx
		return func() tea.Msg {
			return sudoWizardTestMsg{hostIdx: idx, password: pw}
		}
	}, func() tea.Cmd {
		idx := hostIdx
		return func() tea.Msg {
			return sudoWizardResultMsg{hostIdx: idx, success: false}
		}
	})
	return m
}
