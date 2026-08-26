package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

func k8sNodeStatusStyle(status string) lipgloss.Style {
	switch status {
	case "Ready":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")) // green
	case "NotReady":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")) // red
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")) // yellow
	}
}

func (m Model) renderK8sNodeList() string {
	f := m.fleets[m.selectedFleet]
	cluster := m.k8sClusters[m.selectedK8sCluster]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	filtered := m.filteredK8sNodes()
	filterInfo := ""
	if m.filterText != "" {
		filterInfo = fmt.Sprintf(" [filter: %s]", m.filterText)
	}
	breadcrumb := f.Name + " › " + cluster.Name + " › " + m.selectedK8sContext + " › Nodes"
	s := m.renderHeader(breadcrumb+filterInfo, m.k8sNodeCursor+1, len(filtered)) + "\n"
	s += borderStyle.Render("┌"+strings.Repeat("─", iw)+"┐") + "\n"

	if len(filtered) == 0 {
		if m.filterText != "" {
			s += borderedRow(fmt.Sprintf("  No matches for '%s'", m.filterText), iw, normalRowStyle) + "\n"
		} else {
			s += borderedRow("  No nodes.", iw, normalRowStyle) + "\n"
		}
	} else {
		nameCol := len("NAME")
		statusCol := 10
		versionCol := len("VERSION")
		taintsCol := 7
		cpuCol := 7
		cpuPctCol := 5
		memCol := 8
		memPctCol := 5
		cpuACol := 7
		vmCol := len("VM SIZE")
		ageCol := 5
		for _, n := range filtered {
			if len(n.Name) > nameCol {
				nameCol = len(n.Name)
			}
			if len(n.Version) > versionCol {
				versionCol = len(n.Version)
			}
			if len(n.VMSize) > vmCol {
				vmCol = len(n.VMSize)
			}
		}
		nameCol += 2
		versionCol += 2
		vmCol += 2
		_ = ageCol

		hdr := fmt.Sprintf("     %-*s  %-*s  %-*s  %*s  %*s  %*s  %*s  %*s  %*s  %-*s  %s",
			nameCol, "NAME"+m.sortIndicator(1),
			statusCol, "STATUS"+m.sortIndicator(2),
			versionCol, "VERSION"+m.sortIndicator(3),
			taintsCol, "TAINTS"+m.sortIndicator(4),
			cpuCol, "CPU"+m.sortIndicator(5),
			cpuPctCol, "%CPU"+m.sortIndicator(6),
			memCol, "MEM"+m.sortIndicator(7),
			memPctCol, "%MEM"+m.sortIndicator(8),
			cpuACol, "CPU/A"+m.sortIndicator(9),
			vmCol, "VM SIZE",
			"AGE",
		)
		s += borderedRow(hdr, iw, colHeaderStyle) + "\n"
		s += borderStyle.Render("├"+strings.Repeat("─", iw)+"┤") + "\n"

		maxVisible := m.height - 8
		if maxVisible < 1 {
			maxVisible = 1
		}
		offset := 0
		if m.k8sNodeCursor >= offset+maxVisible {
			offset = m.k8sNodeCursor - maxVisible + 1
		}
		end := offset + maxVisible
		if end > len(filtered) {
			end = len(filtered)
		}

		// build group start index map by pool
		groupStarts := make(map[int]string)
		for i, n := range filtered {
			if n.Pool != "" {
				if i == 0 || filtered[i-1].Pool != n.Pool {
					groupStarts[i] = n.Pool
				}
			}
		}

		for i := offset; i < end; i++ {
			// render group header if this node starts a new pool
			if poolName, ok := groupStarts[i]; ok {
				groupLine := fmt.Sprintf("  ── %s ──", poolName)
				s += borderedRow(groupLine, iw, groupHeaderStyle) + "\n"
			}

			n := filtered[i]
			cur := "   "
			if i == m.k8sNodeCursor {
				cur = " ▸ "
			}

			cpuUsage := n.CPUUsage
			if cpuUsage == "" {
				cpuUsage = "\u2014"
			}
			cpuPct := n.CPUPct
			if cpuPct == "" {
				cpuPct = "\u2014"
			}
			memUsage := n.MemUsage
			if memUsage == "" {
				memUsage = "\u2014"
			}
			memPct := n.MemPct
			if memPct == "" {
				memPct = "\u2014"
			}

			line := fmt.Sprintf("%s  %-*s  %-*s  %-*s  %*d  %*s  %*s  %*s  %*s  %*s  %-*s  %s",
				cur, nameCol, n.Name,
				statusCol, n.Status,
				versionCol, n.Version,
				taintsCol, n.Taints,
				cpuCol, cpuUsage,
				cpuPctCol, cpuPct,
				memCol, memUsage,
				memPctCol, memPct,
				cpuACol, n.CPUA,
				vmCol, n.VMSize,
				n.Age,
			)

			var style lipgloss.Style
			if i == m.k8sNodeCursor {
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
			{"Enter", "Detail"},
			{"/", "Filter"},
			{"1-9", "Sort"},
			{"r", "Refresh"},
			{"Esc", "Back"},
			{"q", "Quit"},
		}))
	}
	return s
}

