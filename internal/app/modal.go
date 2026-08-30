package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// StepContent is the interface for modal step content types.
type StepContent interface {
	HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool)
	View(width int) string
	Result() any
}

// erasable is implemented by step content types that hold a credential
// buffer needing explicit erasure — currently SecretInputContent. Content
// that does not implement it (text, select, static, confirm, loading) has
// nothing to erase.
type erasable interface {
	Erase()
}

// ModalStep represents a single step in a multi-step modal.
type ModalStep struct {
	Title   string
	Content StepContent
}

// ModalOverlay is a centered dialog rendered on top of the current view.
type ModalOverlay struct {
	title      string
	steps      []ModalStep
	current    int
	results    []any
	OnComplete func([]any) tea.Cmd
	OnCancel   func() tea.Cmd
	FooterFn   func() string // optional custom footer; if nil, default footer is used
	done       bool
}

// NewModalOverlay creates a new modal overlay.
func NewModalOverlay(title string, steps []ModalStep, onComplete func([]any) tea.Cmd, onCancel func() tea.Cmd) *ModalOverlay {
	return &ModalOverlay{
		title:      title,
		steps:      steps,
		results:    make([]any, 0, len(steps)),
		OnComplete: onComplete,
		OnCancel:   onCancel,
	}
}

// HandleKey processes a key event, returning an optional tea.Cmd.
func (m *ModalOverlay) HandleKey(msg tea.KeyMsg) tea.Cmd {
	step := &m.steps[m.current]

	// Let content handle Esc first — LoadingContent swallows it (done=false),
	// preventing the overlay from dismissing a non-dismissable modal.
	if msg.Type == tea.KeyEsc {
		newContent, cmd, done := step.Content.HandleKey(msg)
		step.Content = newContent
		if done {
			// Content accepted Esc as a dismiss signal
			m.results = append(m.results, step.Content.Result())
			m.current++
			if m.current >= len(m.steps) {
				m.done = true
				if m.OnComplete != nil {
					return m.OnComplete(m.results)
				}
				return cmd
			}
			return cmd
		}
		if cmd != nil {
			return cmd
		}
		// Content did not handle Esc — fall through to overlay Esc behavior
		if m.current == 0 {
			m.done = true
			if m.OnCancel != nil {
				return m.OnCancel()
			}
			return nil
		}
		// Go back: trim results and restore previous step
		m.current--
		if len(m.results) > m.current {
			m.results = m.results[:m.current]
		}
		return nil
	}

	newContent, cmd, done := step.Content.HandleKey(msg)
	step.Content = newContent

	if done {
		m.results = append(m.results, step.Content.Result())
		m.current++
		if m.current >= len(m.steps) {
			m.done = true
			if m.OnComplete != nil {
				return m.OnComplete(m.results)
			}
			return cmd
		}
	}
	return cmd
}

// View renders the modal overlay on top of a background view.
func (m *ModalOverlay) View(bgView string, width, height int) string {
	modalWidth := min(width-10, 80)
	modalWidth = max(modalWidth, 50)
	innerWidth := modalWidth - 4 // padding

	// Title bar
	var titleLine string
	if len(m.steps) > 1 {
		stepInfo := fmt.Sprintf("Step %d/%d", m.current+1, len(m.steps))
		titleGap := innerWidth - len(m.title) - len(stepInfo)
		if titleGap < 1 {
			titleGap = 1
		}
		titleLine = modalTitleStyle.Render(m.title) + strings.Repeat(" ", titleGap) + modalDimStyle.Render(stepInfo)
	} else {
		titleLine = modalTitleStyle.Render(m.title)
	}

	// Step content
	stepTitle := ""
	content := ""
	if m.current < len(m.steps) {
		stepTitle = m.steps[m.current].Title
		content = m.steps[m.current].Content.View(innerWidth)
	}

	// Footer
	var footer string
	if m.FooterFn != nil {
		footer = m.FooterFn()
	} else {
		footer = modalKeyStyle.Render("Enter") + " " + modalDimStyle.Render("confirm") +
			"  " + modalKeyStyle.Render("Esc") + " " + modalDimStyle.Render("cancel")
		if m.current > 0 {
			footer = modalKeyStyle.Render("Enter") + " " + modalDimStyle.Render("confirm") +
				"  " + modalKeyStyle.Render("Esc") + " " + modalDimStyle.Render("back")
		}
	}

	// Build modal box
	box := lipgloss.NewStyle().
		Width(modalWidth).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorBlue).
		Padding(1, 2).
		Render(titleLine + "\n\n" + stepTitle + "\n\n" + content + "\n\n" + footer)

	// Place centered modal; the bgView parameter is available for future
	// compositing but the current approach uses Place's whitespace fill
	// to create the dimmed-background effect.
	_ = bgView
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box,
		lipgloss.WithWhitespaceBackground(lipgloss.Color("0")),
		lipgloss.WithWhitespaceForeground(lipgloss.Color("235")),
		lipgloss.WithWhitespaceChars("░"),
	)
}

// Done returns true if the modal has completed or been cancelled.
func (m *ModalOverlay) Done() bool {
	return m.done
}

// Erase erases every step content in the overlay that holds a credential
// buffer (implements erasable). Safe to call on an overlay whose content
// has already handed its buffer off via Result() and erased itself — a
// completed SecretInputContent's Erase is a no-op (see its Done guard).
func (m *ModalOverlay) Erase() {
	for _, step := range m.steps {
		if e, ok := step.Content.(erasable); ok {
			e.Erase()
		}
	}
}

