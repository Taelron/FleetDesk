package app

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// sshHandoverFinishedMsg is sent when a terminal handover command completes.
type sshHandoverFinishedMsg struct {
	Err error
}

// editFinishedMsg is sent when the editor returns.
type editFinishedMsg struct {
	Err        error
	configEdit bool // true when config.yaml was edited (triggers config reload)
}

// sshExec wraps an SSH command with terminal handover.
type sshExec struct {
	entry  config.HostEntry
	args   []string
	banner string
	err    error
}

func (s *sshExec) Run() error {
	sep := strings.Repeat("\u2501", 50)
	fmt.Printf("\n%s\n\u25b6 %s\n%s\n\n", sep, s.banner, sep)

	// No StrictHostKeyChecking override: ssh's own prompt and known_hosts
	// decide, exactly as they do for the in-app dials.
	sshArgs := append([]string{"-t"}, ssh.SSHArgs(s.entry)...)
	sshArgs = append(sshArgs, s.args...)

	c := exec.Command("ssh", sshArgs...) //nolint:gosec // G204: ssh handover, argv, host/user/port from the user's own fleet YAML
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	s.err = c.Run()

	status := "\u2713 done"
	if s.err != nil {
		status = fmt.Sprintf("\u2717 %v", s.err)
	}
	fmt.Printf("\n%s\n%s\nPress Enter to return to fleetdesk...", sep, status)
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')

	return nil
}

func (s *sshExec) SetStdin(_ io.Reader)  {}
func (s *sshExec) SetStdout(_ io.Writer) {}
func (s *sshExec) SetStderr(_ io.Writer) {}

// editorExec wraps an editor command for terminal handover.
type editorExec struct {
	path   string
	editor string // resolved editor command
	err    error
}

func (e *editorExec) Run() error {
	editor := e.editor
	if editor == "" {
		editor = os.Getenv("EDITOR")
	}
	if editor == "" {
		editor = os.Getenv("VISUAL")
	}
	if editor == "" {
		editor = "vi"
	}

	// Split editor string to support "code --wait" style commands
	parts := strings.Fields(editor)
	if len(parts) == 0 {
		parts = []string{"vi"}
	}
	args := append(parts[1:], e.path)
	c := exec.Command(parts[0], args...) //nolint:gosec // G204: user-configured editor, argv
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	e.err = c.Run()
	return nil
}

func (e *editorExec) SetStdin(_ io.Reader)  {}
func (e *editorExec) SetStdout(_ io.Writer) {}
func (e *editorExec) SetStderr(_ io.Writer) {}

// editFleetFile opens the selected fleet file in the user's editor.
func (m Model) editFleetFile() tea.Cmd {
	f := m.fleets[m.fleetCursor]
	e := &editorExec{path: f.Path, editor: m.appCfg.Editor()}
	return tea.Exec(e, func(err error) tea.Msg {
		return editFinishedMsg{Err: e.err}
	})
}

// editConfigFile opens config.yaml in the user's editor.
func (m Model) editConfigFile() tea.Cmd {
	path := filepath.Join(config.ConfigPath(), "config.yaml")
	e := &editorExec{path: path, editor: m.appCfg.Editor()}
	return tea.Exec(e, func(err error) tea.Msg {
		return editFinishedMsg{Err: e.err, configEdit: true}
	})
}

// cmdExec wraps an arbitrary command with terminal handover.
type cmdExec struct {
	name   string
	args   []string
	banner string
	err    error
}

func (c *cmdExec) Run() error {
	sep := strings.Repeat("\u2501", 50)
	fmt.Printf("\n%s\n\u25b6 %s\n%s\n\n", sep, c.banner, sep)

	cmd := exec.Command(c.name, c.args...) //nolint:gosec // G204: shellQuote'd values, argv
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	c.err = cmd.Run()

	status := "\u2713 done"
	if c.err != nil {
		status = fmt.Sprintf("\u2717 %v", c.err)
	}
	fmt.Printf("\n%s\n%s\nPress Enter to return to fleetdesk...", sep, status)
	_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')

	return nil
}

func (c *cmdExec) SetStdin(_ io.Reader)  {}
func (c *cmdExec) SetStdout(_ io.Writer) {}
func (c *cmdExec) SetStderr(_ io.Writer) {}

// deployKeyScript is the bash script of the deploy-key handover: prepare
// ~/.ssh on the target, then copy the key. Both ssh invocations take their
// arguments from ssh.SSHArgs, so they reach the same machine as every other
// path, and neither disables host-key checking.
func deployKeyScript(e config.HostEntry) string {
	var quoted []string
	for _, a := range ssh.SSHArgs(e) {
		quoted = append(quoted, "'"+shellQuote(a)+"'")
	}
	args := strings.Join(quoted, " ")
	return fmt.Sprintf(
		"ssh -t %s 'sudo mkdir -p $HOME/.ssh && sudo chown -R $(id -u):$(id -g) $HOME && chmod 700 $HOME/.ssh' && ssh-copy-id %s",
		args, args)
}

// cmdHandover creates a tea.Cmd for terminal handover to an arbitrary command.
func cmdHandover(name string, args []string, banner string) tea.Cmd {
	e := &cmdExec{name: name, args: args, banner: banner}
	return tea.Exec(e, func(err error) tea.Msg {
		return sshHandoverFinishedMsg{Err: e.err}
	})
}

// sshHandover creates a tea.Cmd for terminal handover to an SSH command.
func sshHandover(h config.Host, args []string, banner string) tea.Cmd {
	e := &sshExec{
		entry:  h.Entry,
		args:   args,
		banner: banner,
	}
	return tea.Exec(e, func(err error) tea.Msg {
		return sshHandoverFinishedMsg{Err: e.err}
	})
}
