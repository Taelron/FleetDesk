package app

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/crypto/ssh"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
	fdssh "github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// SSHStreamConfig configures a generic SSH command stream rendered in-TUI.
type SSHStreamConfig struct {
	Command     string // shell command to run (sudo will be rewritten automatically)
	Title       string // display title in breadcrumb
	SourceName  string // clean name for file save (no special chars)
	ReturnView  view   // view to return to on Esc
	HostIdx     int    // host index for SSH connection
	Sudo        bool   // whether to use sudo rewrite
	NewestFirst bool   // true = prepend lines (log tail), false = append (command output)
	AutoDone    bool   // true = show "Done" message when command finishes (for one-shot commands)
	// Secret, when set, names a secret the command needs. It is resolved at
	// the point of use — inside the stream, after the sudo prompt and before
	// the session opens — and delivered on stdin; no message ever carries it.
	Secret *SecretConsumer
}

// SecretConsumer describes a secret an SSH stream resolves and feeds to a
// command running as root on the target.
type SecretConsumer struct {
	// Resolve returns the secret. The stream erases the result as soon as the
	// reader that delivers it owns its own copy.
	Resolve func() ([]byte, error)
	// Tag labels the secret's line on the consumer's stdin ("<Tag>=<value>").
	Tag string
	// Consumer is the command that reads that line, run as root via sh -c.
	Consumer string
	// ExitErrors maps an exit status of the rewritten command to the message
	// the user sees for it.
	ExitErrors map[int]string
}

// sshStream holds the runtime state for a generic SSH stream view.
// These fields live on Model — this struct documents the contract.
// Model fields: streamLines, streamCursor, streamCancel, streamChan,
//               streamStreaming, streamTitle, streamReturnView, streamNewestFirst, streamAutoDone

type sshStreamBatchMsg struct {
	lines      []string
	generation int
}

type sshStreamDoneMsg struct {
	generation int
	exitCode   int
	// err is why the stream could not run or did not succeed in a way the
	// user must be told: the secret could not be resolved or delivered, or the
	// command exited with a status its consumer maps to a message. It never
	// carries the secret.
	err error
}

// startSSHStream launches a generic SSH command stream.
func (m *Model) startSSHStream(cfg SSHStreamConfig) tea.Cmd {
	if m.streamStreaming {
		m.stopSSHStream()
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.streamCancel = cancel
	m.streamStreaming = true
	m.streamLines = nil
	m.streamCursor = 0
	m.streamTitle = cfg.Title
	m.streamSourceName = cfg.SourceName
	if m.streamSourceName == "" {
		m.streamSourceName = cfg.Title // fallback
	}
	m.streamReturnView = cfg.ReturnView
	m.streamNewestFirst = cfg.NewestFirst
	m.streamAutoDone = cfg.AutoDone
	m.streamGeneration++
	m.streamLastConfig = &cfg
	m.view = viewSSHStream

	ch := make(chan string, 200)
	m.streamChan = ch

	exitCode := new(int) // shared with goroutine; read after channel close
	m.streamExitCode = exitCode
	streamErr := new(error) // shared with goroutine; read after channel close
	m.streamErr = streamErr

	sm := m.ssh
	idx := cfg.HostIdx
	cmd := cfg.Command
	logger := m.logger

	secretCfg := cfg.Secret

	go func() {
		defer close(ch)
		fail := func(err error) {
			ch <- "ERROR: " + err.Error()
			*streamErr = err
		}

		client := sm.GetConnection(idx)
		if client == nil {
			ch <- "ERROR: no SSH connection to host"
			return
		}

		// The point of use: the secret is resolved here, after the sudo
		// prompt and before the session exists, and only on this goroutine.
		finalCmd, sudoReader, err := prepareStreamCommand(sm, idx, cmd, secretCfg)
		if err != nil {
			fail(err)
			return
		}
		// Whatever happens next, the reader's copy does not outlive the stream.
		if sudoReader != nil {
			defer sudoReader.Erase()
		}

		session, err := client.NewSession()
		if err != nil {
			ch <- fmt.Sprintf("ERROR: SSH session: %v", err)
			return
		}
		defer func() { _ = session.Close() }()

		// Merge stderr so we see errors
		finalCmd = finalCmd + " 2>&1"

		logCmd := finalCmd
		if strings.Contains(logCmd, "| sudo -S") {
			logCmd = "[sudo-rewritten]"
		}
		logger.Debug("ssh stream start", "cmd_prefix", logCmd[:min(len(logCmd), 60)])

		// Assign only when non-nil: a nil *credential.Reader boxed in the
		// io.Reader interface field is a non-nil interface, and the library
		// would call Read on it.
		if sudoReader != nil {
			session.Stdin = sudoReader
		}

		stdout, err := session.StdoutPipe()
		if err != nil {
			ch <- fmt.Sprintf("ERROR: pipe: %v", err)
			return
		}
		if err := session.Start(finalCmd); err != nil {
			ch <- fmt.Sprintf("ERROR: start: %v", err)
			return
		}

		// Cancel watcher: close session when context is cancelled.
		// This unblocks scanner.Scan() which is blocking I/O on the SSH pipe.
		go func() {
			<-ctx.Done()
			_ = session.Close()
		}()

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			ch <- scanner.Text()
		}
		waitErr := session.Wait()
		if waitErr != nil {
			var exitErr *ssh.ExitError
			if errors.As(waitErr, &exitErr) {
				*exitCode = exitErr.ExitStatus()
			} else {
				*exitCode = -1
			}
		}
		// Only meaningful when the command was actually rewritten (sudoReader
		// != nil) — otherwise a plain command that happens to exit 96/97 is
		// not a sudo delivery failure and must not be reported as one.
		if sudoReader != nil {
			if delivErr := fdssh.SudoDeliveryError(*exitCode); delivErr != nil {
				ch <- delivErr.Error()
			}
			if secretCfg != nil {
				if delivErr := fdssh.SecretDeliveryError(*exitCode); delivErr != nil {
					ch <- delivErr.Error()
					*streamErr = delivErr
				} else if msg, ok := secretCfg.ExitErrors[*exitCode]; ok {
					ch <- "ERROR: " + msg
					*streamErr = errors.New(msg)
				}
			}
		}
		logger.Debug("ssh stream complete", "exit_code", *exitCode)
	}()

	return m.listenForStreamLines()
}

