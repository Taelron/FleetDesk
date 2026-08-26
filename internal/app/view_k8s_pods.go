package app

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/Gaetan-Jaminon/fleetdesk/internal/notes"
)

func k8sPodStatusStyle(status string) lipgloss.Style {
	switch status {
	case "Running":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	case "Succeeded":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	case "Pending":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	case "Failed":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	}
}

func (m Model) renderK8sPodList() string {
	f := m.fleets[m.selectedFleet]
	cluster := m.k8sClusters[m.selectedK8sCluster]
	ns := m.k8sNamespaces[m.selectedK8sNamespace]
	wl := m.k8sWorkloads[m.selectedK8sWorkload]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	filtered := m.filteredK8sPodList()
	filterInfo := ""
	if m.filterText != "" {
		filterInfo = fmt.Sprintf(" [filter: %s]", m.filterText)
	}
	breadcrumb := f.Name + " \u203a " + cluster.Name + " \u203a " + m.selectedK8sContext + " \u203a " + ns.Name + " \u203a " + wl.Name
	s := m.renderHeader(breadcrumb+filterInfo, m.k8sPodCursor+1, len(filtered)) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	maxVisible := m.height - 8
	if maxVisible < 1 {
		maxVisible = 1
	}

	emptyMsg := "  No pods."
	if m.filterText != "" {
		emptyMsg = fmt.Sprintf("  No matches for '%s'", m.filterText)
	}

	restartsCol := len("RESTARTS") + 2

	fleetName := m.fleets[m.selectedFleet].Name
	clusterName := m.k8sClusters[m.selectedK8sCluster].Name
	nsName := m.k8sNamespaces[m.selectedK8sNamespace].Name
	s += renderList(ListConfig{
		Columns: []ListColumn{
			{Label: "NAME", SortIndex: 1},
			{Label: "STATUS", SortIndex: 2},
			{Label: "READY", SortIndex: 3},
			{Label: "RESTARTS", Width: restartsCol, SortIndex: 4, RightAlign: true},
			{Label: "NODE", SortIndex: 5},
			{Label: "AGE", SortIndex: 6},
		},
		RowCount: len(filtered),
		RowBuilder: func(i int) []string {
			p := filtered[i]
			status := p.Status
			if t, ok := m.transitions["k8s-pod/"+p.Name]; ok {
				status = t.Display
			}
			return []string{
				p.Name,
				status,
				p.Ready,
				fmt.Sprintf("%d", p.Restarts),
				p.Node,
				p.Age,
			}
		},
		RowPrefix: func(i int) string {
			return m.notePrefix(notes.ResourceRef{
				Fleet:    fleetName,
				Segments: []string{"k8s", clusterName, m.selectedK8sContext, nsName, "pods", filtered[i].Name},
			})
		},
		Cursor:        m.k8sPodCursor,
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
			{"Enter", "Detail"},
			{"n", "Notes"},
			{"l", "Logs"},
			{"d", "Delete"},
			{"/", "Filter"},
			{"1-6", "Sort"},
			{"r", "Refresh"},
			{"Esc", "Back"},
			{"q", "Quit"},
		}))
	}
	return s
}

