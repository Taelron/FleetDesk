package ssh

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

// ErrUnknownHostKey is returned when a host presents a key that is not in
// known_hosts and no entry exists for the host at all — first contact. It
// carries the key so the caller can show its fingerprint and, if the user
// accepts, record it with TrustHostKey.
type ErrUnknownHostKey struct {
	Addr string
	Key  gossh.PublicKey
}

// Fingerprint is the key's SHA256 fingerprint, "SHA256:<base64>".
func (e ErrUnknownHostKey) Fingerprint() string { return gossh.FingerprintSHA256(e.Key) }

func (e ErrUnknownHostKey) Error() string {
	return fmt.Sprintf("host key not trusted: %s presented %s %s", e.Addr, e.Key.Type(), e.Fingerprint())
}

// ErrHostKeyMismatch is returned when known_hosts holds a different key for
// the host than the one it presented — or a revoked one. The connection
// never proceeds and nothing is offered for trust.
type ErrHostKeyMismatch struct {
	Addr string
	Want gossh.PublicKey // what known_hosts has
	Got  gossh.PublicKey // what the host presented
}

func (e ErrHostKeyMismatch) Error() string {
	return fmt.Sprintf("host key mismatch for %s: known_hosts has %s %s, host presented %s %s",
		e.Addr, e.Want.Type(), gossh.FingerprintSHA256(e.Want), e.Got.Type(), gossh.FingerprintSHA256(e.Got))
}

// isHostKeyError reports whether err is one of the two typed host-key
// errors. Used to keep them out of the "authentication failed, ask for a
// password" decision: a password would be sent to a host that was never
// verified.
func isHostKeyError(err error) bool {
	var unk ErrUnknownHostKey
	var mis ErrHostKeyMismatch
	return errors.As(err, &unk) || errors.As(err, &mis)
}

// hostKeyCallback verifies a server key against the user's and the system's
// known_hosts, the files `ssh -G` named. The first user file is created
// (0600, in a 0700 directory) when it does not exist yet, so that a first
// contact accepted later has somewhere to be recorded. A first contact is
// reported as ErrUnknownHostKey, a changed key as ErrHostKeyMismatch.
func hostKeyCallback(t Target) (gossh.HostKeyCallback, error) {
	if len(t.UserKnownHosts) > 0 {
		if err := ensureKnownHostsFile(t.UserKnownHosts[0]); err != nil {
			return nil, err
		}
	}
	var files []string
	for _, f := range slices.Concat(t.UserKnownHosts, t.GlobalKnownHosts) {
		if fileExists(f) {
			files = append(files, f)
		}
	}
	if len(files) == 0 {
		return nil, errors.New("no known_hosts file to verify host keys against")
	}
	cb, err := knownhosts.New(files...)
	if err != nil {
		return nil, fmt.Errorf("reading known_hosts: %w", err)
	}
	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		err := cb(hostname, remote, key)
		if err == nil {
			return nil
		}
		var revoked *knownhosts.RevokedError
		if errors.As(err, &revoked) {
			return ErrHostKeyMismatch{Addr: hostname, Want: revoked.Revoked.Key, Got: key}
		}
		var ke *knownhosts.KeyError
		if errors.As(err, &ke) {
			if len(ke.Want) == 0 {
				return ErrUnknownHostKey{Addr: hostname, Key: key}
			}
			return ErrHostKeyMismatch{Addr: hostname, Want: pickWant(ke.Want, key), Got: key}
		}
		return err
	}, nil
}

// pickWant chooses the known key to show in a mismatch: one of the same
// type as the key presented, when there is one.
func pickWant(want []knownhosts.KnownKey, got gossh.PublicKey) gossh.PublicKey {
	for _, w := range want {
		if w.Key.Type() == got.Type() {
			return w.Key
		}
	}
	return want[0].Key
}

// ensureKnownHostsFile creates path (0600) and its directory (0700) when
// missing.
func ensureKnownHostsFile(path string) error {
	if fileExists(path) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		return fmt.Errorf("creating %s: %w", path, err)
	}
	return nil
}

// preferredHostKeyAlgos is OpenSSH's own preference among the algorithms
// this library supports: ed25519 first, then ECDSA, then RSA with SHA-2.
// The library's default puts RSA first, which would make the fingerprint a
// user is asked about differ from the one `ssh` shows for the same host.
var preferredHostKeyAlgos = []string{
	gossh.KeyAlgoED25519,
	gossh.KeyAlgoECDSA256,
	gossh.KeyAlgoECDSA384,
	gossh.KeyAlgoECDSA521,
	gossh.KeyAlgoRSASHA512,
	gossh.KeyAlgoRSASHA256,
}

// algosForKnownKeys lists the host-key algorithms that can verify the given
// recorded keys, in preference order — what OpenSSH negotiates when
// known_hosts already holds a key for the host.
func algosForKnownKeys(want []gossh.PublicKey) []string {
	var out []string
	for _, a := range preferredHostKeyAlgos {
		for _, w := range want {
			t := w.Type()
			if a == t || (t == gossh.KeyAlgoRSA && (a == gossh.KeyAlgoRSASHA256 || a == gossh.KeyAlgoRSASHA512)) {
				out = append(out, a)
				break
			}
		}
	}
	return out
}

func asMismatch(err error) *ErrHostKeyMismatch {
	var m ErrHostKeyMismatch
	if errors.As(err, &m) {
		return &m
	}
	return nil
}

