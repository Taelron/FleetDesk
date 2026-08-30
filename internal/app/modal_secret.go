package app

import (
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

// SecretInputContent is a single-line masked input over a fixed-capacity
// []byte buffer — the prompt end of TAE-21's credential chain. The
// buffer is allocated once at credential.MaxLen and never appended to;
// KeyRunes and backspace reslice within that capacity so growing never
// copies the backing array and abandons bytes unerased.
//
// Bubble Tea delivers a bracketed paste as a single KeyRunes message, so
// one message can carry more than the cap — a paste over the cap is
// refused whole, at entry, never silently truncated into a password the
// user did not type.
type SecretInputContent struct {
	prompt   string
	buf      []byte // len == bytes entered so far; cap == credential.MaxLen
	runes    int    // rune count, for masking and multi-byte backspace
	validate func([]byte) error
	err      string
	done     bool // Enter has handed buf off via Result(); Erase becomes a no-op
}

// NewSecretInputContent creates a masked []byte input step. validate may
// be nil; when set, it is checked at Enter, before the value is ever
// cached or sent, and its error is shown inline under the field.
func NewSecretInputContent(prompt string, validate func([]byte) error) StepContent {
	return &SecretInputContent{
		prompt:   prompt,
		buf:      make([]byte, 0, credential.MaxLen),
		validate: validate,
	}
}

// HandleKey implements StepContent for SecretInputContent.
func (s *SecretInputContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyEnter:
		if len(s.buf) == 0 {
			s.err = "Value cannot be empty"
			return s, nil, false
		}
		if s.validate != nil {
			if err := s.validate(s.buf); err != nil {
				s.err = err.Error()
				return s, nil, false
			}
		}
		s.err = ""
		s.done = true
		return s, nil, true
	case tea.KeyBackspace:
		if s.runes == 0 {
			return s, nil, false
		}
		_, size := utf8.DecodeLastRune(s.buf)
		newLen := len(s.buf) - size
		credential.Erase(s.buf[newLen:])
		s.buf = s.buf[:newLen]
		s.runes--
		s.err = ""
		return s, nil, false
	default:
		if msg.Type == tea.KeyRunes {
			size := 0
			for _, r := range msg.Runes {
				size += utf8.RuneLen(r)
			}
			if len(s.buf)+size > credential.MaxLen {
				s.err = credential.ErrTooLong.Error()
				clear(msg.Runes)
				return s, nil, false
			}
			old := len(s.buf)
			s.buf = s.buf[:old+size]
			n := 0
			for _, r := range msg.Runes {
				n += utf8.EncodeRune(s.buf[old+n:], r)
			}
			s.runes += len(msg.Runes)
			s.err = ""
			clear(msg.Runes)
		}
	}
	return s, nil, false
}

// View implements StepContent for SecretInputContent.
func (s *SecretInputContent) View(width int) string {
	cursor := "█"
	input := strings.Repeat("*", s.runes) + cursor
	line := modalInputStyle.Width(width).Render(input)
	if s.err != "" {
		line += "\n" + modalErrorStyle.Render(s.err)
	}
	return line
}

// Result implements StepContent for SecretInputContent. Ownership of the
// backing buffer passes to the caller, which becomes responsible for its
// eventual erasure.
func (s *SecretInputContent) Result() any {
	return s.buf
}

// Done reports whether Enter has already handed the buffer off via
// Result — ModalOverlay.replaceModal must not erase a completed
// overlay's content, since it still aliases a buffer a message now owns.
func (s *SecretInputContent) Done() bool {
	return s.done
}

// Erase zeroes the buffer in place. A no-op once Done(): the buffer has
// already been handed to a message by then, and erasing it here would
// zero the credential that message is carrying.
func (s *SecretInputContent) Erase() {
	if s.done {
		return
	}
	credential.Erase(s.buf)
	s.buf = s.buf[:0]
}
