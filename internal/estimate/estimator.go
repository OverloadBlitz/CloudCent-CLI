package estimate

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
)

type batchPricingFetcher interface {
	FetchPricingBatch(api.BatchPricingRequest) (*api.BatchPricingApiResponse, error)
}

const defaultBatchPriceFilter = ">=0"

// defaultUsageQty is the monthly quantity assumed when the user does not
// provide a usage value for a usage-based resource (e.g. API Gateway requests).
const defaultUsageQty = 1_000_000

// hourlyUnits are unit strings that indicate time-based (per-hour) pricing.
// Any unit NOT in this set is treated as usage-based.
var hourlyUnits = map[string]bool{
	"hrs":   true,
	"hours": true,
	"hour":  true,
	"hr":    true,
}

// isHourlyUnit returns true when the unit string represents time-based pricing.
func isHourlyUnit(unit string) bool {
	return hourlyUnits[strings.ToLower(strings.TrimSpace(unit))]
}

// EstimateAllResources estimates costs for all resources.
// usageMap maps resource name → monthly quantity for usage-based resources.
// Pass nil or an empty map to use the built-in default (1 million units/mo).
func EstimateAllResources(client batchPricingFetcher, records []resources.DecodedResource, usageMap map[string]float64) ([]resources.EstimateResult, error) {
	if client == nil {
		return nil, fmt.Errorf("nil pricing client")
	}
	if len(records) == 0 {
		return []resources.EstimateResult{}, nil
	}

	// Separate billable resources from no-pricing ones.
	var billable []resources.DecodedResource
	result := make([]resources.EstimateResult, 0, len(records))

	for _, record := range records {
		if record.NoPricing {
			statusMsg := "Sorry, not supported yet"
			if record.IsFreeType {
				statusMsg = "free resource"
			}
			result = append(result, resources.EstimateResult{
				ResourceName:   record.Name,
				SubLabel:       record.SubLabel,
				RawType:        record.RawType,
				Product:        record.RawType,
				Props:          record.Props,
				InputsJSON:     record.InputsJSON,
				StatusMsg:      statusMsg,
				RegionFallback: record.RegionFallback,
			})
		} else {
			billable = append(billable, record)
		}
	}

	if len(billable) == 0 {
		return result, nil
	}

	requests := api.BatchPricingRequest{
		Requests: make([]api.BatchPricingRequestItem, 0, len(billable)),
	}

	for _, record := range billable {
		item := api.BatchPricingRequestItem{
			Provider: record.Provider,
			Region:   record.Region,
			Product:  record.Service,
			Attrs:    compactAttrs(record.Attrs),
			Price:    effectivePriceFilter(record.PriceFilter),
		}
		requests.Requests = append(requests.Requests, item)
	}

	prices, err := client.FetchPricingBatch(requests)
	if err != nil {
		return nil, err
	}

	for _, record := range billable {
		if prices == nil {
			result = append(result, resources.EstimateResult{
				ResourceName:   record.Name,
				SubLabel:       record.SubLabel,
				RawType:        record.RawType,
				Product:        displayProduct(record.Provider, record.Service),
				Props:          record.Props,
				InputsJSON:     record.InputsJSON,
				StatusMsg:      "Sorry, not supported yet",
				RegionFallback: record.RegionFallback,
			})
			continue
		}

		item, ok := findMatchingPrice(record, *prices)
		if !ok {
			result = append(result, resources.EstimateResult{
				ResourceName:   record.Name,
				SubLabel:       record.SubLabel,
				RawType:        record.RawType,
				Product:        displayProduct(record.Provider, record.Service),
				Props:          record.Props,
				InputsJSON:     record.InputsJSON,
				StatusMsg:      "Sorry, not supported yet",
				RegionFallback: record.RegionFallback,
			})
			continue
		}

		entries, onDemand := buildPriceEntries(item)

		// If every returned price is $0, treat the resource as free rather
		// than showing a confusing $0.00 table.
		if allZeroRates(entries) {
			result = append(result, resources.EstimateResult{
				ResourceName:   record.Name,
				SubLabel:       record.SubLabel,
				RawType:        record.RawType,
				Product:        displayProduct(item.Provider, item.Product),
				Props:          record.Props,
				InputsJSON:     record.InputsJSON,
				StatusMsg:      "free resource",
				RegionFallback: record.RegionFallback,
			})
			continue
		}

		// Detect whether this resource is usage-based (unit != Hrs).
		isUsage, usageUnit := detectUsageBased(entries)

		est := resources.EstimateResult{
			ResourceName:   record.Name,
			SubLabel:       record.SubLabel,
			RawType:        record.RawType,
			Product:        displayProduct(item.Provider, item.Product),
			Props:          record.Props,
			InputsJSON:     record.InputsJSON,
			Prices:         entries,
			OnDemandRate:   onDemand,
			IsUsageBased:   isUsage,
			UsageUnit:      usageUnit,
			RegionFallback: record.RegionFallback,
		}

		if isUsage {
			qty, isDefault := resolveUsageQty(record.Name, usageMap)
			est.UsageQty = qty
			est.UsageDefault = isDefault
			est.UsageMonthly = calcUsageMonthlyCost(entries, qty)
			// Clear OnDemandRate for usage-based resources so the hourly
			// totals box doesn't include them.
			est.OnDemandRate = 0
		}

		result = append(result, est)
	}

	return result, nil
}