func (m Model) renderK8sPodDetail() string {
	f := m.fleets[m.selectedFleet]
	cluster := m.k8sClusters[m.selectedK8sCluster]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	d := m.k8sPodDetail

	podName := d.Name
	if podName == "" {
		podName = "unknown"
	}
	breadcrumb := f.Name + " \u203a " + cluster.Name + " \u203a " + m.selectedK8sContext + " \u203a " + d.Namespace + " \u203a " + podName
	s := m.renderHeader(breadcrumb, 0, 0) + "\n"
	s += borderStyle.Render("\u250c"+strings.Repeat("\u2500", iw)+"\u2510") + "\n"

	type kv struct{ key, val string }

	renderSection := func(title string, items []kv) string {
		var lines []string
		lines = append(lines, fmt.Sprintf("\u2500\u2500 %s \u2500\u2500", title))
		keyW := 0
		for _, item := range items {
			if len(item.key) > keyW {
				keyW = len(item.key)
			}
		}
		for _, item := range items {
			val := item.val
			if val == "" {
				val = "\u2014"
			}
			lines = append(lines, fmt.Sprintf("%-*s  %s", keyW, item.key, val))
		}
		return strings.Join(lines, "\n")
	}

	// Left column: Pod Info
	left := renderSection("Pod Info", []kv{
		{"Name", d.Name},
		{"Namespace", d.Namespace},
		{"Node", d.Node},
		{"IP", d.IP},
	})

	// Middle column: Status
	mid := renderSection("Status", []kv{
		{"Status", k8sPodStatusStyle(d.Status).Render(d.Status)},
		{"Ready", d.Ready},
		{"Restarts", fmt.Sprintf("%d", d.Restarts)},
		{"Age", d.Age},
	})

	// Right column: Conditions
	var condKVs []kv
	for _, c := range d.Conditions {
		condKVs = append(condKVs, kv{c.Type, c.Status})
	}
	right := renderSection("Conditions", condKVs)

	// Three-column layout
	colW := iw / 3
	leftStyle := lipgloss.NewStyle().Width(colW)
	midStyle := lipgloss.NewStyle().Width(colW)
	rightStyle := lipgloss.NewStyle().Width(iw - 2*colW)

	threeCol := lipgloss.JoinHorizontal(lipgloss.Top,
		leftStyle.Render(left),
		midStyle.Render(mid),
		rightStyle.Render(right),
	)

	for _, line := range strings.Split(threeCol, "\n") {
		s += borderedRow(line, iw, normalRowStyle) + "\n"
	}

	// Blank row before container table
	s += borderedRow("", iw, normalRowStyle) + "\n"

	// Container table section
	s += borderedRow(fmt.Sprintf("  \u2500\u2500 Containers (%d) \u2500\u2500", len(d.Containers)), iw, groupHeaderStyle) + "\n"

	if len(d.Containers) == 0 {
		s += borderedRow("  No containers.", iw, normalRowStyle) + "\n"
	} else {
		cNameCol := len("NAME")
		cImageCol := len("IMAGE")
		cStateCol := len("STATE")
		for _, c := range d.Containers {
			if len(c.Name) > cNameCol {
				cNameCol = len(c.Name)
			}
			if len(c.Image) > cImageCol {
				cImageCol = len(c.Image)
			}
			if len(c.State) > cStateCol {
				cStateCol = len(c.State)
			}
		}
		cNameCol += 2
		cImageCol += 2
		cStateCol += 2

		cHdr := fmt.Sprintf("     %-*s  %-*s  %-*s  %-7s  %-10s  %-10s  %-10s  %-10s  %s",
			cNameCol, "NAME"+m.sortIndicator(1),
			cImageCol, "IMAGE"+m.sortIndicator(2),
			cStateCol, "STATE"+m.sortIndicator(3),
			"READY"+m.sortIndicator(4),
			"RESTARTS"+m.sortIndicator(5),
			"CPU REQ"+m.sortIndicator(6),
			"CPU LIM"+m.sortIndicator(7),
			"MEM REQ"+m.sortIndicator(8),
			"MEM LIM"+m.sortIndicator(9),
		)
		s += borderedRow(cHdr, iw, colHeaderStyle) + "\n"

		linesUsed := strings.Count(s, "\n")
		maxVisible := m.height - linesUsed - 4
		if maxVisible < 3 {
			maxVisible = 3
		}
		offset := 0
		if m.k8sPodContainerCursor >= offset+maxVisible {
			offset = m.k8sPodContainerCursor - maxVisible + 1
		}
		end := offset + maxVisible
		if end > len(d.Containers) {
			end = len(d.Containers)
		}

		for i := offset; i < end; i++ {
			c := d.Containers[i]
			cur := "  "
			if i == m.k8sPodContainerCursor {
				cur = " \u25b8"
			}

			readyStr := fmt.Sprintf("%v", c.Ready)
			cpuR := c.CPUReq
			if cpuR == "" {
				cpuR = "\u2014"
			}
			cpuL := c.CPULim
			if cpuL == "" {
				cpuL = "\u2014"
			}
			mR := c.MemReq
			if mR == "" {
				mR = "\u2014"
			}
			mL := c.MemLim
			if mL == "" {
				mL = "\u2014"
			}

			cLine := fmt.Sprintf("%s   %-*s  %-*s  %-*s  %-7s  %-10d  %-10s  %-10s  %-10s  %s",
				cur, cNameCol, c.Name, cImageCol, c.Image, cStateCol, c.State,
				readyStr, c.Restarts, cpuR, cpuL, mR, mL)

			var style lipgloss.Style
			if i == m.k8sPodContainerCursor {
				style = selectedRowStyle
			} else if i%2 == 0 {
				style = altRowStyle
			} else {
				style = normalRowStyle
			}
			s += borderedRow(cLine, iw, style) + "\n"
		}
	}

	// Init/Sidecar Containers section (if any)
	if len(d.InitContainers) > 0 {
		s += borderedRow("", iw, normalRowStyle) + "\n"
		s += borderedRow(fmt.Sprintf("  \u2500\u2500 Init/Sidecar Containers (%d) \u2500\u2500", len(d.InitContainers)), iw, groupHeaderStyle) + "\n"

		initNameW := len("NAME")
		initImageW := len("IMAGE")
		initStateW := len("STATE")
		for _, c := range d.InitContainers {
			if len(c.Name) > initNameW {
				initNameW = len(c.Name)
			}
			if len(c.Image) > initImageW {
				initImageW = len(c.Image)
			}
			if len(c.State) > initStateW {
				initStateW = len(c.State)
			}
		}
		initNameW += 2
		initImageW += 2
		initStateW += 2

		initHdr := fmt.Sprintf("     %-*s  %-*s  %-*s  %-7s  %-10s  %-10s  %-10s  %-10s  %s",
			initNameW, "NAME", initImageW, "IMAGE", initStateW, "STATE",
			"READY", "RESTARTS", "CPU REQ", "CPU LIM", "MEM REQ", "MEM LIM")
		s += borderedRow(initHdr, iw, colHeaderStyle) + "\n"

		for i, c := range d.InitContainers {
			ready := fmt.Sprintf("%v", c.Ready)
			cpuR := c.CPUReq
			if cpuR == "" {
				cpuR = "\u2014"
			}
			cpuL := c.CPULim
			if cpuL == "" {
				cpuL = "\u2014"
			}
			mR := c.MemReq
			if mR == "" {
				mR = "\u2014"
			}
			mL := c.MemLim
			if mL == "" {
				mL = "\u2014"
			}

			line := fmt.Sprintf("     %-*s  %-*s  %-*s  %-7s  %-10d  %-10s  %-10s  %-10s  %s",
				initNameW, c.Name, initImageW, c.Image, initStateW, c.State,
				ready, c.Restarts, cpuR, cpuL, mR, mL)
			var style lipgloss.Style
			if i%2 == 0 {
				style = altRowStyle
			} else {
				style = normalRowStyle
			}
			s += borderedRow(line, iw, style) + "\n"
		}
	}

	// Labels section
	if len(d.Labels) > 0 {
		s += borderedRow("", iw, normalRowStyle) + "\n"
		s += borderedRow(fmt.Sprintf("  \u2500\u2500 Labels (%d) \u2500\u2500", len(d.Labels)), iw, groupHeaderStyle) + "\n"
		labelKeys := make([]string, 0, len(d.Labels))
		for k := range d.Labels {
			labelKeys = append(labelKeys, k)
		}
		sort.Strings(labelKeys)
		keyW := 0
		for _, k := range labelKeys {
			if len(k) > keyW {
				keyW = len(k)
			}
		}
		for i, k := range labelKeys {
			line := fmt.Sprintf("     %-*s  %s", keyW, k, d.Labels[k])
			var style lipgloss.Style
			if i%2 == 0 {
				style = altRowStyle
			} else {
				style = normalRowStyle
			}
			s += borderedRow(line, iw, style) + "\n"
		}
	}

	// Annotations section
	if len(d.Annotations) > 0 {
		s += borderedRow("", iw, normalRowStyle) + "\n"
		s += borderedRow(fmt.Sprintf("  \u2500\u2500 Annotations (%d) \u2500\u2500", len(d.Annotations)), iw, groupHeaderStyle) + "\n"
		annoKeys := make([]string, 0, len(d.Annotations))
		for k := range d.Annotations {
			annoKeys = append(annoKeys, k)
		}
		sort.Strings(annoKeys)
		keyW := 0
		for _, k := range annoKeys {
			if len(k) > keyW {
				keyW = len(k)
			}
		}
		for i, k := range annoKeys {
			line := fmt.Sprintf("     %-*s  %s", keyW, k, d.Annotations[k])
			var style lipgloss.Style
			if i%2 == 0 {
				style = altRowStyle
			} else {
				style = normalRowStyle
			}
			s += borderedRow(line, iw, style) + "\n"
		}
	}

	// Apply scrollable viewport
	contentLines := strings.Split(s, "\n")
	// headerLines = 2 (breadcrumb + top border), footerLines = 2 (bottom border + hint bar)
	maxVisible := m.height - 4
	if maxVisible < 5 {
		maxVisible = 5
	}

	// Clamp scroll offset
	totalLines := len(contentLines)
	maxScroll := totalLines - maxVisible
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.k8sPodContainerCursor > maxScroll {
		m.k8sPodContainerCursor = maxScroll
	}

	// Take visible window
	startLine := m.k8sPodContainerCursor
	endLine := startLine + maxVisible
	if endLine > totalLines {
		endLine = totalLines
	}
	s = strings.Join(contentLines[startLine:endLine], "\n") + "\n"

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("\u2514"+strings.Repeat("\u2500", iw)+"\u2518") + "\n"

	s += m.renderHintBar(hintWithHelp([][]string{
		{"\u2191\u2193", "Scroll"},
		{"g", "Top"},
		{"l", "Logs"},
		{"Esc", "Back"},
		{"q", "Quit"},
	}))
	return s
}
