package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
)

func (m Model) renderLogFileList() string {
	h := m.hosts[m.selectedHost]
	f := m.fleets[m.selectedFleet]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	filtered := m.filteredLogFiles()
	filterInfo := ""
	if m.filterText != "" {
		filterInfo = fmt.Sprintf(" [filter: %s]", m.filterText)
	}

	breadcrumb := f.Name + " › " + h.Entry.Name + " › Logs"
	cur := 0
	if len(filtered) > 0 {
		cur = m.logFileCursor + 1
	}
	s := m.renderHeader(breadcrumb+filterInfo, cur, len(filtered)) + "\n"
	s += borderStyle.Render("┌"+strings.Repeat("─", iw)+"┐") + "\n"

	if len(filtered) == 0 {
		msg := "  No log files configured"
		if m.filterText != "" {
			msg = fmt.Sprintf("  No matches for '%s'", m.filterText)
		}
		s += borderedRow(msg, iw, normalRowStyle) + "\n"
	} else {
		nameCol := len("NAME")
		pathCol := len("PATH")
		// modCol not needed — last column is not fixed-width
		for _, e := range filtered {
			if len(e.entry.Name) > nameCol {
				nameCol = len(e.entry.Name)
			}
			if len(e.entry.Path) > pathCol {
				pathCol = len(e.entry.Path)
			}
		}
		nameCol += 2
		// Cap path column
		maxPath := iw - nameCol - 40
		if maxPath < 10 {
			maxPath = 10
		}
		if pathCol > maxPath {
			pathCol = maxPath
		}
		sizeCol := 8

		hdr := fmt.Sprintf("     %-*s  %-*s  %-*s  %s",
			nameCol, "NAME"+m.sortIndicator(1),
			pathCol, "PATH"+m.sortIndicator(2),
			sizeCol, "SIZE"+m.sortIndicator(3),
			"LAST MODIFIED"+m.sortIndicator(4),
		)
		s += borderedRow(hdr, iw, colHeaderStyle) + "\n"
		s += borderStyle.Render("├"+strings.Repeat("─", iw)+"┤") + "\n"

		maxVisible := m.height - 8
		if maxVisible < 1 {
			maxVisible = 1
		}
		offset := 0
		if m.logFileCursor >= offset+maxVisible {
			offset = m.logFileCursor - maxVisible + 1
		}
		end := offset + maxVisible
		if end > len(filtered) {
			end = len(filtered)
		}

		for i := offset; i < end; i++ {
			item := filtered[i]
			cursor := "   "
			if i == m.logFileCursor {
				cursor = " ▸ "
			}

			pathStr := item.entry.Path
			if len(pathStr) > pathCol {
				pathStr = pathStr[:pathCol-1] + "…"
			}

			sizeStr := "---"
			modStr := "---"
			if stat, ok := m.logFileStats[item.origIdx]; ok {
				sizeStr = stat.Size
				modStr = stat.ModTime
			}

			// Sudo indicator
			sudoPrefix := " "
			if item.entry.Sudo {
				sudoPrefix = ansiColor("⚡", "33")
			}

			line := fmt.Sprintf("%s%s %-*s  %-*s  %-*s  %s",
				cursor, sudoPrefix,
				nameCol, item.entry.Name,
				pathCol, pathStr,
				sizeCol, sizeStr,
				modStr,
			)

			var style lipgloss.Style
			if i == m.logFileCursor {
				style = selectedRowStyle
			} else if i%2 == 0 {
				style = altRowStyle
			} else {
				style = normalRowStyle
			}
			s += borderedRow(line, iw, style) + "\n"
		}
	}

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("└"+strings.Repeat("─", iw)+"┘") + "\n"

	if m.filterActive {
		s += hintBarStyle.Width(m.width).Render(fmt.Sprintf("  /%s█", m.filterText))
	} else {
		s += m.renderHintBar(hintWithHelp([][]string{
			{"↑↓", "Navigate"},
			{"Enter", "Tail"},
			{"/", "Filter"},
			{"r", "Refresh"},
			{"Esc", "Back"},
		}))
	}
	return s
}

type logFileItem struct {
	entry   config.LogEntry
	origIdx int // index into m.logFileEntries for stat lookup
}

func (m Model) filteredLogFiles() []logFileItem {
	var items []logFileItem
	for i, e := range m.logFileEntries {
		items = append(items, logFileItem{entry: e, origIdx: i})
	}
	if m.filterText == "" {
		return items
	}
	f := strings.ToLower(m.filterText)
	var result []logFileItem
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.entry.Name), f) ||
			strings.Contains(strings.ToLower(item.entry.Path), f) {
			result = append(result, item)
		}
	}
	return result
}
