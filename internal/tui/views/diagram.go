//go:build tui

package views

import (
	"fmt"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/drawio"
	"github.com/charmbracelet/lipgloss"
)

type DiagramSection int

const (
	DiagramSectionHeader DiagramSection = iota
	DiagramSectionPathInput
	DiagramSectionComponentList
	DiagramSectionSpecPanel
)

type DiagramEvent int

const (
	DiagramEventNone DiagramEvent = iota
	DiagramEventQuit
	DiagramEventPrevView
	DiagramEventNextView
)

type DiagramView struct {
	ActiveSection     DiagramSection
	PathInput         string
	PathCursor        int
	Diagram           *drawio.Diagram
	ParseError        string
	SelectedComponent int
	ComponentScroll   int
	Width             int
	Height            int
}

func NewDiagramView(width, height int) *DiagramView {
	return &DiagramView{
		ActiveSection: DiagramSectionHeader,
		Width:         width,
		Height:        height,
	}
}

func (v *DiagramView) HandleKey(key string) DiagramEvent {
	switch v.ActiveSection {
	case DiagramSectionHeader:
		return v.handleKeyHeader(key)
	case DiagramSectionPathInput:
		return v.handleKeyPath(key)
	case DiagramSectionComponentList:
		return v.handleKeyComponents(key)
	case DiagramSectionSpecPanel:
		return v.handleKeySpec(key)
	}
	return DiagramEventNone
}

func (v *DiagramView) handleKeyHeader(key string) DiagramEvent {
	switch key {
	case "left":
		return DiagramEventPrevView
	case "right":
		return DiagramEventNextView
	case "down":
		v.ActiveSection = DiagramSectionPathInput
	case "esc":
		return DiagramEventQuit
	}
	return DiagramEventNone
}

func (v *DiagramView) handleKeyPath(key string) DiagramEvent {
	switch key {
	case "esc":
		return DiagramEventQuit
	case "up":
		v.ActiveSection = DiagramSectionHeader
	case "down":
		if v.Diagram != nil && len(v.Diagram.Components) > 0 {
			v.ActiveSection = DiagramSectionComponentList
		}
	case "enter":
		v.loadDiagram()
	case "backspace":
		if v.PathCursor > 0 {
			runes := []rune(v.PathInput)
			v.PathInput = string(append(runes[:v.PathCursor-1], runes[v.PathCursor:]...))
			v.PathCursor--
		}
	case "left":
		if v.PathCursor > 0 {
			v.PathCursor--
		}
	case "right":
		if v.PathCursor < len([]rune(v.PathInput)) {
			v.PathCursor++
		}
	default:
		if len(key) == 1 {
			runes := []rune(v.PathInput)
			runes = append(runes[:v.PathCursor], append([]rune(key), runes[v.PathCursor:]...)...)
			v.PathInput = string(runes)
			v.PathCursor++
		}
	}
	return DiagramEventNone
}

func (v *DiagramView) handleKeyComponents(key string) DiagramEvent {
	count := 0
	if v.Diagram != nil {
		count = len(v.Diagram.Components)
	}
	switch key {
	case "esc":
		return DiagramEventQuit
	case "up":
		if v.SelectedComponent == 0 {
			v.ActiveSection = DiagramSectionPathInput
		} else {
			v.SelectedComponent--
		}
	case "down":
		if count > 0 && v.SelectedComponent+1 < count {
			v.SelectedComponent++
		}
	case "right":
		v.ActiveSection = DiagramSectionSpecPanel
	}
	return DiagramEventNone
}

func (v *DiagramView) handleKeySpec(key string) DiagramEvent {
	switch key {
	case "esc":
		return DiagramEventQuit
	case "left":
		v.ActiveSection = DiagramSectionComponentList
	}
	return DiagramEventNone
}

func (v *DiagramView) loadDiagram() {
	d, err := drawio.ParseFile(strings.TrimSpace(v.PathInput))
	if err != nil {
		v.ParseError = err.Error()
		v.Diagram = nil
		return
	}
	v.ParseError = ""
	v.SelectedComponent = 0
	v.ComponentScroll = 0
	v.Diagram = d
	if len(d.Components) > 0 {
		v.ActiveSection = DiagramSectionComponentList
	}
}

// formatServiceTag renders the `<provider>:<service>` tag shown next to a
// component's label. `unknown` is the placeholder for components whose
// shape couldn't be identified (no AWS/Azure/GCP/OCI stencil).
func formatServiceTag(c drawio.Component, unknown string) string {
	if c.ServiceType == "" {
		return unknown
	}
	if c.Provider == "" {
		return c.ServiceType
	}
	return c.Provider + ":" + c.ServiceType
}

