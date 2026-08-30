//go:build testhost

package main_test

// Acceptance tests for TAE-21: Credential lifecycle -- []byte storage,
// erasure, and idle timeout. Written from the issue's acceptance criteria
// only -- no plan comment, no decision, no review, and no implementation
// were read to produce these.
//
// Why this file is red at a compile failure, against the issue's own
// instruction that a compile failure proves nothing: the issue's central
// criterion is that the storage boundary is []byte, not string --
// "a single string inside that span... invalidates the rest". Manager's
// Set... methods are string-typed today (internal/ssh/manager.go); calling
// them with []byte, as the issue's own ownership decision requires
// ("Set... methods take ownership of the caller's slice... this is what
// makes erasure observable through an alias, and therefore testable"), is
// the only way to write the erasure and idle-timeout tests below at all --
// there is no runtime-only route to a []byte-shaped assertion against a
// string-shaped API. The resulting compile failure is not incidental: it
// is the Storage AC made visible by the type checker, and every other
// assertion in this file is unreachable until it is fixed. See
// credential_lifecycle_test.go for the criteria that do not require the
// new signatures and are red at runtime today.
//
// Target Manager surface these tests fix as the acceptance contract (the
// issue's ownership decision plus the minimum this file needs to measure
// erasure and idle timeout without inventing anything the issue does not
// already require):
//
//	func (sm *Manager) SetSudoPassword(idx int, password []byte)
//	func (sm *Manager) SetCachedPassword(password []byte)
//	func (sm *Manager) ConnectWithPassword(idx int, h config.Host, password []byte) PasswordRetryResult
//	func (sm *Manager) SetCredentialTimeout(d time.Duration)
//
// All four take ownership of the slice passed in (no defensive copy), so a
// slice the test retains before the call is a valid alias for reading the
// buffer back after the manager is expected to have erased it -- read back
// via readByte below, a //go:noinline function, so the read cannot be
// folded away by the compiler seeing the buffer is otherwise unused after
// the call (the issue's own methodology: "via a function the compiler
// cannot inline").
//
// TestTAE21IdleTimeoutErasesCredentialAtDeadline is the idle-timeout
// measurement TestTAE21MakeTargetRunsIdleTimeoutMeasurement (in
// credential_lifecycle_test.go) requires a Make target for.
//
// Run (once Manager exposes the surface above):
//
//	go test -tags testhost -race -run TestTAE21 -v .
import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	fdssh "github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// readByte is a deliberately non-inlinable read through an index, so a
// buffer this test still holds an alias to cannot have its post-call
// zero-check optimized away by the compiler treating the buffer as dead
// after the call that (should have) erased it.
//
//go:noinline
func readByte(b []byte, i int) byte { return b[i] }

func allZero(t *testing.T, b []byte) bool {
	t.Helper()
	for i := range b {
		if readByte(b, i) != 0 {
			return false
		}
	}
	return true
}

func newTestManager() *fdssh.Manager {
	return fdssh.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
}

// testhostConfigHost builds the config.Host the TAE-98 fixture's connection
// info describes, for the given user -- the same shape
// newSSHManagerConnected (testhost_sudo_stdin_test.go) builds, without
// dialing immediately.
func testhostConfigHost(t *testing.T, info *testhostInfo, user string) config.Host {
	t.Helper()
	port, err := strconv.Atoi(info.Port)
	if err != nil {
		t.Fatalf("parse testhost port %q: %v", info.Port, err)
	}
	return config.Host{Entry: config.HostEntry{
		Hostname: info.Host,
		Port:     port,
		User:     user,
		Timeout:  10 * time.Second,
	}}
}

// --- AC (Erasure): a rejected password is erased before the next attempt
// is stored. Tested as the general invariant that makes this true
// regardless of which app-layer call site triggers the next Set: storing a
// new credential for a slot erases whatever was there before, in place. ---

func TestTAE21SetSudoPasswordErasesPreviousBufferBeforeStoringNext(t *testing.T) {
	sm := newTestManager()
	defer sm.Close()

	first := []byte("wrong-sudo-password")
	sm.SetSudoPassword(0, first)

	second := []byte("second-attempt-password")
	sm.SetSudoPassword(0, second)

	if !allZero(t, first) {
		t.Errorf("expected the first sudo password buffer to be erased in place once a second one is stored for the same host (TAE-21 Erasure AC -- \"a rejected password is erased before the next attempt is stored\"); alias still reads %q", first)
	}
}

