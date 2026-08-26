package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
)

const noteIcon = "\U0001f4dd " // 📝 + space

// notePrefix returns the note indicator for ref based on the cached count,
// or an empty string when no notes are recorded. The count cache is
// populated asynchronously — until loaded, the view renders without
// indicators (no flicker on first render).
func (m Model) notePrefix(ref notes.ResourceRef) string {
	if m.noteCounts[ref.Key()] > 0 {
		return noteIcon
	}
	return ""
}

// refreshNoteCountsCmd returns a Cmd that batch-loads note counts for the
// currently visible refs in the current view. Returns nil if the engine
// is not initialized or the view does not support notes.
func (m Model) refreshNoteCountsCmd() tea.Cmd {
	if m.noteEngine == nil {
		return nil
	}
	refs := m.refsInView()
	if len(refs) == 0 {
		return nil
	}
	return m.loadNoteCountsCmd(refs)
}
