package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderAccountList() string {
	h := m.hosts[m.selectedHost]
	f := m.fleets[m.selectedFleet]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	accts := m.filteredAccounts()

	// detail view
	if m.showAccountDetail && m.accountCursor < len(accts) {
		a := accts[m.accountCursor]
		breadcrumb := f.Name + " \u203a " + h.Entry.Name + " \u203a Accounts \u203a " + a.User
		s := m.renderHeader(breadcrumb, 0, 0) + "\n"
		s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

		for si, sec := range m.accountDetailSections {
			// section title
			s += borderedRow("  "+sec.title, iw, colHeaderStyle) + "\n"

			// find max key width for alignment
			keyWidth := 0
			for _, kv := range sec.items {
				if len(kv[0]) > keyWidth {
					keyWidth = len(kv[0])
				}
			}

			for i, kv := range sec.items {
				var line string
				if kv[0] == "" {
					line = "    " + kv[1]
				} else {
					line = fmt.Sprintf("    %-*s  %s", keyWidth, kv[0], kv[1])
				}
				var style lipgloss.Style
				if i%2 == 0 {
					style = altRowStyle
				} else {
					style = normalRowStyle
				}
				s += borderedRow(line, iw, style) + "\n"
			}

			if si < len(m.accountDetailSections)-1 {
				s += borderStyle.Render("\u251c"+strings.Repeat("\u2500", iw)+"\u2524") + "\n"
			}
		}

		s = m.padToBottom(s, iw)
		s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"
		s += hintBarStyle.Width(m.width).Render("  Press any key to return")
		return s
	}

	breadcrumb := f.Name + " \u203a " + h.Entry.Name + " \u203a Accounts"
	s := m.renderHeader(breadcrumb, m.accountCursor+1, len(accts)) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	if m.filterActive {
		s += borderedRow(fmt.Sprintf("  / %s\u2588", m.filterText), iw, normalRowStyle) + "\n"
	}

	if len(accts) == 0 {
		s += borderedRow("  No accounts found.", iw, normalRowStyle) + "\n"
	} else {
		// compute dynamic column widths
		userCol := len("USER")
		uidCol := len("UID")
		groupCol := len("GROUPS")
		for _, a := range accts {
			if len(a.User) > userCol {
				userCol = len(a.User)
			}
			uidStr := fmt.Sprintf("%d", a.UID)
			if len(uidStr) > uidCol {
				uidCol = len(uidStr)
			}
			g := a.Groups
			if a.IsSudo {
				g = "\u2605 " + g
			}
			if len(g) > groupCol {
				groupCol = len(g)
			}
		}
		userCol += 2
		uidCol += 2
		groupCol += 2

		hdr := fmt.Sprintf("     %-*s  %-*s  %-*s  %s", userCol, "USER"+m.sortIndicator(1), uidCol, "UID"+m.sortIndicator(2), groupCol, "GROUPS"+m.sortIndicator(3), "SHELL"+m.sortIndicator(4))
		s += borderedRow(hdr, iw, colHeaderStyle) + "\n"
		s += borderStyle.Render("\u251c"+strings.Repeat("\u2500", iw)+"\u2524") + "\n"

		maxVisible := m.height - 8
		if m.filterActive {
			maxVisible--
		}
		if maxVisible < 1 {
			maxVisible = 1
		}
		offset := 0
		if m.accountCursor >= offset+maxVisible {
			offset = m.accountCursor - maxVisible + 1
		}
		end := offset + maxVisible
		if end > len(accts) {
			end = len(accts)
		}

		for i := offset; i < end; i++ {
			a := accts[i]

			cur := "   "
			if i == m.accountCursor {
				cur = " \u25b8 "
			}

			user := a.User
			if a.IsLocked {
				user = "\U0001f512 " + user
			}

			groups := a.Groups
			if a.IsSudo {
				groups = "\u2605 " + groups
			}

			line := fmt.Sprintf("%s  %-*s  %-*d  %-*s  %s", cur, userCol, user, uidCol, a.UID, groupCol, groups, a.Shell)

			var style lipgloss.Style
			if i == m.accountCursor {
				style = selectedRowStyle
			} else if a.IsLocked {
				style = lipgloss.NewStyle().Foreground(colorRed)
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
		{"\u2191\u2193", "Navigate"},
		{"1-4", "Sort"},
		{"Enter", "Detail"},
		{"/", "Search"},
		{"r", "Refresh"},
		{"Esc", "Back"},
	}))
	return s
}
