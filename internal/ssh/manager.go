package ssh

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

// ErrSudoDelivery is returned when a sudo password cannot be delivered
// safely to the remote shell — a malformed password (ValidateSudoPassword)
// or the rewritten command's relay preamble failing on the remote host
// (SudoDeliveryError). It is never returned for a wrong password; that
// still fails the way it always has (IsSudoOutput/IsSudoError).
var ErrSudoDelivery = errors.New("sudo password cannot be delivered safely")

// ValidateSudoPassword rejects a password sudo -S's stdin relay cannot
// carry: the relay is a single "read -r" line, so an embedded newline,
// carriage return or NUL would either truncate the password or spill the
// remainder into the command stream.
func ValidateSudoPassword(pw []byte) error {
	if bytes.ContainsAny(pw, "\n\r\x00") {
		return fmt.Errorf("%w: password contains a line break, which sudo -S cannot accept", ErrSudoDelivery)
	}
	return nil
}

// SudoDeliveryError maps the rewritten command's relay preamble exit codes
// to ErrSudoDelivery. 96: no password line reached the remote shell before
// EOF. 97: printf is not a shell builtin on the remote host, so it cannot
// be trusted to relay the password without exec'ing something that would
// expose it. Any other status is the wrapped command's own exit code, not
// a delivery failure.
//
// Exported so a caller streaming a rewritten command's own exit code (that
// path doesn't go through RunSudoCommand, which maps this internally) can
// distinguish a delivery failure from the wrapped command's own status —
// applying it only when the caller knows the command was rewritten, so a
// user command that happens to exit 96 or 97 is never misread.
func SudoDeliveryError(status int) error {
	switch status {
	case 97:
		return fmt.Errorf("%w: printf is not a shell builtin on the remote host", ErrSudoDelivery)
	case 96:
		return fmt.Errorf("%w: no password line reached the remote shell", ErrSudoDelivery)
	default:
		return nil
	}
}

// sudoDeliveryErrorFromErr extracts the remote exit status from err, if any,
// and maps it via SudoDeliveryError. Only meaningful when called on the
// result of a rewritten sudo command — a plain command exiting 96 or 97 on
// its own has nothing to do with sudo delivery.
func sudoDeliveryErrorFromErr(err error) error {
	var exitErr *gossh.ExitError
	if errors.As(err, &exitErr) {
		return SudoDeliveryError(exitErr.ExitStatus())
	}
	return nil
}

// sudoEntry is one resident sudo credential, shared by every host index
// that currently holds the same accepted value — today's shape is N
// host-index keys pointing at one backing array (in practice one, or two
// while a re-prompt candidate coexists with the previously accepted
// value). refs counts how many host indices reference buf; the buffer is
// erased only when refs drops to zero, so releasing one sharing host does
// not erase a value the others still hold.
//
// This is not TAE-89's work done early: TAE-89 changes *when* a host is
// authorised (on demand rather than at broadcast time) and what happens
// when one host rejects a shared value. Both are the model's broadcast
// loop and ClearSudoPassword call sites, not this storage — this gives
// TAE-89 a primitive that supports either answer.
type sudoEntry struct {
	buf  []byte
	refs int
}

// credKind names the two independent session credentials the idle timer
// tracks separately — one timer per kind, not per host, per ADR-F007: a
// per-host timer would re-prompt on an idle host B while host A is in
// active use.
type credKind int

const (
	kindSSH credKind = iota
	kindSudo
	credKindCount
)

// Manager holds persistent SSH connections and cached credentials for all
// hosts in a fleet.
type Manager struct {
	mu             sync.Mutex
	conns          map[int]*gossh.Client
	cachedPassword []byte // session SSH password; nil when none cached
	sudoPasswords  map[int]*sudoEntry
	// sudoVerified records, per host index, that the sudo password held for
	// it was tested on that host and accepted, as the sequence number of that
	// verification (0 = not verified). A password that was merely shared
	// from another host, or is a candidate under test, has no entry.
	sudoVerified map[int]uint64
	sudoSeq      uint64

	// pendingHostKeys holds, per host index, a first-contact host key
	// awaiting the user's accept/decline (never a credential).
	pendingHostKeys map[int]ErrUnknownHostKey

	credentialTimeout time.Duration
	timers            [credKindCount]*time.Timer
	lastUse           [credKindCount]time.Time

	logger *slog.Logger
}

// NewManager creates a new SSH manager.
func NewManager(logger *slog.Logger) *Manager {
	return &Manager{
		conns:           make(map[int]*gossh.Client),
		sudoPasswords:   make(map[int]*sudoEntry),
		sudoVerified:    make(map[int]uint64),
		pendingHostKeys: make(map[int]ErrUnknownHostKey),
		logger:          logger,
	}
}

// SetCredentialTimeout sets the idle timeout after which a session
// credential unused for that long is erased at the deadline (D1: a
// time.Timer per credential kind, reset on use, firing the erase under
// sm.mu — not a tea.Tick, since the event loop is blocked during a
// tea.Exec terminal handover and a Tick could not fire then). 0 disables
// expiry, which is also the zero-value default.
func (sm *Manager) SetCredentialTimeout(d time.Duration) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.credentialTimeout = d
}

