package app

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestTextInputContent_HandleKey(t *testing.T) {
	t.Run("rune accumulation", func(t *testing.T) {
		ti := NewTextInputContent("Enter path:", nil)
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}
		ti2, _, done := ti.HandleKey(msg)
		if done {
			t.Error("should not be done after rune")
		}
		if ti2.(*TextInputContent).Value() != "a" {
			t.Errorf("value = %q, want %q", ti2.(*TextInputContent).Value(), "a")
		}
	})

	t.Run("backspace", func(t *testing.T) {
		ti := NewTextInputContent("Enter path:", nil)
		// type "abc"
		for _, r := range "abc" {
			ti, _, _ = ti.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		// backspace
		ti, _, _ = ti.HandleKey(tea.KeyMsg{Type: tea.KeyBackspace})
		if ti.(*TextInputContent).Value() != "ab" {
			t.Errorf("value = %q, want %q", ti.(*TextInputContent).Value(), "ab")
		}
	})

	t.Run("enter with valid input", func(t *testing.T) {
		ti := NewTextInputContent("Enter path:", nil)
		// type something
		for _, r := range "/tmp" {
			ti, _, _ = ti.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		_, _, done := ti.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if !done {
			t.Error("expected done on Enter with valid input")
		}
	})

	t.Run("enter with validation error", func(t *testing.T) {
		validator := func(s string) error {
			return fmt.Errorf("invalid")
		}
		ti := NewTextInputContent("Enter path:", validator)
		for _, r := range "bad" {
			ti, _, _ = ti.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
		ti2, _, done := ti.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if done {
			t.Error("should not be done when validation fails")
		}
		if ti2.(*TextInputContent).err == "" {
			t.Error("expected error message set")
		}
	})
}

func TestSelectContent_HandleKey(t *testing.T) {
	t.Run("down moves cursor", func(t *testing.T) {
		sc := NewSelectContent("Pick:", []string{"vim", "nvim", "nano"})
		sc2, _, _ := sc.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
		if sc2.(*SelectContent).cursor != 1 {
			t.Errorf("cursor = %d, want 1", sc2.(*SelectContent).cursor)
		}
	})

	t.Run("up at 0 stays", func(t *testing.T) {
		sc := NewSelectContent("Pick:", []string{"vim", "nvim"})
		sc2, _, _ := sc.HandleKey(tea.KeyMsg{Type: tea.KeyUp})
		if sc2.(*SelectContent).cursor != 0 {
			t.Errorf("cursor = %d, want 0", sc2.(*SelectContent).cursor)
		}
	})

	t.Run("enter selects", func(t *testing.T) {
		sc := NewSelectContent("Pick:", []string{"vim", "nvim", "nano"})
		// move to nvim
		sc, _, _ = sc.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
		_, _, done := sc.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
		if !done {
			t.Error("expected done on Enter")
		}
		if sc.(*SelectContent).Result().(string) != "nvim" {
			t.Errorf("result = %q, want %q", sc.(*SelectContent).Result(), "nvim")
		}
	})
}

func TestModalOverlay_EscCancels(t *testing.T) {
	cancelled := false
	m := NewModalOverlay("Test", []ModalStep{
		{Title: "Step 1", Content: NewTextInputContent("Input:", nil)},
	}, func(results []any) tea.Cmd { return nil }, func() tea.Cmd {
		cancelled = true
		return nil
	})

	m.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Esc at step 0 should call OnCancel")
	}
}

func TestModalOverlay_EscGoesBack(t *testing.T) {
	m := NewModalOverlay("Test", []ModalStep{
		{Title: "Step 1", Content: NewTextInputContent("Input:", nil)},
		{Title: "Step 2", Content: NewSelectContent("Pick:", []string{"a", "b"})},
	}, func(results []any) tea.Cmd { return nil }, func() tea.Cmd { return nil })

	// type something and advance to step 2
	for _, r := range "/tmp" {
		m.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.current != 1 {
		t.Fatalf("expected step 1, got %d", m.current)
	}

	// Esc should go back to step 0
	m.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if m.current != 0 {
		t.Errorf("expected step 0 after Esc, got %d", m.current)
	}
}

func TestModalOverlay_MultiStepAdvance(t *testing.T) {
	var gotResults []any
	m := NewModalOverlay("Test", []ModalStep{
		{Title: "Step 1", Content: NewTextInputContent("Input:", nil)},
		{Title: "Step 2", Content: NewSelectContent("Pick:", []string{"vim", "nvim"})},
	}, func(results []any) tea.Cmd {
		gotResults = results
		return nil
	}, func() tea.Cmd { return nil })

	// Step 1: type path and Enter
	for _, r := range "/tmp" {
		m.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})

	// Step 2: select nvim (down + Enter)
	m.HandleKey(tea.KeyMsg{Type: tea.KeyDown})
	m.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})

	if len(gotResults) != 2 {
		t.Fatalf("got %d results, want 2", len(gotResults))
	}
	if gotResults[0] != "/tmp" {
		t.Errorf("result[0] = %v, want /tmp", gotResults[0])
	}
	if gotResults[1] != "nvim" {
		t.Errorf("result[1] = %v, want nvim", gotResults[1])
	}
}

