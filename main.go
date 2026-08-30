// Command fleetdesk is a terminal UI for managing fleets of Linux VMs over
// SSH, Azure resources via the az CLI, and Kubernetes clusters via kubectl.
package main

import (
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/app"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/logging"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

var (
	version = "dev"
	commit  = "none"
)

func main() {
	debug := false
	showVersion := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "--debug":
			debug = true
		case "--version":
			showVersion = true
		}
	}

	if showVersion {
		fmt.Printf("fleetdesk %s (%s)\n", version, commit)
		os.Exit(0)
	}

	// Ensure config directory exists
	configDir := config.ConfigPath()
	if configDir != "" {
		if err := os.MkdirAll(configDir, 0755); err != nil { //nolint:gosec // G301: config dir 0755, known permission defect — TAE-23
			fmt.Fprintf(os.Stderr, "error creating config dir: %v\n", err)
			os.Exit(1)
		}
	}

	// Load app config
	appCfg, err := config.LoadAppConfig(configDir)
	if err != nil && err != config.ErrNoConfig {
		fmt.Fprintf(os.Stderr, "error loading config: %v\n", err)
		os.Exit(1)
	}

	// Scan fleets (only if config exists and has a fleet dir)
	var fleets []config.Fleet
	if appCfg.FleetDir != "" {
		fleets, err = config.ScanFleets(appCfg.FleetDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error scanning fleets: %v\n", err)
			os.Exit(1)
		}
	}

	logger := logging.InitLogger(debug, logging.LogDir())
	defer logging.CloseAll()
	logger.Info("fleetdesk starting", "version", version, "debug", debug, "fleets", len(fleets))

	sm := ssh.NewManager(logger)
	sm.SetCredentialTimeout(appCfg.CredentialTimeout)
	defer sm.Close()

	m := app.NewModel(fleets, appCfg, logger, sm, version, commit)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// If wizard was cancelled or failed, show message
	if fm, ok := finalModel.(app.Model); ok && fm.WizardCancelled() {
		if wizErr := fm.WizardError(); wizErr != nil {
			fmt.Fprintf(os.Stderr, "Setup failed: %v\n", wizErr)
		} else {
			fmt.Println("FleetDesk requires a configuration file. Run fleetdesk again to complete setup.")
		}
	}
}
