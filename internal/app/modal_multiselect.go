package app

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// MultiSelectOption is one toggleable option in a MultiSelectContent step.
type MultiSelectOption struct {
	Key         string // returned in Result when selected
	Label       string // short display name
	Description string // shown dimmed after label
}

// MultiSelectContent is a checkbox-style multi-select modal step.
type MultiSelectContent struct {
	options  []MultiSelectOption
	selected map[int]bool
	cursor   int
}

// NewMultiSelectContent creates a multi-select step with the given options.
func NewMultiSelectContent(options []MultiSelectOption) StepContent {
	return &MultiSelectContent{
		options:  options,
		selected: make(map[int]bool),
	}
}

// HandleKey implements StepContent for MultiSelectContent.
func (ms *MultiSelectContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if ms.cursor > 0 {
			ms.cursor--
		}
	case "down", "j":
		if ms.cursor < len(ms.options)-1 {
			ms.cursor++
		}
	case " ":
		ms.selected[ms.cursor] = !ms.selected[ms.cursor]
		if !ms.selected[ms.cursor] {
			delete(ms.selected, ms.cursor)
		}
	case "enter":
		return ms, nil, true
	}
	return ms, nil, false
}

// View implements StepContent for MultiSelectContent.
func (ms *MultiSelectContent) View(width int) string {
	var lines []string
	for i, opt := range ms.options {
		check := "[ ]"
		if ms.selected[i] {
			check = "\033[32m[x]\033[0m" // green
		}

		label := opt.Label
		desc := ""
		if opt.Description != "" {
			desc = "  \033[38;5;241m" + opt.Description + "\033[0m" // dimmed
		}

		row := check + " " + label + desc

		style := normalRowStyle
		if i == ms.cursor {
			style = selectedRowStyle
		}
		lines = append(lines, style.Render(row))
	}
	return strings.Join(lines, "\n")
}

// Result implements StepContent for MultiSelectContent.
func (ms *MultiSelectContent) Result() any {
	var keys []string
	for i, opt := range ms.options {
		if ms.selected[i] {
			keys = append(keys, opt.Key)
		}
	}
	if keys == nil {
		return []string{}
	}
	return keys
}
