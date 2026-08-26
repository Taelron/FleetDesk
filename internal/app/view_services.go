package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
)

func (m Model) renderServiceList() string {
	h := m.hosts[m.selectedHost]
	f := m.fleets[m.selectedFleet]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	// detail view
	if m.showServiceDetail {
		svcName := m.serviceStatus.Name
		if svcName == "" {
			svcName = "unknown"
		}
		breadcrumb := f.Name + " \u203a " + h.Entry.Name + " \u203a Services \u203a " + svcName
		s := m.renderHeader(breadcrumb, 0, 0) + "\n"
		s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

		// Status section
		s += borderedRow("  Status", iw, colHeaderStyle) + "\n"

		activeDisplay := m.serviceStatus.ActiveState
		if m.serviceStatus.SubState != "" && m.serviceStatus.SubState != m.serviceStatus.ActiveState {
			activeDisplay += " (" + m.serviceStatus.SubState + ")"
		}
		activeStyled := activeDisplay
		switch m.serviceStatus.ActiveState {
		case "active":
			activeStyled = lipgloss.NewStyle().Foreground(colorGreen).Render(activeDisplay)
		case "failed":
			activeStyled = lipgloss.NewStyle().Foreground(colorRed).Render(activeDisplay)
		case "inactive":
			activeStyled = lipgloss.NewStyle().Foreground(colorYellow).Render(activeDisplay)
		}

		statusItems := []struct{ key, val string }{
			{"Unit", svcName},
			{"Description", m.serviceStatus.Description},
			{"Active", activeDisplay},
			{"PID", m.serviceStatus.PID},
			{"Memory", m.serviceStatus.Memory},
			{"Tasks", m.serviceStatus.Tasks},
			{"Since", m.serviceStatus.Since},
			{"Enabled", m.serviceStatus.Enabled},
		}

		keyWidth := 0
		for _, item := range statusItems {
			if len(item.key) > keyWidth {
				keyWidth = len(item.key)
			}
		}

		for i, item := range statusItems {
			val := item.val
			if item.key == "Active" {
				val = activeStyled
			}
			line := fmt.Sprintf("    %-*s  %s", keyWidth, item.key, val)
			var style lipgloss.Style
			if i%2 == 0 {
				style = altRowStyle
			} else {
				style = normalRowStyle
			}
			s += borderedRow(line, iw, style) + "\n"
		}

		// separator
		s += borderStyle.Render("\u251c"+strings.Repeat("\u2500", iw)+"\u2524") + "\n"

		// Logs section
		s += borderedRow("  Recent Logs", iw, colHeaderStyle) + "\n"

		logs := m.filteredServiceLogs()

		if m.filterActive {
			s += borderedRow(fmt.Sprintf("  / %s\u2588", m.filterText), iw, normalRowStyle) + "\n"
		}

		if len(logs) == 0 {
			if m.filterText != "" {
				s += borderedRow("    No matching log entries", iw, normalRowStyle) + "\n"
			} else {
				s += borderedRow("    No log entries", iw, normalRowStyle) + "\n"
			}
		} else {
			// scrollable log area
			maxVisible := m.height - 16
			if m.filterActive {
				maxVisible--
			}
			if maxVisible < 3 {
				maxVisible = 3
			}
			offset := 0
			if m.serviceLogCursor >= offset+maxVisible {
				offset = m.serviceLogCursor - maxVisible + 1
			}
			end := offset + maxVisible
			if end > len(logs) {
				end = len(logs)
			}

			errorStyle := lipgloss.NewStyle().Foreground(colorRed)
			for i := offset; i < end; i++ {
				line := logs[i]
				cur := "  "
				if i == m.serviceLogCursor {
					cur = " \u25b8"
				}
				display := cur + "  " + line
				lower := strings.ToLower(line)
				var style lipgloss.Style
				if i == m.serviceLogCursor {
					style = selectedRowStyle
				} else if strings.Contains(lower, "error") || strings.Contains(lower, "fail") {
					style = errorStyle
				} else if i%2 == 0 {
					style = altRowStyle
				} else {
					style = normalRowStyle
				}
				s += borderedRow(display, iw, style) + "\n"
			}
		}

		s = m.padToBottom(s, iw)
		s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"

		s += m.renderHintBar(hintWithHelp([][]string{
			{"\u2191\u2193", "Scroll"},
			{"/", "Search"},
			{"s", "Start"},
			{"o", "Stop"},
			{"t", "Restart"},
			{"r", "Refresh"},
			{"Esc", "Back"},
		}))
		return s
	}

	filtered := m.filteredServices()
	filterInfo := ""
	if m.filterText != "" {
		filterInfo = fmt.Sprintf(" [filter: %s]", m.filterText)
	}
	breadcrumb := f.Name + " \u203a " + h.Entry.Name + " \u203a Services"
	s := m.renderHeader(breadcrumb+filterInfo, m.serviceCursor+1, len(filtered)) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	maxVisible := m.height - 8
	if maxVisible < 1 {
		maxVisible = 1
	}

	emptyMsg := "  No services found."
	if m.filterText != "" {
		emptyMsg = fmt.Sprintf("  No matches for '%s'", m.filterText)
	}

	fleetName := m.fleets[m.selectedFleet].Name
	hostName := m.hosts[m.selectedHost].Entry.Name
	s += renderList(ListConfig{
		Columns: []ListColumn{
			{Label: "SERVICE", SortIndex: 1},
			{Label: "STATE", Width: 10, SortIndex: 2},
			{Label: "ENABLED", SortIndex: 3},
			{Label: "DESCRIPTION", SortIndex: 4},
		},
		RowCount: len(filtered),
		RowBuilder: func(i int) []string {
			svc := filtered[i]
			desc := svc.Description
			if desc == "" {
				desc = "\u2014"
			}
			return []string{svc.Name, svc.State, svc.Enabled, desc}
		},
		RowPrefix: func(i int) string {
			note := m.notePrefix(notes.ResourceRef{
				Fleet:    fleetName,
				Segments: []string{"hosts", hostName, "services", filtered[i].Name},
			})
			if filtered[i].State == "failed" {
				return note + "\u2717 "
			}
			return note
		},
		Cursor:        m.serviceCursor,
		MaxVisible:    maxVisible,
		InnerWidth:    iw,
		SortIndicator: m.sortIndicator,
		EmptyMessage:  emptyMsg,
	})

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"

	if m.filterActive {
		s += hintBarStyle.Width(m.width).Render(fmt.Sprintf("  /%s\u2588", m.filterText))
	} else {
		s += m.renderHintBar(hintWithHelp([][]string{
			{"\u2191\u2193", "Navigate"},
			{"1-4", "Sort"},
			{"Enter", "Detail"},
			{"n", "Notes"},
			{"/", "Search"},
			{"r", "Refresh"},
			{"Esc", "Back"},
		}))
	}
	return s
}