// dialVerified dials addr and verifies the host key against known_hosts. If
// the host presents a key of a type known_hosts holds nothing for while it
// does hold another type, it dials once more asking only for the recorded
// types — as OpenSSH does — so a host that offers several key types is
// never reported as changed merely because the library preferred a type
// that was never recorded.
func dialVerified(t Target, auths []gossh.AuthMethod, user string, timeout time.Duration) (*gossh.Client, error) {
	cb, err := hostKeyCallback(t)
	if err != nil {
		return nil, err
	}
	var recorded []gossh.PublicKey
	recordWant := func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		err := cb(hostname, remote, key)
		var mis ErrHostKeyMismatch
		if errors.As(err, &mis) {
			recorded = append(recorded, mis.Want)
		}
		return err
	}
	addr := t.Addr()
	cfg := &gossh.ClientConfig{
		User:              user,
		Auth:              auths,
		HostKeyCallback:   recordWant,
		HostKeyAlgorithms: preferredHostKeyAlgos,
		Timeout:           timeout,
	}
	client, err := gossh.Dial("tcp", addr, cfg)
	var mis ErrHostKeyMismatch
	if err != nil && errors.As(err, &mis) && mis.Want.Type() != mis.Got.Type() {
		if algos := algosForKnownKeys(recorded); len(algos) > 0 {
			cfg.HostKeyAlgorithms = algos
			var retryErr error
			client, retryErr = gossh.Dial("tcp", addr, cfg)
			// A host that offers none of the recorded types is reported as the
			// mismatch it is, not as a negotiation failure.
			if retryErr == nil || asMismatch(retryErr) != nil || !strings.Contains(retryErr.Error(), "no common algorithm for host key") {
				err = retryErr
			}
		}
	}
	if err != nil {
		var unk ErrUnknownHostKey
		switch {
		case errors.As(err, &unk):
			return nil, unk
		case errors.As(err, &mis):
			return nil, mis
		}
		return nil, err
	}
	return client, nil
}

// TrustHostKey records key in the first user known_hosts file for the entry,
// the way `ssh` does after "yes" at its prompt. It resolves the entry again
// so the key lands in the file ssh itself would have used.
func (sm *Manager) TrustHostKey(e config.HostEntry, key gossh.PublicKey) error {
	t, err := Resolve(e)
	if err != nil {
		return err
	}
	return recordHostKey(sm, t, key)
}

// recordHostKey appends key, for the target's address, to the first user
// known_hosts file.
func recordHostKey(sm *Manager, t Target, key gossh.PublicKey) error {
	if len(t.UserKnownHosts) == 0 {
		return errors.New("no user known_hosts file to record the key in")
	}
	path := t.UserKnownHosts[0]
	if err := ensureKnownHostsFile(path); err != nil {
		return err
	}
	line := knownhosts.Line([]string{knownhosts.Normalize(t.Addr())}, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600) //nolint:gosec // G304: the known_hosts path ssh -G resolved
	if err != nil {
		return fmt.Errorf("opening %s: %w", path, err)
	}
	if _, err := f.WriteString(line + "\n"); err != nil {
		_ = f.Close()
		return fmt.Errorf("writing %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	sm.logger.Debug("host key trusted", "addr", t.Addr(), "type", key.Type())
	return nil
}

// notePendingHostKey remembers a first-contact key for host idx until the
// user accepts or declines it.
func (sm *Manager) notePendingHostKey(idx int, err error) {
	var unk ErrUnknownHostKey
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if errors.As(err, &unk) {
		sm.pendingHostKeys[idx] = unk
	} else {
		delete(sm.pendingHostKeys, idx)
	}
}

// PendingHostKey returns the first-contact key host idx presented, if one
// is awaiting the user's decision.
func (sm *Manager) PendingHostKey(idx int) (ErrUnknownHostKey, bool) {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	k, ok := sm.pendingHostKeys[idx]
	return k, ok
}

// ErrPendingKeyChanged is returned when the key awaiting trust is no longer
// the one the user was shown.
var ErrPendingKeyChanged = errors.New("the host presented a different key since the prompt; nothing was recorded")

// TrustPendingHostKey records the pending key for host idx, the user having
// accepted it, and forgets it. shownFingerprint is the fingerprint the user
// was shown: if the pending key has been replaced since (a later probe saw a
// different key), or the entry now resolves to a different address, nothing
// is recorded — a user must never trust a key they were not shown.
func (sm *Manager) TrustPendingHostKey(idx int, e config.HostEntry, shownFingerprint string) error {
	k, ok := sm.PendingHostKey(idx)
	if !ok {
		return errors.New("no host key is pending for this host")
	}
	if k.Fingerprint() != shownFingerprint {
		return ErrPendingKeyChanged
	}
	t, err := Resolve(e)
	if err != nil {
		return err
	}
	if t.Addr() != k.Addr {
		return fmt.Errorf("%s now resolves to %s, not %s that the key was seen at; nothing was recorded", e.Hostname, t.Addr(), k.Addr)
	}
	if err := recordHostKey(sm, t, k.Key); err != nil {
		return err
	}
	sm.mu.Lock()
	delete(sm.pendingHostKeys, idx)
	sm.mu.Unlock()
	return nil
}

// DeclinePendingHostKey forgets the pending key for host idx.
func (sm *Manager) DeclinePendingHostKey(idx int) {
	sm.mu.Lock()
	delete(sm.pendingHostKeys, idx)
	sm.mu.Unlock()
}

// keyTypeAndFingerprint renders "<type> SHA256:<fp>".
func keyTypeAndFingerprint(k gossh.PublicKey) string {
	return strings.TrimSpace(k.Type() + " " + gossh.FingerprintSHA256(k))
}

// KeyDescription renders a pending key as "<keytype> SHA256:<fingerprint>".
func (e ErrUnknownHostKey) KeyDescription() string { return keyTypeAndFingerprint(e.Key) }
