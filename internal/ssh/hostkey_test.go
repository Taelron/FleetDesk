package ssh

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

func newEd25519Signer(t *testing.T) gossh.Signer {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func newECDSASigner(t *testing.T) gossh.Signer {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	s, err := gossh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// startSSHServer runs a server that accepts any client and presents the
// given host keys. It returns host and port.
func startSSHServer(t *testing.T, hostKeys ...gossh.Signer) (string, int) {
	t.Helper()
	cfg := &gossh.ServerConfig{NoClientAuth: true}
	for _, k := range hostKeys {
		cfg.AddHostKey(k)
	}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				sc, chans, reqs, err := gossh.NewServerConn(c, cfg)
				if err != nil {
					return
				}
				go gossh.DiscardRequests(reqs)
				go func() {
					for ch := range chans {
						_ = ch.Reject(gossh.Prohibited, "no")
					}
				}()
				_ = sc.Wait()
			}()
		}
	}()
	a := l.Addr().(*net.TCPAddr)
	return a.IP.String(), a.Port
}

func targetFor(t *testing.T, host string, port int) (Target, string) {
	t.Helper()
	kh := filepath.Join(t.TempDir(), ".ssh", "known_hosts") // directory does not exist yet
	return Target{Hostname: host, Port: port, User: "u", UserKnownHosts: []string{kh}}, kh
}

func dialTarget(t Target) (*gossh.Client, error) {
	return dialVerified(t, []gossh.AuthMethod{gossh.Password("x")}, t.User, 5*time.Second)
}

func TestDialVerified_TOFUThenSilentThenMismatch(t *testing.T) {
	key := newEd25519Signer(t)
	host, port := startSSHServer(t, key)
	tg, kh := targetFor(t, host, port)

	// first contact: unknown, with the fingerprint, and known_hosts created 0600/0700
	_, err := dialTarget(tg)
	var unk ErrUnknownHostKey
	if !errors.As(err, &unk) {
		t.Fatalf("first contact: got %v, want ErrUnknownHostKey", err)
	}
	if want := gossh.FingerprintSHA256(key.PublicKey()); unk.Fingerprint() != want {
		t.Errorf("Fingerprint = %q, want %q", unk.Fingerprint(), want)
	}
	if !strings.Contains(unk.KeyDescription(), "ssh-ed25519 SHA256:") {
		t.Errorf("KeyDescription = %q", unk.KeyDescription())
	}
	if unk.Addr != net.JoinHostPort(host, itoa(port)) {
		t.Errorf("Addr = %q", unk.Addr)
	}
	if IsAuthError(err) {
		t.Error("a host-key error must never read as an authentication error (a password would be offered to an unverified host)")
	}
	fi, err := os.Stat(kh)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("known_hosts should exist at 0600: %v %v", fi, err)
	}
	di, _ := os.Stat(filepath.Dir(kh))
	if di.Mode().Perm() != 0o700 {
		t.Errorf("~/.ssh mode = %04o, want 0700", di.Mode().Perm())
	}
	if b, _ := os.ReadFile(kh); len(b) != 0 {
		t.Fatalf("nothing may be recorded before the user accepts; known_hosts holds %q", b)
	}

	// accept: recorded, and the same key is now silent
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := trustForTest(sm, tg, unk.Key); err != nil {
		t.Fatal(err)
	}
	c, err := dialTarget(tg)
	if err != nil {
		t.Fatalf("after trusting: %v", err)
	}
	_ = c.Close()

	// the host presents a different key on the same address: mismatch, never trusted
	host2, port2 := startSSHServer(t, newEd25519Signer(t))
	_ = host2
	tg2 := tg
	tg2.Port = port2
	// reuse the recorded entry under the new port by recording the old key for it
	if err := trustForTest(sm, tg2, key.PublicKey()); err != nil {
		t.Fatal(err)
	}
	_, err = dialTarget(tg2)
	var mis ErrHostKeyMismatch
	if !errors.As(err, &mis) {
		t.Fatalf("changed key: got %v, want ErrHostKeyMismatch", err)
	}
	for _, want := range []string{"host key mismatch for ", "known_hosts has ssh-ed25519 SHA256:", ", host presented ssh-ed25519 SHA256:"} {
		if !strings.Contains(mis.Error(), want) {
			t.Errorf("message %q lacks %q", mis.Error(), want)
		}
	}
	if IsAuthError(err) || IsPasswordRejected(err) {
		t.Error("a mismatch is not an authentication error")
	}
	if _, isUnknown := err.(ErrUnknownHostKey); isUnknown {
		t.Error("a changed key must never be offered for trust")
	}
}

