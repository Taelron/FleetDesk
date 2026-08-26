package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func (m Model) renderK8sContextList() string {
	f := m.fleets[m.selectedFleet]
	cluster := m.k8sClusters[m.selectedK8sCluster]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	filtered := m.filteredK8sContexts()
	breadcrumb := f.Name + " › " + cluster.Name
	s := m.renderHeader(breadcrumb, m.k8sContextCursor+1, len(filtered)) + "\n"
	s += borderStyle.Render("┌"+strings.Repeat("─", iw)+"┐") + "\n"
	if len(filtered) == 0 {
		s += borderedRow("  No contexts found.", iw, normalRowStyle) + "\n"
	} else {
		nameCol := len("CONTEXT")
		for _, ctx := range filtered {
			if len(ctx.Name) > nameCol {
				nameCol = len(ctx.Name)
			}
		}
		nameCol += 2

		hdr := fmt.Sprintf("     %-*s  %s",
			nameCol, "CONTEXT"+m.sortIndicator(1),
			"USER"+m.sortIndicator(2),
		)
		s += borderedRow(hdr, iw, colHeaderStyle) + "\n"
		s += borderStyle.Render("├"+strings.Repeat("─", iw)+"┤") + "\n"

		maxVisible := m.height - 8
		if maxVisible < 1 {
			maxVisible = 1
		}
		offset := 0
		if m.k8sContextCursor >= offset+maxVisible {
			offset = m.k8sContextCursor - maxVisible + 1
		}
		end := offset + maxVisible
		if end > len(filtered) {
			end = len(filtered)
		}

		for i := offset; i < end; i++ {
			ctx := filtered[i]
			cur := "   "
			if i == m.k8sContextCursor {
				cur = " ▸ "
			}

			prefix := ""
			if ctx.Current {
				prefix = "* "
			}

			displayName := ctx.Name
			if t, ok := m.transitions["k8s-context/"+ctx.Name]; ok {
				displayName = ctx.Name + " " + t.Display
			}

			line := fmt.Sprintf("%s  %s%-*s  %s",
				cur, prefix, nameCol-len(prefix), displayName, ctx.User)

			var style lipgloss.Style
			if i == m.k8sContextCursor {
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
			{"Enter", "Resources"},
			{"d", "Delete Context"},
			{"/", "Filter"},
			{"r", "Refresh"},
			{"Esc", "Back"},
			{"q", "Quit"},
		}))
	}
	return s
}
