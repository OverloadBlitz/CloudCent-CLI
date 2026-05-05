//go:build tui

package views

import (
	"fmt"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/db"
	"github.com/charmbracelet/lipgloss"
)

type HistorySection int

const (
	HistorySectionHeader HistorySection = iota
	HistorySectionContent
)

type HistoryEvent int

const (
	HistoryEventNone HistoryEvent = iota
	HistoryEventQuit
	HistoryEventPrevView
	HistoryEventNextView
	HistoryEventClearAll
	HistoryEventOpenInPricing
)

type HistoryView struct {
	ActiveSection   HistorySection
	History         []db.QueryHistory
	Selected        int
	ListOffset      int // index of the first visible history row
	listVisible     int // cached from last Render(); used by clampListOffset
	SelectedResults []PricingDisplayItem
	Width           int
	Height          int
}

func NewHistoryView(width, height int) *HistoryView {
	return &HistoryView{
		ActiveSection: HistorySectionContent,
		listVisible:   10, // sensible default before first render
		Width:         width,
		Height:        height,
	}
}

// clampListOffset ensures ListOffset keeps Selected in the visible window.
// It uses the listVisible value set during the last Render().
func (v *HistoryView) clampListOffset() {
	visible := v.listVisible
	if visible < 1 {
		visible = 1
	}
	if v.Selected < v.ListOffset {
		v.ListOffset = v.Selected
	}
	if v.Selected >= v.ListOffset+visible {
		v.ListOffset = v.Selected - visible + 1
	}
	if v.ListOffset < 0 {
		v.ListOffset = 0
	}
}

func (v *HistoryView) HandleKey(key string) (HistoryEvent, int) {
	switch key {
	case "up":
		if v.ActiveSection == HistorySectionContent {
			if v.Selected > 0 {
				v.Selected--
				v.clampListOffset()
			} else {
				v.ActiveSection = HistorySectionHeader
			}
		}
		return HistoryEventNone, 0
	case "down":
		if v.ActiveSection == HistorySectionHeader {
			v.ActiveSection = HistorySectionContent
		} else if len(v.History) > 0 && v.Selected+1 < len(v.History) {
			v.Selected++
			v.clampListOffset()
		}
		return HistoryEventNone, 0
	case "left":
		if v.ActiveSection == HistorySectionHeader {
			return HistoryEventPrevView, 0
		}
	case "right":
		if v.ActiveSection == HistorySectionHeader {
			return HistoryEventNextView, 0
		}
	case "c", "C":
		return HistoryEventClearAll, 0
	case "enter":
		if v.ActiveSection == HistorySectionContent && len(v.History) > 0 {
			return HistoryEventOpenInPricing, v.Selected
		}
	case "esc":
		return HistoryEventQuit, 0
	}
	return HistoryEventNone, 0
}

