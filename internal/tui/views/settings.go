//go:build tui

package views

import (
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/config"
	"github.com/charmbracelet/lipgloss"
)

type SettingsSection int

const (
	SettingsSectionHeader SettingsSection = iota
	SettingsSectionContent
)

type SettingsEvent int

const (
	SettingsEventNone SettingsEvent = iota
	SettingsEventQuit
	SettingsEventPrevView
	SettingsEventNextView
)

type SettingsView struct {
	ActiveSection SettingsSection
	Width         int
	Height        int
}

func NewSettingsView(width, height int) *SettingsView {
	return &SettingsView{
		ActiveSection: SettingsSectionContent,
		Width:         width,
		Height:        height,
	}
}

func (v *SettingsView) HandleKey(key string) SettingsEvent {
	switch key {
	case "up":
		if v.ActiveSection == SettingsSectionContent {
			v.ActiveSection = SettingsSectionHeader
		}
	case "down":
		if v.ActiveSection == SettingsSectionHeader {
			v.ActiveSection = SettingsSectionContent
		}
	case "left":
		if v.ActiveSection == SettingsSectionHeader {
			return SettingsEventPrevView
		}
	case "right":
		if v.ActiveSection == SettingsSectionHeader {
			return SettingsEventNextView
		}
	case "esc":
		return SettingsEventQuit
	}
	return SettingsEventNone
}

func (v *SettingsView) Render(active bool, cfg *config.Config, configPath, version string) string {
	headerFocused := active && v.ActiveSection == SettingsSectionHeader
	contentFocused := active && v.ActiveSection == SettingsSectionContent

	contentBorderColor := colorDarkGray
	if contentFocused {
		contentBorderColor = colorGreen
	}

	// Pass 1: render fixed-height sections (header and help).

	headerStr := renderNavHeader("Settings", headerFocused, active, version, v.Width)

	var helpText string
	switch v.ActiveSection {
	case SettingsSectionHeader:
		helpText = styleHelpKey("[↔]") + " Switch  " + styleHelpKey("[↓]") + " Content  " + styleHelpKey("[Esc]") + " Quit"
	case SettingsSectionContent:
		helpText = styleHelpKey("[↑]") + " Header  " + styleHelpKey("[Esc]") + " Quit"
	}
	helpTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Help")
	helpStr := boxWithTitle(helpTitle, helpText, colorDarkGray, v.Width)

	// Content section gets whatever's left. strings.Join("\n") does not add
	// extra lines beyond the sum of each section's lineCount.
	fixedLines := lineCount(headerStr) + lineCount(helpStr)
	contentMinHeight := v.Height - fixedLines
	if contentMinHeight < 3 {
		contentMinHeight = 3
	}

	// Pass 2: build content lines and render with min height.
	var contentLines []string
	contentLines = append(contentLines, "")
	contentLines = append(contentLines, lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render("Configuration"))
	contentLines = append(contentLines, "")

	if cfg != nil {
		maskedKey := "Not set"
		if cfg.APIKey != nil {
			key := *cfg.APIKey
			if len(key) > 12 {
				maskedKey = key[:8] + "..." + key[len(key)-4:]
			} else {
				maskedKey = "****"
			}
		}
		contentLines = append(contentLines,
			lipgloss.NewStyle().Foreground(colorCyan).Render("CLI ID:       ")+
				lipgloss.NewStyle().Foreground(colorWhite).Render(cfg.CliID))
		contentLines = append(contentLines, "")
		contentLines = append(contentLines,
			lipgloss.NewStyle().Foreground(colorCyan).Render("API Key:      ")+
				lipgloss.NewStyle().Foreground(colorWhite).Render(maskedKey))
		contentLines = append(contentLines, "")
		contentLines = append(contentLines,
			lipgloss.NewStyle().Foreground(colorCyan).Render("Config Path:  ")+
				lipgloss.NewStyle().Foreground(colorWhite).Render(configPath))
		contentLines = append(contentLines, "")
		contentLines = append(contentLines, "")
		contentLines = append(contentLines, lipgloss.NewStyle().Foreground(colorGreen).Render("Status: Authenticated"))
	} else {
		contentLines = append(contentLines, lipgloss.NewStyle().Foreground(colorRed).Render("Status: Not authenticated"))
		contentLines = append(contentLines, "")
		contentLines = append(contentLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render("Run `cloudcent init` to authenticate."))
	}

	settingsTitle := lipgloss.NewStyle().Foreground(contentBorderColor).Render("Settings")
	contentStr := boxWithTitleMinHeight(settingsTitle, strings.Join(contentLines, "\n"), contentBorderColor, v.Width, contentMinHeight)

	return strings.Join([]string{headerStr, contentStr, helpStr}, "\n")
}