func TestTAE21SetCachedPasswordErasesPreviousBufferBeforeStoringNext(t *testing.T) {
	sm := newTestManager()
	defer sm.Close()

	first := []byte("wrong-ssh-password")
	sm.SetCachedPassword(first)

	second := []byte("second-attempt-password")
	sm.SetCachedPassword(second)

	if !allZero(t, first) {
		t.Errorf("expected the first cached SSH password buffer to be erased in place once a second one is stored (TAE-21 Erasure AC -- \"this covers both credentials\"); alias still reads %q", first)
	}
}

// --- AC (Erasure): session credentials are erased on every path
// tea.Program.Run returns -- one erase after Run in main covers all of
// them, per the issue. Close is the primitive that erase must call; this
// pins Close itself actually erases, correcting the false :436 claim. ---

func TestTAE21CloseErasesAllCachedCredentials(t *testing.T) {
	sm := newTestManager()

	sshPw := []byte("session-ssh-password")
	sm.SetCachedPassword(sshPw)
	sudoPw := []byte("session-sudo-password")
	sm.SetSudoPassword(0, sudoPw)

	sm.Close()

	if !allZero(t, sshPw) {
		t.Errorf("expected Close to erase the cached SSH password in place (TAE-21 Erasure AC; corrects manager.go:436's false \"clears all cached credentials\"); alias still reads %q", sshPw)
	}
	if !allZero(t, sudoPw) {
		t.Errorf("expected Close to erase the cached sudo password in place (TAE-21 Erasure AC); alias still reads %q", sudoPw)
	}
}

// --- AC (Erasure): the SSH instance of "a rejected password is erased" --
// today a failed ConnectWithPassword leaves the wrong password cached
// (manager.go:1360-1369); this issue fixes that. ---

func TestTAE21ConnectWithPasswordErasesRejectedPasswordOnAuthFailure(t *testing.T) {
	info := requireTesthost(t)
	sm := newTestManager()
	defer sm.Close()

	host := testhostConfigHost(t, info, testUser1)
	wrong := []byte(testUser1Password + "-wrong")

	res := sm.ConnectWithPassword(0, host, wrong)
	if res.Err == nil {
		t.Fatalf("expected ConnectWithPassword with a wrong password against the TAE-98 fixture to fail, so this test actually exercises the rejection path")
	}
	if !allZero(t, wrong) {
		t.Errorf("expected a wrong password to be erased in place after ConnectWithPassword rejects it (TAE-21 Erasure AC; corrects the manager.go:1360-1369 defect the issue names -- \"a failed ConnectWithPassword currently leaves the wrong password cached\"); alias still reads %q", wrong)
	}
}

// --- AC (Idle timeout): a session credential unused for the timeout
// period is erased at the deadline, within a granularity of 30 seconds --
// not lazily at the next attempt to use it. This is the measurement
// TestTAE21MakeTargetRunsIdleTimeoutMeasurement requires a Make target
// for. ---

