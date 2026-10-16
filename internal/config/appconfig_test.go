package config

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestLoadAppConfig_FileNotFound(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadAppConfig(dir)
	if err != ErrNoConfig {
		t.Errorf("got %v, want ErrNoConfig", err)
	}
}

func TestLoadAppConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(":::invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAppConfig(dir)
	if err == nil || err == ErrNoConfig {
		t.Errorf("expected parse error, got %v", err)
	}
}

func TestLoadAppConfig_MissingFleetDir(t *testing.T) {
	dir := t.TempDir()
	content := "editor: vim\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAppConfig(dir)
	if err == nil {
		t.Error("expected validation error for missing fleet_dir")
	}
}

func TestLoadAppConfig_DirNotExist(t *testing.T) {
	dir := t.TempDir()
	content := "fleet_dir: /nonexistent/path/that/does/not/exist\neditor: vim\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAppConfig(dir)
	if err == nil {
		t.Error("expected error for non-existent fleet_dir")
	}
}

func TestLoadAppConfig_Valid(t *testing.T) {
	dir := t.TempDir()
	fleetDir := t.TempDir()
	content := "fleet_dir: " + fleetDir + "\neditor: nvim\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadAppConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FleetDir != fleetDir {
		t.Errorf("FleetDir = %q, want %q", cfg.FleetDir, fleetDir)
	}
	if cfg.Editor() != "nvim" {
		t.Errorf("Editor() = %q, want %q", cfg.Editor(), "nvim")
	}
}

func TestAppConfig_Editor_ConfigWins(t *testing.T) {
	t.Setenv("EDITOR", "emacs")
	t.Setenv("VISUAL", "code")
	cfg := AppConfig{FleetDir: "/tmp", editor: "nvim"}
	if got := cfg.Editor(); got != "nvim" {
		t.Errorf("Editor() = %q, want %q", got, "nvim")
	}
}

func TestAppConfig_Editor_EnvFallback(t *testing.T) {
	t.Setenv("EDITOR", "emacs")
	t.Setenv("VISUAL", "code")
	cfg := AppConfig{FleetDir: "/tmp"}
	if got := cfg.Editor(); got != "emacs" {
		t.Errorf("Editor() = %q, want %q", got, "emacs")
	}
}

func TestAppConfig_Editor_VisualFallback(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "code")
	cfg := AppConfig{FleetDir: "/tmp"}
	if got := cfg.Editor(); got != "code" {
		t.Errorf("Editor() = %q, want %q", got, "code")
	}
}

func TestAppConfig_Editor_Default(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	cfg := AppConfig{FleetDir: "/tmp"}
	if got := cfg.Editor(); got != "vi" {
		t.Errorf("Editor() = %q, want %q", got, "vi")
	}
}

func TestWriteDefaultAppConfig(t *testing.T) {
	dir := t.TempDir()
	fleetDir := t.TempDir()
	err := WriteDefaultAppConfig(dir, fleetDir, "nvim")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// verify file was created and is readable
	cfg, err := LoadAppConfig(dir)
	if err != nil {
		t.Fatalf("LoadAppConfig after write: %v", err)
	}
	if cfg.FleetDir != fleetDir {
		t.Errorf("FleetDir = %q, want %q", cfg.FleetDir, fleetDir)
	}
	if cfg.Editor() != "nvim" {
		t.Errorf("Editor() = %q, want %q", cfg.Editor(), "nvim")
	}
}

func TestValidateAppConfig_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	// Create a temp dir inside home to test tilde expansion
	dir := t.TempDir()
	fleetDir := t.TempDir()

	// Write config with ~/... path
	rel, err := filepath.Rel(home, fleetDir)
	if err != nil {
		t.Skip("fleet dir not under home")
	}
	content := "fleet_dir: ~/" + rel + "\neditor: vim\n"
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadAppConfig(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.FleetDir != fleetDir {
		t.Errorf("FleetDir = %q, want %q (tilde not expanded)", cfg.FleetDir, fleetDir)
	}
}

func TestLoadAppConfig_ModeCheck(t *testing.T) {
	cases := []struct {
		mode    os.FileMode
		wantErr string
	}{
		{0o600, ""},
		{0o644, "config.yaml is too permissive (mode 0644); expected 0600"},
		{0o640, "config.yaml is too permissive (mode 0640); expected 0600"},
		{0o604, "config.yaml is too permissive (mode 0604); expected 0600"},
		{0o400, "config.yaml has mode 0400; expected 0600"},
	}
	for _, c := range cases {
		t.Run(c.mode.String(), func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte("fleet_dir: "+dir+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, c.mode); err != nil {
				t.Fatal(err)
			}
			_, err := LoadAppConfig(dir)
			if c.wantErr == "" {
				if err != nil {
					t.Fatalf("0600 must load: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), c.wantErr) || !strings.Contains(err.Error(), "chmod 0600 "+path) {
				t.Fatalf("want error containing %q and the chmod fix, got %v", c.wantErr, err)
			}
		})
	}
}

func TestWriteDefaultAppConfig_OwnerOnly(t *testing.T) {
	old := syscall.Umask(0)
	defer syscall.Umask(old)
	dir := filepath.Join(t.TempDir(), "fleetdesk")
	if err := WriteDefaultAppConfig(dir, t.TempDir(), "vim"); err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("config dir mode = %04o, want 0700", di.Mode().Perm())
	}
	fi, err := os.Stat(filepath.Join(dir, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("config.yaml mode = %04o, want 0600", fi.Mode().Perm())
	}
	if _, err := LoadAppConfig(dir); err != nil {
		t.Errorf("a file FleetDesk wrote must load: %v", err)
	}
}

func TestConfigPath_XDG(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cases := []struct{ name, xdg, want string }{
		{"absolute", "/srv/xdg", "/srv/xdg/fleetdesk"},
		{"relative is ignored", "relative/dir", filepath.Join(home, ".config/fleetdesk")},
		{"unset", "", filepath.Join(home, ".config/fleetdesk")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", c.xdg)
			if got := ConfigPath(); got != c.want {
				t.Errorf("ConfigPath() = %q, want %q", got, c.want)
			}
		})
	}
}

// A config.yaml that is not ours is reported as such, not as the "permission
// denied" the open of an unreadable foreign file would give.
func TestLoadAppConfig_ForeignOwnerReportedBeforeOpen(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("running as root")
	}
	if _, err := os.Stat("/etc/shadow"); err != nil {
		t.Skip("no /etc/shadow")
	}
	dir := t.TempDir()
	if err := os.Symlink("/etc/shadow", filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatal(err)
	}
	_, err := LoadAppConfig(dir)
	if err == nil || !strings.Contains(err.Error(), "is owned by uid 0, not by you") {
		t.Fatalf("got %v, want the ownership message", err)
	}
}
