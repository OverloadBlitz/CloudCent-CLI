package cmd

import (
	"fmt"

	"github.com/OverloadBlitz/cloudcent-cli/internal/db"
	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:   "history",
	Short: "Show query history",
	RunE:  runHistory,
}

var historyLimit int

func init() {
	historyCmd.Flags().IntVarP(&historyLimit, "limit", "n", 20, "Number of history entries to show")
}

func runHistory(cmd *cobra.Command, args []string) error {
	database, err := db.New()
	if err != nil {
		return err
	}
	defer database.Close()

	history, err := database.GetHistory(historyLimit)
	if err != nil {
		return err
	}

	if len(history) == 0 {
		fmt.Println("No query history found.")
		return nil
	}

	fmt.Printf("%-4s  %-19s  %-30s  %-20s  %s\n", "ID", "Time", "Products", "Region", "Results")
	fmt.Println(repeat("-", 90))
	for _, h := range history {
		ts := h.CreatedAt.Format("2006-01-02 15:04:05")
		fmt.Printf("%-4d  %-19s  %-30s  %-20s  %d\n",
			h.ID, ts,
			truncate(h.ProductFamilies, 30),
			truncate(h.Regions, 20),
			h.ResultCount,
		)
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}

func repeat(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}