// prepareStreamCommand returns the command to run on the target and, when it
// needs one, the reader that is the session's stdin. Without a secret that is
// the cached sudo password's rewrite (RewriteSudoInCmd). With one, the secret
// is resolved now — the point of use — and delivered with the sudo password
// through RewriteSudoWithSecret; the resolved buffer is erased here, the
// reader keeping its own copy. A resolution failure returns the error and no
// command: nothing proceeds on an empty or default value.
func prepareStreamCommand(sm *fdssh.Manager, idx int, cmd string, sc *SecretConsumer) (string, *credential.Reader, error) {
	if sc == nil {
		return sm.RewriteSudoInCmd(idx, cmd)
	}
	secret, err := sc.Resolve()
	if err != nil {
		return "", nil, err
	}
	defer credential.Erase(secret) // the reader holds its own copy
	return sm.RewriteSudoWithSecret(idx, cmd, secret, sc.Tag, sc.Consumer)
}

// stopSSHStream cancels the active stream.
func (m *Model) stopSSHStream() {
	if m.streamCancel != nil {
		m.streamCancel()
		m.streamCancel = nil
	}
	m.streamChan = nil
	m.streamStreaming = false
}

// listenForStreamLines blocks on the stream channel, returns sshStreamBatchMsg.
func (m Model) listenForStreamLines() tea.Cmd {
	ch := m.streamChan
	if ch == nil {
		return nil
	}
	gen := m.streamGeneration
	exitCode := m.streamExitCode
	streamErr := m.streamErr
	return func() tea.Msg {
		first, ok := <-ch
		if !ok {
			code := 0
			if exitCode != nil {
				code = *exitCode
			}
			var err error
			if streamErr != nil {
				err = *streamErr
			}
			return sshStreamDoneMsg{generation: gen, exitCode: code, err: err}
		}
		batch := []string{first}
		for len(batch) < 50 {
			select {
			case line, ok := <-ch:
				if !ok {
					return sshStreamBatchMsg{lines: batch, generation: gen}
				}
				batch = append(batch, line)
			default:
				return sshStreamBatchMsg{lines: batch, generation: gen}
			}
		}
		return sshStreamBatchMsg{lines: batch, generation: gen}
	}
}

// renderSSHStream renders the generic SSH stream view.
func (m Model) renderSSHStream() string {
	h := m.hosts[m.selectedHost]
	f := m.fleets[m.selectedFleet]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	breadcrumb := f.Name + " › " + h.Entry.Name + " › " + m.streamTitle
	s := m.renderHeader(breadcrumb, m.streamCursor+1, len(m.streamLines)) + "\n"

	// Status bar
	var statusParts []string
	if m.streamStreaming {
		statusParts = append(statusParts, ansiColor("● LIVE", "32"))
	} else {
		statusParts = append(statusParts, ansiColor("■ STOPPED", "33"))
	}
	if m.streamNewestFirst && m.streamCursor == 0 {
		statusParts = append(statusParts, ansiColor("↑ AUTO-SCROLL", "36"))
	}
	statusLine := "  " + strings.Join(statusParts, "  ")
	s += borderedRow(statusLine, iw+2, colHeaderStyle) + "\n"
	s += borderStyle.Render("┌"+strings.Repeat("─", iw)+"┐") + "\n"

	if len(m.streamLines) == 0 {
		s += borderedRow("  Waiting for output...", iw, normalRowStyle) + "\n"
	} else {
		maxVisible := m.height - 6
		if maxVisible < 1 {
			maxVisible = 1
		}

		var startIdx, endIdx int
		if m.streamNewestFirst {
			// Cursor 0 = newest at top, scroll down into history
			startIdx = m.streamCursor
			if startIdx >= len(m.streamLines) {
				startIdx = len(m.streamLines) - 1
			}
			if startIdx < 0 {
				startIdx = 0
			}
			endIdx = startIdx + maxVisible
			if endIdx > len(m.streamLines) {
				endIdx = len(m.streamLines)
			}
		} else {
			// Newest at bottom (command output style)
			endIdx = len(m.streamLines)
			startIdx = endIdx - maxVisible
			if startIdx < 0 {
				startIdx = 0
			}
		}

		for i := startIdx; i < endIdx; i++ {
			line := m.streamLines[i]
			if len(line) > iw-2 {
				line = line[:iw-3] + "…"
			}
			s += borderedRow("  "+line, iw, normalRowStyle) + "\n"
		}
	}

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("└"+strings.Repeat("─", iw)+"┘") + "\n"

	var hints [][]string
	if m.streamNewestFirst {
		hints = [][]string{
			{"↑↓", "Scroll"},
			{"Space", "Pause/Resume"},
			{"G", "Latest"},
			{"w", "Save"},
			{"Esc", "Stop & Back"},
		}
	} else {
		hints = [][]string{
			{"w", "Save"},
			{"Esc", "Back"},
		}
	}
	s += m.renderHintBar(hintWithHelp(hints))
	return s
}