func (m Model) renderK8sNodeDetail() string {
	f := m.fleets[m.selectedFleet]
	cluster := m.k8sClusters[m.selectedK8sCluster]
	w := m.width
	if w < 20 {
		w = 80
	}
	iw := w - 2

	d := m.k8sNodeDetail

	nodeName := d.Name
	if nodeName == "" {
		nodeName = "unknown"
	}
	breadcrumb := f.Name + " › " + cluster.Name + " › " + m.selectedK8sContext + " › Nodes › " + nodeName
	s := m.renderHeader(breadcrumb, 0, 0) + "\n"
	s += borderStyle.Render("┌"+strings.Repeat("─", iw)+"┐") + "\n"

	// Usage values (nil-safe)
	cpuUsage := "..."
	memUsage := "..."
	if m.k8sNodeUsage != nil {
		cpuUsage = fmt.Sprintf("%s (%s)", m.k8sNodeUsage.CPUUsage, m.k8sNodeUsage.CPUPercent)
		memUsage = fmt.Sprintf("%s (%s)", m.k8sNodeUsage.MemUsage, m.k8sNodeUsage.MemPercent)
	}
	runningPods := "..."
	if m.k8sNodePods != nil {
		runningPods = fmt.Sprintf("%d/%s", len(m.k8sNodePods), d.Pods)
	}

	type kv struct{ key, val string }

	renderSection := func(title string, items []kv) string {
		var lines []string
		lines = append(lines, fmt.Sprintf("── %s ──", title))
		keyW := 0
		for _, item := range items {
			if len(item.key) > keyW {
				keyW = len(item.key)
			}
		}
		for _, item := range items {
			val := item.val
			if val == "" {
				val = "—"
			}
			lines = append(lines, fmt.Sprintf("%-*s  %s", keyW, item.key, val))
		}
		return strings.Join(lines, "\n")
	}

	// Left column: System
	left := renderSection("System", []kv{
		{"Version", d.Version},
		{"OS Image", d.OSImage},
		{"Kernel", d.KernelVersion},
		{"Runtime", d.ContainerRuntime},
		{"VM Size", d.VMSize},
		{"Pool", d.Pool},
		{"Internal", d.InternalIP},
		{"Pod CIDR", d.PodCIDR},
		{"Created", d.Created},
		{"Images", fmt.Sprintf("%d", d.ImageCount)},
	})

	// Middle column: Status + Taints
	mid := renderSection("Status", []kv{
		{"Status", k8sNodeStatusStyle(d.Status).Render(d.Status)},
		{"Unschedulable", fmt.Sprintf("%v", d.Unschedulable)},
		{"CPU Usage", cpuUsage},
		{"Memory Usage", memUsage},
		{"Running Pods", runningPods},
	})
	mid += "\n"
	if len(d.Taints) == 0 {
		mid += "\n" + renderSection("Taints", []kv{{"(none)", ""}})
	} else {
		var taintItems []kv
		for _, t := range d.Taints {
			var taintStr string
			if t.Value != "" {
				taintStr = fmt.Sprintf("%s=%s:%s", t.Key, t.Value, t.Effect)
			} else {
				taintStr = fmt.Sprintf("%s:%s", t.Key, t.Effect)
			}
			taintItems = append(taintItems, kv{taintStr, ""})
		}
		mid += "\n" + renderSection("Taints", taintItems)
	}

	// Right column: Capacity + Conditions
	right := renderSection("Capacity", []kv{
		{"CPU", fmt.Sprintf("%s (alloc: %s)", d.CPU, d.AllocatableCPU)},
		{"Memory", fmt.Sprintf("%s (alloc: %s)", d.Memory, d.AllocatableMemory)},
		{"Pods", fmt.Sprintf("%s (alloc: %s)", d.Pods, d.AllocatablePods)},
	})
	right += "\n"

	allowedConditions := map[string]bool{
		"Ready":              true,
		"MemoryPressure":     true,
		"DiskPressure":       true,
		"PIDPressure":        true,
		"NetworkUnavailable": true,
	}
	var condItems []kv
	for _, c := range d.Conditions {
		if !allowedConditions[c.Type] {
			continue
		}
		condItems = append(condItems, kv{c.Type, c.Status})
	}
	if len(condItems) == 0 {
		condItems = []kv{{"(none)", ""}}
	}
	right += "\n" + renderSection("Conditions", condItems)

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

	// Blank row before pod table (no separator)
	s += borderedRow("", iw, normalRowStyle) + "\n"

	// Pod table section (full-width)
	podCount := 0
	if m.k8sNodePods != nil {
		podCount = len(m.k8sNodePods)
	}
	s += borderedRow(fmt.Sprintf("  ── Non-terminated Pods (%d in total) ──", podCount), iw, groupHeaderStyle) + "\n"

	if len(m.k8sNodePods) == 0 {
		s += borderedRow("  No pods.", iw, normalRowStyle) + "\n"
	} else {
		filteredPods := m.filteredK8sNodePods()
		if len(filteredPods) == 0 {
			s += borderedRow("  No pods.", iw, normalRowStyle) + "\n"
		} else {
			nsCol := len("NAMESPACE")
			nameCol := len("NAME")
			for _, p := range filteredPods {
				if len(p.Namespace) > nsCol {
					nsCol = len(p.Namespace)
				}
				if len(p.Name) > nameCol {
					nameCol = len(p.Name)
				}
			}
			nsCol += 2
			nameCol += 2

			podHdr := fmt.Sprintf("     %-*s  %-*s  %-10s  %-7s  %-10s  %-10s  %-10s  %-10s  %s",
				nsCol, "NAMESPACE"+m.sortIndicator(1), nameCol, "NAME"+m.sortIndicator(2),
				"STATUS"+m.sortIndicator(3), "READY"+m.sortIndicator(4),
				"CPU REQ"+m.sortIndicator(5), "CPU LIM"+m.sortIndicator(6),
				"MEM REQ"+m.sortIndicator(7), "MEM LIM"+m.sortIndicator(8),
				"AGE"+m.sortIndicator(9))
			s += borderedRow(podHdr, iw, colHeaderStyle) + "\n"

			linesUsed := strings.Count(s, "\n")
			maxVisible := m.height - linesUsed - 4 // 4 = bottom border + hint bar + padding
			if maxVisible < 3 {
				maxVisible = 3
			}
			offset := 0
			if m.k8sNodePodCursor >= offset+maxVisible {
				offset = m.k8sNodePodCursor - maxVisible + 1
			}
			end := offset + maxVisible
			if end > len(filteredPods) {
				end = len(filteredPods)
			}

			for i := offset; i < end; i++ {
				p := filteredPods[i]
				cur := "  "
				if i == m.k8sNodePodCursor {
					cur = " ▸"
				}
				podLine := fmt.Sprintf("%s   %-*s  %-*s  %-10s  %-7s  %-10s  %-10s  %-10s  %-10s  %s",
					cur, nsCol, p.Namespace, nameCol, p.Name, p.Status, p.Ready, p.CPUReq, p.CPULim, p.MemReq, p.MemLim, p.Age)

				var style lipgloss.Style
				if i == m.k8sNodePodCursor {
					style = selectedRowStyle
				} else if i%2 == 0 {
					style = altRowStyle
				} else {
					style = normalRowStyle
				}
				s += borderedRow(podLine, iw, style) + "\n"
			}
		}
	}

	s = m.padToBottom(s, iw)
	s += borderStyle.Render("└"+strings.Repeat("─", iw)+"┘") + "\n"

	if m.filterActive {
		s += hintBarStyle.Width(m.width).Render(fmt.Sprintf("  /%s█", m.filterText))
	} else {
		s += m.renderHintBar(hintWithHelp([][]string{
			{"↑↓", "Scroll Pods"},
			{"/", "Filter"},
			{"1-9", "Sort"},
			{"r", "Refresh"},
			{"Esc", "Back"},
			{"q", "Quit"},
		}))
	}
	return s
}
