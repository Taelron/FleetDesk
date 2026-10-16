package app

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/ssh"
)

// g needs rh_org_id, the secret name AND a secrets: block; nothing is
// resolved by pressing it or by confirming the prompt — the stream does that.
func TestRegisterKeyDoesNotResolveAtKeypress(t *testing.T) {
	m := subscriptionModel("Unknown")
	m.hosts[0].Entry.RHOrgID = "12345"
	m.hosts[0].Entry.RHActivationKeySecretName = "ak-name"

	out, cmd := m.handleSubscriptionKeys(keyRune('g'))
	m = out.(Model)
	if cmd != nil || m.modal == nil {
		t.Fatalf("g opens the confirmation and starts nothing: cmd=%v modal=%v", cmd != nil, m.modal != nil)
	}
	if !strings.Contains(m.modal.View("", 100, 30), "Register to Red Hat CDN? [Y/n]") {
		t.Errorf("ADR-F005 confirmation text:\n%s", m.modal.View("", 100, 30))
	}
	done := m.modal.HandleKey(keyRune('Y'))
	if done == nil {
		t.Fatal("confirming returns the start message")
	}
	start, ok := done().(startCommandStreamMsg)
	if !ok {
		t.Fatalf("got %T", done())
	}
	c := start.cfg
	if c.Secret == nil || c.Secret.Tag != ssh.RegisterTag || !c.Sudo || !c.AutoDone || c.ReturnView != viewSubscription || c.Title != "Register to Red Hat CDN" {
		t.Errorf("cfg = %+v secret=%+v", c, c.Secret)
	}
	if c.Command != "" {
		t.Errorf("CDN has no prefix command, got %q", c.Command)
	}
	if strings.Contains(c.Secret.Consumer, "subscription-manager") {
		t.Error("registration never uses the CLI")
	}
	if c.Secret.ExitErrors[ssh.RegisterExitRHELTooOld] != "registration requires RHEL 8 or later (RHSM D-Bus API)" {
		t.Errorf("exit errors = %v", c.Secret.ExitErrors)
	}
}

func TestRegisterKeyRequiresNameOrgAndBlock(t *testing.T) {
	for name, edit := range map[string]func(*Model){
		"no org":  func(m *Model) { m.hosts[0].Entry.RHActivationKeySecretName = "n" },
		"no name": func(m *Model) { m.hosts[0].Entry.RHOrgID = "o" },
		"no secrets": func(m *Model) {
			m.hosts[0].Entry.RHOrgID, m.hosts[0].Entry.RHActivationKeySecretName = "o", "n"
			m.fleets[0].Secrets = nil
		},
	} {
		m := subscriptionModel("Unknown")
		edit(&m)
		out, cmd := m.handleSubscriptionKeys(keyRune('g'))
		got := out.(Model)
		if cmd != nil || got.modal != nil || !got.flashError || got.flash != "rh_org_id and rh_activation_key_secret_name required in config" {
			t.Errorf("%s: modal=%v flash=%q", name, got.modal != nil, got.flash)
		}
	}
}

func TestRegisterSatelliteTitleAndPrefix(t *testing.T) {
	m := subscriptionModel("Unknown")
	m.hosts[0].Entry.RHOrgID, m.hosts[0].Entry.RHActivationKeySecretName = "o", "n"
	m.hosts[0].Entry.SatelliteURL = "sat.example.com"
	out, _ := m.handleSubscriptionKeys(keyRune('g'))
	m = out.(Model)
	start := m.modal.HandleKey(keyRune('Y'))().(startCommandStreamMsg)
	if start.cfg.Title != "Register to Satellite" || !strings.Contains(start.cfg.Command, "katello-ca-consumer-latest") {
		t.Errorf("cfg = %+v", start.cfg)
	}
}

// A resolution error is the stream's error, with no command and no reader.
func TestPrepareStreamCommand_ResolutionFailureStopsEverything(t *testing.T) {
	m := newTestModel()
	boom := errors.New(`secret "n": not found in vault "v"`)
	called := 0
	cmd, reader, err := prepareStreamCommand(m.ssh, 0, "sudo true", &SecretConsumer{
		Resolve: func() ([]byte, error) { called++; return nil, boom },
		Tag:     "TAG", Consumer: "true",
	})
	if !errors.Is(err, boom) || cmd != "" || reader != nil || called != 1 {
		t.Errorf("cmd=%q reader=%v err=%v called=%d", cmd, reader, err, called)
	}
}

