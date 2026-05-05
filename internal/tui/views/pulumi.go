//go:build tui

package views

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type PulumiSection int

const (
	PulumiSectionHeader PulumiSection = iota
	PulumiSectionContent
)

type PulumiEvent int

const (
	PulumiEventNone PulumiEvent = iota
	PulumiEventQuit
	PulumiEventPrevView
	PulumiEventNextView
)

type PulumiView struct {
	ActiveSection PulumiSection
	Width         int
	Height        int
}

func NewPulumiView(width, height int) *PulumiView {
	return &PulumiView{
		ActiveSection: PulumiSectionContent,
		Width:         width,
		Height:        height,
	}
}

func (v *PulumiView) HandleKey(key string) PulumiEvent {
	switch key {
	case "up":
		if v.ActiveSection == PulumiSectionContent {
			v.ActiveSection = PulumiSectionHeader
		}
	case "down":
		if v.ActiveSection == PulumiSectionHeader {
			v.ActiveSection = PulumiSectionContent
		}
	case "left":
		if v.ActiveSection == PulumiSectionHeader {
			return PulumiEventPrevView
		}
	case "right":
		if v.ActiveSection == PulumiSectionHeader {
			return PulumiEventNextView
		}
	case "esc":
		return PulumiEventQuit
	}
	return PulumiEventNone
}

func (v *PulumiView) Render(active bool, version string) string {
	headerFocused := active && v.ActiveSection == PulumiSectionHeader
	contentFocused := active && v.ActiveSection == PulumiSectionContent

	contentBorderColor := colorDarkGray
	if contentFocused {
		contentBorderColor = colorGreen
	}

	headerStr := renderNavHeader("Pulumi", headerFocused, active, version, v.Width)

	var helpText string
	switch v.ActiveSection {
	case PulumiSectionHeader:
		helpText = styleHelpKey("[↔]") + " Switch  " + styleHelpKey("[↓]") + " Content  " + styleHelpKey("[Esc]") + " Quit"
	case PulumiSectionContent:
		helpText = styleHelpKey("[↑]") + " Header  " + styleHelpKey("[Esc]") + " Quit"
	}
	helpTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Help")
	helpStr := boxWithTitle(helpTitle, helpText, colorDarkGray, v.Width)

	fixedLines := lineCount(headerStr) + lineCount(helpStr)
	contentMinHeight := v.Height - fixedLines
	if contentMinHeight < 3 {
		contentMinHeight = 3
	}

	var contentLines []string
	contentLines = append(contentLines, "")
	contentLines = append(contentLines, lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("Pulumi Integration"))
	contentLines = append(contentLines, "")
	contentLines = append(contentLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render(
		fmt.Sprintf("Use %s or %s from the CLI to interact with Pulumi.",
			lipgloss.NewStyle().Foreground(colorCyan).Render("cloudcent pulumi preview"),
			lipgloss.NewStyle().Foreground(colorCyan).Render("cloudcent pulumi mock"))))
	contentLines = append(contentLines, "")
	contentLines = append(contentLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render("TUI integration coming soon."))

	pulumiTitle := lipgloss.NewStyle().Foreground(contentBorderColor).Render("Pulumi")
	contentStr := boxWithTitleMinHeight(pulumiTitle, strings.Join(contentLines, "\n"), contentBorderColor, v.Width, contentMinHeight)

	return strings.Join([]string{headerStr, contentStr, helpStr}, "\n")
}