// touch records use of the given credential kind and (re)arms its idle
// timer. Called only for a genuine use — delivery to a host — never for a
// presence check, Set…, or Share…, except that storing a credential also
// starts its own idle clock: a credential that is never subsequently
// delivered still has a well-defined moment its idle window began.
// Caller must hold sm.mu.
func (sm *Manager) touch(kind credKind) {
	sm.lastUse[kind] = time.Now()
	if sm.credentialTimeout <= 0 {
		return
	}
	if sm.timers[kind] == nil {
		sm.timers[kind] = time.AfterFunc(sm.credentialTimeout, sm.expire(kind))
		return
	}
	sm.timers[kind].Reset(sm.credentialTimeout)
}

// expire returns the callback armed for kind's idle timer. It fires on
// its own goroutine (sm.mu is never held while a timer is scheduled to
// fire), re-locks, and — if a use slipped in between the timer firing and
// this callback acquiring the lock — re-arms for the remainder instead of
// erasing early.
func (sm *Manager) expire(kind credKind) func() {
	return func() {
		sm.mu.Lock()
		defer sm.mu.Unlock()
		if sm.credentialTimeout <= 0 {
			return
		}
		elapsed := time.Since(sm.lastUse[kind])
		if elapsed < sm.credentialTimeout {
			if sm.timers[kind] != nil {
				sm.timers[kind].Reset(sm.credentialTimeout - elapsed)
			}
			return
		}
		switch kind {
		case kindSSH:
			sm.eraseCachedPasswordLocked()
		case kindSudo:
			sm.eraseAllSudoLocked()
		}
		sm.timers[kind] = nil
	}
}

// eraseCachedPasswordLocked erases and clears the cached SSH password, if
// any. Caller must hold sm.mu.
func (sm *Manager) eraseCachedPasswordLocked() {
	if sm.cachedPassword == nil {
		return
	}
	credential.Erase(sm.cachedPassword)
	sm.cachedPassword = nil
}

// sameBacking reports whether a and b share the same backing array —
// used to confirm a buffer cloned earlier is still the value currently
// cached before erasing it, since a nil or empty slice never aliases
// anything.
func sameBacking(a, b []byte) bool {
	return len(a) > 0 && len(b) > 0 && &a[0] == &b[0]
}

// eraseAllSudoLocked erases every distinct sudo entry exactly once —
// deduped by pointer, since a value shared by several host indices has
// one backing array — and empties the map. Caller must hold sm.mu.
func (sm *Manager) eraseAllSudoLocked() {
	seen := make(map[*sudoEntry]bool, len(sm.sudoPasswords))
	for _, e := range sm.sudoPasswords {
		if seen[e] {
			continue
		}
		seen[e] = true
		credential.Erase(e.buf)
	}
	sm.sudoPasswords = make(map[int]*sudoEntry)
	sm.sudoVerified = make(map[int]uint64)
}

// releaseSudoEntryLocked drops idx's reference to its sudo entry, if any,
// erasing the backing buffer once no host index references it any more.
// Caller must hold sm.mu.
func (sm *Manager) releaseSudoEntryLocked(idx int) {
	e, ok := sm.sudoPasswords[idx]
	if !ok {
		return
	}
	delete(sm.sudoPasswords, idx)
	delete(sm.sudoVerified, idx)
	e.refs--
	if e.refs <= 0 {
		credential.Erase(e.buf)
	}
}

// SetSudoPassword stores idx's candidate sudo password, taking ownership
// of pw — the caller must not reuse or erase it afterward. Erases
// whatever idx held before (a rejected candidate is erased before the
// next attempt is stored) without touching a value still shared with
// other host indices, which is still theirs.
func (sm *Manager) SetSudoPassword(idx int, pw []byte) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.releaseSudoEntryLocked(idx)
	sm.sudoPasswords[idx] = &sudoEntry{buf: pw, refs: 1}
	sm.touch(kindSudo)
}

// ClearSudoPassword releases idx's sudo entry: erased if idx held the
// last reference to it, left alone if other host indices still share it.
func (sm *Manager) ClearSudoPassword(idx int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.releaseSudoEntryLocked(idx)
}

// GetSudoPassword reports whether a sudo password is currently cached for
// idx. A presence check, not a use: it must not reset the idle timer, so
// idling in the host list cannot hold a credential open indefinitely — and
// it never returns the credential itself.
func (sm *Manager) GetSudoPassword(idx int) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	_, ok := sm.sudoPasswords[idx]
	return ok
}

// ShareSudoPassword makes host index to reference the same accepted sudo
// value as from, releasing whatever to held before. Called for every
// other host once one host's sudo password is confirmed, so the fleet
// shares one backing array instead of N copies. from == to is a no-op
// success, not an error: releasing to's entry before re-sharing from's
// would otherwise erase the very buffer being shared, since they are the
// same entry.
func (sm *Manager) ShareSudoPassword(from, to int) bool {
	if from == to {
		sm.mu.Lock()
		_, ok := sm.sudoPasswords[from]
		sm.mu.Unlock()
		return ok
	}
	sm.mu.Lock()
	defer sm.mu.Unlock()
	src, ok := sm.sudoPasswords[from]
	if !ok {
		return false
	}
	sm.releaseSudoEntryLocked(to) // also drops to's verified mark: the shared value is untested on to
	src.refs++
	sm.sudoPasswords[to] = src
	return true
}

