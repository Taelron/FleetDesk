package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// optionsStep wraps a MultiSelectContent and mutates a ConfirmContent's message
// when the user confirms their selection. This bridges step 1 → step 2 in a
// 2-step ModalOverlay without modifying the overlay core.
type optionsStep struct {
	inner      *MultiSelectContent
	confirmRef *ConfirmContent
	cmdBuilder func([]string) string
}

func (o *optionsStep) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	newInner, cmd, done := o.inner.HandleKey(msg)
	o.inner = newInner.(*MultiSelectContent)
	if done {
		keys := o.inner.Result().([]string)
		resolved := o.cmdBuilder(keys)
		o.confirmRef.SetMessage(resolved)
	}
	return o, cmd, done
}

func (o *optionsStep) View(width int) string {
	return o.inner.View(width)
}

func (o *optionsStep) Result() any {
	return o.inner.Result()
}

// buildDnfCommand assembles a dnf command from a base and selected flags.
// The base includes the dnf verb (e.g. "sudo dnf update" or "sudo dnf update --security").
// Flags are inserted in order. --setopt=skip_if_unavailable=1 and -y are always appended.
func buildDnfCommand(base string, flags []string) string {
	parts := []string{base}
	parts = append(parts, flags...)
	parts = append(parts, "--setopt=skip_if_unavailable=1", "-y")
	cmd := strings.Join(parts, " ")
	cmd += "; echo ''; echo 'Done. Press Enter to return...'"
	return cmd
}

// NewOptionsConfirmModal creates a 2-step modal: MultiSelect options → Confirm resolved command.
//
// Parameters:
//   - title: modal title
//   - step1Title: title shown above the options list
//   - options: toggleable options for step 1
//   - cmdBuilder: builds the resolved command string from selected keys
//   - onConfirmFn: called with resolved command on confirmation, returns the tea.Cmd to execute
//   - onCancel: called when the user cancels at step 1 or presses N at step 2
func NewOptionsConfirmModal(
	title string,
	step1Title string,
	options []MultiSelectOption,
	cmdBuilder func([]string) string,
	onConfirmFn func(string) tea.Cmd,
	onCancel func() tea.Cmd,
) *ModalOverlay {
	confirm := &ConfirmContent{}
	inner := NewMultiSelectContent(options).(*MultiSelectContent)

	adapter := &optionsStep{
		inner:      inner,
		confirmRef: confirm,
		cmdBuilder: cmdBuilder,
	}

	m := NewModalOverlay(title, []ModalStep{
		{Title: step1Title, Content: adapter},
		{Title: "", Content: confirm},
	}, func(results []any) tea.Cmd {
		confirmed := results[1].(bool)
		if confirmed {
			keys := results[0].([]string)
			resolved := cmdBuilder(keys)
			return onConfirmFn(resolved)
		}
		return func() tea.Msg {
			return confirmCancelledMsg{}
		}
	}, onCancel)

	m.FooterFn = func() string {
		if m.current == 0 {
			return modalKeyStyle.Render("Space") + " " + modalDimStyle.Render("toggle") +
				"  " + modalKeyStyle.Render("Enter") + " " + modalDimStyle.Render("confirm") +
				"  " + modalKeyStyle.Render("Esc") + " " + modalDimStyle.Render("cancel")
		}
		return modalKeyStyle.Render("Y/Enter") + " " + modalDimStyle.Render("confirm") +
			"  " + modalKeyStyle.Render("N") + " " + modalDimStyle.Render("cancel") +
			"  " + modalKeyStyle.Render("Esc") + " " + modalDimStyle.Render("back")
	}

	return m
}
