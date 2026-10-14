package app

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

func subscriptionModel(regType string, extraSubs ...config.Subscription) Model {
	m := newTestModel()
	m.view = viewSubscription
	m.fleets = []config.Fleet{{Name: "test-fleet", Type: "vm",
		Secrets: &config.SecretsConfig{Provider: "azure-keyvault", Vault: "kv", Subscription: "sub"}}}
	m.selectedFleet = 0
	m.hosts = []config.Host{{
		Entry:  config.HostEntry{Name: "host1", Hostname: "10.0.0.1", User: "ansible", Port: 22},
		Status: config.HostOnline,
	}}
	m.selectedHost = 0
	m.subscriptions = []config.Subscription{
		{Field: "Registration", Value: regType},
	}
	m.subscriptions = append(m.subscriptions, extraSubs...)
	return m
}

// --- Unregister (u key) ---

func TestUnregisterAction(t *testing.T) {
	t.Run("u on unregistered host flashes error, no modal", func(t *testing.T) {
		m := subscriptionModel("Unknown")
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}
		result, cmd := m.handleSubscriptionKeys(msg)
		m2 := result.(Model)
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		if m2.modal != nil {
			t.Error("expected modal = nil")
		}
		if m2.flash != "Host is not registered" {
			t.Errorf("flash = %q, want %q", m2.flash, "Host is not registered")
		}
		if !m2.flashError {
			t.Error("expected flashError = true")
		}
	})

	t.Run("u on empty registration flashes error, no modal", func(t *testing.T) {
		m := subscriptionModel("")
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}
		result, cmd := m.handleSubscriptionKeys(msg)
		m2 := result.(Model)
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		if m2.modal != nil {
			t.Error("expected modal = nil")
		}
		if !m2.flashError {
			t.Error("expected flashError = true")
		}
	})

	t.Run("u on CDN host shows confirm modal", func(t *testing.T) {
		m := subscriptionModel("Red Hat CDN")
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}
		result, cmd := m.handleSubscriptionKeys(msg)
		m2 := result.(Model)
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
	})

	t.Run("u on Satellite host shows confirm modal", func(t *testing.T) {
		m := subscriptionModel("Satellite")
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}}
		result, cmd := m.handleSubscriptionKeys(msg)
		m2 := result.(Model)
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
	})

	t.Run("u confirm yes completes modal", func(t *testing.T) {
		m := subscriptionModel("Red Hat CDN")
		result, _ := m.handleSubscriptionKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
		m2 := result.(Model)
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
		cmd := m2.modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
		if cmd == nil {
			t.Error("expected non-nil cmd after confirming unregister")
		}
		if !m2.modal.Done() {
			t.Error("expected modal to be done after confirm")
		}
	})

	t.Run("u confirm no cancels", func(t *testing.T) {
		m := subscriptionModel("Red Hat CDN")
		result, _ := m.handleSubscriptionKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
		m2 := result.(Model)
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
		cmd := m2.modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
		if cmd == nil {
			t.Error("expected non-nil cmd from cancel (confirmCancelledMsg)")
		}
		if !m2.modal.Done() {
			t.Error("expected modal to be done after cancel")
		}
	})
}

// --- Register CDN (g key) ---

