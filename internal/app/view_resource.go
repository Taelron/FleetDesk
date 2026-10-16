package app

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pickerRow represents a single row in the dynamic resource picker.
type pickerRow struct {
	label   string
	total   int
	running int
	failed  int
	target  view
}

// countUnknown as a row's total means the count could not be read, shown as
// "n/a" — which is not the same as there being none.
const countUnknown = -1

// visibleResourceRows builds the dynamic resource picker rows based on current host state.
func (m Model) visibleResourceRows() []pickerRow {
	h := m.hosts[m.selectedHost]

	// Services + Containers counts
	svcTotal, svcRunning, svcFailed := 0, 0, 0
	if len(m.services) > 0 {
		svcTotal = len(m.services)
		for _, s := range m.services {
			switch s.State {
			case "running":
				svcRunning++
			case "failed":
				svcFailed++
			}
		}
	}
	ctnTotal, ctnRunning, ctnFailed := 0, 0, 0
	if len(m.containers) > 0 {
		ctnTotal = len(m.containers)
		for _, c := range m.containers {
			if strings.HasPrefix(c.Status, "Up") {
				ctnRunning++
			} else if !strings.HasPrefix(c.Status, "Exited (0)") && c.Status != "Created" {
				ctnFailed++
			}
		}
	}
	updTotal, updFailed := 0, 0
	if len(m.updates) > 0 {
		for _, u := range m.updates {
			if u.Type == "error" {
				updFailed++
			} else {
				updTotal++
			}
		}
	}

	// Process count
	procTotal, procFailed := 0, 0
	if len(m.processes) > 0 {
		procTotal = len(m.processes)
		for _, p := range m.processes {
			if p.State == "FATAL" || p.State == "BACKOFF" {
				procFailed++
			}
		}
	}

	sysLogTotal := h.ErrorCount
	if !h.ErrorCountKnown {
		sysLogTotal = countUnknown
	}

	rows := []pickerRow{
		{"Services", svcTotal, svcRunning, svcFailed, viewServiceList},
	}

	// Processes — conditional on supervisord presence
	if h.SupervisorctlPresent {
		rows = append(rows, pickerRow{"Processes", procTotal, 0, procFailed, viewProcessList})
	}

	rows = append(rows,
		pickerRow{"Containers", ctnTotal, ctnRunning, ctnFailed, viewContainerList},
		pickerRow{"Cron Jobs", h.CronCount, 0, 0, viewCronList},
		pickerRow{"System Logs", sysLogTotal, 0, 0, viewLogLevelPicker},
		pickerRow{"Updates", updTotal, 0, updFailed, viewUpdateList},
		pickerRow{"Disk", h.DiskCount, 0, h.DiskHighCount, viewDiskList},
		pickerRow{"Subscription", 0, 0, 0, viewSubscription},
		pickerRow{"Accounts", h.UserCount, 0, 0, viewAccountList},
		pickerRow{"Network", h.InterfacesTotal, h.InterfacesUp, 0, viewNetworkPicker},
	)

	// Security views
	rows = append(rows,
		pickerRow{"Failed Logins", 0, 0, 0, viewSecurityFailedLogins},
		pickerRow{"Sudo Activity", 0, 0, 0, viewSecuritySudo},
		pickerRow{"SELinux Denials", 0, 0, 0, viewSecuritySELinux},
		pickerRow{"Audit Summary", 0, 0, 0, viewSecurityAudit},
	)

	// Logs — conditional on configured log paths
	if len(h.Entry.Logs) > 0 {
		rows = append(rows, pickerRow{"Logs", len(h.Entry.Logs), 0, 0, viewLogFileList})
	}

	return rows
}

