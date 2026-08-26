package app

import (
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// commandGroupStep wraps a SelectContent for group picking.
// On done, it filters commands by the selected group and rebuilds the
// downstream commandPickStep's inner SelectContent.
type commandGroupStep struct {
	inner       *SelectContent
	groups      []string
	allCommands []config.CommandEntry
	nextStep    *commandPickStep
}

func (s *commandGroupStep) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	newInner, cmd, done := s.inner.HandleKey(msg)
	s.inner = newInner.(*SelectContent)
	if done {
		group := s.inner.Result().(string)
		s.nextStep.setGroup(group, s.allCommands)
	}
	return s, cmd, done
}

func (s *commandGroupStep) View(width int) string { return s.inner.View(width) }
func (s *commandGroupStep) Result() any           { return s.inner.Result() }

// commandPickStep wraps a SelectContent for command picking.
// On done, it sets the confirm message to the resolved run: string.
type commandPickStep struct {
	inner      *SelectContent
	commands   []config.CommandEntry
	confirmRef *ConfirmContent
	selected   *config.CommandEntry // set on done for result extraction
}

func (s *commandPickStep) setGroup(group string, all []config.CommandEntry) {
	var filtered []config.CommandEntry
	for _, c := range all {
		if c.Group == group {
			filtered = append(filtered, c)
		}
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].Name < filtered[j].Name
	})
	s.commands = filtered
	names := make([]string, len(filtered))
	for i, c := range filtered {
		if c.Description != "" {
			names[i] = c.Name + " — " + c.Description
		} else {
			names[i] = c.Name
		}
	}
	s.inner = NewSelectContent("", names).(*SelectContent)
}

func (s *commandPickStep) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	newInner, cmd, done := s.inner.HandleKey(msg)
	s.inner = newInner.(*SelectContent)
	if done && s.inner.cursor < len(s.commands) {
		sel := s.commands[s.inner.cursor]
		s.selected = &sel
		s.confirmRef.SetMessage(sel.Run)
	}
	return s, cmd, done
}

func (s *commandPickStep) View(width int) string { return s.inner.View(width) }
func (s *commandPickStep) Result() any           { return s.inner.Result() }

// NewCommandWizard builds a 2-step or 3-step modal for user-defined commands.
// 1 group  → 2-step: command picker → confirm
// 2+ groups → 3-step: group picker → command picker → confirm
func NewCommandWizard(
	hostName string,
	commands []config.CommandEntry,
	onStream func(cmd config.CommandEntry) tea.Cmd,
	onCancel func() tea.Cmd,
) *ModalOverlay {
	// Derive distinct groups, preserving first-appearance order
	groups := deriveGroups(commands)

	confirm := &ConfirmContent{}
	pickStep := &commandPickStep{
		inner:      NewSelectContent("", nil).(*SelectContent),
		confirmRef: confirm,
	}

	var steps []ModalStep

	if len(groups) == 1 {
		// 2-step: pre-populate with the single group's commands
		pickStep.setGroup(groups[0], commands)
		steps = []ModalStep{
			{Title: "Select command — " + groups[0], Content: pickStep},
			{Title: "", Content: confirm},
		}
	} else {
		// 3-step: group picker first
		groupSelect := NewSelectContent("", groups).(*SelectContent)
		groupStep := &commandGroupStep{
			inner:       groupSelect,
			groups:      groups,
			allCommands: commands,
			nextStep:    pickStep,
		}
		steps = []ModalStep{
			{Title: "Select command group", Content: groupStep},
			{Title: "Select command", Content: pickStep},
			{Title: "", Content: confirm},
		}
	}

	m := NewModalOverlay("Run Command", steps,
		func(results []any) tea.Cmd {
			// Last result is the confirm bool
			confirmed := results[len(results)-1].(bool)
			if confirmed && pickStep.selected != nil {
				return onStream(*pickStep.selected)
			}
			return onCancel()
		},
		onCancel,
	)

	m.FooterFn = func() string {
		isConfirmStep := m.current == len(m.steps)-1
		if isConfirmStep {
			return modalKeyStyle.Render("Y/Enter") + " " + modalDimStyle.Render("confirm") +
				"  " + modalKeyStyle.Render("N") + " " + modalDimStyle.Render("cancel") +
				"  " + modalKeyStyle.Render("Esc") + " " + modalDimStyle.Render("back")
		}
		footer := modalKeyStyle.Render("↑↓") + " " + modalDimStyle.Render("navigate") +
			"  " + modalKeyStyle.Render("Enter") + " " + modalDimStyle.Render("select") +
			"  " + modalKeyStyle.Render("Esc") + " "
		if m.current == 0 {
			footer += modalDimStyle.Render("cancel")
		} else {
			footer += modalDimStyle.Render("back")
		}
		return footer
	}

	return m
}

// openCommandWizard sets up the command wizard modal for the currently selected host.
func openCommandWizard(m *Model) {
	h := m.hosts[m.selectedHost]
	hostIdx := m.selectedHost
	m.modal = NewCommandWizard(h.Entry.Name, h.Entry.Commands,
		func(cmd config.CommandEntry) tea.Cmd {
			cfg := SSHStreamConfig{
				Command:     cmd.Run,
				Title:       cmd.Name,
				SourceName:  cmd.Name,
				ReturnView:  viewHostList,
				HostIdx:     hostIdx,
				Sudo:        false,
				NewestFirst: false,
				AutoDone:    true,
			}
			return func() tea.Msg {
				return startCommandStreamMsg{cfg: cfg}
			}
		},
		func() tea.Cmd {
			return func() tea.Msg { return confirmCancelledMsg{} }
		},
	)
}

// deriveGroups extracts distinct group names from commands, sorted alphabetically.
func deriveGroups(commands []config.CommandEntry) []string {
	seen := make(map[string]bool)
	var groups []string
	for _, c := range commands {
		g := strings.TrimSpace(c.Group)
		if g != "" && !seen[g] {
			seen[g] = true
			groups = append(groups, g)
		}
	}
	sort.Strings(groups)
	return groups
}
