package app

import (
	"bytes"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// newNotesTestModel builds a Model with a real notes engine rooted at fleetDir.
// Used by FLE-78 UI tests that need to seed note files on disk.
func newNotesTestModel(t *testing.T, fleetDir string, fleets []config.Fleet) Model {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	appCfg := config.AppConfig{FleetDir: fleetDir}
	return NewModel(fleets, appCfg, logger, ssh.NewManager(logger), "test", "none")
}

// seedNote writes a note file directly on disk to simulate a pre-existing note.
func seedNote(t *testing.T, fleetDir string, ref notes.ResourceRef, content string) {
	t.Helper()
	dir := ref.Dir(filepath.Join(fleetDir, "notes"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	name := time.Now().UTC().Format("2006-01-02T15-04-05.000") + "_note.txt"
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	// Sleep 1ms to avoid filename collision on rapid seeds within the same test.
	time.Sleep(time.Millisecond)
}

func TestNotes_N_OnHostList_OpensEmptyNoteList(t *testing.T) {
	fleetDir := t.TempDir()
	m := newNotesTestModel(t, fleetDir, []config.Fleet{{Name: "test", Type: "vm"}})
	m.view = viewHostList
	m.selectedFleet = 0
	m.hosts = []config.Host{{Entry: config.HostEntry{Name: "host-a"}, Status: config.HostOnline}}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool {
			// Note List breadcrumb includes the fleet + resource path
			return bytes.Contains(bts, []byte("Notes")) &&
				bytes.Contains(bts, []byte("host-a")) &&
				bytes.Contains(bts, []byte("No notes for this resource"))
		},
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestNotes_N_OnHostList_ShowsExistingNotes(t *testing.T) {
	fleetDir := t.TempDir()
	ref := notes.ResourceRef{Fleet: "test", Segments: []string{"hosts", "host-a"}}
	seedNote(t, fleetDir, ref, "first note about host-a")
	seedNote(t, fleetDir, ref, "second note later")

	m := newNotesTestModel(t, fleetDir, []config.Fleet{{Name: "test", Type: "vm"}})
	m.view = viewHostList
	m.selectedFleet = 0
	m.hosts = []config.Host{{Entry: config.HostEntry{Name: "host-a"}, Status: config.HostOnline}}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool {
			return bytes.Contains(bts, []byte("DATE")) &&
				bytes.Contains(bts, []byte("PREVIEW")) &&
				bytes.Contains(bts, []byte("first note about host-a")) &&
				bytes.Contains(bts, []byte("second note later"))
		},
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestNotes_NoteList_Esc_ReturnsToHostList(t *testing.T) {
	fleetDir := t.TempDir()
	m := newNotesTestModel(t, fleetDir, []config.Fleet{{Name: "test", Type: "vm"}})
	m.view = viewHostList
	m.selectedFleet = 0
	m.hosts = []config.Host{{Entry: config.HostEntry{Name: "host-a"}, Status: config.HostOnline}}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool { return bytes.Contains(bts, []byte("No notes for this resource")) },
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool {
			// Back on Host List: "HOST" header + host-a visible, no "No notes" message
			return bytes.Contains(bts, []byte("HOST")) &&
				bytes.Contains(bts, []byte("host-a")) &&
				!bytes.Contains(bts, []byte("No notes for this resource"))
		},
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestNotes_Indicator_AppearsOnHostWithNotes(t *testing.T) {
	fleetDir := t.TempDir()
	// Seed a note on host-b only.
	seedNote(t, fleetDir, notes.ResourceRef{Fleet: "test", Segments: []string{"hosts", "host-b"}}, "operational note")

	m := newNotesTestModel(t, fleetDir, []config.Fleet{{Name: "test", Type: "vm"}})
	m.view = viewHostList
	m.selectedFleet = 0
	m.hosts = []config.Host{
		{Entry: config.HostEntry{Name: "host-a"}, Status: config.HostOnline, OS: "RHEL 9"},
		{Entry: config.HostEntry{Name: "host-b"}, Status: config.HostOnline, OS: "RHEL 9"},
	}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	// The count load fires via Init(). WaitFor the indicator to appear on host-b.
	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool {
			// The note icon 📝 should appear before host-b but not host-a.
			lines := bytes.Split(bts, []byte("\n"))
			hostARow := findLineContaining(lines, []byte("host-a"))
			hostBRow := findLineContaining(lines, []byte("host-b"))
			if hostARow == nil || hostBRow == nil {
				return false
			}
			return bytes.Contains(hostBRow, []byte("\U0001f4dd")) &&
				!bytes.Contains(hostARow, []byte("\U0001f4dd"))
		},
		teatest.WithDuration(3*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func findLineContaining(lines [][]byte, needle []byte) []byte {
	for _, line := range lines {
		if bytes.Contains(line, needle) {
			return line
		}
	}
	return nil
}

func TestNotes_NoteList_DeleteShowsConfirmModal(t *testing.T) {
	fleetDir := t.TempDir()
	ref := notes.ResourceRef{Fleet: "test", Segments: []string{"hosts", "host-a"}}
	seedNote(t, fleetDir, ref, "important context from the incident")

	m := newNotesTestModel(t, fleetDir, []config.Fleet{{Name: "test", Type: "vm"}})
	m.view = viewHostList
	m.selectedFleet = 0
	m.hosts = []config.Host{{Entry: config.HostEntry{Name: "host-a"}, Status: config.HostOnline}}

	tm := teatest.NewTestModel(t, m, teatest.WithInitialTermSize(120, 30))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool { return bytes.Contains(bts, []byte("important context from the incident")) },
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool {
			return bytes.Contains(bts, []byte("Delete note")) &&
				bytes.Contains(bts, []byte("important context from the incident"))
		},
		teatest.WithDuration(2*time.Second),
	)

	// Cancel the modal — ensures we don't actually delete during test.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
