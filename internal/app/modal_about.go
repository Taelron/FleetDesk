package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const repoURL = "github.com/Gaetan-Jaminon/fleetdesk"

// aboutFieldMsg carries an async CLI result back to the model.
type aboutFieldMsg struct {
	field string // "azVersion", "azIdentity", "kubectl"
	value string
}

// AboutContent implements StepContent for the About modal.
// Fields are mutable — async goroutines update them via aboutFieldMsg.
type AboutContent struct {
	version    string
	repo       string
	azVersion  string
	azIdentity string
	kubectl    string
}

// NewAboutContent creates an AboutContent with static fields set and CLI fields loading.
func NewAboutContent(version, commit string) *AboutContent {
	ver := version
	if commit != "" && commit != "none" {
		ver = fmt.Sprintf("%s (%s)", version, commit)
	}
	return &AboutContent{
		version:    ver,
		repo:       repoURL,
		azVersion:  "loading...",
		azIdentity: "loading...",
		kubectl:    "loading...",
	}
}

// UpdateField sets a CLI field by name.
func (a *AboutContent) UpdateField(field, value string) {
	switch field {
	case "azVersion":
		a.azVersion = value
	case "azIdentity":
		a.azIdentity = value
	case "kubectl":
		a.kubectl = value
	}
}

// HandleKey implements StepContent for AboutContent.
func (a *AboutContent) HandleKey(msg tea.KeyMsg) (StepContent, tea.Cmd, bool) {
	switch msg.Type {
	case tea.KeyEnter, tea.KeyEsc:
		return a, nil, true
	}
	return a, nil, false
}

// View implements StepContent for AboutContent.
func (a *AboutContent) View(width int) string {
	labelW := 16
	var b strings.Builder
	fmt.Fprintf(&b, "  %-*s %s\n", labelW, "Version", a.version)
	fmt.Fprintf(&b, "  %-*s %s\n", labelW, "Repository", a.repo)
	b.WriteString("\n")
	fmt.Fprintf(&b, "  %-*s %s\n", labelW, "Azure CLI", a.azVersion)
	fmt.Fprintf(&b, "  %-*s %s\n", labelW, "Azure Identity", a.azIdentity)
	fmt.Fprintf(&b, "  %-*s %s\n", labelW, "kubectl", a.kubectl)
	return b.String()
}

// Result implements StepContent for AboutContent.
func (a *AboutContent) Result() any {
	return nil
}

// NewAboutModal creates the About modal with async CLI version fetching.
func NewAboutModal(version, commit string) (*ModalOverlay, tea.Cmd) {
	content := NewAboutContent(version, commit)
	m := NewModalOverlay("About FleetDesk", []ModalStep{
		{Title: "", Content: content},
	}, func(_ []any) tea.Cmd { return nil },
		func() tea.Cmd { return nil })
	m.FooterFn = func() string {
		return modalKeyStyle.Render("Esc") + " " + modalDimStyle.Render("close")
	}

	cmd := tea.Batch(
		fetchAzVersion(),
		fetchAzIdentity(),
		fetchKubectlVersion(),
	)
	return m, cmd
}

func fetchAzVersion() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		out, err := exec.CommandContext(ctx, "az", "version", "--output", "json").Output()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return aboutFieldMsg{field: "azVersion", value: "timeout"}
			}
			return aboutFieldMsg{field: "azVersion", value: "not found"}
		}

		var result map[string]any
		if err := json.Unmarshal(out, &result); err != nil {
			return aboutFieldMsg{field: "azVersion", value: "unknown"}
		}
		if v, ok := result["azure-cli"].(string); ok {
			return aboutFieldMsg{field: "azVersion", value: v}
		}
		return aboutFieldMsg{field: "azVersion", value: "unknown"}
	}
}

func fetchAzIdentity() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		out, err := exec.CommandContext(ctx, "az", "account", "show", "--output", "json").Output()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return aboutFieldMsg{field: "azIdentity", value: "timeout"}
			}
			return aboutFieldMsg{field: "azIdentity", value: "not found"}
		}

		var result map[string]any
		if err := json.Unmarshal(out, &result); err != nil {
			return aboutFieldMsg{field: "azIdentity", value: "unknown"}
		}
		if user, ok := result["user"].(map[string]any); ok {
			if name, ok := user["name"].(string); ok {
				return aboutFieldMsg{field: "azIdentity", value: name}
			}
		}
		return aboutFieldMsg{field: "azIdentity", value: "unknown"}
	}
}

func fetchKubectlVersion() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		out, err := exec.CommandContext(ctx, "kubectl", "version", "--client", "-o", "json").Output()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return aboutFieldMsg{field: "kubectl", value: "timeout"}
			}
			return aboutFieldMsg{field: "kubectl", value: "not found"}
		}

		var result map[string]any
		if err := json.Unmarshal(out, &result); err != nil {
			return aboutFieldMsg{field: "kubectl", value: "unknown"}
		}
		if ci, ok := result["clientVersion"].(map[string]any); ok {
			if v, ok := ci["gitVersion"].(string); ok {
				return aboutFieldMsg{field: "kubectl", value: v}
			}
		}
		return aboutFieldMsg{field: "kubectl", value: "unknown"}
	}
}