// The resolved buffer is erased as soon as the reader owns its copy.
func TestPrepareStreamCommand_ErasesTheResolvedSecret(t *testing.T) {
	m := newTestModel()
	storePW(t, m, 0, "sudo-pw")
	var alias []byte
	cmd, reader, err := prepareStreamCommand(m.ssh, 0, "", &SecretConsumer{
		Resolve: func() ([]byte, error) {
			b := []byte("resolved-secret")
			alias = b
			return b, nil
		},
		Tag: "TAG", Consumer: "true",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Erase()
	for i, c := range alias {
		if c != 0 {
			t.Fatalf("byte %d of the resolved buffer survived: %q", i, alias)
		}
	}
	if strings.Contains(cmd, "resolved-secret") || strings.Contains(cmd, "sudo-pw") {
		t.Errorf("the command carries a credential: %s", cmd)
	}
	// and a plain stream is untouched
	plain, r, err := prepareStreamCommand(m.ssh, 0, "echo hi", nil)
	if err != nil || r != nil || plain != "echo hi" {
		t.Errorf("no secret, no sudo: %q %v %v", plain, r, err)
	}
}

// The done message carries the error to the user as a red flash, and the view
// says it did not run instead of "Done".
func TestStreamDoneWithError(t *testing.T) {
	m := newTestModel()
	m.fleets = []config.Fleet{{Name: "f", Type: "vm"}}
	m.hosts = []config.Host{{Entry: config.HostEntry{Name: "h"}}}
	m.streamGeneration = 3
	m.streamStreaming = true
	m.streamAutoDone = true
	out, _ := m.Update(sshStreamDoneMsg{generation: 3, err: errors.New(`secret "n": az CLI not found in PATH`)})
	got := out.(Model)
	if !got.flashError || got.flash != `secret "n": az CLI not found in PATH` || got.streamStreaming {
		t.Errorf("flash=%q error=%v streaming=%v", got.flash, got.flashError, got.streamStreaming)
	}
	joined := strings.Join(got.streamLines, "\n")
	if strings.Contains(joined, "Done") || !strings.Contains(joined, "Did not run") {
		t.Errorf("stream lines: %q", joined)
	}
	// a stale done from an earlier stream is ignored
	out, _ = m.Update(sshStreamDoneMsg{generation: 1, err: errors.New("old")})
	if g := out.(Model); g.flash == "old" {
		t.Error("stale done message acted on")
	}
}

// The error reaches the done message through the shared pointer the goroutine
// writes before closing the channel.
func TestListenForStreamLines_CarriesTheStreamError(t *testing.T) {
	m := newTestModel()
	ch := make(chan string)
	close(ch)
	want := errors.New("boom")
	m.streamChan, m.streamGeneration = ch, 7
	m.streamExitCode = new(int)
	e := want
	m.streamErr = &e
	msg := m.listenForStreamLines()().(sshStreamDoneMsg)
	if msg.err != want || msg.generation != 7 {
		t.Errorf("%+v", msg)
	}
}

// No message FleetDesk sends through the Bubble Tea loop may carry secret
// bytes: a message is copied, queued and may be logged by the runtime.
func TestNoMessageCarriesCredentialBytes(t *testing.T) {
	types := []any{
		sshStreamBatchMsg{}, sshStreamDoneMsg{}, startCommandStreamMsg{}, SSHStreamConfig{}, SecretConsumer{},
		hostKeyDecidedMsg{}, sudoReuseMsg{}, sudoProbeMsg{}, sudoTestMsg{}, sudoWizardResultMsg{},
	}
	for _, v := range types {
		walkNoBytes(t, reflect.TypeOf(v), reflect.TypeOf(v).String(), map[reflect.Type]bool{})
	}
	_ = tea.Msg(nil)
}

func walkNoBytes(t *testing.T, ty reflect.Type, path string, seen map[reflect.Type]bool) {
	t.Helper()
	if seen[ty] {
		return
	}
	seen[ty] = true
	switch ty.Kind() {
	case reflect.Slice, reflect.Array:
		if ty.Elem().Kind() == reflect.Uint8 {
			t.Errorf("%s is a byte slice: a message must never carry credential bytes", path)
			return
		}
		walkNoBytes(t, ty.Elem(), path+"[]", seen)
	case reflect.Struct:
		for i := range ty.NumField() {
			f := ty.Field(i)
			walkNoBytes(t, f.Type, path+"."+f.Name, seen)
		}
	case reflect.Pointer, reflect.Map:
		walkNoBytes(t, ty.Elem(), path+"*", seen)
	}
}