func (v *DiagramView) Render(active bool, version string) string {
	headerFocused := active && v.ActiveSection == DiagramSectionHeader

	// Nav header
	headerStr := renderNavHeader("Diagram", headerFocused, active, version, v.Width)

	// Path input
	pathFocused := active && v.ActiveSection == DiagramSectionPathInput
	pathBorderColor := colorDarkGray
	if pathFocused {
		pathBorderColor = colorGreen
	}
	var pathContent string
	if pathFocused {
		pathContent = lipgloss.NewStyle().Foreground(colorWhite).Render(v.PathInput) +
			lipgloss.NewStyle().Foreground(colorCyan).Render("▌")
	} else if v.PathInput == "" {
		pathContent = lipgloss.NewStyle().Foreground(colorDarkGray).Render("Enter path to .drawio file...")
	} else {
		pathContent = lipgloss.NewStyle().Foreground(colorWhite).Render(v.PathInput)
	}
	pathTitle := " Diagram File "
	if v.ParseError != "" {
		pathTitle = " Error: " + truncate(v.ParseError, 40) + " "
	}
	pathStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(pathBorderColor).Width(v.Width - 2)
	pathStr := pathStyle.Render(pathTitle + "\n" + " Path: " + pathContent)

	// Component list
	compFocused := active && v.ActiveSection == DiagramSectionComponentList
	compBorderColor := colorDarkGray
	if compFocused {
		compBorderColor = colorGreen
	}

	var compLines []string
	if v.Diagram != nil {
		for i, comp := range v.Diagram.Components {
			label := fmt.Sprintf(" %s [%s]", comp.Label, formatServiceTag(comp, "?"))
			if i == v.SelectedComponent && compFocused {
				compLines = append(compLines, lipgloss.NewStyle().Foreground(colorBlack).Background(colorGreen).Bold(true).Render(label))
			} else if i == v.SelectedComponent {
				compLines = append(compLines, lipgloss.NewStyle().Foreground(colorYellow).Render(label))
			} else {
				compLines = append(compLines, label)
			}
		}
	} else {
		compLines = append(compLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render(" No diagram loaded."))
	}

	// Spec panel
	specFocused := active && v.ActiveSection == DiagramSectionSpecPanel
	specBorderColor := colorDarkGray
	if specFocused {
		specBorderColor = colorGreen
	}
	var specLines []string
	if v.Diagram != nil && v.SelectedComponent < len(v.Diagram.Components) {
		comp := v.Diagram.Components[v.SelectedComponent]
		specLines = append(specLines, lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(
			fmt.Sprintf(" %s — %s", comp.Label, formatServiceTag(comp, "Unknown"))))
		specLines = append(specLines, "")

		cat := drawio.CatalogFor(comp.ServiceType)
		if cat.Product == "" {
			specLines = append(specLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render("  No pricing for this service."))
		} else {
			specLines = append(specLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render(
				fmt.Sprintf("  Pricing product: %s", cat.Product)))
			specLines = append(specLines, "")
			specLines = append(specLines, lipgloss.NewStyle().Foreground(colorYellow).Render("  Required attrs:"))
			for _, key := range cat.AttrKeys {
				specLines = append(specLines, "    "+lipgloss.NewStyle().Foreground(colorWhite).Render(key))
			}
			if len(cat.FixedAttrs) > 0 {
				specLines = append(specLines, "")
				specLines = append(specLines, lipgloss.NewStyle().Foreground(colorYellow).Render("  Fixed attrs:"))
				for k, v := range cat.FixedAttrs {
					specLines = append(specLines, "    "+lipgloss.NewStyle().Foreground(colorWhite).Render(k+"="+v))
				}
			}
			specLines = append(specLines, "")
			specLines = append(specLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render(
				"  Run `cloudcent diagram init <path>` to generate a YAML spec."))
		}
	} else {
		specLines = append(specLines, lipgloss.NewStyle().Foreground(colorDarkGray).Render(" Select a component."))
	}

	halfW := (v.Width - 4) / 2
	compStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(compBorderColor).Width(halfW)
	specStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(specBorderColor).Width(v.Width - halfW - 4)
	mainStr := lipgloss.JoinHorizontal(lipgloss.Top,
		compStyle.Render(" Components \n"+strings.Join(compLines, "\n")),
		specStyle.Render(" Spec Configuration \n"+strings.Join(specLines, "\n")),
	)

	// Help
	var helpText string
	switch v.ActiveSection {
	case DiagramSectionHeader:
		helpText = styleHelpKey("[←→]") + " Switch View  " + styleHelpKey("[↓]") + " Path  " + styleHelpKey("[Esc]") + " Quit"
	case DiagramSectionPathInput:
		helpText = styleHelpKey("[Enter]") + " Load  " + styleHelpKey("[↑]") + " Header  " + styleHelpKey("[↓]") + " Components  " + styleHelpKey("[Esc]") + " Quit"
	case DiagramSectionComponentList:
		helpText = styleHelpKey("[↑↓]") + " Navigate  " + styleHelpKey("[→]") + " View Spec  " + styleHelpKey("[Esc]") + " Quit"
	case DiagramSectionSpecPanel:
		helpText = styleHelpKey("[←]") + " Back  " + styleHelpKey("[Esc]") + " Quit"
	}

	return strings.Join([]string{headerStr, pathStr, mainStr, helpText}, "\n")
}