// MarkSudoVerified records that the sudo password held for idx was tested on
// that host and accepted. A no-op when idx holds no password: a host that
// needs none has nothing to offer another.
func (sm *Manager) MarkSudoVerified(idx int) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if _, ok := sm.sudoPasswords[idx]; !ok {
		return
	}
	sm.sudoSeq++
	sm.sudoVerified[idx] = sm.sudoSeq
}

// HasVerifiedSudo reports whether the sudo password held for idx was
// verified on idx itself. A presence check, not a use: it does not reset the
// idle timer and never returns the credential.
func (sm *Manager) HasVerifiedSudo(idx int) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	_, held := sm.sudoPasswords[idx]
	return held && sm.sudoVerified[idx] > 0
}

// MostRecentVerifiedSudo returns the host, other than exclude, whose sudo
// password was verified most recently. One candidate, never several: a host
// offered a foreign secret receives at most one, and so suffers at most one
// failed authentication for it.
func (sm *Manager) MostRecentVerifiedSudo(exclude int) (int, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	best, bestSeq := -1, uint64(0)
	for idx, seq := range sm.sudoVerified {
		if idx == exclude || seq <= bestSeq {
			continue
		}
		if _, held := sm.sudoPasswords[idx]; !held {
			continue
		}
		best, bestSeq = idx, seq
	}
	return best, best >= 0
}

// SeedSudoFromCachedPassword copies the cached SSH password as idx's sudo
// candidate, refusing — without storing anything — an SSH password
// ValidateSudoPassword cannot accept for delivery. Reports whether a
// candidate was stored.
func (sm *Manager) SeedSudoFromCachedPassword(idx int) bool {
	// Clone while holding the lock: releasing it between reading
	// sm.cachedPassword and copying out of it would let a concurrent
	// idle-timeout erasure zero the backing array mid-copy.
	sm.mu.Lock()
	var cp []byte
	var err error
	if sm.cachedPassword != nil {
		cp, err = credential.Clone(sm.cachedPassword)
	}
	sm.mu.Unlock()
	if cp == nil || err != nil || ValidateSudoPassword(cp) != nil {
		credential.Erase(cp)
		return false
	}
	sm.SetSudoPassword(idx, cp)
	return true
}

// EraseCredentials erases every cached credential — the cached SSH
// password and every resident sudo entry — without touching live
// connections. Called on every path by which the program quits.
func (sm *Manager) EraseCredentials() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.eraseCachedPasswordLocked()
	sm.eraseAllSudoLocked()
}

// HostProbeResult is sent when an SSH probe completes for a host.
type HostProbeResult struct {
	Index int
	Info  ProbeInfo
	Err   error
	// User is the account ssh resolved for the host, for display when the
	// fleet file sets none.
	User string
}

// PasswordRetryResult is sent after retrying connection with a password.
type PasswordRetryResult struct {
	Index int
	Info  ProbeInfo
	Err   error
	// User is the account ssh resolved for the host.
	User string
}

// ConnectAndProbe connects to a single host and runs the probe command.
// Reuses an existing connection if available.
func (sm *Manager) ConnectAndProbe(idx int, h config.Host) HostProbeResult {
	start := time.Now()
	sm.logger.Debug("probe start", "host", h.Entry.Name)

	// reuse existing connection if available
	sm.mu.Lock()
	client, ok := sm.conns[idx]
	sm.mu.Unlock()

	if ok && client != nil {
		// test the connection with a probe
		info, err := sm.probe(idx, client, h.Entry.SystemdMode, h.ErrorLogSince)
		if err == nil {
			sm.logger.Debug("probe complete", "host", h.Entry.Name, "reused", true, "elapsed", time.Since(start))
			return HostProbeResult{Index: idx, Info: info}
		}
		// connection is stale, remove and reconnect
		sm.logger.Debug("probe stale connection", "host", h.Entry.Name, "err", err)
		_ = client.Close()
		sm.mu.Lock()
		delete(sm.conns, idx)
		sm.mu.Unlock()
	}

	// establish new connection
	client, target, err := sm.dial(h)
	sm.notePendingHostKey(idx, err)
	if err != nil {
		sm.logger.Error("probe failed", "host", h.Entry.Name, "err", err)
		return HostProbeResult{Index: idx, Err: err, User: target.User}
	}

	info, err := sm.probe(idx, client, h.Entry.SystemdMode, h.ErrorLogSince)
	if err != nil {
		_ = client.Close()
		sm.logger.Error("probe failed", "host", h.Entry.Name, "err", err)
		return HostProbeResult{Index: idx, Err: fmt.Errorf("probe: %w", err), User: target.User}
	}

	sm.mu.Lock()
	sm.conns[idx] = client
	sm.mu.Unlock()

	sm.logger.Debug("probe complete", "host", h.Entry.Name, "reused", false, "elapsed", time.Since(start))
	return HostProbeResult{Index: idx, Info: info, User: target.User}
}

