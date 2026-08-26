package app

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/k8s"
)

func (m Model) renderK8sPodLogs() string {
	f := m.fleets[m.selectedFleet]
	cluster := m.k8sClusters[m.selectedK8sCluster]
	ns := m.k8sNamespaces[m.selectedK8sNamespace]
	wl := m.k8sWorkloads[m.selectedK8sWorkload]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	filtered := m.filteredK8sPodLogs()

	// Log detail view
	if m.showK8sLogDetail && len(filtered) > 0 && m.k8sPodLogCursor < len(filtered) {
		return m.renderK8sLogDetail(filtered[m.k8sPodLogCursor], f.Name, cluster.Name, ns.Name, wl.Name, iw)
	}
	filterInfo := ""
	if m.filterText != "" {
		filterInfo = fmt.Sprintf(" [filter: %s]", m.filterText)
	}
	logTarget := wl.Name
	if m.k8sPodLogFromDetail {
		logTarget = m.k8sPodDetail.Name
	}
	breadcrumb := f.Name + " \u203a " + cluster.Name + " \u203a " + m.selectedK8sContext + " \u203a " + ns.Name + " \u203a " + logTarget + " \u203a Logs"
	s := m.renderHeader(breadcrumb+filterInfo, m.k8sPodLogCursor+1, len(filtered)) + "\n"

	// Status bar with colored indicators
	var statusParts []string
	if m.k8sPodLogStreaming {
		statusParts = append(statusParts, ansiColor("● LIVE", "32"))
	} else {
		statusParts = append(statusParts, ansiColor("■ PAUSED", "33"))
	}
	if m.k8sPodLogCursor == 0 {
		statusParts = append(statusParts, ansiColor("↓ AUTO-SCROLL", "36"))
	}
	if m.k8sPodLogAllContainers {
		statusParts = append(statusParts, ansiColor("◉ ALL CONTAINERS", "35"))
	}
	switch m.k8sPodLogMinLevel {
	case 0:
		statusParts = append(statusParts, ansiColor("◆ ALL LEVELS", "32"))
	case 1:
		statusParts = append(statusParts, ansiColor("◆ INFO+", "36"))
	case 2:
		statusParts = append(statusParts, ansiColor("◆ WARN+", "33"))
	case 3:
		statusParts = append(statusParts, ansiColor("◆ ERROR+", "31"))
	}
	statusLine := "  " + strings.Join(statusParts, "  ")
	s += borderedRow(statusLine, iw+2, colHeaderStyle) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	if len(filtered) == 0 {
		if m.filterText != "" {
			s += borderedRow(fmt.Sprintf("  No matches for '%s'", m.filterText), iw, normalRowStyle) + "\n"
		} else {
			s += borderedRow("  No logs.", iw, normalRowStyle) + "\n"
		}
	} else {
		// Compute column widths
		timeCol := len("TIMESTAMP")
		levelCol := len("LEVEL")
		for _, e := range filtered {
			if len(e.Timestamp) > timeCol {
				timeCol = len(e.Timestamp)
			}
			if len(e.Level) > levelCol {
				levelCol = len(e.Level)
			}
		}
		if levelCol < 11 {
			levelCol = 11 // minimum for "Information"
		}
		timeCol += 2
		levelCol += 2

		hdr := fmt.Sprintf("     %-*s  %-*s  %s",
			timeCol, "TIMESTAMP",
			levelCol, "LEVEL",
			"MESSAGE",
		)
		s += borderedRow(hdr, iw, colHeaderStyle) + "\n"
		s += borderStyle.Render("\u251c"+strings.Repeat("\u2500", iw)+"\u2524") + "\n"

		maxVisible := m.height - 9
		if maxVisible < 1 {
			maxVisible = 1
		}
		offset := 0
		if m.k8sPodLogCursor >= offset+maxVisible {
			offset = m.k8sPodLogCursor - maxVisible + 1
		}
		end := offset + maxVisible
		if end > len(filtered) {
			end = len(filtered)
		}

		for i := offset; i < end; i++ {
			e := filtered[i]
			cur := "   "
			if i == m.k8sPodLogCursor {
				cur = " \u25b8 "
			}

			level := e.Level
			if level == "" {
				level = "-"
			}

			// Compute message width
			msgWidth := iw - timeCol - levelCol - 12 // account for cursor + spacing
			msg := e.Message
			if msgWidth > 0 && len(msg) > msgWidth {
				msg = msg[:msgWidth-3] + "..."
			}

			line := fmt.Sprintf("%s  %-*s  %-*s  %s",
				cur, timeCol, e.Timestamp,
				levelCol, level,
				msg,
			)

			var style lipgloss.Style
			if i == m.k8sPodLogCursor {
				style = selectedRowStyle
			} else {
				// Color by level
				lower := strings.ToLower(e.Level)
				switch {
				case lower == "error" || lower == "fatal" || lower == "critical":
					style = lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
				case lower == "warning" || lower == "warn":
					style = lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
				case i%2 == 0:
					style = altRowStyle
				default:
					style = normalRowStyle
				}
			}
			s += borderedRow(line, iw, style) + "\n"
		}
	}

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"

	{
		streamHint := "s"
		streamLabel := "Stop"
		if !m.k8sPodLogStreaming {
			streamLabel = "Resume"
		}
		containerLabel := "All Containers"
		if m.k8sPodLogAllContainers {
			containerLabel = "App Only"
		}
		levelLabel := "Log Level"
		s += m.renderHintBar(hintWithHelp([][]string{
			{"\u2191\u2193", "Navigate"},
			{"Enter", "Detail"},
			{streamHint, streamLabel},
			{"d", levelLabel},
			{"c", containerLabel},
			{"g/G", "Top/Bottom"},
			{"Esc", "Back"},
			{"q", "Quit"},
		}))
	}
	return s
}

