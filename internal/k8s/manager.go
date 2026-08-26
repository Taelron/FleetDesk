package k8s

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

// ErrKubectlNotInstalled indicates kubectl was not found on PATH.
var ErrKubectlNotInstalled = errors.New("kubectl not found in PATH")

// Manager wraps kubectl execution for Kubernetes fleet operations.
type Manager struct {
	logger *slog.Logger
}

// NewManager creates a new kubectl manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{logger: logger}
}

// CheckPrerequisites verifies kubectl is installed.
func (m *Manager) CheckPrerequisites() error {
	if _, err := exec.LookPath("kubectl"); err != nil {
		return ErrKubectlNotInstalled
	}
	return nil
}

// RunCommand executes `kubectl <args...>` and returns stdout.
func (m *Manager) RunCommand(args ...string) ([]byte, error) {
	cmdDesc := strings.Join(args, " ")
	m.logger.Debug("kubectl run start", "cmd", cmdDesc)
	start := time.Now()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "kubectl", args...) //nolint:gosec // G204: kubectl, argv, no shell
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			m.logger.Error("kubectl run failed", "cmd", cmdDesc, "exit", exitErr.ExitCode(), "stderr", stderr, "elapsed", time.Since(start))
			return nil, fmt.Errorf("kubectl %s: %s", cmdDesc, stderr)
		}
		if errors.Is(err, exec.ErrNotFound) {
			return nil, ErrKubectlNotInstalled
		}
		return nil, fmt.Errorf("kubectl exec: %w", err)
	}

	m.logger.Debug("kubectl run complete", "cmd", cmdDesc, "bytes", len(out), "elapsed", time.Since(start))
	return out, nil
}

// Close releases any resources.
func (m *Manager) Close() {}