// HasCachedPassword reports whether an SSH connection password is
// currently cached. A presence check — never returns the credential.
func (sm *Manager) HasCachedPassword() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.cachedPassword != nil
}

// RunSudoCommand executes a command on the given host, wrapping sudo invocations
// so the cached password is delivered over the session's stdin rather than the
// command string, if a sudo password is cached for this host.
// If no sudo password is cached, delegates to RunCommand unchanged.
//
// Stdin contract: the session's stdin is exactly the password line the
// rewritten command's relay reads. A cmd that also needs its own stdin
// must be given one through a parameter that does not yet exist — the
// relay consumes the session's stdin before the wrapped command runs.
func (sm *Manager) RunSudoCommand(idx int, cmd string) (string, error) {
	sm.mu.Lock()
	e, ok := sm.sudoPasswords[idx]
	if !ok || !strings.Contains(cmd, "sudo ") {
		sm.mu.Unlock()
		return sm.runCommand(idx, cmd, nil)
	}
	if err := ValidateSudoPassword(e.buf); err != nil {
		sm.mu.Unlock()
		return "", err
	}
	r, err := credential.NewReader(e.buf, "\n")
	if err != nil {
		sm.mu.Unlock()
		return "", err
	}
	sm.touch(kindSudo)
	sm.mu.Unlock()

	defer r.Erase()
	rewritten := rewriteSudoCmd(cmd)
	out, err := sm.runCommand(idx, rewritten, r)
	if delivErr := sudoDeliveryErrorFromErr(err); delivErr != nil {
		err = delivErr
	}
	return stripSudoPrompt(out), err
}

// stripSudoPrompt removes "[sudo] password for ..." lines from output.
// These leak into stdout when commands use 2>&1 with sudo -S.
func stripSudoPrompt(s string) string {
	var clean []string
	for _, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[sudo] password for") {
			continue
		}
		clean = append(clean, line)
	}
	return strings.Join(clean, "\n")
}

// sudoPreamble reads exactly one line from the session's stdin into a
// variable, then verifies printf is a shell builtin before the rewritten
// command's first use of it — per command, not once at password-test time,
// because the password cache is fleet-wide and a host may never have been
// individually tested. IFS= keeps leading/trailing spaces, -r keeps
// backslashes. Nothing is appended after the wrapped command: an unset
// would clobber the exit status every caller reads, so the variable simply
// dies with the shell.
//
// The builtin check uses `command -v printf`, not `type printf`: bash
// localizes `type`'s human-readable output ("printf is a shell builtin" is
// only the C-locale wording), so a case matching on it silently fails
// closed on any host whose SSH session sets a non-English LANG — every
// rewritten sudo command exits 97 there, even though printf really is a
// builtin. `command -v` prints the bare command name for a builtin and an
// absolute path otherwise, identically across locales in bash and dash.
// The property this actually needs is narrower than "is a builtin": it
// confirms printf is not an external binary, so it can never exec, so the
// password can never reach an argv. (It also passes for a shell function
// named printf, which shares that same non-exec property — reaching that
// requires the remote's non-interactive shell startup to define one, e.g.
// via BASH_ENV, not an ordinary bashrc.)
const sudoPreamble = `IFS= read -r FLEETDESK_SUDO_PW || exit 96; case "$(command -v printf)" in printf) ;; *) exit 97;; esac; `

// rewriteSudoCmd rewrites every "sudo " occurrence in cmd into a relay that
// pipes the password FleetDesk delivers over stdin to sudo -S, via a shell
// variable rather than the literal password — so the password never
// appears in the command string, in any argument, or in the environment.
// cmd is returned unchanged, with no preamble, when it contains no "sudo ".
func rewriteSudoCmd(cmd string) string {
	if !strings.Contains(cmd, "sudo ") {
		return cmd
	}
	return sudoPreamble + rewriteSudoOccurrences(cmd)
}

// rewriteSudoOccurrences is rewriteSudoCmd without the preamble, for a
// command that supplies its own (RewriteSudoWithSecret reads a second line
// after it, so the preamble must come exactly once, first).
func rewriteSudoOccurrences(cmd string) string {
	relay := `printf '%s\n' "$FLEETDESK_SUDO_PW" | sudo -S 2>/dev/null `
	return strings.ReplaceAll(cmd, "sudo ", relay)
}

// ErrSecretDelivery is returned when a secret cannot be delivered safely to
// the remote shell: a malformed secret (a line break, which would truncate
// it or spill into the command stream) or the remote shell finding no
// secret line on its stdin.
var ErrSecretDelivery = errors.New("secret cannot be delivered safely")

// secretMissingStatus is the exit status of a rewritten command whose remote
// shell found no secret line on stdin before EOF.
const secretMissingStatus = 95

// SecretDeliveryError maps a rewritten command's exit status to
// ErrSecretDelivery when it is the secret preamble's own: 95. Like
// SudoDeliveryError it is only meaningful for a command RewriteSudoWithSecret
// produced.
func SecretDeliveryError(status int) error {
	if status == secretMissingStatus {
		return fmt.Errorf("%w: no secret line reached the remote shell", ErrSecretDelivery)
	}
	return nil
}