func TestTAE21IdleTimeoutErasesCredentialAtDeadline(t *testing.T) {
	sm := newTestManager()
	defer sm.Close()

	const timeout = 2 * time.Second
	sm.SetCredentialTimeout(timeout)

	buf := []byte("idle-timeout-measurement-password")
	sm.SetSudoPassword(0, buf)
	// Anchor the deadline to the SetSudoPassword call, not to time.Now()
	// after the sleep below -- otherwise it drifts past the real deadline.
	deadline := time.Now().Add(timeout)

	time.Sleep(timeout / 2)
	if allZero(t, buf) {
		t.Fatalf("expected the credential to still be resident well before its %s idle timeout elapses (TAE-21 Idle timeout AC -- erasure must happen at the deadline, not early); it was already zeroed", timeout)
	}

	// The default granularity is 30s; the deadline itself is 2s out, so
	// poll up to 30s past it before declaring the deadline missed.
	pollUntil := deadline.Add(30 * time.Second)
	for time.Now().Before(pollUntil) {
		if allZero(t, buf) {
			if time.Now().Before(deadline) {
				t.Fatalf("expected the credential to be erased at the %s deadline, not before it (TAE-21 Idle timeout AC)", timeout)
			}
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("expected the credential to be erased within 30s of its %s idle deadline (TAE-21 Idle timeout AC -- \"granularity of 30 seconds\"); it was still resident after %s", timeout, 30*time.Second+timeout)
}

// --- AC (Idle timeout): the timer resets on use, not on elapsed time; a
// presence check (GetSudoPassword, called on navigation per model.go:610,
// :718, :1251) is not use and must not reset it. ---

func TestTAE21PresenceCheckDoesNotResetIdleTimer(t *testing.T) {
	sm := newTestManager()
	defer sm.Close()

	const timeout = 2 * time.Second
	sm.SetCredentialTimeout(timeout)

	buf := []byte("presence-check-password")
	sm.SetSudoPassword(0, buf)

	// Repeated presence checks (GetSudoPassword), spanning past the
	// configured timeout, must not delay erasure.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		_ = sm.GetSudoPassword(0)
		time.Sleep(300 * time.Millisecond)
	}

	pollUntil := time.Now().Add(30 * time.Second)
	for time.Now().Before(pollUntil) {
		if allZero(t, buf) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("expected repeated GetSudoPassword presence checks (model.go:610/:718/:1251-shaped, fired on navigation) not to reset the idle timer (TAE-21 Idle timeout AC -- \"the timer resets on use... not on elapsed time\"); the credential was still resident %s after it was stored, well past the %s timeout", 3*time.Second+30*time.Second, timeout)
}

// --- AC (Idle timeout): "use" means delivery to a host -- a sudo relay.
// Real deliveries within the window must keep resetting the timer, so an
// actively-used credential does not expire mid-task. ---

func TestTAE21UseResetsIdleTimer(t *testing.T) {
	info := requireTesthost(t)
	sm := newTestManager()
	defer sm.Close()

	const timeout = 2 * time.Second
	sm.SetCredentialTimeout(timeout)

	host := testhostConfigHost(t, info, testUser1)
	res := sm.ConnectWithPassword(0, host, []byte(testUser1Password))
	if res.Err != nil {
		t.Fatalf("connect to the TAE-98 fixture: %v", res.Err)
	}

	buf := []byte(testUser1Password)
	sm.SetSudoPassword(0, buf)

	// Deliver every 800ms (well inside the 2s timeout) for 5s, comfortably
	// longer than the timeout itself: if delivery did not reset the timer,
	// this would already have expired.
	usingUntil := time.Now().Add(5 * time.Second)
	for time.Now().Before(usingUntil) {
		if _, err := sm.RunSudoCommand(0, "sudo true"); err != nil {
			t.Fatalf("RunSudoCommand during the active-use window: %v", err)
		}
		if allZero(t, buf) {
			t.Fatalf("expected an actively-delivered credential not to expire mid-task (TAE-21 Idle timeout AC -- \"an eight-hour session actively managing hosts does not re-prompt mid-task\"); it was erased while still in active use")
		}
		time.Sleep(800 * time.Millisecond)
	}

	// Now go idle and confirm it does still expire once use stops.
	pollUntil := time.Now().Add(timeout + 30*time.Second)
	for time.Now().Before(pollUntil) {
		if allZero(t, buf) {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Errorf("expected the credential to expire after use stopped and the %s idle timeout (plus 30s granularity) elapsed (TAE-21 Idle timeout AC); it never did", timeout)
}

// --- AC (Idle timeout): after expiry the next privileged action prompts
// with no expiry-specific messaging -- no "session expired" modal, no
// warning, no special-case text. Static canary, not a live measurement:
// protects the assumption that this issue's fix introduces no new
// expiry-specific user-facing string, the same style
// TestTAE20NoAppLayerCallSiteClaimsSessionStdin (testhost_sudo_stdin_test.go)
// uses to protect a stated assumption rather than measure a live system. ---

func TestTAE21NoExpirySpecificMessagingIntroduced(t *testing.T) {
	files, err := filepath.Glob("internal/app/*.go")
	if err != nil {
		t.Fatalf("glob internal/app/*.go: %v", err)
	}
	forbidden := []string{"session expired", "credential expired", "password expired", "your session has expired"}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		lower := strings.ToLower(string(data))
		for _, phrase := range forbidden {
			if strings.Contains(lower, phrase) {
				t.Errorf("expected no expiry-specific user-facing text in %s (TAE-21 Idle timeout AC -- \"no expiry-specific messaging\"); found %q", f, phrase)
			}
		}
	}
}
