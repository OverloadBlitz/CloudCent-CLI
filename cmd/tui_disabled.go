//go:build !tui

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var uiCmd = &cobra.Command{
	Use:    "ui",
	Short:  "Launch the interactive TUI",
	Hidden: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		return fmt.Errorf("CloudCent TUI is disabled in this build")
	},
}