func (v *HistoryView) Render(active bool, cacheCount, cacheBytes int, cachePath, version string) string {
	headerFocused := active && v.ActiveSection == HistorySectionHeader
	contentFocused := active && v.ActiveSection == HistorySectionContent

	contentBorderColor := colorDarkGray
	if contentFocused {
		contentBorderColor = colorGreen
	}

	// Pass 1: render all fixed-height sections (header, preview, help).

	// Nav header
	headerStr := renderNavHeader("History", headerFocused, active, version, v.Width)

	// Price preview (fixed height, capped at 5 data rows + 1 header)
	var previewLines []string
	if len(v.SelectedResults) > 0 {
		previewLines = append(previewLines, lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render(
			fmt.Sprintf("%-30s  %-20s  %s", "Product", "Region", "Price")))
		for _, item := range v.SelectedResults {
			if len(previewLines) > 5 {
				break
			}
			previewLines = append(previewLines, fmt.Sprintf("%-30s  %-20s  %s",
				truncate(item.Product, 30), truncate(item.Region, 20), item.MinPrice))
		}
	} else {
		previewLines = append(previewLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render("No cached results for this query."))
	}
	prevTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Price Preview")
	previewStr := boxWithTitle(prevTitle, strings.Join(previewLines, "\n"), colorDarkGray, v.Width)

	// Help block
	var helpText string
	switch v.ActiveSection {
	case HistorySectionHeader:
		helpText = styleHelpKey("[↔]") + " Switch  " + styleHelpKey("[↓]") + " Content  " + styleHelpKey("[Esc]") + " Quit"
	case HistorySectionContent:
		helpText = styleHelpKey("[↑↓]") + " Navigate  " + styleHelpKey("[Enter]") + " Open  " + styleHelpKey("[c]") + " Clear  " + styleHelpKey("[Esc]") + " Quit"
	}
	helpTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Help")
	helpStr := boxWithTitle(helpTitle, helpText, colorDarkGray, v.Width)

	// Measure fixed lines for header + preview + help (cache is side-by-side with list).
	fixedLines := lineCount(headerStr) + lineCount(previewStr) + lineCount(helpStr)

	// The side-by-side row (history list | cache) gets all remaining height; minimum 3.
	rowMinHeight := v.Height - fixedLines
	if rowMinHeight < 3 {
		rowMinHeight = 3
	}

	// Split width: history gets ~2/3, cache gets ~1/3.
	cacheWidth := v.Width / 3
	if cacheWidth < 20 {
		cacheWidth = 20
	}
	listWidth := v.Width - cacheWidth

	// Update listVisible so clampListOffset used from HandleKey stays accurate.
	v.listVisible = rowMinHeight - 2 // inner rows = height minus 2 border lines
	if v.listVisible < 1 {
		v.listVisible = 1
	}

	// Clamp scroll offset now that listVisible is fresh.
	v.clampListOffset()

	// Pass 2: render history list.
	var listLines []string
	end := v.ListOffset + v.listVisible
	if end > len(v.History) {
		end = len(v.History)
	}
	for i := v.ListOffset; i < end; i++ {
		h := v.History[i]
		ts := h.CreatedAt.Format("15:04")
		isSel := i == v.Selected && contentFocused

		line := fmt.Sprintf("[%s] %-30s @%-20s -> %d items",
			ts,
			truncate(h.ProductFamilies, 30),
			truncate(h.Regions, 20),
			h.ResultCount,
		)
		if isSel {
			listLines = append(listLines, lipgloss.NewStyle().Background(lipgloss.Color("0")).Foreground(colorWhite).Render(line))
		} else {
			listLines = append(listLines, line)
		}
	}
	if len(listLines) == 0 {
		listLines = append(listLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render("No history found."))
	}

	histTitleText := fmt.Sprintf("Command History (%d total)", len(v.History))
	if len(v.History) > v.listVisible {
		histTitleText += fmt.Sprintf(" [%d-%d]", v.ListOffset+1, end)
	}
	histTitle := lipgloss.NewStyle().Foreground(contentBorderColor).Render(histTitleText)
	listStr := boxWithTitleMinHeight(histTitle, strings.Join(listLines, "\n"), contentBorderColor, listWidth, rowMinHeight)

	// Cache stats block (same height as history list).
	sizeMB := float64(cacheBytes) / 1024 / 1024
	cacheContent := lipgloss.NewStyle().Foreground(colorDarkGray).Render(
		fmt.Sprintf("Items:    %d\nLocation: %s\nSize:     %.2f MB", cacheCount, cachePath, sizeMB),
	)
	cacheTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Cache")
	cacheStr := boxWithTitleMinHeight(cacheTitle, cacheContent, colorDarkGray, cacheWidth, rowMinHeight)

	// Join history and cache side by side.
	rowStr := lipgloss.JoinHorizontal(lipgloss.Top, listStr, cacheStr)

	return strings.Join([]string{headerStr, rowStr, previewStr, helpStr}, "\n")
}