// buildPriceEntries converts an api.PricingItem into a sorted slice of PriceEntry.
// OnDemand is always first; the rest are sorted by model then rate.
func buildPriceEntries(item api.PricingItem) ([]resources.PriceEntry, float64) {
	var entries []resources.PriceEntry
	var onDemandRate float64

	for _, p := range item.Prices {
		model := ""
		if p.PricingModel != nil {
			model = *p.PricingModel
		}
		purchaseOption := ""
		if p.PurchaseOption != nil {
			purchaseOption = *p.PurchaseOption
		}
		term := ""
		if p.Year != nil {
			term = p.Year.String()
		}
		upfront := ""
		if p.UpfrontFee != nil {
			upfront = p.UpfrontFee.String()
		}
		unit := ""
		if p.Unit != nil {
			unit = *p.Unit
		}

		rate := 0.0
		if len(p.Rates) > 0 && p.Rates[0].Price != nil {
			fmt.Sscanf(p.Rates[0].Price.String(), "%f", &rate)
		}

		isCurrent := strings.EqualFold(model, "OnDemand")
		if isCurrent && rate > onDemandRate {
			onDemandRate = rate
		}

		isUsage := !isHourlyUnit(unit)

		// Build rate tiers for volume-based pricing (more than one rate).
		var tiers []resources.RateTier
		if len(p.Rates) > 1 {
			for _, r := range p.Rates {
				tier := resources.RateTier{}
				if r.Price != nil {
					tier.Price = r.Price.String()
				}
				if r.StartRange != nil {
					tier.StartRange = r.StartRange.String()
				}
				if r.EndRange != nil {
					tier.EndRange = r.EndRange.String()
				}
				tiers = append(tiers, tier)
			}
		}

		entries = append(entries, resources.PriceEntry{
			Model:          model,
			PurchaseOption: purchaseOption,
			Term:           term,
			UpfrontFee:     upfront,
			RatePerHr:      rate,
			Unit:           unit,
			IsCurrent:      isCurrent,
			IsUsageBased:   isUsage,
			Tiers:          tiers,
		})
	}

	// Sort: OnDemand first, then by model name, then by rate ascending.
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsCurrent != entries[j].IsCurrent {
			return entries[i].IsCurrent
		}
		if entries[i].Model != entries[j].Model {
			return entries[i].Model < entries[j].Model
		}
		return entries[i].RatePerHr < entries[j].RatePerHr
	})

	return entries, onDemandRate
}

// allZeroRates returns true when every entry has a zero hourly rate,
// allZeroRates returns true when every entry has a zero rate across all tiers,
// meaning the resource is effectively free. For tiered pricing, any non-zero
// tier price means the resource is not free (the zero tier is just a free allowance).
func allZeroRates(entries []resources.PriceEntry) bool {
	if len(entries) == 0 {
		return false
	}
	for _, e := range entries {
		if e.RatePerHr != 0 {
			return false
		}
		// Check tiered pricing — a non-zero price in any tier means not free.
		for _, t := range e.Tiers {
			p := 0.0
			fmt.Sscanf(t.Price, "%f", &p)
			if p != 0 {
				return false
			}
		}
	}
	return true
}

func effectivePriceFilter(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return defaultBatchPriceFilter
	}
	return trimmed
}

func findMatchingPrice(record resources.DecodedResource, response map[string][]api.PricingItem) (api.PricingItem, bool) {
	for _, items := range response {
		for _, item := range items {
			if pricingItemMatchesRecord(item, record) {
				return item, true
			}
		}
	}
	return api.PricingItem{}, false
}

