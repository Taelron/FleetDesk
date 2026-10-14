package app

import (
	"errors"
	"os"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

func keyRune(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func hostListModel(hosts ...config.Host) Model {
	m := newTestModel()
	m.fleets = []config.Fleet{{Name: "f", Type: "vm"}}
	m.view = viewHostList
	m.hosts = hosts
	m.width, m.height = 120, 30
	return m
}

func TestHandoverOnlyRow(t *testing.T) {
	m := hostListModel(config.Host{
		Entry:  config.HostEntry{Name: "bastioned", Hostname: "b"},
		Status: config.HostHandoverOnly,
		Error:  "ProxyJump via jump.invalid",
	})
	out := m.renderHostList()
	want := "bastioned"
	if !strings.Contains(out, want) {
		t.Fatalf("host name missing:\n%s", out)
	}
	want = "handover only (ProxyJump via jump.invalid; in-app views unavailable)"
	if !strings.Contains(out, want) {
		t.Errorf("host row should read %q; got:\n%s", want, out)
	}
}

func TestHandoverOnlyKeys(t *testing.T) {
	h := config.Host{Entry: config.HostEntry{Name: "bastioned", Hostname: "b", User: "u"}, Status: config.HostHandoverOnly, Error: "ProxyJump via j"}
	for _, k := range []tea.KeyMsg{{Type: tea.KeyEnter}, keyRune('c')} {
		m := hostListModel(h)
		out, _ := m.handleHostListKeys(k)
		got := out.(Model)
		if got.flash != handoverOnlyFlash || got.view != viewHostList {
			t.Errorf("key %q: flash %q view %v, want the ProxyJump flash and no navigation", k.String(), got.flash, got.view)
		}
	}
	if !strings.Contains(handoverOnlyFlash, "Needs ProxyJump") || !strings.Contains(handoverOnlyFlash, "only shell, reboot and deploy-key work on this host") {
		t.Errorf("flash wording: %q", handoverOnlyFlash)
	}
	// shell and reboot and deploy-key are still offered
	for _, r := range []rune{'x', 'R', 'K'} {
		m := hostListModel(h)
		out, cmd := m.handleHostListKeys(keyRune(r))
		got := out.(Model)
		if r == 'x' && cmd == nil {
			t.Error("x on a handover-only host must start the handover")
		}
		if r != 'x' && got.modal == nil {
			t.Errorf("%c on a handover-only host must open its confirmation", r)
		}
		if got.flashError {
			t.Errorf("%c: unexpected error flash %q", r, got.flash)
		}
	}
	// d never fetches for it
	m := hostListModel(h)
	if cmd := m.fetchAllMetrics(); cmd != nil {
		if msg := cmd(); msg != nil {
			if _, ok := msg.(tea.BatchMsg); ok && len(msg.(tea.BatchMsg)) > 0 {
				t.Error("metrics must skip a handover-only host")
			}
		}
	}
	if cmd := m.startProbe(); cmd != nil {
		if b, ok := cmd().(tea.BatchMsg); ok && len(b) > 0 {
			t.Error("startProbe must skip a handover-only host")
		}
	}
	// a plain unreachable host is still refused
	m = hostListModel(config.Host{Entry: config.HostEntry{Name: "x"}, Status: config.HostUnreachable})
	out, _ := m.handleHostListKeys(keyRune('x'))
	if g := out.(Model); g.flash != "Host is not reachable" {
		t.Errorf("unreachable host: flash %q", g.flash)
	}
}

func TestApplyDialError(t *testing.T) {
	m := hostListModel(config.Host{Entry: config.HostEntry{Name: "h"}})
	if !m.applyDialError(0, ssh.ErrHandoverOnly{Jump: "j"}) || m.hosts[0].Status != config.HostHandoverOnly || m.hosts[0].Error != "ProxyJump via j" {
		t.Errorf("handover: %+v", m.hosts[0])
	}
	if !m.applyDialError(0, ssh.ErrSSHBinaryMissing) || m.hosts[0].Status != config.HostUnreachable || m.hosts[0].Error != "ssh binary not found on PATH" {
		t.Errorf("no ssh: %+v", m.hosts[0])
	}
	if m.applyDialError(0, errors.New("dial tcp: refused")) {
		t.Error("an ordinary error is not one of the special cases")
	}
}

func TestHostUserFallbacks(t *testing.T) {
	t.Setenv("USER", "localuser")
	m := hostListModel(
		config.Host{Entry: config.HostEntry{User: "fromfleet"}, ResolvedUser: "fromssh"},
		config.Host{ResolvedUser: "fromssh"},
		config.Host{},
	)
	for i, want := range []string{"fromfleet", "fromssh", "localuser"} {
		if got := m.hostUser(i); got != want {
			t.Errorf("hostUser(%d) = %q, want %q", i, got, want)
		}
	}
	if got := m.hostUser(99); got != os.Getenv("USER") {
		t.Errorf("out of range = %q", got)
	}
}

func TestHostKeyModal(t *testing.T) {
	answer := func(k tea.KeyMsg) (hostKeyDecidedMsg, bool) {
		mod := NewHostKeyModal(2, "th", "127.0.0.1:2222", "ssh-ed25519 SHA256:abc", "SHA256:abc")
		view := mod.View("bg", 100, 30)
		for _, want := range []string{"Unknown host key", "127.0.0.1:2222", "ssh-ed25519 SHA256:abc", "Trust this host and add it to known_hosts? [y/N]"} {
			if !strings.Contains(view, want) {
				t.Fatalf("modal should show %q:\n%s", want, view)
			}
		}
		cmd := mod.HandleKey(k)
		if cmd == nil {
			return hostKeyDecidedMsg{}, false
		}
		msg, ok := cmd().(hostKeyDecidedMsg)
		return msg, ok
	}
	if msg, ok := answer(keyRune('y')); !ok || !msg.accepted || msg.hostIdx != 2 || msg.fingerprint != "SHA256:abc" {
		t.Errorf("y: %+v %v", msg, ok)
	}
	for name, k := range map[string]tea.KeyMsg{"n": keyRune('n'), "enter (the default)": {Type: tea.KeyEnter}, "esc": {Type: tea.KeyEsc}} {
		if msg, ok := answer(k); !ok || msg.accepted {
			t.Errorf("%s must decline: %+v %v", name, msg, ok)
		}
	}
	// any other key does nothing
	mod := NewHostKeyModal(0, "th", "a", "k", "fp")
	if cmd := mod.HandleKey(keyRune('q')); cmd != nil || mod.Done() {
		t.Error("stray key must not answer the prompt")
	}
}

func TestResolveHostKey_DeclineAndNoPending(t *testing.T) {
	m := hostListModel(config.Host{Entry: config.HostEntry{Name: "h"}, Status: config.HostUnreachable, HostKeyPending: true})
	if cmd := m.resolveHostKey(hostKeyDecidedMsg{hostIdx: 0, accepted: false}); cmd != nil {
		t.Error("declining starts nothing")
	}
	if m.hosts[0].HostKeyPending || m.hosts[0].Error != "host key declined" || m.hosts[0].Status != config.HostUnreachable {
		t.Errorf("after decline: %+v", m.hosts[0])
	}
	m.hosts[0].HostKeyPending = true
	if cmd := m.resolveHostKey(hostKeyDecidedMsg{hostIdx: 0, accepted: true}); cmd != nil {
		t.Error("accepting a key that is not pending must not reconnect")
	}
	if !strings.Contains(m.hosts[0].Error, "cannot record host key") {
		t.Errorf("error = %q", m.hosts[0].Error)
	}
	m.openNextHostKeyModal() // nothing pending in the manager: must not open a modal
	if m.modal != nil {
		t.Error("no modal without a pending key")
	}
}

func TestDeployKeyScript(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	s := deployKeyScript(config.HostEntry{Hostname: "h'; rm -rf /; echo '", Port: 2200, User: "u"})
	for _, want := range []string{"ssh -t ", "ssh-copy-id ", "'-p' '2200'", "'User=u'", "'--'"} {
		if !strings.Contains(s, want) {
			t.Errorf("script lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "StrictHostKeyChecking") {
		t.Errorf("host-key checking must not be touched:\n%s", s)
	}
	// the hostile hostname stays inside one quoted argument
	if !strings.Contains(s, `'h'\''; rm -rf /; echo '\'''`) {
		t.Errorf("hostname not quoted as a single argument:\n%s", s)
	}
	if strings.Count(s, "'--'") != 2 {
		t.Errorf("both invocations need the -- terminator:\n%s", s)
	}
}
