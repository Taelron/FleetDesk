package app

import (
	tea "github.com/charmbracelet/bubbletea"
)

// LoadingContent is a non-dismissable modal step that shows a loading message.
// It swallows all key events — only dismissed programmatically via dismissLoadingFor.
type LoadingContent struct {
	message string
	tag     string // identifies which fetch set this loading modal
}

// HandleKey implements StepContent for LoadingContent.
func (l *LoadingContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	// Return a no-op cmd to signal that we consumed the key event.
	// This prevents ModalOverlay from falling through to its own Esc handling.
	return l, func() tea.Msg { return nil }, false
}

// View implements StepContent for LoadingContent.
func (l *LoadingContent) View(width int) string {
	return "  " + l.message
}

// Result implements StepContent for LoadingContent.
func (l *LoadingContent) Result() any {
	return nil
}

// showLoading sets a loading modal overlay on the model with a tag.
// The tag identifies the fetch so only the matching dismissLoadingFor clears it.
func showLoading(m *Model, tag, message string) {
	m.modal = NewModalOverlay("", []ModalStep{
		{Title: "", Content: &LoadingContent{message: message, tag: tag}},
	}, func(_ []any) tea.Cmd { return nil },
		func() tea.Cmd { return nil })
	m.modal.FooterFn = func() string { return "" }
}

// dismissLoadingFor clears the modal only if it is a loading modal with the given tag.
// This prevents unrelated fetch results from dismissing another view's loading modal.
func dismissLoadingFor(m *Model, tag string) {
	if m.modal == nil || m.modal.Done() {
		return
	}
	if m.modal.current < len(m.modal.steps) {
		if lc, ok := m.modal.steps[m.modal.current].Content.(*LoadingContent); ok && lc.tag == tag {
			m.modal = nil
		}
	}
}
