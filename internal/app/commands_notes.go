package app

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
)

// --- Messages for the notes feature ---

// noteListLoadedMsg is sent after the note list for the current ref is loaded.
type noteListLoadedMsg struct {
	ref   notes.ResourceRef
	notes []notes.Note
	err   error
}

// noteReadLoadedMsg is sent after a note file's contents have been read.
type noteReadLoadedMsg struct {
	path    string
	content string
	err     error
}

// noteCreatedMsg is sent after a new (empty) note file has been created on disk.
// Triggers an editor handover; on editor exit, we reload the list and drop
// empty files.
type noteCreatedMsg struct {
	ref  notes.ResourceRef
	path string
	err  error
}

// noteEditFinishedMsg is sent when the editor returns after a note create/edit.
type noteEditFinishedMsg struct {
	path      string // path of the note being edited (may be the just-created empty file)
	createdAt bool   // true if this was the post-create flow (delete if still empty)
}

// noteDeletedMsg is sent after Engine.Delete completes.
type noteDeletedMsg struct {
	path string
	err  error
}

// noteCountsLoadedMsg is sent after a batch stat of note counts for refs in
// the current view (FLE-79).
type noteCountsLoadedMsg struct {
	counts map[string]int
}

// --- Commands ---

// loadNoteListCmd reads notes for ref and emits noteListLoadedMsg.
func (m Model) loadNoteListCmd(ref notes.ResourceRef) tea.Cmd {
	engine := m.noteEngine
	return func() tea.Msg {
		items, err := engine.List(ref)
		return noteListLoadedMsg{ref: ref, notes: items, err: err}
	}
}

// loadNoteReadCmd reads a single note file and emits noteReadLoadedMsg.
func (m Model) loadNoteReadCmd(path string) tea.Cmd {
	return func() tea.Msg {
		data, err := os.ReadFile(path) //nolint:gosec // G304: path from an os.ReadDir listing of a sanitised note directory
		if err != nil {
			return noteReadLoadedMsg{path: path, err: err}
		}
		return noteReadLoadedMsg{path: path, content: string(data)}
	}
}

// createNoteCmd creates an empty note file for ref and emits noteCreatedMsg.
func (m Model) createNoteCmd(ref notes.ResourceRef) tea.Cmd {
	engine := m.noteEngine
	return func() tea.Msg {
		path, err := engine.Create(ref)
		return noteCreatedMsg{ref: ref, path: path, err: err}
	}
}

// editNoteCmd opens the given note path in the user's editor. On exit, a
// noteEditFinishedMsg fires. `afterCreate` distinguishes the post-create
// flow (where we drop the file if still empty) from a normal edit.
func (m Model) editNoteCmd(path string, afterCreate bool) tea.Cmd {
	e := &editorExec{path: path, editor: m.appCfg.Editor()}
	return tea.Exec(e, func(err error) tea.Msg {
		return noteEditFinishedMsg{path: path, createdAt: afterCreate}
	})
}

// deleteNoteCmd removes a note file and emits noteDeletedMsg.
func (m Model) deleteNoteCmd(path string) tea.Cmd {
	engine := m.noteEngine
	return func() tea.Msg {
		err := engine.Delete(path)
		return noteDeletedMsg{path: path, err: err}
	}
}

// loadNoteCountsCmd stats each ref's note directory and emits
// noteCountsLoadedMsg with a map keyed by ref.Key().
func (m Model) loadNoteCountsCmd(refs []notes.ResourceRef) tea.Cmd {
	engine := m.noteEngine
	if engine == nil || len(refs) == 0 {
		return nil
	}
	return func() tea.Msg {
		counts := make(map[string]int, len(refs))
		for _, r := range refs {
			counts[r.Key()] = engine.Count(r)
		}
		return noteCountsLoadedMsg{counts: counts}
	}
}

// fileIsEmpty reports whether the file at path has zero bytes of
// non-whitespace content. Used to clean up abandoned create flows.
func fileIsEmpty(path string) bool {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from an os.ReadDir listing of a sanitised note directory
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == ""
}
