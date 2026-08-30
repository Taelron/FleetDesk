package app

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// baselineModel builds a Model for teatest UI tests.
// FleetDir is set to a placeholder so the first-run wizard does not fire.
func baselineModel(fleets []config.Fleet) Model {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	appCfg := config.AppConfig{FleetDir: "/tmp/fleetdesk-teatest"}
	return NewModel(fleets, appCfg, logger, ssh.NewManager(logger), "test", "none")
}

func TestTeatestBaseline_FleetPickerEmptyState(t *testing.T) {
	tm := teatest.NewTestModel(t, baselineModel(nil),
		teatest.WithInitialTermSize(100, 30),
	)

	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool {
			return bytes.Contains(bts, []byte("No fleet files found"))
		},
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}

func TestTeatestBaseline_FleetPickerRendersFleets(t *testing.T) {
	fleets := []config.Fleet{
		{Name: "test-vm", Type: "vm", Path: "/tmp/test-vm.yaml"},
		{Name: "test-azure", Type: "azure", Path: "/tmp/test-azure.yaml"},
	}
	tm := teatest.NewTestModel(t, baselineModel(fleets),
		teatest.WithInitialTermSize(100, 30),
	)

	teatest.WaitFor(t, tm.Output(),
		func(bts []byte) bool {
			return bytes.Contains(bts, []byte("test-vm")) &&
				bytes.Contains(bts, []byte("test-azure"))
		},
		teatest.WithDuration(2*time.Second),
	)

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	tm.WaitFinished(t, teatest.WithFinalTimeout(2*time.Second))
}