// secretTag is a variable-name-like label for a secret line on the consumer's
// stdin ("FLEETDESK_AK=<value>").
var secretTag = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

// RewriteSudoWithSecret builds the command that runs consumer as root with a
// secret delivered over stdin, together with the sudo password, and the
// reader that is the session's stdin. Neither the password nor the secret
// appears in the command string, in any argv on the target (the shell, sudo,
// the consumer) or in the environment: both travel as lines of stdin and are
// held only in shell variables that are not exported, then re-sent through a
// builtin printf into sudo -S and on to the consumer's stdin.
//
//	<preamble: read the sudo password, check printf>
//	IFS= read -r FLEETDESK_SECRET || exit 95;
//	<prefix, every "sudo " rewritten to the relay> &&
//	printf '%s\n%s\n' "$FLEETDESK_SUDO_PW" "<tag>=$FLEETDESK_SECRET" |
//	    sudo -S 2>/dev/null sh -c '<consumer>'
//
// The consumer reads stdin up to its "<tag>=" line; if sudo asks for no
// password it does not read the password line, which the consumer skips.
// prefix may be empty. The caller owns secret and must erase it once this
// returns: the reader holds its own copy, erased when the last byte is
// delivered or on Reader.Erase, which the caller must call when the stream
// ends. consumer is run through "sh -c", so it may be a compound command.
func (sm *Manager) RewriteSudoWithSecret(idx int, prefix string, secret []byte, tag, consumer string) (string, *credential.Reader, error) {
	if !secretTag.MatchString(tag) {
		return "", nil, fmt.Errorf("invalid secret tag %q", tag)
	}
	if bytes.ContainsAny(secret, "\n\r\x00") {
		return "", nil, fmt.Errorf("%w: the secret contains a line break", ErrSecretDelivery)
	}
	sm.mu.Lock()
	e, ok := sm.sudoPasswords[idx]
	if !ok {
		sm.mu.Unlock()
		return "", nil, fmt.Errorf("%w: no sudo password is held for this host", ErrSudoDelivery)
	}
	if err := ValidateSudoPassword(e.buf); err != nil {
		sm.mu.Unlock()
		return "", nil, err
	}
	r, err := credential.NewMultiReader("\n", e.buf, secret)
	if err != nil {
		sm.mu.Unlock()
		return "", nil, err
	}
	sm.touch(kindSudo)
	sm.mu.Unlock()

	var b strings.Builder
	b.WriteString(sudoPreamble)
	b.WriteString(`IFS= read -r FLEETDESK_SECRET || exit 95; `)
	if prefix != "" {
		b.WriteString(rewriteSudoOccurrences(prefix))
		b.WriteString(" && ")
	}
	fmt.Fprintf(&b, `printf '%%s\n%%s\n' "$FLEETDESK_SUDO_PW" "%s=$FLEETDESK_SECRET" | sudo -S 2>/dev/null sh -c '%s'`,
		tag, strings.ReplaceAll(consumer, "'", `'\''`))
	return b.String(), r, nil
}

// RunCommand executes a command on the given host index and returns stdout.
func (sm *Manager) RunCommand(idx int, cmd string) (string, error) {
	return sm.runCommand(idx, cmd, nil)
}

// runCommand is RunCommand's body, plus an optional stdin for the sudo
// password relay. stdin is left unset on the session when nil, matching
// RunCommand's behavior exactly.
func (sm *Manager) runCommand(idx int, cmd string, stdin io.Reader) (string, error) {
	logPrefix := cmd[:min(len(cmd), 60)]
	if strings.Contains(cmd, "| sudo -S") {
		logPrefix = "[sudo-rewritten]"
	}
	sm.logger.Debug("runCommand", "idx", idx, "cmd_prefix", logPrefix)

	sm.mu.Lock()
	client, ok := sm.conns[idx]
	sm.mu.Unlock()

	if !ok || client == nil {
		return "", fmt.Errorf("no connection for host")
	}

	session, err := client.NewSession()
	if err != nil {
		sm.logger.Error("runCommand failed", "idx", idx, "err", err)
		return "", fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	if stdin != nil {
		session.Stdin = stdin
	}

	out, err := session.CombinedOutput(cmd)
	result := stripShellWarnings(string(out))
	if err != nil {
		sm.logger.Error("runCommand failed", "idx", idx, "err", err)
		return result, err
	}
	return result, nil
}

// stripShellWarnings removes common shell login warnings that appear before
// command output (e.g., when the user's home directory doesn't exist on the host).
func stripShellWarnings(s string) string {
	for {
		if strings.HasPrefix(s, "Could not chdir to home directory") ||
			strings.HasPrefix(s, "-bash: warning:") {
			if idx := strings.Index(s, "\n"); idx >= 0 {
				s = s[idx+1:]
				continue
			}
		}
		break
	}
	return s
}

// SetCachedPassword stores the session's SSH password, taking ownership
// of pw — the caller must not reuse or erase it afterward. Erases
// whatever was cached before it.
func (sm *Manager) SetCachedPassword(pw []byte) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.eraseCachedPasswordLocked()
	sm.cachedPassword = pw
	sm.touch(kindSSH)
}