func TestConfirmContent_YConfirms(t *testing.T) {
	cc := NewConfirmContent("Delete this? [Y/n]")
	_, _, done := cc.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
	if !done {
		t.Error("expected done on Y")
	}
	if cc.Result().(bool) != true {
		t.Error("expected Result()=true on Y")
	}
}

func TestConfirmContent_EnterConfirms(t *testing.T) {
	cc := NewConfirmContent("Delete this? [Y/n]")
	_, _, done := cc.HandleKey(tea.KeyMsg{Type: tea.KeyEnter})
	if !done {
		t.Error("expected done on Enter")
	}
	if cc.Result().(bool) != true {
		t.Error("expected Result()=true on Enter")
	}
}

func TestConfirmContent_NRejects(t *testing.T) {
	cc := NewConfirmContent("Delete this? [Y/n]")
	_, _, done := cc.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if !done {
		t.Error("expected done on n")
	}
	if cc.Result().(bool) != false {
		t.Error("expected Result()=false on n")
	}
}

func TestConfirmContent_OtherKeyNoOp(t *testing.T) {
	cc := NewConfirmContent("Delete this? [Y/n]")
	_, _, done := cc.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if done {
		t.Error("expected no-op on arbitrary key")
	}
}

func TestNewConfirmModal_EscCallsOnCancel(t *testing.T) {
	cancelled := false
	m := NewModalOverlay("Confirm", []ModalStep{
		{Title: "", Content: NewConfirmContent("Do it? [Y/n]")},
	}, func(results []any) tea.Cmd {
		return nil
	}, func() tea.Cmd {
		cancelled = true
		return nil
	})
	m.HandleKey(tea.KeyMsg{Type: tea.KeyEsc})
	if !cancelled {
		t.Error("Esc should call OnCancel")
	}
}

func TestNewConfirmModal_NCallsOnCancel(t *testing.T) {
	var completedWith []any
	m := NewModalOverlay("Confirm", []ModalStep{
		{Title: "", Content: NewConfirmContent("Do it? [Y/n]")},
	}, func(results []any) tea.Cmd {
		completedWith = results
		return nil
	}, func() tea.Cmd {
		return nil
	})
	m.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
	if completedWith == nil {
		t.Fatal("expected OnComplete to be called")
	}
	if completedWith[0].(bool) != false {
		t.Error("expected Result()=false for N")
	}
}

func TestModalOverlay_SingleStepHidesCounter(t *testing.T) {
	m := NewModalOverlay("Test", []ModalStep{
		{Title: "Only step", Content: NewTextInputContent("Input:", nil)},
	}, func(results []any) tea.Cmd { return nil }, func() tea.Cmd { return nil })
	view := m.View("", 100, 40)
	if strings.Contains(view, "Step 1/1") {
		t.Error("single-step modal should not show step counter")
	}
}

func TestModalOverlay_MultiStepShowsCounter(t *testing.T) {
	m := NewModalOverlay("Test", []ModalStep{
		{Title: "Step 1", Content: NewTextInputContent("Input:", nil)},
		{Title: "Step 2", Content: NewSelectContent("Pick:", []string{"a"})},
	}, func(results []any) tea.Cmd { return nil }, func() tea.Cmd { return nil })
	view := m.View("", 100, 40)
	if !strings.Contains(view, "Step 1/2") {
		t.Error("multi-step modal should show step counter")
	}
}

func TestModalOverlay_FooterFn(t *testing.T) {
	m := NewModalOverlay("Test", []ModalStep{
		{Title: "", Content: NewConfirmContent("Do it?")},
	}, func(results []any) tea.Cmd { return nil }, func() tea.Cmd { return nil })
	m.FooterFn = func() string { return "custom footer" }
	view := m.View("", 100, 40)
	if !strings.Contains(view, "custom footer") {
		t.Error("expected custom footer in view")
	}
}
