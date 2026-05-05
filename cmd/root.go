package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

const Version = "0.0.3-beta"

var rootCmd = &cobra.Command{
	Use:     "cloudcent",
	Version: Version,
	Short:   "CloudCent — cloud pricing CLI",
	Long:    "CloudCent is a CLI for estimating cloud costs of drawio and pulumi",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

// Execute is the entry point called from main.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(
		initCmd,
		pricingCmd,
		diagramCmd,
		historyCmd,
		cacheCmd,
		configCmd,
		metadataCmd,
		pulumiCmd,
		uiCmd,
	)
}
