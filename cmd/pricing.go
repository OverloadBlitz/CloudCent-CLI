package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/spf13/cobra"
)

var pricingCmd = &cobra.Command{
	Use:   "pricing",
	Short: "Query cloud pricing data",
	RunE:  runPricing,
}

var (
	pricingProducts []string
	pricingRegions  []string
	pricingAttrs    []string
	pricingPrices   []string
	pricingLimit    int
	pricingOutput   string
)

func init() {
	pricingCmd.Flags().StringArrayVarP(&pricingProducts, "products", "p", nil, "Product filters e.g. \"AWS EC2\"")
	pricingCmd.Flags().StringArrayVarP(&pricingRegions, "regions", "r", nil, "Region filters e.g. us-east-1")
	pricingCmd.Flags().StringArrayVarP(&pricingAttrs, "attrs", "a", nil, "Attribute filters e.g. instanceType=t3.micro")
	pricingCmd.Flags().StringArrayVar(&pricingPrices, "price", nil, "Price filters e.g. \"<0.1\"")
	pricingCmd.Flags().IntVarP(&pricingLimit, "limit", "l", 20, "Maximum results to show")
	pricingCmd.Flags().StringVarP(&pricingOutput, "output", "o", "table", "Output format: table or json")
}

func runPricing(cmd *cobra.Command, args []string) error {
	if len(pricingProducts) == 0 {
		return fmt.Errorf("at least one --products value is required, e.g. \"AWS EC2\"")
	}

	attrMap := map[string]string{}
	for _, a := range pricingAttrs {
		parts := strings.SplitN(a, "=", 2)
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" {
			return fmt.Errorf("invalid attribute filter %q — expected key=value format", a)
		}
		attrMap[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
	}

	client, err := api.New()
	if err != nil {
		return err
	}
	if !client.IsInitialized() {
		return fmt.Errorf("not authenticated — run 'cloudcent init' first")
	}

	regionStr := "all regions"
	if len(pricingRegions) > 0 {
		regionStr = strings.Join(pricingRegions, ", ")
	}
	fmt.Printf("Querying pricing for %s in %s…\n", strings.Join(pricingProducts, ", "), regionStr)

	resp, err := client.FetchPricing(pricingProducts, pricingRegions, attrMap, pricingPrices)
	if err != nil {
		return err
	}

	if len(resp.Data) == 0 {
		fmt.Println("No results found.")
		return nil
	}

	limit := pricingLimit
	if limit > len(resp.Data) {
		limit = len(resp.Data)
	}
	items := resp.Data[:limit]
	fmt.Printf("Showing %d of %d results\n\n", len(items), resp.Total)

	if pricingOutput == "json" {
		out, _ := json.MarshalIndent(resp.Data, "", "  ")
		fmt.Println(string(out))
		return nil
	}

	for _, item := range items {
		product := item.Product
		if item.Provider != "" {
			product = item.Provider + " " + item.Product
		}
		fmt.Printf("  %s | %s\n", product, item.Region)

		if len(item.Attributes) > 0 {
			parts := []string{}
			for k, v := range item.Attributes {
				if v != nil {
					parts = append(parts, fmt.Sprintf("%s=%s", k, v.String()))
				}
			}
			if len(parts) > 0 {
				fmt.Printf("    Specs: %s\n", strings.Join(parts, ", "))
			}
		}

		for _, p := range item.Prices {
			model := "OnDemand"
			if p.PricingModel != nil {
				model = *p.PricingModel
			}
			unit := ""
			if p.Unit != nil {
				unit = *p.Unit
			}
			option := ""
			if p.PurchaseOption != nil {
				option = *p.PurchaseOption
			}

			if len(p.Rates) == 1 {
				priceVal := ""
				if p.Rates[0].Price != nil {
					priceVal = p.Rates[0].Price.String()
				}
				label := model
				if option != "" {
					label = fmt.Sprintf("%s (%s)", model, option)
				}
				fmt.Printf("    %s: $%s/%s\n", label, priceVal, unit)
			} else if len(p.Rates) > 1 {
				fmt.Printf("    %s (tiered):\n", model)
				for _, rate := range p.Rates {
					priceVal := ""
					if rate.Price != nil {
						priceVal = rate.Price.String()
					}
					start := ""
					if rate.StartRange != nil {
						start = rate.StartRange.String()
					}
					end := ""
					if rate.EndRange != nil {
						end = rate.EndRange.String()
					}
					fmt.Printf("      %s-%s: $%s/%s\n", start, end, priceVal, unit)
				}
			}
		}

		if item.MinPrice != nil && item.MaxPrice != nil {
			fmt.Printf("    Range: $%s – $%s\n", item.MinPrice.String(), item.MaxPrice.String())
		}
		fmt.Println()
	}
	return nil
}