// replaceModal assigns next as the model's modal overlay, first erasing
// the outgoing overlay's credential content unless it is nil or already
// Done(). The Done() guard is load-bearing: an overlay completed by Enter
// has handed its buffer to a message and still aliases it, so erasing
// here would zero the credential that message is carrying — a
// cancelled overlay has already erased its own content before Done()
// is observed here.
//
// Every m.modal assignment reachable from Update (the 24 direct
// assignments in its switch, plus showLoading and dismissLoadingFor,
// which assign on Update's behalf) goes through this, not just the sites
// measured as live drop races — so the property does not depend on that
// classification staying true as the code changes. Key-handler sites
// (keys.go) are direct assignments: they cannot fire while a modal is
// open (keys.go's modal interception hands every key to the modal first),
// so there is nothing in flight for them to drop.
func (m *Model) replaceModal(next *ModalOverlay) {
	if m.modal != nil && !m.modal.Done() {
		m.modal.Erase()
	}
	m.modal = next
}

// --- TextInputContent ---

// TextInputContent is a single-line text input with optional validation.
type TextInputContent struct {
	prompt   string
	value    string
	validate func(string) error
	err      string
}

// NewTextInputContent creates a text input step.
func NewTextInputContent(prompt string, validate func(string) error) StepContent {
	return &TextInputContent{prompt: prompt, validate: validate}
}

// HandleKey implements StepContent for TextInputContent.
func (t *TextInputContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyEnter:
		if t.value == "" {
			t.err = "Value cannot be empty"
			return t, nil, false
		}
		if t.validate != nil {
			if err := t.validate(t.value); err != nil {
				t.err = err.Error()
				return t, nil, false
			}
		}
		t.err = ""
		return t, nil, true
	case tea.KeyBackspace:
		if len(t.value) > 0 {
			runes := []rune(t.value)
			t.value = string(runes[:len(runes)-1])
		}
		t.err = ""
	default:
		if msg.Type == tea.KeyRunes {
			t.value += string(msg.Runes)
			t.err = ""
		}
	}
	return t, nil, false
}

// View implements StepContent for TextInputContent.
func (t *TextInputContent) View(width int) string {
	cursor := "█"
	input := t.value + cursor
	line := modalInputStyle.Width(width).Render(input)
	if t.err != "" {
		line += "\n" + modalErrorStyle.Render(t.err)
	}
	return line
}

// Result implements StepContent for TextInputContent.
func (t *TextInputContent) Result() any {
	return t.value
}

// Value returns the current text input value.
func (t *TextInputContent) Value() string {
	return t.value
}

// SetValue sets the text input value (used for pre-populating).
func (t *TextInputContent) SetValue(v string) {
	t.value = v
}

// --- SelectContent ---

// SelectContent is an arrow-key list picker.
type SelectContent struct {
	prompt  string
	options []string
	cursor  int
}

// NewSelectContent creates a select picker step.
func NewSelectContent(prompt string, options []string) StepContent {
	return &SelectContent{prompt: prompt, options: options}
}

// HandleKey implements StepContent for SelectContent.
func (s *SelectContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if s.cursor > 0 {
			s.cursor--
		}
	case "down", "j":
		if s.cursor < len(s.options)-1 {
			s.cursor++
		}
	case "enter":
		return s, nil, true
	}
	return s, nil, false
}

// View implements StepContent for SelectContent.
func (s *SelectContent) View(width int) string {
	var lines []string
	for i, opt := range s.options {
		cursor := "  "
		if i == s.cursor {
			cursor = "▸ "
		}
		style := normalRowStyle
		if i == s.cursor {
			style = selectedRowStyle
		}
		lines = append(lines, style.Render(cursor+opt))
	}
	return strings.Join(lines, "\n")
}

// Result implements StepContent for SelectContent.
func (s *SelectContent) Result() any {
	if s.cursor < len(s.options) {
		return s.options[s.cursor]
	}
	return ""
}

// --- StaticContent ---

// StaticContent is scrollable read-only text (for help overlays).
type StaticContent struct {
	text   string
	scroll int
}

// NewStaticContent creates a read-only text step.
func NewStaticContent(text string) StepContent {
	return &StaticContent{text: text}
}

// HandleKey implements StepContent for StaticContent.
func (s *StaticContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.String() {
	case "up", "k":
		if s.scroll > 0 {
			s.scroll--
		}
	case "down", "j":
		s.scroll++
	case "enter", "?":
		return s, nil, true
	}
	return s, nil, false
}

// View implements StepContent for StaticContent.
func (s *StaticContent) View(width int) string {
	lines := strings.Split(s.text, "\n")
	if s.scroll >= len(lines) {
		s.scroll = len(lines) - 1
	}
	if s.scroll < 0 {
		s.scroll = 0
	}
	visible := lines[s.scroll:]
	if len(visible) > 20 {
		visible = visible[:20]
	}
	return strings.Join(visible, "\n")
}

// Result implements StepContent for StaticContent.
func (s *StaticContent) Result() any {
	return nil
}

// --- ConfirmContent ---

// ConfirmContent is a Y/N confirmation dialog.
type ConfirmContent struct {
	message   string
	confirmed bool
}

// NewConfirmContent creates a confirmation step.
func NewConfirmContent(message string) StepContent {
	return &ConfirmContent{message: message}
}

// HandleKey implements StepContent for ConfirmContent.
func (c *ConfirmContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.String() {
	case "y", "Y", "enter":
		c.confirmed = true
		return c, nil, true
	case "n", "N":
		c.confirmed = false
		return c, nil, true
	}
	return c, nil, false
}

// View implements StepContent for ConfirmContent.
func (c *ConfirmContent) View(width int) string {
	return flashErrorStyle.Render(c.message)
}

// Result implements StepContent for ConfirmContent.
func (c *ConfirmContent) Result() any {
	return c.confirmed
}

// SetMessage updates the confirmation message (used by multi-step wizards).
func (c *ConfirmContent) SetMessage(s string) {
	c.message = s
}
