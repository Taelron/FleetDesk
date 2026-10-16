package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// fleetTypeOrder returns sort priority for fleet types.
func fleetTypeOrder(t string) int {
	switch t {
	case "vm":
		return 0
	case "azure":
		return 1
	case "kubernetes":
		return 2
	case "probes":
		return 3
	default:
		return 4
	}
}

const configDir = ".config/fleetdesk"

// ConfigPath returns the full path to the fleetdesk config directory:
// $XDG_CONFIG_HOME/fleetdesk when that variable holds an absolute path,
// otherwise ~/.config/fleetdesk. A relative $XDG_CONFIG_HOME is ignored, as
// the XDG Base Directory specification requires.
func ConfigPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" && filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "fleetdesk")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, configDir)
}

// ScanFleets reads all .yaml files from the given directory (excluding config.yaml)
// and returns parsed fleet definitions.
func ScanFleets(dir string) ([]Fleet, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading config dir: %w", err)
	}

	var fleets []Fleet
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			continue
		}
		if name == "config.yaml" || name == "config.yml" {
			continue
		}
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		f, err := ParseFleetFile(path)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", name, err)
		}
		fleets = append(fleets, f)
	}

	// sort by type: vm → azure → kubernetes
	sort.Slice(fleets, func(i, j int) bool {
		oi, oj := fleetTypeOrder(fleets[i].Type), fleetTypeOrder(fleets[j].Type)
		if oi != oj {
			return oi < oj
		}
		return fleets[i].Name < fleets[j].Name
	})

	return fleets, nil
}
