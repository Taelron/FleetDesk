package app

import (
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/credential"
)

func twoHostModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel()
	m.fleets = []config.Fleet{{Name: "f", Type: "vm"}}
	m.hosts = []config.Host{
		{Entry: config.HostEntry{Name: "hostA", Hostname: "a", User: "ua"}, Status: config.HostOnline},
		{Entry: config.HostEntry{Name: "hostB", Hostname: "b", User: "ub"}, Status: config.HostOnline},
	}
	return m
}

func storePW(t *testing.T, m Model, idx int, pw string) {
	t.Helper()
	b, err := credential.Clone([]byte(pw))
	if err != nil {
		t.Fatal(err)
	}
	m.ssh.SetSudoPassword(idx, b)
}

// A success on host A marks A, sets SudoReady on A only, and shares nothing.
func TestSudoSuccessStaysWithItsHost(t *testing.T) {
	m := twoHostModel(t)
	storePW(t, m, 0, "pwA")

	out, cmd := m.Update(sudoTestMsg{Success: true, HostIdx: 0, Entered: true})
	m = out.(Model)
	if cmd == nil {
		t.Fatal("expected navigation to continue")
	}
	if !m.hosts[0].SudoReady || m.hosts[1].SudoReady {
		t.Errorf("SudoReady = %v/%v, want true/false", m.hosts[0].SudoReady, m.hosts[1].SudoReady)
	}
	if !m.ssh.HasVerifiedSudo(0) {
		t.Error("A's password was tested on A: verified")
	}
	if m.ssh.GetSudoPassword(1) {
		t.Error("the password must not be shared to B in advance")
	}

	// the wizard path behaves the same
	m2 := twoHostModel(t)
	storePW(t, m2, 0, "pwA")
	out, _ = m2.Update(sudoWizardResultMsg{hostIdx: 0, tested: true, success: true})
	m2 = out.(Model)
	if !m2.hosts[0].SudoReady || m2.hosts[1].SudoReady || m2.ssh.GetSudoPassword(1) || !m2.ssh.HasVerifiedSudo(0) {
		t.Errorf("wizard success: %v/%v shared=%v verified=%v", m2.hosts[0].SudoReady, m2.hosts[1].SudoReady, m2.ssh.GetSudoPassword(1), m2.ssh.HasVerifiedSudo(0))
	}
}

// B needing a password is offered A's verified one, with a flash and no
// confirmation; with nothing verified nothing is offered.
func TestOfferVerifiedSudo(t *testing.T) {
	m := twoHostModel(t)
	if _, ok := m.offerVerifiedSudo(1, nil, func(*Model) {}); ok {
		t.Fatal("nothing verified: nothing to offer")
	}
	storePW(t, m, 0, "pwA")
	if _, ok := m.offerVerifiedSudo(1, nil, func(*Model) {}); ok {
		t.Fatal("a password that was never verified is not offered")
	}
	m.ssh.MarkSudoVerified(0)

	cmd, ok := m.offerVerifiedSudo(1, nil, func(*Model) {})
	if !ok || cmd == nil {
		t.Fatal("A's verified password should be offered to B")
	}
	if m.flash != "sudo: reusing the password verified on hostA" || m.flashError {
		t.Errorf("flash = %q (error %v)", m.flash, m.flashError)
	}
	if m.modal != nil {
		t.Error("reuse has no confirmation prompt")
	}
	if !m.ssh.GetSudoPassword(1) || m.ssh.HasVerifiedSudo(1) {
		t.Error("B holds the shared password but it is not verified on B yet")
	}
	// B has no connection here, so the test fails: B's copy is dropped, A's stays
	msg, ok := cmd().(sudoReuseMsg)
	if !ok || msg.success || msg.hostIdx != 1 {
		t.Fatalf("result = %+v", msg)
	}
	if m.ssh.GetSudoPassword(1) {
		t.Error("a failed reuse must drop B's copy")
	}
	if !m.ssh.HasVerifiedSudo(0) || !m.ssh.GetSudoPassword(0) {
		t.Error("a failure on B must not invalidate A's password")
	}

	// the failure opens the prompt; a success continues
	prompted := false
	out, _ := m.Update(sudoReuseMsg{hostIdx: 1, success: false, prompt: func(mm *Model) { prompted = true }})
	_ = out
	if !prompted {
		t.Error("a failed reuse opens the prompt")
	}
	ran := false
	proceed := func() tea.Msg { ran = true; return nil }
	storePW(t, m, 1, "pwA")
	out, next := m.Update(sudoReuseMsg{hostIdx: 1, success: true, proceed: proceed})
	m = out.(Model)
	if next == nil {
		t.Fatal("a successful reuse continues")
	}
	next()
	if !ran || !m.hosts[1].SudoReady || !m.ssh.HasVerifiedSudo(1) {
		t.Errorf("after success: ran=%v ready=%v verified=%v", ran, m.hosts[1].SudoReady, m.ssh.HasVerifiedSudo(1))
	}
}