func pricingItemMatchesRecord(item api.PricingItem, record resources.DecodedResource) bool {
	if record.Provider != "" && !equalFoldTrim(item.Provider, record.Provider) {
		return false
	}
	if record.Region != "" && !equalFoldTrim(item.Region, record.Region) {
		return false
	}
	if record.Service != "" && !equalFoldTrim(item.Product, record.Service) {
		return false
	}

	expectedAttrs := compactAttrs(record.Attrs)
	for key, expectedValue := range expectedAttrs {
		actualValue, ok := lookupAttrCaseInsensitive(item.Attributes, key)
		if !ok || actualValue == nil || !equalFoldTrim(actualValue.String(), expectedValue) {
			return false
		}
	}

	return true
}

func lookupAttrCaseInsensitive(attrs map[string]*api.AttrValue, key string) (*api.AttrValue, bool) {
	if value, ok := attrs[key]; ok {
		return value, true
	}

	for actualKey, value := range attrs {
		if equalFoldTrim(actualKey, key) {
			return value, true
		}
	}

	return nil, false
}

func equalFoldTrim(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}

func compactAttrs(attrs map[string]string) map[string]string {
	if len(attrs) == 0 {
		return nil
	}

	compacted := make(map[string]string)
	for key, value := range attrs {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		compacted[key] = trimmed
	}

	if len(compacted) == 0 {
		return nil
	}

	return compacted
}

func displayProduct(provider, product string) string {
	switch {
	case provider != "" && product != "":
		return provider + " " + product
	case product != "":
		return product
	default:
		return provider
	}
}

// detectUsageBased returns (true, unit) when the dominant pricing unit is not
// time-based (i.e. not "Hrs"/"Hours"). It picks the unit from the first
// OnDemand entry, falling back to the first entry overall.
func detectUsageBased(entries []resources.PriceEntry) (bool, string) {
	for _, e := range entries {
		if e.IsCurrent {
			return !isHourlyUnit(e.Unit), e.Unit
		}
	}
	if len(entries) > 0 {
		return !isHourlyUnit(entries[0].Unit), entries[0].Unit
	}
	return false, ""
}

// resolveUsageQty returns the monthly quantity to use for a resource.
// It checks usageMap first; if not found it returns the default and isDefault=true.
func resolveUsageQty(resourceName string, usageMap map[string]float64) (qty float64, isDefault bool) {
	if usageMap != nil {
		if v, ok := usageMap[resourceName]; ok && v > 0 {
			return v, false
		}
	}
	return defaultUsageQty, true
}

// calcUsageMonthlyCost computes the monthly cost for a usage-based resource
// given a monthly quantity. It uses the OnDemand entry's tiers when available,
// otherwise falls back to the flat rate.
func calcUsageMonthlyCost(entries []resources.PriceEntry, monthlyQty float64) float64 {
	// Find the OnDemand entry.
	var target *resources.PriceEntry
	for i := range entries {
		if entries[i].IsCurrent {
			target = &entries[i]
			break
		}
	}
	if target == nil && len(entries) > 0 {
		target = &entries[0]
	}
	if target == nil {
		return 0
	}

	if len(target.Tiers) == 0 {
		// Flat rate: price per unit × quantity.
		return target.RatePerHr * monthlyQty
	}

	// Tiered pricing: walk through tiers and accumulate cost.
	return calcTieredCost(target.Tiers, monthlyQty)
}

// calcTieredCost applies volume-tiered pricing to a total quantity.
// Each tier covers [startRange, endRange) units at its price.
func calcTieredCost(tiers []resources.RateTier, totalQty float64) float64 {
	remaining := totalQty
	total := 0.0

	for _, tier := range tiers {
		if remaining <= 0 {
			break
		}

		price := 0.0
		fmt.Sscanf(tier.Price, "%f", &price)

		start := 0.0
		if tier.StartRange != "" {
			fmt.Sscanf(tier.StartRange, "%f", &start)
		}

		// endRange of "" or "Inf" means unlimited.
		isInf := tier.EndRange == "" ||
			strings.EqualFold(tier.EndRange, "inf") ||
			strings.EqualFold(tier.EndRange, "infinity")

		var tierSize float64
		if isInf {
			tierSize = remaining
		} else {
			end := 0.0
			if _, err := fmt.Sscanf(tier.EndRange, "%f", &end); err == nil {
				tierSize = end - start
			} else {
				tierSize = remaining
			}
		}

		units := remaining
		if units > tierSize {
			units = tierSize
		}

		total += price * units
		remaining -= units
	}

	return total
}

// formatUsageQty formats a usage quantity for display (e.g. 1000000 → "1,000,000").
func formatUsageQty(qty float64) string {
	s := strconv.FormatFloat(qty, 'f', 0, 64)
	// Insert thousand separators.
	n := len(s)
	if n <= 3 {
		return s
	}
	var out []byte
	for i, c := range s {
		if i > 0 && (n-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, byte(c))
	}
	return string(out)
}