func (m Model) renderK8sLogDetail(e k8s.K8sLogEntry, fleetName, clusterName, nsName, wlName string, iw int) string {
	breadcrumb := fleetName + " \u203a " + clusterName + " \u203a " + nsName + " \u203a " + wlName + " \u203a Log Detail"
	s := m.renderHeader(breadcrumb, 0, 0) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	// Build key/value items — fixed fields first
	items := []struct{ key, val string }{
		{"Timestamp", e.RawTime},
		{"Pod", e.Pod},
		{"Level", e.Level},
	}

	// Parse structured message into key/value pairs
	msg := e.RawMessage
	parsed := false
	if len(msg) > 0 && msg[0] == '{' {
		var j map[string]any
		if err := json.Unmarshal([]byte(msg), &j); err == nil {
			keys := make([]string, 0, len(j))
			for k := range j {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				items = append(items, struct{ key, val string }{k, fmt.Sprintf("%v", j[k])})
			}
			parsed = true
		}
	}
	if !parsed && strings.Contains(msg, "=") {
		// Try logfmt: key=value key="quoted value" ...
		pairs := parseLogfmtPairs(msg)
		if len(pairs) > 1 {
			for _, p := range pairs {
				items = append(items, struct{ key, val string }{p[0], p[1]})
			}
			parsed = true
		}
	}
	if !parsed && msg != "" {
		items = append(items, struct{ key, val string }{"Message", msg})
	}

	// Compute key column width
	keyWidth := 0
	for _, item := range items {
		if len(item.key) > keyWidth {
			keyWidth = len(item.key)
		}
	}

	for i, item := range items {
		val := item.val
		if val == "" {
			val = "—"
		}
		// Color the Level value
		if item.key == "Level" || item.key == "level" {
			lower := strings.ToLower(val)
			switch lower {
			case "error", "fatal", "critical":
				val = ansiColor(val, "31")
			case "warning", "warn":
				val = ansiColor(val, "33")
			case "debug":
				val = ansiColor(val, "36")
			case "information", "info":
				val = ansiColor(val, "32")
			}
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

	// Apply scrollable viewport
	contentLines := strings.Split(s, "\n")
	maxVisible := m.height - 4
	if maxVisible < 5 {
		maxVisible = 5
	}
	totalLines := len(contentLines)
	maxScroll := totalLines - maxVisible
	if maxScroll < 0 {
		maxScroll = 0
	}
	scrollOffset := m.k8sLogDetailScroll
	if scrollOffset > maxScroll {
		scrollOffset = maxScroll
	}
	endLine := scrollOffset + maxVisible
	if endLine > totalLines {
		endLine = totalLines
	}
	s = strings.Join(contentLines[scrollOffset:endLine], "\n") + "\n"

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"
	s += m.renderHintBar(hintWithHelp([][]string{
		{"\u2191\u2193", "Scroll"},
		{"g", "Top"},
		{"Esc", "Back"},
	}))
	return s
}

// parseLogfmtPairs parses a logfmt string into key/value pairs.
// Handles both key=value and key="quoted value" forms.
func parseLogfmtPairs(s string) [][2]string {
	var pairs [][2]string
	for len(s) > 0 {
		s = strings.TrimLeft(s, " ")
		if s == "" {
			break
		}
		eq := strings.IndexByte(s, '=')
		if eq < 0 {
			break
		}
		key := s[:eq]
		s = s[eq+1:]

		var val string
		if len(s) > 0 && s[0] == '"' {
			// Quoted value
			end := strings.IndexByte(s[1:], '"')
			if end < 0 {
				val = s[1:]
				s = ""
			} else {
				val = s[1 : end+1]
				s = s[end+2:]
			}
		} else {
			// Unquoted value — ends at next space
			sp := strings.IndexByte(s, ' ')
			if sp < 0 {
				val = s
				s = ""
			} else {
				val = s[:sp]
				s = s[sp:]
			}
		}
		pairs = append(pairs, [2]string{key, val})
	}
	return pairs
}
