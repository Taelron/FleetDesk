package notes

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	// filenameLayout is the timestamp layout used for note filenames. It
	// uses "-" in place of ":" so the name is filesystem-safe on all
	// platforms, plus ".%03d" milliseconds to avoid collisions within the
	// same second.
	filenameLayout = "2006-01-02T15-04-05"
	noteSuffix     = "_note.txt"
	previewMaxLen  = 80
)

// Engine provides CRUD operations over notes stored under a base directory.
// Typical usage: notes.New(appCfg.FleetDir).
type Engine struct {
	base string // {fleet_dir}/notes
}

// New returns an Engine rooted under fleetDir/notes. It does not create the
// directory — directories are created on demand by Create.
func New(fleetDir string) *Engine {
	return &Engine{base: filepath.Join(fleetDir, "notes")}
}

// Count returns the number of notes stored for the given resource. Returns
// 0 for non-existent directories.
func (e *Engine) Count(ref ResourceRef) int {
	entries, err := os.ReadDir(ref.Dir(e.base))
	if err != nil {
		return 0
	}
	n := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if strings.HasSuffix(entry.Name(), noteSuffix) {
			n++
		}
	}
	return n
}

// List returns all notes for the given resource, sorted newest-first by
// filename (the filename includes the creation timestamp). A non-existent
// directory returns (nil, nil) — this is not an error condition.
func (e *Engine) List(ref ResourceRef) ([]Note, error) {
	dir := ref.Dir(e.base)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading notes dir: %w", err)
	}

	notes := make([]Note, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, noteSuffix) {
			continue
		}
		path := filepath.Join(dir, name)
		created, ok := parseTimestamp(name)
		if !ok {
			continue
		}
		preview, _ := readPreview(path)
		notes = append(notes, Note{
			Path:      path,
			CreatedAt: created,
			Preview:   preview,
		})
	}

	sort.Slice(notes, func(i, j int) bool {
		return notes[i].CreatedAt.After(notes[j].CreatedAt)
	})
	return notes, nil
}

// Create creates a new empty note file at the resource's directory and
// returns its absolute path. The filename is a UTC timestamp with
// millisecond precision; parent directories are created as needed.
//
// The returned path is intended to be opened in the user's editor via the
// usual terminal-handover flow.
func (e *Engine) Create(ref ResourceRef) (string, error) {
	dir := ref.Dir(e.base)
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // G301: notes dir 0755, known permission defect — TAE-42
		return "", fmt.Errorf("creating notes dir: %w", err)
	}
	name := filename(time.Now().UTC())
	path := filepath.Join(dir, name)
	f, err := os.Create(path) //nolint:gosec // G304: dir already sanitised per segment, name a generated timestamp
	if err != nil {
		return "", fmt.Errorf("creating note file: %w", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing note file: %w", err)
	}
	return path, nil
}

// Delete removes the given note file. Non-existent files are not an error.
func (e *Engine) Delete(notePath string) error {
	if err := os.Remove(notePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("deleting note: %w", err)
	}
	return nil
}

// filename builds a note filename for the given timestamp.
// Format: 2006-01-02T15-04-05.123_note.txt
func filename(t time.Time) string {
	return fmt.Sprintf("%s.%03d%s", t.Format(filenameLayout), t.Nanosecond()/int(time.Millisecond), noteSuffix)
}

// parseTimestamp extracts the creation time from a note filename. Returns
// (_, false) when the filename does not match the expected layout.
func parseTimestamp(name string) (time.Time, bool) {
	if !strings.HasSuffix(name, noteSuffix) {
		return time.Time{}, false
	}
	stem := strings.TrimSuffix(name, noteSuffix)
	// stem = "2006-01-02T15-04-05.123"
	dot := strings.LastIndex(stem, ".")
	if dot < 0 {
		return time.Time{}, false
	}
	datePart := stem[:dot]
	msPart := stem[dot+1:]
	t, err := time.Parse(filenameLayout, datePart)
	if err != nil {
		return time.Time{}, false
	}
	var ms int
	if _, err := fmt.Sscanf(msPart, "%d", &ms); err != nil {
		return time.Time{}, false
	}
	return t.Add(time.Duration(ms) * time.Millisecond), true
}

// readPreview returns the first non-empty line of the note, truncated.
func readPreview(path string) (string, error) {
	data, err := os.ReadFile(path) //nolint:gosec // G304: path from an os.ReadDir listing of a sanitised note directory
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if len(line) > previewMaxLen {
			return line[:previewMaxLen] + "\u2026", nil
		}
		return line, nil
	}
	return "", nil
}