// ClearPassword erases the cached SSH password.
func (sm *Manager) ClearPassword() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	sm.eraseCachedPasswordLocked()
}

// ConnectWithCachedPassword connects to a host using the cached SSH
// password, through a fresh per-operation copy that is erased when the
// dial (and its probe) finishes, regardless of outcome. Counts as a use
// of the cached credential. On a rejected password — never a broader
// handshake failure such as a key-exchange mismatch, which would falsely
// implicate a password that was never tried — the cached password itself
// is erased, but only if it still aliases the value this call cloned
// from: a slower, wrong-password dial completing after the user has
// already entered a new, correct one must not erase that new one (TAE-21
// Erasure AC: "a rejected password is erased before the next attempt is
// stored"; this is the SSH instance the AC names).
func (sm *Manager) ConnectWithCachedPassword(idx int, h config.Host) PasswordRetryResult {
	// Clone while holding the lock: releasing it between reading
	// sm.cachedPassword and copying out of it would let a concurrent
	// idle-timeout erasure (or EraseCredentials/Close) zero the backing
	// array mid-copy.
	sm.mu.Lock()
	src := sm.cachedPassword
	hadCached := src != nil
	var cp []byte
	var err error
	if hadCached {
		sm.touch(kindSSH)
		cp, err = credential.Clone(src)
	}
	sm.mu.Unlock()

	if !hadCached {
		return PasswordRetryResult{Index: idx, Err: fmt.Errorf("no cached password")}
	}
	if err != nil {
		return PasswordRetryResult{Index: idx, Err: err}
	}
	res := sm.ConnectWithPassword(idx, h, cp)
	credential.Erase(cp) // no-op if ConnectWithPassword already erased it on auth failure
	if res.Err != nil && IsPasswordRejected(res.Err) {
		sm.mu.Lock()
		if sameBacking(sm.cachedPassword, src) {
			sm.eraseCachedPasswordLocked()
		}
		sm.mu.Unlock()
	}
	return res
}

// ConnectWithPassword connects to a host using password auth and probes
// it. pw is read for this dial only — the manager does not store it. On
// an authentication rejection (never on a network or probe failure), pw
// is erased in place before this returns (a rejected password is erased
// before the next attempt is stored). The string password auth itself
// constructs at the moment it authenticates is outside this package's
// reach — it is referenced by nothing FleetDesk holds afterward.
func (sm *Manager) ConnectWithPassword(idx int, h config.Host, pw []byte) PasswordRetryResult {
	entry := h.Entry
	t, err := Resolve(entry)
	if err == nil {
		err = t.handoverOnly()
	}
	if err != nil {
		sm.logger.Error("password dial failed", "host", entry.Name, "idx", idx, "err", err)
		return PasswordRetryResult{Index: idx, Err: err}
	}

	auths := []gossh.AuthMethod{
		gossh.PasswordCallback(func() (string, error) { return string(pw), nil }),
		gossh.KeyboardInteractive(func(user, instruction string, questions []string, echos []bool) ([]string, error) {
			answers := make([]string, len(questions))
			for i := range questions {
				answers[i] = string(pw)
			}
			return answers, nil
		}),
	}

	addr := t.Addr()
	start := time.Now()
	sm.logger.Debug("password dial start", "addr", addr, "idx", idx)
	client, err := dialVerified(t, auths, t.User, entry.Timeout)
	if err != nil {
		sm.logger.Error("password dial failed", "addr", addr, "idx", idx, "err", err, "elapsed", time.Since(start))
		if isHostKeyError(err) {
			sm.notePendingHostKey(idx, err)
			return PasswordRetryResult{Index: idx, Err: err, User: t.User}
		}
		wrapped := fmt.Errorf("password auth: %w", err)
		if IsAuthError(wrapped) {
			credential.Erase(pw)
		}
		return PasswordRetryResult{Index: idx, Err: wrapped, User: t.User}
	}
	sm.logger.Debug("password dial success", "addr", addr, "idx", idx, "elapsed", time.Since(start))

	sm.mu.Lock()
	sm.conns[idx] = client
	sm.mu.Unlock()

	info, err := sm.probe(idx, client, entry.SystemdMode, h.ErrorLogSince)
	if err != nil {
		sm.logger.Error("password probe failed", "addr", addr, "idx", idx, "err", err)
		return PasswordRetryResult{Index: idx, Err: fmt.Errorf("probe: %w", err), User: t.User}
	}

	sm.logger.Debug("password probe success", "addr", addr, "idx", idx)
	return PasswordRetryResult{Index: idx, Info: info, User: t.User}
}

