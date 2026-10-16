package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// hostKeyDecidedMsg is sent when the unknown-host-key modal is answered.
// It carries no key material beyond what is public: the key itself stays in
// the SSH manager until the user accepts it.
type hostKeyDecidedMsg struct {
	hostIdx  int
	accepted bool
	// fingerprint is the fingerprint the user was shown, so that accepting
	// can only ever record that key (public data, not a credential).
	fingerprint string
}

// hostKeyPrompt is the question the unknown-host-key modal asks.
const hostKeyPrompt = "Trust this host and add it to known_hosts? [y/N]"

// HostKeyContent is the body of the unknown-host-key modal. Unlike
// ConfirmContent, Enter means No: trusting a host is the dangerous answer,
// and it must be typed.
type HostKeyContent struct {
	addr, key string
	accepted  bool
}

// NewHostKeyContent creates the body for a first-contact host key.
func NewHostKeyContent(addr, key string) *HostKeyContent {
	return &HostKeyContent{addr: addr, key: key}
}

// HandleKey implements StepContent: y accepts; n or Enter declines.
func (c *HostKeyContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "Y":
		c.accepted = true
		return c, nil, true
	case "n", "N", "enter":
		c.accepted = false
		return c, nil, true
	}
	return c, nil, false
}

// View implements StepContent.
func (c *HostKeyContent) View(width int) string {
	return strings.Join([]string{
		c.addr,
		c.key,
		"",
		flashErrorStyle.Render(hostKeyPrompt),
	}, "\n")
}

// Result implements StepContent.
func (c *HostKeyContent) Result() any { return c.accepted }

// NewHostKeyModal asks whether to trust a host seen for the first time.
// Declining — N, Enter or Esc — is the default.
func NewHostKeyModal(hostIdx int, hostName, addr, key, fingerprint string) *ModalOverlay {
	m := NewModalOverlay("Unknown host key", []ModalStep{
		{Title: fmt.Sprintf("%s is not in known_hosts. It presented:", hostName), Content: NewHostKeyContent(addr, key)},
	}, func(results []any) tea.Cmd {
		accepted := results[0].(bool)
		return func() tea.Msg {
			return hostKeyDecidedMsg{hostIdx: hostIdx, accepted: accepted, fingerprint: fingerprint}
		}
	}, func() tea.Cmd {
		return func() tea.Msg { return hostKeyDecidedMsg{hostIdx: hostIdx, accepted: false} }
	})
	m.FooterFn = func() string {
		return modalKeyStyle.Render("y") + " " + modalDimStyle.Render("trust") +
			"  " + modalKeyStyle.Render("N/Enter/Esc") + " " + modalDimStyle.Render("decline")
	}
	return m
}
