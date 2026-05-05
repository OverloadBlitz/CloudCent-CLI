//go:build tui

package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// navItems is the canonical ordered list of top-level views.
var navItems = []string{"Pricing", "History", "Settings"}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}

// lineCount returns the number of terminal lines in s.
func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

// boxWithTitle renders content in a rounded-border box whose title is embedded
// in the top border line:
//
//	╭─ Title ─────────────────────────────╮
//	│ content                              │
//	╰──────────────────────────────────────╯
//
// styledTitle may contain ANSI escape codes; pass "" for a plain box.
// totalWidth is the full rendered width including the border characters.
func boxWithTitle(styledTitle string, content string, borderColor lipgloss.Color, totalWidth int) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(totalWidth - 2)
	rendered := style.Render(content)
	if styledTitle == "" {
		return rendered
	}
	return injectBorderTitle(rendered, styledTitle, lipgloss.Width(styledTitle), borderColor)
}

// boxWithTitleMinHeight is like boxWithTitle but forces the box to be at least
// minHeight terminal lines tall (including the two border lines). Content
// shorter than minHeight-2 is padded with blank lines by lipgloss.
func boxWithTitleMinHeight(styledTitle string, content string, borderColor lipgloss.Color, totalWidth, minHeight int) string {
	innerH := minHeight - 2
	if innerH < 1 {
		innerH = 1
	}
	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(totalWidth - 2).
		Height(innerH)
	rendered := style.Render(content)
	if styledTitle == "" {
		return rendered
	}
	return injectBorderTitle(rendered, styledTitle, lipgloss.Width(styledTitle), borderColor)
}

// injectBorderTitle replaces the first line (top border) of a lipgloss-rendered
// box with a new top border containing styledTitle.
func injectBorderTitle(rendered, styledTitle string, titleWidth int, borderColor lipgloss.Color) string {
	bs := lipgloss.NewStyle().Foreground(borderColor)

	idx := strings.IndexByte(rendered, '\n')
	var topLine, rest string
	if idx >= 0 {
		topLine = rendered[:idx]
		rest = rendered[idx:] // includes the \n
	} else {
		topLine = rendered
		rest = ""
	}

	topWidth := lipgloss.Width(topLine)
	innerWidth := topWidth - 2 // strip ╭ and ╮

	// ╭─ <title> <rightDashes>╮
	// visual width: 1+1+1 + titleWidth + 1 + rightDashes + 1 = topWidth
	rightDashes := innerWidth - 3 - titleWidth
	if rightDashes < 0 {
		rightDashes = 0
	}

	newTop := bs.Render("╭─ ") + styledTitle + bs.Render(" "+strings.Repeat("─", rightDashes)+"╮")
	return newTop + rest
}

// renderNavHeader builds the shared navigation header box used by every view.
// activeView is the name of the currently active view (must match an entry in
// navItems). The nav is left-aligned inside the box.
func renderNavHeader(activeView string, headerFocused, viewActive bool, version string, width int) string {
	navStrs := make([]string, len(navItems))
	for i, item := range navItems {
		if item == activeView && viewActive {
			if headerFocused {
				navStrs[i] = styleFocused("> " + item)
			} else {
				navStrs[i] = lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render("> " + item)
			}
		} else {
			navStrs[i] = lipgloss.NewStyle().Foreground(colorDarkGray).Render(item)
		}
	}
	nav := strings.Join(navStrs, lipgloss.NewStyle().Foreground(colorDarkGray).Render(" | "))

	borderColor := colorDarkGray
	if headerFocused {
		borderColor = colorGreen
	} else if viewActive {
		borderColor = colorCyan
	}

	rawTitle := fmt.Sprintf(" CloudCent CLI v%s ", version)
	styledTitle := lipgloss.NewStyle().Foreground(colorWhite).Bold(true).Render(rawTitle)
	return boxWithTitle(styledTitle, nav, borderColor, width)
}
