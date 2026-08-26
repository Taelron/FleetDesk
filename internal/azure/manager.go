package azure

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Manager wraps az CLI execution for Azure fleet operations.
type Manager struct {
	mu      sync.Mutex
	ctx     context.Context
	cancel  context.CancelFunc
	version string // cached from az version
	logger  *slog.Logger
}

// NewManager creates a new Azure CLI manager.
func NewManager(logger *slog.Logger) *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		ctx:    ctx,
		cancel: cancel,
		logger: logger,
	}
}

// CheckPrerequisites verifies az CLI is installed and retrieves the CLI version.
// Must be called before other operations.
func (m *Manager) CheckPrerequisites() error {
	start := time.Now()
	m.logger.Debug("az prerequisites check start")

	// Check az binary exists
	if _, err := exec.LookPath("az"); err != nil {
		m.logger.Error("az not found", "err", err)
		return ErrAzNotInstalled
	}

	// Get version (also validates az works)
	out, err := m.RunCommand("version")
	if err != nil {
		return fmt.Errorf("az version check: %w", err)
	}

	ver, err := ParseCLIVersion(out)
	if err != nil {
		return fmt.Errorf("parsing az version: %w", err)
	}

	m.mu.Lock()
	m.version = ver
	m.mu.Unlock()

	m.logger.Debug("az prerequisites check complete", "version", ver, "elapsed", time.Since(start))
	return nil
}

// Version returns the cached az CLI version string.
func (m *Manager) Version() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.version
}

// RunCommand executes `az <args...>` and returns the raw stdout bytes.
// Appends --output json unless already specified. Returns CLIError on non-zero exit.
func (m *Manager) RunCommand(args ...string) ([]byte, error) {
	hasOutput := false
	for _, a := range args {
		if a == "--output" || a == "-o" {
			hasOutput = true
			break
		}
	}
	if !hasOutput {
		args = append(args, "--output", "json")
	}

	cmdDesc := strings.Join(args, " ")
	m.logger.Debug("az run start", "cmd", cmdDesc)
	start := time.Now()

	ctx, cancel := context.WithTimeout(m.ctx, 120*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "az", args...) //nolint:gosec // G204: az, argv, no shell
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := string(exitErr.Stderr)
			m.logger.Error("az run failed", "cmd", cmdDesc, "exit", exitErr.ExitCode(), "stderr", stderr, "elapsed", time.Since(start))

			if strings.Contains(stderr, "az login") ||
				strings.Contains(stderr, "AADSTS") ||
				strings.Contains(stderr, "Please run 'az login'") {
				return nil, ErrNotLoggedIn
			}

			return nil, &CLIError{
				Command:  cmdDesc,
				ExitCode: exitErr.ExitCode(),
				Stderr:   strings.TrimSpace(stderr),
			}
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrAzNotInstalled
		}
		return nil, fmt.Errorf("az exec: %w", err)
	}

	m.logger.Debug("az run complete", "cmd", cmdDesc, "bytes", len(out), "elapsed", time.Since(start))
	return out, nil
}

// Close cancels the root context and releases any resources held by the manager.
func (m *Manager) Close() {
	m.cancel()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.version = ""
}
