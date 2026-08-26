// Package logging provides FleetDesk's structured application logger.
package logging

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/fspath"
)

// openFiles tracks log file handles for cleanup.
var openFiles []*os.File

// CloseAll closes all open log file handles.
func CloseAll() {
	for _, f := range openFiles {
		_ = f.Close()
	}
	openFiles = nil
}

// LogDir returns the default log directory path.
func LogDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".local", "share", "fleetdesk")
}

// InitLogger creates the global logger.
// When debug=false, returns a no-op logger.
// When debug=true, writes to dir/debug.log (truncated on startup).
func InitLogger(debug bool, dir string) *slog.Logger {
	if !debug {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	_ = os.MkdirAll(dir, 0755)                           //nolint:gosec // G301: log dir 0755, known permission defect — TAE-42
	f, err := os.Create(filepath.Join(dir, "debug.log")) //nolint:gosec // G304: constant dir, literal filename
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	openFiles = append(openFiles, f)
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// multiHandler fans out log records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, r.Level) {
			if err := handler.Handle(ctx, r); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithAttrs(attrs)
	}
	return &multiHandler{handlers: handlers}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		handlers[i] = handler.WithGroup(name)
	}
	return &multiHandler{handlers: handlers}
}

// NewTargetLogger creates a per-target logger that writes to both
// the global log and a target-specific file.
// prefix: "host", "sub", "ctx". name: target name.
func NewTargetLogger(global *slog.Logger, debug bool, dir string, prefix, name string) *slog.Logger {
	if !debug {
		return global
	}
	_ = os.MkdirAll(dir, 0755) //nolint:gosec // G301: log dir 0755, known permission defect — TAE-42
	filename := prefix + "-" + fspath.Sanitize(name) + ".log"
	f, err := os.Create(filepath.Join(dir, filename)) //nolint:gosec // G304: constant dir, sanitised filename
	if err != nil {
		return global
	}
	openFiles = append(openFiles, f)
	targetHandler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelDebug})
	// combine global handler + target handler
	combined := &multiHandler{
		handlers: []slog.Handler{
			global.Handler(),
			targetHandler,
		},
	}
	return slog.New(combined)
}
