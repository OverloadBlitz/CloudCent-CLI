package cmd

import (
	"fmt"
	"time"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/config"
	"github.com/pkg/browser"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: "Initialize CloudCent authentication (opens browser)",
	RunE:  runInit,
}

func runInit(cmd *cobra.Command, args []string) error {
	client, err := api.New()
	if err != nil {
		return err
	}

	if client.IsInitialized() {
		fmt.Println("Already authenticated. Use `cloudcent config` to view your credentials.")
		return nil
	}

	fmt.Println("Opening browser for authentication…")

	tokenResp, err := client.GenerateToken()
	if err != nil {
		return fmt.Errorf("failed to generate token: %w", err)
	}

	authURL := fmt.Sprintf("%s?token=%s&exchange=%s", api.CLIBaseURL, tokenResp.AccessToken, tokenResp.ExchangeCode)
	if err := browser.OpenURL(authURL); err != nil {
		fmt.Printf("Could not open browser automatically. Please visit:\n%s\n", authURL)
	}

	fmt.Println("Waiting for authentication (up to 5 minutes)…")

	const maxAttempts = 150
	for i := 0; i < maxAttempts; i++ {
		time.Sleep(2 * time.Second)

		resp, err := client.ExchangeToken(tokenResp.ExchangeCode)
		if err != nil {
			continue
		}

		if resp.CliID != nil && resp.APIKey != nil {
			cfg := &config.Config{
				CliID:  *resp.CliID,
				APIKey: resp.APIKey,
			}
			if err := client.SaveConfig(cfg); err != nil {
				return fmt.Errorf("failed to save config: %w", err)
			}
			fmt.Printf("Authentication successful! CLI ID: %s\n", *resp.CliID)
			return nil
		}

		if resp.Status != nil {
			switch *resp.Status {
			case "expired":
				return fmt.Errorf("authentication token expired")
			case "pending":
				continue
			}
		}
	}

	return fmt.Errorf("authentication timed out after 5 minutes")
}