// RewriteSudoInCmd rewrites sudo commands so the cached password for the
// given host is delivered over stdin rather than the command string. If no
// password is cached, or cmd has no "sudo ", returns (cmd, nil, nil)
// unchanged. If the cached password cannot be delivered safely
// (ValidateSudoPassword), returns ("", nil, err) before anything is
// rewritten or sent. Otherwise returns the rewritten command and the
// reader the caller must set as the session's stdin before starting it —
// assigning it only when non-nil, since a nil *credential.Reader boxed in
// an io.Reader is a non-nil interface — and erase (Reader.Erase) once the
// stream it feeds ends.
func (sm *Manager) RewriteSudoInCmd(idx int, cmd string) (string, *credential.Reader, error) {
	sm.mu.Lock()
	e, ok := sm.sudoPasswords[idx]
	if !ok || !strings.Contains(cmd, "sudo ") {
		sm.mu.Unlock()
		return cmd, nil, nil
	}
	if err := ValidateSudoPassword(e.buf); err != nil {
		sm.mu.Unlock()
		return "", nil, err
	}
	r, err := credential.NewReader(e.buf, "\n")
	if err != nil {
		sm.mu.Unlock()
		return "", nil, err
	}
	sm.touch(kindSudo)
	sm.mu.Unlock()
	return rewriteSudoCmd(cmd), r, nil
}

// GetConnection returns the SSH client for the given host index, or nil if not connected.
func (sm *Manager) GetConnection(idx int) *gossh.Client {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.conns[idx]
}

// HasConnection returns true if the manager has an active SSH client for the given host index.
func (sm *Manager) HasConnection(idx int) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.conns[idx] != nil
}

// Close shuts down all SSH connections and erases every cached credential.
func (sm *Manager) Close() {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, c := range sm.conns {
		if c != nil {
			_ = c.Close()
		}
	}
	sm.conns = make(map[int]*gossh.Client)
	sm.pendingHostKeys = make(map[int]ErrUnknownHostKey)
	sm.eraseCachedPasswordLocked()
	sm.eraseAllSudoLocked()
	for i, t := range sm.timers {
		if t != nil {
			t.Stop()
			sm.timers[i] = nil
		}
	}
}

// probeCommand is the host probe's one compound command. sudo is the text
// of its privileged read's prefix: "sudo -n " (never prompts, so it fails
// rather than hangs when a password is required) or "sudo " (rewritten into
// the stdin relay when a verified password is delivered). Line four is the
// error count: an integer when the read ran, "-" when sudo could not be
// obtained or the journal could not be read, so that "could not read" never
// looks like a successful zero.
func probeCommand(sudo, errorLogSince string) string {
	return `echo '---PROBE---' && ` +
		`(hostname -f 2>/dev/null || hostname) | head -1 && ` +
		`uptime -s 2>/dev/null || echo unknown && ` +
		`(grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2 | tr -d '"') || echo unknown && ` +
		`echo $(( $(crontab -l 2>/dev/null | grep -v '^#' | grep -v '^$' | wc -l) + $(ls /etc/cron.d/ 2>/dev/null | wc -l) )) && ` +
		fmt.Sprintf(`if o=$(%sjournalctl -p err --since '%s' --no-pager -q 2>/dev/null); then { printf '%%s' "$o" | grep -c '^' || true; }; else echo -; fi && `, sudo, errorLogSince) +
		`df -h --output=pcent -x tmpfs -x devtmpfs 2>/dev/null | tail -n+2 | wc -l && ` +
		`df -h --output=pcent -x tmpfs -x devtmpfs 2>/dev/null | tail -n+2 | awk '{gsub(/%%/,""); if ($1 >= 80) print}' | wc -l && ` +
		`(getent passwd | awk -F: '$3 >= 1000 && $3 != 65534 {print $1}'; for d in /home/*/; do u=$(basename "$d"); getent passwd "$u" >/dev/null 2>&1 && echo "$u"; done) | sort -u | wc -l && ` +
		`(ip -br link | grep -c UP || echo 0) && ` +
		`ip -br link | wc -l && ` +
		`(ss -tlnp 2>/dev/null | tail -n +2 | wc -l || echo 0) && ` +
		`(dnf check-update --quiet 2>/dev/null | grep -c '^\S' || echo 0) && ` +
		`(command -v supervisorctl >/dev/null 2>&1 && echo 1 || echo 0)`
}

// Probe runs a single SSH command to gather all host info in one roundtrip.
// Its privileged read is `sudo -n`: it uses sudo only when sudo needs no
// password, and otherwise reports that it could not read (ErrorCountKnown
// false). The manager's own probe delivers a verified password instead.
func Probe(client *gossh.Client, systemdMode string, errorLogSince string) (ProbeInfo, error) {
	return runProbe(client, systemdMode, errorLogSince, "sudo -n ", nil)
}

// probe is Probe for host idx. When a sudo password was verified on this very
// host it is delivered to the privileged read over the session's stdin
// (TAE-20's relay), so the read runs even where sudo wants a password. A
// password held for another host, or one never verified here, is never
// offered: the unverified form is used, and it either runs or says it could
// not. A background probe is not a use of the credential for the idle
// timeout: it must not keep an idle session's password alive.
func (sm *Manager) probe(idx int, client *gossh.Client, systemdMode, errorLogSince string) (ProbeInfo, error) {
	sm.mu.Lock()
	e, held := sm.sudoPasswords[idx]
	verified := held && sm.sudoVerified[idx] > 0
	var r *credential.Reader
	if verified && ValidateSudoPassword(e.buf) == nil {
		var err error
		if r, err = credential.NewReader(e.buf, "\n"); err != nil {
			r = nil
		}
	}
	sm.mu.Unlock()
	if r == nil {
		return runProbe(client, systemdMode, errorLogSince, "sudo -n ", nil)
	}
	defer r.Erase()
	return runProbe(client, systemdMode, errorLogSince, "sudo ", r)
}

