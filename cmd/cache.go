package cmd

import (
	"fmt"

	"github.com/OverloadBlitz/cloudcent-cli/internal/db"
	"github.com/spf13/cobra"
)

var cacheCmd = &cobra.Command{
	Use:   "cache",
	Short: "Manage the pricing cache",
}

var cacheClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear all cached data and history",
	RunE:  runCacheClear,
}

var cacheStatsCmd = &cobra.Command{
	Use:   "stats",
	Short: "Show cache statistics",
	RunE:  runCacheStats,
}

func init() {
	cacheCmd.AddCommand(cacheClearCmd, cacheStatsCmd)
}

func runCacheClear(cmd *cobra.Command, args []string) error {
	database, err := db.New()
	if err != nil {
		return err
	}
	defer database.Close()

	if err := database.ClearAll(); err != nil {
		return err
	}
	fmt.Println("Cache and history cleared.")
	return nil
}

func runCacheStats(cmd *cobra.Command, args []string) error {
	database, err := db.New()
	if err != nil {
		return err
	}
	defer database.Close()

	count, size, err := database.GetCacheStats()
	if err != nil {
		return err
	}
	sizeMB := float64(size) / 1024 / 1024
	fmt.Printf("Cache entries: %d\n", count)
	fmt.Printf("Cache size:    %.2f MB\n", sizeMB)
	return nil
}
