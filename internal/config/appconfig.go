// Package config loads and validates FleetDesk's fleet definitions and
// application configuration.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// ErrNoConfig is returned when the config file does not exist.
var ErrNoConfig = errors.New("config file not found")

// AppConfig holds the application-level configuration.
type AppConfig struct {
	FleetDir string `yaml:"fleet_dir"`
	editor   string // unexported — access via Editor()
}

// configFile is the on-disk YAML representation.
type configFile struct {
	FleetDir string `yaml:"fleet_dir"`
	Editor   string `yaml:"editor"`
}

// Editor returns the configured editor, falling back to $EDITOR, $VISUAL, then vi.
func (c AppConfig) Editor() string {
	if c.editor != "" {
		return c.editor
	}
	if e := strings.TrimSpace(os.Getenv("EDITOR")); e != "" {
		return e
	}
	if v := strings.TrimSpace(os.Getenv("VISUAL")); v != "" {
		return v
	}
	return "vi"
}

// LoadAppConfig reads and validates config.yaml from the given config directory.
func LoadAppConfig(configDir string) (AppConfig, error) {
	path := filepath.Join(configDir, "config.yaml")
	data, err := os.ReadFile(path) //nolint:gosec // G304: constant config dir
	if err != nil {
		if os.IsNotExist(err) {
			return AppConfig{}, ErrNoConfig
		}
		return AppConfig{}, fmt.Errorf("reading config: %w", err)
	}

	var cf configFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return AppConfig{}, fmt.Errorf("parsing config: %w", err)
	}

	if cf.FleetDir == "" {
		return AppConfig{}, fmt.Errorf("fleet_dir is required in config.yaml")
	}

	// Expand tilde
	fleetDir := expandTilde(cf.FleetDir)

	// Validate directory exists and is readable+writable
	if err := validateFleetDir(fleetDir); err != nil {
		return AppConfig{}, err
	}

	return AppConfig{
		FleetDir: fleetDir,
		editor:   cf.Editor,
	}, nil
}

// WriteDefaultAppConfig creates config.yaml with the given values.
func WriteDefaultAppConfig(configDir, fleetDir, editor string) error {
	if err := os.MkdirAll(configDir, 0755); err != nil { //nolint:gosec // G301: config dir 0755, known permission defect — TAE-23
		return fmt.Errorf("creating config dir: %w", err)
	}

	cf := configFile{
		FleetDir: fleetDir,
		Editor:   editor,
	}
	data, err := yaml.Marshal(&cf)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	path := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil { //nolint:gosec // G306: config.yaml 0644, known permission defect — TAE-23
		return fmt.Errorf("writing config: %w", err)
	}
	return nil
}

// expandTilde replaces a leading ~/ (or bare ~) with the user's home directory.
// Does not expand ~user paths.
func expandTilde(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return path
		}
		return home
	}
	if !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	return filepath.Join(home, path[2:])
}

// validateFleetDir checks that the directory exists and is readable+writable.
func validateFleetDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("fleet directory does not exist: %s", dir)
		}
		return fmt.Errorf("checking fleet directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("fleet_dir is not a directory: %s", dir)
	}

	// Check read+write by attempting to open for reading and create a temp file
	f, err := os.CreateTemp(dir, ".fleetdesk-check-*")
	if err != nil {
		return fmt.Errorf("fleet directory is not writable: %s", dir)
	}
	_ = f.Close()
	_ = os.Remove(f.Name())

	return nil
}

// ValidateFleetDir is the exported version for use by the wizard.
func ValidateFleetDir(path string) error {
	return validateFleetDir(expandTilde(path))
}