// A password the user typed that is rejected goes straight back to the
// prompt — no second offer, no second authentication attempt.
func TestRejectedTypedSudoPasswordIsNotReoffered(t *testing.T) {
	m := twoHostModel(t)
	storePW(t, m, 0, "pwA")
	m.ssh.MarkSudoVerified(0)
	out, cmd := m.Update(sudoTestMsg{Success: false, HostIdx: 1, Entered: true})
	m = out.(Model)
	if cmd != nil || m.modal == nil || m.ssh.GetSudoPassword(1) {
		t.Errorf("typed rejection: cmd=%v modal=%v shared=%v", cmd != nil, m.modal != nil, m.ssh.GetSudoPassword(1))
	}
	if strings.Contains(m.flash, "reusing") {
		t.Errorf("flash = %q", m.flash)
	}
	// whereas a failed SSH-password seed does get the offer
	m = twoHostModel(t)
	storePW(t, m, 0, "pwA")
	m.ssh.MarkSudoVerified(0)
	out, cmd = m.Update(sudoTestMsg{Success: false, HostIdx: 1})
	m = out.(Model)
	if cmd == nil || !strings.Contains(m.flash, "reusing the password verified on hostA") {
		t.Errorf("seed failure: cmd=%v flash=%q", cmd != nil, m.flash)
	}
}

var picker = regexp.MustCompile(`System Logs\s+(\S+)`)

func TestResourcePickerShowsNAWhenCountUnknown(t *testing.T) {
	for _, c := range []struct {
		known bool
		count int
		want  string
	}{{false, 0, "n/a"}, {true, 0, "0"}, {true, 12, "12"}} {
		m := twoHostModel(t)
		m.selectedHost, m.selectedFleet, m.width, m.height = 0, 0, 100, 40
		m.hosts[0].ErrorCount, m.hosts[0].ErrorCountKnown = c.count, c.known
		got := picker.FindStringSubmatch(m.renderResourcePicker())
		if got == nil || got[1] != c.want {
			t.Errorf("known=%v count=%d: TOTAL = %v, want %q", c.known, c.count, got, c.want)
		}
	}
}

// A sudo stream on a host with no password held first asks whether the host
// wants one; a NOPASSWD host is neither prompted nor handed another host's.
func TestStreamSudoCheck(t *testing.T) {
	m := twoHostModel(t)
	storePW(t, m, 0, "pwA")
	m.ssh.MarkSudoVerified(0)
	cfg := SSHStreamConfig{Command: "sudo true", HostIdx: 1, Sudo: true, ReturnView: viewSubscription}

	out, cmd := m.Update(startCommandStreamMsg{cfg: cfg})
	m = out.(Model)
	if cmd == nil || m.modal != nil || m.view == viewSSHStream || m.ssh.GetSudoPassword(1) {
		t.Fatalf("nothing happens until the host has been asked: cmd=%v modal=%v shared=%v", cmd != nil, m.modal != nil, m.ssh.GetSudoPassword(1))
	}
	if msg, ok := cmd().(streamSudoCheckMsg); !ok || msg.noPassword {
		t.Fatalf("no connection: the check finds a password is needed, got %+v", msg)
	}

	// needs a password: A's is offered once
	out, cmd = m.Update(streamSudoCheckMsg{cfg: cfg})
	m = out.(Model)
	if cmd == nil || !strings.Contains(m.flash, "reusing the password verified on hostA") {
		t.Errorf("offer: cmd=%v flash=%q", cmd != nil, m.flash)
	}

	// needs none: the stream starts, A's password is not shared, no prompt
	m2 := twoHostModel(t)
	m2.fleets[0].Name = "f"
	m2.selectedHost = 1
	storePW(t, m2, 0, "pwA")
	m2.ssh.MarkSudoVerified(0)
	out, _ = m2.Update(streamSudoCheckMsg{cfg: cfg, noPassword: true})
	m2 = out.(Model)
	if m2.view != viewSSHStream || m2.modal != nil || m2.ssh.GetSudoPassword(1) || !m2.hosts[1].SudoReady {
		t.Errorf("NOPASSWD: view=%v modal=%v shared=%v ready=%v", m2.view, m2.modal != nil, m2.ssh.GetSudoPassword(1), m2.hosts[1].SudoReady)
	}
}

func TestHandoverOnlyFlashNamesWhatTheHostUses(t *testing.T) {
	jump := handoverOnlyFlashFor(config.Host{Error: "ProxyJump via j"})
	cmd := handoverOnlyFlashFor(config.Host{Error: "ProxyCommand"})
	if !strings.HasPrefix(jump, "Needs ProxyJump") || !strings.HasPrefix(cmd, "Needs a ProxyCommand") || !strings.Contains(cmd, "only shell, reboot and deploy-key work on this host") {
		t.Errorf("%q / %q", jump, cmd)
	}
}