// runProbe runs the probe command; a non-nil stdin means the command's
// "sudo " is rewritten into the password relay that reads it.
func runProbe(client *gossh.Client, systemdMode, errorLogSince, sudo string, stdin *credential.Reader) (ProbeInfo, error) {
	session, err := client.NewSession()
	if err != nil {
		return ProbeInfo{}, fmt.Errorf("new session: %w", err)
	}
	defer func() { _ = session.Close() }()

	cmd := probeCommand(sudo, errorLogSince)
	if stdin != nil {
		cmd = rewriteSudoCmd(cmd)
		session.Stdin = stdin
	}

	out, err := session.CombinedOutput(cmd)
	if err != nil {
		session2, err2 := client.NewSession()
		if err2 != nil {
			return ProbeInfo{}, fmt.Errorf("probe failed: %w", err)
		}
		defer func() { _ = session2.Close() }()

		if systemdMode == "user" {
			systemdMode = "system"
		} else {
			systemdMode = "user"
		}

		cmd = `echo '---PROBE---' && ` +
			`(hostname -f 2>/dev/null || hostname) | head -1 && ` +
			`uptime -s 2>/dev/null || echo unknown && ` +
			`(grep PRETTY_NAME /etc/os-release 2>/dev/null | cut -d= -f2 | tr -d '"') || echo unknown`

		out, err = session2.CombinedOutput(cmd)
		if err != nil {
			return ProbeInfo{}, fmt.Errorf("probe failed both modes: %w", err)
		}
	}

	return ParseProbeOutput(string(out), systemdMode)
}

// dial establishes an SSH connection to a host. The target comes from
// `ssh -G` (Resolve), the same place the handover gets it, and the host key
// is verified against known_hosts on every attempt.
func (sm *Manager) dial(h config.Host) (*gossh.Client, Target, error) {
	entry := h.Entry
	t, err := Resolve(entry)
	if err == nil {
		err = t.handoverOnly()
	}
	if err != nil {
		return nil, t, err
	}

	timeout := entry.Timeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	var authMethods []gossh.AuthMethod
	var authNames []string

	agentAuth, agentConn := sshAgentAuth()
	if agentAuth != nil {
		authMethods = append(authMethods, agentAuth)
		authNames = append(authNames, "agent")
	}

	seen := make(map[string]bool)
	for _, path := range t.IdentityFiles {
		if seen[path] || !fileExists(path) {
			continue
		}
		seen[path] = true
		if key := publicKeyFile(path); key != nil {
			authMethods = append(authMethods, key)
			authNames = append(authNames, "identity:"+path)
		}
	}

	if len(authMethods) == 0 {
		return nil, t, fmt.Errorf("no SSH auth methods available")
	}

	addr := t.Addr()
	start := time.Now()
	sm.logger.Debug("dial start", "addr", addr, "user", t.User)

	var lastErr error
	for i, auth := range authMethods {
		sm.logger.Debug("dial trying", "auth", authNames[i], "addr", addr)
		client, err := dialVerified(t, []gossh.AuthMethod{auth}, t.User, timeout)
		if err == nil {
			if agentConn != nil {
				_ = agentConn.Close()
			}
			sm.logger.Debug("dial success", "addr", addr, "elapsed", time.Since(start))
			return client, t, nil
		}
		if isHostKeyError(err) {
			// Not an authentication failure: the host was never verified, so no
			// further method is tried and no password may be offered to it.
			if agentConn != nil {
				_ = agentConn.Close()
			}
			sm.logger.Error("dial refused", "addr", addr, "err", err, "elapsed", time.Since(start))
			return nil, t, err
		}
		lastErr = err
	}

	// close agent socket on failure — no SSH client to use it
	if agentConn != nil {
		_ = agentConn.Close()
	}

	sm.logger.Error("dial failed", "addr", addr, "err", lastErr, "elapsed", time.Since(start))
	return nil, t, fmt.Errorf("dial %s: %w", addr, lastErr)
}

// sshAgentAuth returns an auth method from the SSH agent and the underlying
// connection. The caller must close agentConn when the SSH client is established
// or when all auth attempts fail.
func sshAgentAuth() (gossh.AuthMethod, net.Conn) {
	sock := os.Getenv("SSH_AUTH_SOCK")
	if sock == "" {
		return nil, nil
	}
	conn, err := net.Dial("unix", sock) //nolint:gosec // G704: Unix-socket dial to $SSH_AUTH_SOCK, not a network dial
	if err != nil {
		return nil, nil
	}
	return gossh.PublicKeysCallback(agent.NewClient(conn).Signers), conn
}

// publicKeyFile returns an auth method from a private key file, or nil.
func publicKeyFile(path string) gossh.AuthMethod {
	data, err := os.ReadFile(path) //nolint:gosec // G304: the user's own ~/.ssh/config IdentityFile or a stat-gated default key
	if err != nil {
		return nil
	}
	signer, err := gossh.ParsePrivateKey(data)
	if err != nil {
		return nil
	}
	return gossh.PublicKeys(signer)
}
