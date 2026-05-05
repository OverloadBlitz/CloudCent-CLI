package cmd

import (
	"fmt"

	"github.com/OverloadBlitz/cloudcent-cli/internal/config"
	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show current configuration",
	RunE:  runConfig,
}

func runConfig(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg == nil {
		fmt.Println("Not authenticated. Run `cloudcent init` to authenticate.")
		return nil
	}

	p, _ := config.Path()
	maskedKey := "Not set"
	if cfg.APIKey != nil {
		key := *cfg.APIKey
		if len(key) > 12 {
			maskedKey = key[:8] + "..." + key[len(key)-4:]
		} else {
			maskedKey = "****"
		}
	}

	fmt.Printf("Config Path:  %s\n", p)
	fmt.Printf("CLI ID:       %s\n", cfg.CliID)
	fmt.Printf("API Key:      %s\n", maskedKey)
	fmt.Println("Status:       Authenticated")
	return nil
}
