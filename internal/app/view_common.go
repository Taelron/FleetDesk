package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mattn/go-runewidth"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/azure"
)

func (m Model) renderHeader(breadcrumb string, current, total int) string {
	title := "fleetdesk"
	if breadcrumb != "" {
		title += " \u203a " + breadcrumb
	}
	left := headerStyle.Render(title)
	right := headerCountStyle.Render(fmt.Sprintf("%d/%d", current, total))
	if m.version != "" {
		ver := lipgloss.NewStyle().Foreground(colorDimmed).Render("FleetDesk " + m.version)
		right = ver + "  " + right
	}
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 0 {
		gap = 0
	}
	return left + strings.Repeat(" ", gap) + right
}

// hintWithHelp appends the "? Help" hint to any hint bar.
func hintWithHelp(hints [][]string) [][]string {
	return append(hints, []string{"?", "Help"})
}

func (m Model) renderHintBar(hints [][]string) string {
	var parts []string
	for _, h := range hints {
		parts = append(parts, hintKeyStyle.Render("<"+h[0]+">")+" "+hintActionStyle.Render(h[1]))
	}
	left := strings.Join(parts, "  ")
	hintsOnly := hintBarStyle.Width(m.width).Render(left)
	if m.flash == "" {
		return hintsOnly
	}
	style := flashStyle
	if m.flashError {
		style = flashErrorStyle
	}
	fl := style.Render(m.flash)
	// The flash follows the hints on the same line when it fits there whole;
	// word-wrapping it mid-sentence across the end of the hint line makes it
	// unreadable, so otherwise it starts a line of its own.
	combined := hintBarStyle.Width(m.width).Render(left + "  " + fl)
	if strings.Contains(ansi.Strip(combined), m.flash) {
		return combined
	}
	return hintsOnly + "\n" + hintBarStyle.Width(m.width).Render(fl)
}

// borderedRow wraps content with | on each side, clamped to exactly w display columns.
func borderedRow(content string, w int, style lipgloss.Style) string {
	dw := lipgloss.Width(content)
	if dw > w {
		truncated := ""
		col := 0
		for _, r := range content {
			rw := runewidth.RuneWidth(r)
			if col+rw >= w {
				break
			}
			truncated += string(r)
			col += rw
		}
		content = truncated + "\u2026"
		dw = lipgloss.Width(content)
	}
	if dw < w {
		content += strings.Repeat(" ", w-dw)
	}
	b := borderStyle.Render("\u2502")
	return b + style.Render(content) + b
}

// renderActivityLog renders the activity log section for detail views.
func (m Model) renderActivityLog(iw int) string {
	hours := m.fleets[m.selectedFleet].ActivityLogHours
	var s string
	s += borderedRow("", iw, normalRowStyle) + "\n"
	s += borderedRow(fmt.Sprintf("  ── Recent Activity (Resource Group, last %dh) ──", hours), iw, colHeaderStyle) + "\n"
	s += borderedRow("", iw, normalRowStyle) + "\n"

	if len(m.azureActivityLog) == 0 {
		s += borderedRow("  No recent activity.", iw, normalRowStyle) + "\n"
	} else {
		s += m.renderActivityLogTable(iw, m.azureActivityLog, m.azureActivityCursor)
	}
	return s
}

func (m Model) renderActivityLogTable(iw int, entries []azure.ActivityLogEntry, cursor int) string {
	var s string
	timeCol := len("TIME")
	opCol := len("OPERATION")
	resCol := len("RESOURCE")
	statusCol := len("STATUS")
	for _, e := range entries {
		if len(e.Timestamp) > timeCol {
			timeCol = len(e.Timestamp)
		}
		if len(e.Operation) > opCol {
			opCol = len(e.Operation)
		}
		if len(e.Resource) > resCol {
			resCol = len(e.Resource)
		}
		if len(e.Status) > statusCol {
			statusCol = len(e.Status)
		}
	}
	timeCol += 2
	opCol += 2
	resCol += 2
	statusCol += 2

	logHdr := fmt.Sprintf("     %-*s  %-*s  %-*s  %-*s  %s",
		timeCol, "TIME", opCol, "OPERATION", resCol, "RESOURCE", statusCol, "STATUS", "CALLER")
	s += borderedRow(logHdr, iw, colHeaderStyle) + "\n"

	maxVisible := m.height - 24
	if maxVisible < 3 {
		maxVisible = 3
	}
	offset := 0
	if cursor >= offset+maxVisible {
		offset = cursor - maxVisible + 1
	}
	end := offset + maxVisible
	if end > len(entries) {
		end = len(entries)
	}

	for i := offset; i < end; i++ {
		e := entries[i]
		cur := "  "
		if i == cursor {
			cur = " ▸"
		}
		logLine := fmt.Sprintf("%s   %-*s  %-*s  %-*s  %-*s  %s",
			cur, timeCol, e.Timestamp, opCol, e.Operation, resCol, e.Resource, statusCol, e.Status, e.Caller)
		var style lipgloss.Style
		if i == cursor {
			style = selectedRowStyle
		} else if strings.Contains(strings.ToLower(e.Status), "fail") {
			style = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
		} else if i%2 == 0 {
			style = altRowStyle
		} else {
			style = normalRowStyle
		}
		s += borderedRow(logLine, iw, style) + "\n"
	}
	return s
}

func (m Model) padToBottom(s string, iw int) string {
	lines := strings.Count(s, "\n")
	for i := lines; i < m.height-3; i++ {
		s += borderedRow("", iw, normalRowStyle) + "\n"
	}
	return s
}
