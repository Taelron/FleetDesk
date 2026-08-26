package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderDiskList() string {
	h := m.hosts[m.selectedHost]
	f := m.fleets[m.selectedFleet]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	filtered := m.filteredDisks()

	// detail view
	if m.showDiskDetail {
		breadcrumb := f.Name + " \u203a " + h.Entry.Name + " \u203a Disk \u203a Detail"
		s := m.renderHeader(breadcrumb, m.diskDetailCursor+1, len(m.diskDetailLines)) + "\n"
		s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

		if len(m.diskDetailLines) == 0 {
			s += borderedRow("  No details available.", iw, normalRowStyle) + "\n"
		} else {
			maxVisible := m.height - 6
			if maxVisible < 1 {
				maxVisible = 1
			}
			offset := 0
			if m.diskDetailCursor >= offset+maxVisible {
				offset = m.diskDetailCursor - maxVisible + 1
			}
			end := offset + maxVisible
			if end > len(m.diskDetailLines) {
				end = len(m.diskDetailLines)
			}
			for i := offset; i < end; i++ {
				cur := "  "
				if i == m.diskDetailCursor {
					cur = "\u25b8 "
				}
				line := "  " + cur + m.diskDetailLines[i]
				var style lipgloss.Style
				if i == m.diskDetailCursor {
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
		s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"
		s += m.renderHintBar(hintWithHelp([][]string{
			{"\u2191\u2193", "Scroll"},
			{"Esc", "Back"},
		}))
		return s
	}

	breadcrumb := f.Name + " \u203a " + h.Entry.Name + " \u203a Disk"
	s := m.renderHeader(breadcrumb, m.diskCursor+1, len(filtered)) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	if m.filterText != "" {
		filterLine := fmt.Sprintf("  Filter: %s", m.filterText)
		s += borderedRow(filterLine, iw, lipgloss.NewStyle().Foreground(colorCyan)) + "\n"
		s += borderStyle.Render("\u251c"+strings.Repeat("\u2500", iw)+"\u2524") + "\n"
	}

	if len(filtered) == 0 {
		s += borderedRow("  No partitions found.", iw, normalRowStyle) + "\n"
	} else {
		fsCol := len("FILESYSTEM")
		mountCol := len("MOUNT")
		for _, d := range filtered {
			if len(d.Filesystem) > fsCol {
				fsCol = len(d.Filesystem)
			}
			if len(d.Mount) > mountCol {
				mountCol = len(d.Mount)
			}
		}
		fsCol += 2
		mountCol += 2

		hdr := fmt.Sprintf("     %-*s  %6s  %6s  %6s  %5s  %-*s", fsCol, "FILESYSTEM"+m.sortIndicator(1), "SIZE"+m.sortIndicator(2), "USED"+m.sortIndicator(3), "AVAIL"+m.sortIndicator(4), "USE%"+m.sortIndicator(5), mountCol, "MOUNT"+m.sortIndicator(6))
		s += borderedRow(hdr, iw, colHeaderStyle) + "\n"
		s += borderStyle.Render("\u251c"+strings.Repeat("\u2500", iw)+"\u2524") + "\n"

		maxVisible := m.height - 8
		if m.filterText != "" {
			maxVisible -= 2
		}
		if maxVisible < 1 {
			maxVisible = 1
		}
		offset := 0
		if m.diskCursor >= offset+maxVisible {
			offset = m.diskCursor - maxVisible + 1
		}
		end := offset + maxVisible
		if end > len(filtered) {
			end = len(filtered)
		}

		for i := offset; i < end; i++ {
			d := filtered[i]
			cur := "   "
			if i == m.diskCursor {
				cur = " \u25b8 "
			}
			line := fmt.Sprintf("%s  %-*s  %6s  %6s  %6s  %5s  %-*s", cur, fsCol, d.Filesystem, d.Size, d.Used, d.Avail, d.UsePercent, mountCol, d.Mount)

			var style lipgloss.Style
			if i == m.diskCursor {
				style = selectedRowStyle
			} else if i%2 == 0 {
				style = altRowStyle
			} else {
				style = normalRowStyle
			}

			// highlight high disk usage
			pct := strings.TrimSuffix(d.UsePercent, "%")
			var pctVal int
			fmt.Sscanf(pct, "%d", &pctVal) //nolint:errcheck,gosec // TAE-82: a failed parse silently skips the disk-usage highlight threshold
			if pctVal >= 90 && i != m.diskCursor {
				style = flashErrorStyle
			} else if pctVal >= 80 && i != m.diskCursor {
				style = lipgloss.NewStyle().Foreground(colorYellow)
			}

			s += borderedRow(line, iw, style) + "\n"
		}
	}

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"

	s += m.renderHintBar(hintWithHelp([][]string{
		{"↑↓", "Navigate"},
		{"Enter", "Detail"},
		{"1-6", "Sort"},
		{"/", "Search"},
		{"r", "Refresh"},
		{"Esc", "Back"},
	}))
	return s
}
