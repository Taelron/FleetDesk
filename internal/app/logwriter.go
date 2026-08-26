package app

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/fspath"
)

// logWriter manages saving streamed log output to a file.
// Usage: create with newLogWriter, write lines with WriteLine, close with Close.
type logWriter struct {
	file *os.File
}

// newLogWriter creates a log save file at:
//
//	<fleetDir>/logs/<fleet>/<host>/<source>-<datetime>.log
//
// Returns nil (no error) if the file cannot be created — log saving is best-effort.
func newLogWriter(fleetDir, fleetName, hostName, sourceName string) *logWriter {
	dir := filepath.Join(fleetDir, "logs", fspath.Sanitize(fleetName), fspath.Sanitize(hostName))
	_ = os.MkdirAll(dir, 0755) //nolint:gosec // G301: log dir 0755, known permission defect — TAE-42

	fileName := fmt.Sprintf("%s-%s.log", fspath.Sanitize(sourceName), time.Now().Format("2006-01-02_150405"))
	f, err := os.Create(filepath.Join(dir, fileName)) //nolint:gosec // G304: every dynamic segment goes through fspath.Sanitize
	if err != nil {
		return nil
	}
	return &logWriter{file: f}
}

// WriteLine writes a single line to the log file.
func (lw *logWriter) WriteLine(line string) {
	if lw == nil || lw.file == nil {
		return
	}
	lw.file.WriteString(line + "\n") //nolint:errcheck,gosec // TAE-82: a failed write still flashes "Saved N lines" as success
}

// WriteLines writes multiple lines to the log file.
func (lw *logWriter) WriteLines(lines []string) {
	if lw == nil || lw.file == nil {
		return
	}
	for _, line := range lines {
		lw.file.WriteString(line + "\n") //nolint:errcheck,gosec // TAE-82: a failed write still flashes "Saved N lines" as success
	}
}

// Close closes the log file. Safe to call on nil.
func (lw *logWriter) Close() {
	if lw == nil || lw.file == nil {
		return
	}
	_ = lw.file.Close()
	lw.file = nil
}

// Path returns the file path, or empty if no file is open.
func (lw *logWriter) Path() string {
	if lw == nil || lw.file == nil {
		return ""
	}
	return lw.file.Name()
}