func TestRegisterCDNAction(t *testing.T) {
	t.Run("g without config shows flash error", func(t *testing.T) {
		m := subscriptionModel("Unknown")
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
		result, cmd := m.handleSubscriptionKeys(msg)
		m2 := result.(Model)
		if cmd != nil {
			t.Error("expected nil cmd")
		}
		if m2.modal != nil {
			t.Error("expected modal = nil without config")
		}
		if !m2.flashError {
			t.Error("expected flashError = true")
		}
	})

	t.Run("g with CDN config shows confirm modal", func(t *testing.T) {
		m := subscriptionModel("Unknown")
		m.hosts[0].Entry.RHOrgID = "12345"
		m.hosts[0].Entry.RHActivationKeySecretName = "ak-cdn"
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
		result, cmd := m.handleSubscriptionKeys(msg)
		m2 := result.(Model)
		if cmd != nil {
			t.Error("expected nil cmd — confirm not yet fired")
		}
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
	})

	t.Run("g with satellite_url shows confirm modal", func(t *testing.T) {
		m := subscriptionModel("Unknown")
		m.hosts[0].Entry.RHOrgID = "Fluxys"
		m.hosts[0].Entry.RHActivationKeySecretName = "ak-sat"
		m.hosts[0].Entry.SatelliteURL = "flxsatprd01.central.fluxys.int"
		msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
		result, _ := m.handleSubscriptionKeys(msg)
		m2 := result.(Model)
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
	})

	t.Run("g is always available regardless of registration state", func(t *testing.T) {
		for _, regType := range []string{"", "Unknown", "Red Hat CDN", "Satellite"} {
			m := subscriptionModel(regType)
			m.hosts[0].Entry.RHOrgID = "12345"
			m.hosts[0].Entry.RHActivationKeySecretName = "ak-test"
			msg := tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}}
			result, _ := m.handleSubscriptionKeys(msg)
			m2 := result.(Model)
			if m2.modal == nil {
				t.Errorf("regType=%q: expected modal to be set", regType)
			}
		}
	})

	t.Run("g confirm yes completes modal", func(t *testing.T) {
		m := subscriptionModel("Unknown")
		m.hosts[0].Entry.RHOrgID = "12345"
		m.hosts[0].Entry.RHActivationKeySecretName = "ak-test"
		result, _ := m.handleSubscriptionKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
		m2 := result.(Model)
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
		cmd := m2.modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'Y'}})
		if cmd == nil {
			t.Error("expected non-nil cmd after confirming register")
		}
		if !m2.modal.Done() {
			t.Error("expected modal to be done after confirm")
		}
	})

	t.Run("g confirm no cancels", func(t *testing.T) {
		m := subscriptionModel("Unknown")
		m.hosts[0].Entry.RHOrgID = "12345"
		m.hosts[0].Entry.RHActivationKeySecretName = "ak-test"
		result, _ := m.handleSubscriptionKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
		m2 := result.(Model)
		if m2.modal == nil {
			t.Fatal("expected modal to be set")
		}
		cmd := m2.modal.HandleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'N'}})
		if cmd == nil {
			t.Error("expected non-nil cmd from cancel")
		}
		if !m2.modal.Done() {
			t.Error("expected modal to be done after cancel")
		}
	})
}

// --- Hint bar ---

func TestSubscriptionHintBar(t *testing.T) {
	t.Run("CDN host shows Register CDN", func(t *testing.T) {
		m := subscriptionModel("Red Hat CDN")
		rendered := m.renderSubscription()
		if !strings.Contains(rendered, "Unregister") {
			t.Error("hint bar missing Unregister")
		}
		if !strings.Contains(rendered, "Register CDN") {
			t.Error("hint bar missing Register CDN")
		}
	})

	t.Run("Satellite host shows Register Satellite", func(t *testing.T) {
		m := subscriptionModel("Satellite")
		m.hosts[0].Entry.SatelliteURL = "sat.example.com"
		rendered := m.renderSubscription()
		if !strings.Contains(rendered, "Register Satellite") {
			t.Error("hint bar missing Register Satellite")
		}
	})

	t.Run("hint bar advertises Check Repo", func(t *testing.T) {
		m := subscriptionModel("Satellite")
		rendered := m.renderSubscription()
		if !strings.Contains(rendered, "Check Repo") {
			t.Error("hint bar missing Check Repo")
		}
	})
}

// --- Check Repo (c key) ---

func TestCheckRepoAction(t *testing.T) {
	t.Run("c on a Repo entry switches to ssh stream view", func(t *testing.T) {
		m := subscriptionModel("Satellite",
			config.Subscription{Field: "Repo: rhel-9-for-x86_64-baseos-rpms", Value: "ERROR"},
		)
		// cursor on the Repo entry (Registration is index 0, Repo is index 1)
		m.subscriptionCursor = 1

		result, cmd := m.handleSubscriptionKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
		m2 := result.(Model)

		if cmd == nil {
			t.Fatal("expected non-nil cmd from check repo")
		}
		if m2.view != viewSSHStream {
			t.Errorf("view = %v, want viewSSHStream", m2.view)
		}
		if !strings.Contains(m2.streamTitle, "rhel-9-for-x86_64-baseos-rpms") {
			t.Errorf("streamTitle = %q, want it to include the repo id", m2.streamTitle)
		}
		if m2.streamReturnView != viewSubscription {
			t.Errorf("streamReturnView = %v, want viewSubscription", m2.streamReturnView)
		}
	})

	t.Run("c on non-Repo entry is a no-op", func(t *testing.T) {
		m := subscriptionModel("Satellite") // only Registration entry, no Repo
		m.subscriptionCursor = 0

		result, cmd := m.handleSubscriptionKeys(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
		m2 := result.(Model)

		if cmd != nil {
			t.Error("expected nil cmd when cursor is not on a Repo entry")
		}
		if m2.view == viewSSHStream {
			t.Error("view should not switch when not on a Repo entry")
		}
	})
}
