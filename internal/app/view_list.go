package app

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ListColumn describes one column in a resource list view.
type ListColumn struct {
	Label      string // header label, e.g. "SERVICE"
	Width      int    // fixed width in chars; 0 = computed from max(label, data) + 2
	SortIndex  int    // 0 = not sortable; positive = sort key number (matches ListConfig.SortIndicator argument)
	RightAlign bool   // right-align cell content (for numeric columns)
}

// ListConfig is the input to renderList.
type ListConfig struct {
	Columns []ListColumn

	RowCount   int
	RowBuilder func(i int) []string // returns cell strings for row i, same order as Columns

	// RowPrefix, if set, returns a short marker inserted between the cursor slot and
	// the first cell for row i — e.g. "✗ " for failed, "📝 " for notes. Empty = no prefix.
	RowPrefix func(i int) string

	// RowOverride, if set and returns non-empty for row i, replaces the column-based
	// cell layout with the provided string. Cursor marker and RowPrefix are still
	// applied before the override. Use for status-dependent rows that don't fit the
	// normal column structure (e.g. "connecting..." or "unreachable (reason)").
	RowOverride func(i int) string

	// GroupHeader, if set, returns (label, true) to inject a group-header line
	// before row i. Called once per row in the viewport.
	GroupHeader func(i int) (string, bool)

	Cursor     int
	MaxVisible int // viewport height in rows (excluding column header and separator)

	// SortIndicator returns the ▲/▼/"" indicator for the given sort key. Only
	// called for columns where ListColumn.SortIndex > 0.
	SortIndicator func(key int) string

	InnerWidth int // content width of the surrounding box (box width minus both borders)

	// EmptyMessage is shown when RowCount == 0. If empty, a sensible default is used.
	EmptyMessage string
}

// renderList renders the body of a resource list view: column header, separator,
// and the visible rows (with optional group headers and per-row prefixes).
//
// The caller owns the surrounding frame — breadcrumb header, top and bottom borders,
// filter bar, padToBottom, and hint bar.
//
// Returns a multi-line string ending with a trailing newline.
func renderList(cfg ListConfig) string {
	iw := cfg.InnerWidth

	if cfg.RowCount == 0 {
		msg := cfg.EmptyMessage
		if msg == "" {
			msg = "  No items found."
		}
		return borderedRow(msg, iw, normalRowStyle) + "\n"
	}

	widths := computeListWidths(cfg)

	var s strings.Builder
	s.WriteString(borderedRow(listHeaderLine(cfg, widths), iw, colHeaderStyle) + "\n")
	s.WriteString(borderStyle.Render("\u251c"+strings.Repeat("\u2500", iw)+"\u2524") + "\n")

	offset := 0
	if cfg.MaxVisible > 0 && cfg.Cursor >= cfg.MaxVisible {
		offset = cfg.Cursor - cfg.MaxVisible + 1
	}
	end := min(offset+cfg.MaxVisible, cfg.RowCount)

	for r := offset; r < end; r++ {
		if cfg.GroupHeader != nil {
			if label, ok := cfg.GroupHeader(r); ok {
				groupLine := fmt.Sprintf("  \u2500\u2500 %s \u2500\u2500", label)
				s.WriteString(borderedRow(groupLine, iw, groupHeaderStyle) + "\n")
			}
		}

		s.WriteString(borderedRow(listRowLine(cfg, widths, r), iw, rowStyle(r, cfg.Cursor)) + "\n")
	}

	return s.String()
}

// computeListWidths returns the effective width for each column. Fixed widths
// (ListColumn.Width > 0) are used as-is. Computed widths are max(label, data) + 2.
func computeListWidths(cfg ListConfig) []int {
	widths := make([]int, len(cfg.Columns))
	for i, c := range cfg.Columns {
		if c.Width > 0 {
			widths[i] = c.Width
			continue
		}
		w := len(c.Label)
		for r := 0; r < cfg.RowCount; r++ {
			cells := cfg.RowBuilder(r)
			if i < len(cells) && lipgloss.Width(cells[i]) > w {
				w = lipgloss.Width(cells[i])
			}
		}
		widths[i] = w + 2
	}
	return widths
}

// listHeaderLine builds the column header line including sort indicators.
// Leading "     " = 3-char cursor slot + 2-char gap, matching row layout.
func listHeaderLine(cfg ListConfig, widths []int) string {
	var b strings.Builder
	b.WriteString("     ")
	for i, c := range cfg.Columns {
		label := c.Label
		if c.SortIndex > 0 && cfg.SortIndicator != nil {
			label += cfg.SortIndicator(c.SortIndex)
		}
		if i == len(cfg.Columns)-1 {
			b.WriteString(label)
			continue
		}
		if c.RightAlign {
			fmt.Fprintf(&b, "%*s", widths[i], label)
		} else {
			fmt.Fprintf(&b, "%-*s", widths[i], label)
		}
		b.WriteString("  ")
	}
	return b.String()
}

// listRowLine builds a single data row: cursor marker, prefix, and either the
// column-based cells from RowBuilder or the override string from RowOverride.
func listRowLine(cfg ListConfig, widths []int, r int) string {
	cur := "   "
	if r == cfg.Cursor {
		cur = " \u25b8 "
	}
	prefix := ""
	if cfg.RowPrefix != nil {
		prefix = cfg.RowPrefix(r)
	}

	var b strings.Builder
	b.WriteString(cur)
	b.WriteString("  ")
	b.WriteString(prefix)

	if cfg.RowOverride != nil {
		if override := cfg.RowOverride(r); override != "" {
			b.WriteString(override)
			return b.String()
		}
	}

	cells := cfg.RowBuilder(r)
	for i, c := range cfg.Columns {
		val := ""
		if i < len(cells) {
			val = cells[i]
		}
		if i == len(cfg.Columns)-1 {
			b.WriteString(val)
			continue
		}
		if c.RightAlign {
			fmt.Fprintf(&b, "%*s", widths[i], val)
		} else {
			fmt.Fprintf(&b, "%-*s", widths[i], val)
		}
		b.WriteString("  ")
	}
	return b.String()
}

// rowStyle picks the lipgloss style based on cursor position and row index parity.
func rowStyle(r, cursor int) lipgloss.Style {
	if r == cursor {
		return selectedRowStyle
	}
	if r%2 == 0 {
		return altRowStyle
	}
	return normalRowStyle
}
