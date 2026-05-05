package cmd

import (
	"fmt"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/spf13/cobra"
)

var metadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Manage pricing metadata",
}

var metadataRefreshCmd = &cobra.Command{
	Use:   "refresh",
	Short: "Download latest pricing metadata from server",
	RunE:  runMetadataRefresh,
}

var metadataStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show metadata statistics",
	RunE:  runMetadataStats,
}

func init() {
	metadataCmd.AddCommand(metadataRefreshCmd, metadataStatsCmd)
}

func runMetadataRefresh(cmd *cobra.Command, args []string) error {
	client, err := api.New()
	if err != nil {
		return err
	}
	if !client.IsInitialized() {
		return fmt.Errorf("not authenticated — run 'cloudcent init' first")
	}

	fmt.Println("Downloading metadata…")
	if err := client.DownloadMetadataGz(); err != nil {
		return err
	}
	fmt.Println("Metadata downloaded successfully.")
	return nil
}

func runMetadataStats(cmd *cobra.Command, args []string) error {
	meta, err := api.LoadMetadataFromFile()
	if err != nil {
		return err
	}

	fmt.Printf("Products:    %d\n", len(meta.ProductRegions))
	fmt.Printf("Attr groups: %d\n", len(meta.ProductAttrs))

	totalRegions := map[string]struct{}{}
	for _, regs := range meta.ProductRegions {
		for _, r := range regs {
			if r != "" {
				totalRegions[r] = struct{}{}
			}
		}
	}
	fmt.Printf("Regions:     %d\n", len(totalRegions))
	return nil
}
