package app

import (
	"fmt"
	"strings"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/config"
	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
)

func (m Model) renderHostList() string {
	f := m.fleets[m.selectedFleet]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	s := m.renderHeader(f.Name, m.hostCursor+1, len(m.hosts)) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	maxVisible := m.height - 8
	if maxVisible < 1 {
		maxVisible = 1
	}

	nameCol := len("HOST")
	for _, h := range m.hosts {
		if len(h.Entry.Name) > nameCol {
			nameCol = len(h.Entry.Name)
		}
	}
	nameCol += 2

	// Group starts keyed by host index (only when group label differs from previous).
	groupStarts := make(map[int]string)
	for i, h := range m.hosts {
		if h.Group != "" {
			if i == 0 || m.hosts[i-1].Group != h.Group {
				groupStarts[i] = h.Group
			}
		}
	}

	fleetName := m.fleets[m.selectedFleet].Name
	s += renderList(ListConfig{
		Columns: []ListColumn{
			{Label: "HOST", Width: nameCol},
			{Label: "OS"},
			{Label: "UP SINCE"},
			{Label: "UPD"},
		},
		RowCount: len(m.hosts),
		RowBuilder: func(i int) []string {
			h := m.hosts[i]
			updStr := fmt.Sprintf("%d", h.UpdateCount)
			if h.UpdateCount == 0 {
				updStr = "\u2014"
			}
			return []string{h.Entry.Name, h.OS, h.UpSince, updStr}
		},
		RowPrefix: func(i int) string {
			return m.notePrefix(notes.ResourceRef{
				Fleet:    fleetName,
				Segments: []string{"hosts", m.hosts[i].Entry.Name},
			})
		},
		RowOverride: func(i int) string {
			h := m.hosts[i]
			switch h.Status {
			case config.HostConnecting:
				return fmt.Sprintf("%-*s  connecting...", nameCol, h.Entry.Name)
			case config.HostUnreachable:
				reason := h.Error
				if reason == "" {
					reason = "unknown"
				}
				return fmt.Sprintf("%-*s  unreachable (%s)", nameCol, h.Entry.Name, reason)
			case config.HostHandoverOnly:
				return fmt.Sprintf("%-*s  handover only (%s; in-app views unavailable)", nameCol, h.Entry.Name, h.Error)
			}
			return ""
		},
		GroupHeader: func(i int) (string, bool) {
			label, ok := groupStarts[i]
			return label, ok
		},
		Cursor:       m.hostCursor,
		MaxVisible:   maxVisible,
		InnerWidth:   iw,
		EmptyMessage: "  No hosts in fleet.",
	})

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"

	hints := [][]string{
		{"↑↓", "Navigate"},
		{"Enter", "Drill In"},
		{"n", "Notes"},
		{"x", "Shell"},
		{"c", "Commands"},
		{"K", "Deploy Key"},
		{"d", "Metrics"},
		{"R", "Reboot"},
		{"r", "Refresh"},
		{"Esc", "Back"},
		{"q", "Quit"},
	}
	s += m.renderHintBar(hintWithHelp(hints))
	return s
}
