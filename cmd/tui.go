//go:build tui

package cmd

import (
	"github.com/OverloadBlitz/cloudcent-cli/internal/tui"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:   "ui",
	Short: "Launch the interactive TUI",
	RunE: func(cmd *cobra.Command, args []string) error {
		return runTUI()
	},
}

func runTUI() error {
	app, err := tui.NewApp(Version)
	if err != nil {
		return err
	}
	p := tea.NewProgram(app, tea.WithAltScreen())
	_, err = p.Run()
	return err
}