func (m Model) renderResourcePicker() string {
	h := m.hosts[m.selectedHost]
	f := m.fleets[m.selectedFleet]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	rows := m.visibleResourceRows()

	breadcrumb := f.Name + " › " + h.Entry.Name
	s := m.renderHeader(breadcrumb, m.resourceCursor+1, len(rows)) + "\n"
	s += borderStyle.Render("┌"+strings.Repeat("─", iw)+"┐") + "\n"

	nameCol := len("SELinux Denials") + 2

	hdr := fmt.Sprintf("     %-*s  %7s  %7s  %7s", nameCol, "RESOURCE", "TOTAL", "RUNNING", "FAILED")
	s += borderedRow(hdr, iw, colHeaderStyle) + "\n"
	s += borderStyle.Render("├"+strings.Repeat("─", iw)+"┤") + "\n"

	for i, r := range rows {
		cur := "   "
		if i == m.resourceCursor {
			cur = " ▸ "
		}
		failedStr := fmt.Sprintf("%d", r.failed)
		totalStr := fmt.Sprintf("%d", r.total)
		if r.total == countUnknown {
			totalStr = "n/a"
		}
		line := fmt.Sprintf("%s  %-*s  %7s  %7d  %7s", cur, nameCol, r.label, totalStr, r.running, failedStr)

		var style lipgloss.Style
		if i == m.resourceCursor {
			style = selectedRowStyle
		} else if i%2 == 0 {
			style = altRowStyle
		} else {
			style = normalRowStyle
		}
		s += borderedRow(line, iw, style) + "\n"
	}

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("└"+strings.Repeat("─", iw)+"┘") + "\n"
	s += m.renderHintBar(hintWithHelp([][]string{
		{"↑↓", "Navigate"},
		{"Enter", "Select"},
		{"r", "Refresh"},
		{"Esc", "Back"},
	}))
	return s
}

// resourcePickerEnter handles the Enter key in the resource picker.
// Returns the target view and fetch command based on the current cursor row.
func (m Model) resourcePickerEnter() (view, func() tea.Cmd, string) {
	rows := m.visibleResourceRows()
	if m.resourceCursor >= len(rows) {
		return viewResourcePicker, nil, ""
	}
	row := rows[m.resourceCursor]
	switch row.target {
	case viewServiceList:
		return viewServiceList, func() tea.Cmd { return m.fetchServices() }, "services"
	case viewProcessList:
		return viewProcessList, func() tea.Cmd { return m.fetchProcesses() }, "processes"
	case viewContainerList:
		return viewContainerList, func() tea.Cmd { return m.fetchContainers() }, "containers"
	case viewCronList:
		return viewCronList, func() tea.Cmd { return m.fetchCronJobs() }, "cron"
	case viewLogLevelPicker:
		return viewLogLevelPicker, func() tea.Cmd { return m.fetchLogLevels() }, "loglevels"
	case viewUpdateList:
		return viewUpdateList, func() tea.Cmd { return m.fetchUpdates() }, "updates"
	case viewDiskList:
		return viewDiskList, func() tea.Cmd { return m.fetchDisk() }, "disk"
	case viewSubscription:
		return viewSubscription, func() tea.Cmd { return m.fetchSubscription() }, "subscription"
	case viewAccountList:
		return viewAccountList, func() tea.Cmd { return m.fetchAccounts() }, "accounts"
	case viewNetworkPicker:
		return viewNetworkPicker, func() tea.Cmd { return m.fetchNetworkInfo() }, "network"
	case viewSecurityFailedLogins:
		return viewSecurityFailedLogins, func() tea.Cmd { return m.fetchFailedLogins() }, "failedlogins"
	case viewSecuritySudo:
		return viewSecuritySudo, func() tea.Cmd { return m.fetchSudoActivity() }, "sudo"
	case viewSecuritySELinux:
		return viewSecuritySELinux, func() tea.Cmd { return m.fetchSELinuxDenials() }, "selinux"
	case viewSecurityAudit:
		return viewSecurityAudit, func() tea.Cmd { return m.fetchAuditSummary() }, "audit"
	case viewLogFileList:
		return viewLogFileList, func() tea.Cmd { return m.fetchLogFileStats() }, "logfiles"
	}
	return viewResourcePicker, nil, ""
}