// trustForTest records key for the target through the same code TrustHostKey
// uses, without needing ssh -G.
func trustForTest(sm *Manager, tg Target, key gossh.PublicKey) error {
	return recordHostKey(sm, tg, key)
}

func itoa(n int) string { return fmtInt(n) }

func fmtInt(n int) string {
	var b []byte
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// A host that offers ed25519 and ECDSA, with only the ECDSA key recorded,
// must verify against the recorded key — not be reported as changed because
// the library preferred ed25519.
func TestDialVerified_UsesTheRecordedKeyType(t *testing.T) {
	ed, ec := newEd25519Signer(t), newECDSASigner(t)
	host, port := startSSHServer(t, ed, ec)
	tg, _ := targetFor(t, host, port)
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := trustForTest(sm, tg, ec.PublicKey()); err != nil {
		t.Fatal(err)
	}
	c, err := dialTarget(tg)
	if err != nil {
		t.Fatalf("recorded ECDSA key, host offers both: %v", err)
	}
	_ = c.Close()
}

// The same host presenting only a key type different from the recorded one
// is a mismatch.
func TestDialVerified_OtherTypeOnlyIsMismatch(t *testing.T) {
	host, port := startSSHServer(t, newEd25519Signer(t))
	tg, _ := targetFor(t, host, port)
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := trustForTest(sm, tg, newECDSASigner(t).PublicKey()); err != nil {
		t.Fatal(err)
	}
	_, err := dialTarget(tg)
	var mis ErrHostKeyMismatch
	if !errors.As(err, &mis) {
		t.Fatalf("got %v, want ErrHostKeyMismatch", err)
	}
}

func TestIsAuthError_HostKeyErrorsAreNot(t *testing.T) {
	k := newEd25519Signer(t).PublicKey()
	for name, err := range map[string]error{
		"mismatch": ErrHostKeyMismatch{Addr: "h:22", Want: k, Got: k},
		"unknown":  ErrUnknownHostKey{Addr: "h:22", Key: k},
		"wrapped":  errors.Join(errors.New("ssh: handshake failed"), ErrHostKeyMismatch{Addr: "h:22", Want: k, Got: k}),
	} {
		if IsAuthError(err) {
			t.Errorf("IsAuthError(%s) = true", name)
		}
		if IsPasswordRejected(err) {
			t.Errorf("IsPasswordRejected(%s) = true", name)
		}
	}
	if !IsAuthError(errors.New("ssh: handshake failed: ssh: unable to authenticate")) {
		t.Error("a real authentication failure must still be one")
	}
}

func TestPendingHostKeyLifecycle(t *testing.T) {
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	k := newEd25519Signer(t).PublicKey()
	sm.notePendingHostKey(3, ErrUnknownHostKey{Addr: "h:22", Key: k})
	if got, ok := sm.PendingHostKey(3); !ok || got.Addr != "h:22" {
		t.Fatalf("pending = %v %v", got, ok)
	}
	sm.DeclinePendingHostKey(3)
	if _, ok := sm.PendingHostKey(3); ok {
		t.Error("declining forgets the key")
	}
	sm.notePendingHostKey(3, ErrUnknownHostKey{Addr: "h:22", Key: k})
	sm.notePendingHostKey(3, nil) // a later clean connect clears it
	if _, ok := sm.PendingHostKey(3); ok {
		t.Error("a later success clears a stale pending key")
	}
	if err := sm.TrustPendingHostKey(9, config.HostEntry{}, "SHA256:x"); err == nil {
		t.Error("trusting with nothing pending must fail")
	}
}

// What is recorded is what was shown: a pending key replaced after the prompt
// opened is refused, not silently trusted.
func TestTrustPendingHostKey_RefusesAKeyThatWasNotShown(t *testing.T) {
	isolatedHome(t, "")
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	shown := newEd25519Signer(t).PublicKey()
	other := newEd25519Signer(t).PublicKey()
	e := config.HostEntry{Hostname: "somewhere.example"}
	tg, err := Resolve(e)
	if err != nil {
		t.Fatal(err)
	}
	sm.notePendingHostKey(0, ErrUnknownHostKey{Addr: tg.Addr(), Key: shown})
	shownFP := gossh.FingerprintSHA256(shown)
	// a later probe saw a different key
	sm.notePendingHostKey(0, ErrUnknownHostKey{Addr: tg.Addr(), Key: other})
	if err := sm.TrustPendingHostKey(0, e, shownFP); !errors.Is(err, ErrPendingKeyChanged) {
		t.Fatalf("got %v, want ErrPendingKeyChanged", err)
	}
	if b, _ := os.ReadFile(tg.UserKnownHosts[0]); len(b) != 0 {
		t.Errorf("nothing may be recorded: %q", b)
	}
	// the address the key was seen at must still be the address recorded
	sm.notePendingHostKey(0, ErrUnknownHostKey{Addr: "elsewhere.example:22", Key: shown})
	if err := sm.TrustPendingHostKey(0, e, shownFP); err == nil || !strings.Contains(err.Error(), "nothing was recorded") {
		t.Errorf("address changed: got %v", err)
	}
	// the key that was shown, at the address it was seen at, is recorded
	sm.notePendingHostKey(0, ErrUnknownHostKey{Addr: tg.Addr(), Key: shown})
	if err := sm.TrustPendingHostKey(0, e, shownFP); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(tg.UserKnownHosts[0])
	if !strings.Contains(string(b), "somewhere.example") || !strings.Contains(string(b), "ssh-ed25519") {
		t.Errorf("known_hosts = %q", b)
	}
	if _, ok := sm.PendingHostKey(0); ok {
		t.Error("a recorded key is no longer pending")
	}
}

// With HostKeyAlias, ssh looks the key up and records it under the alias; so
// does the library, or the two would disagree about what is trusted.
func TestHostKeyAlias(t *testing.T) {
	key := newEd25519Signer(t)
	host, port := startSSHServer(t, key)
	tg, kh := targetFor(t, host, port)
	tg.HostKeyAlias = "pinned-name"
	if want := "pinned-name:" + itoa(port); tg.KeyAddr() != want {
		t.Fatalf("KeyAddr = %q, want %q", tg.KeyAddr(), want)
	}
	if tg.Addr() != host+":"+itoa(port) {
		t.Errorf("Addr is still the dialled address: %q", tg.Addr())
	}
	sm := NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := dialTarget(tg)
	var unk ErrUnknownHostKey
	if !errors.As(err, &unk) || unk.Addr != tg.KeyAddr() {
		t.Fatalf("first contact: %v (addr %q)", err, unk.Addr)
	}
	if err := recordHostKey(sm, tg, unk.Key); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(kh)
	if !strings.Contains(string(b), "pinned-name") || strings.Contains(string(b), host) {
		t.Errorf("recorded under the alias, not the address: %q", b)
	}
	c, err := dialTarget(tg)
	if err != nil {
		t.Fatalf("trusted under the alias: %v", err)
	}
	_ = c.Close()
	// the same server without the alias is a different, unknown name
	tg.HostKeyAlias = ""
	if _, err := dialTarget(tg); !errors.As(err, &unk) {
		t.Errorf("without the alias the address is unknown: %v", err)
	}
}
