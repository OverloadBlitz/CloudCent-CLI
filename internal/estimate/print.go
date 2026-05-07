package estimate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/pulumi/resources"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/shopspring/decimal"
)

var (
	colHeader  = lipgloss.Color("#94A3B8")
	colBorder  = lipgloss.Color("#475569")
	colCurrent = lipgloss.Color("#22C55E")
	colMuted   = lipgloss.Color("#64748B")
	colTitle   = lipgloss.Color("#FFFFFF")
	colWarn    = lipgloss.Color("#F59E0B")
	colFree    = lipgloss.Color("#22C55E")
)

// resultGroup holds one or more EstimateResults that share the same resource name.
// When a resource produces multiple pricing queries (e.g. Lambda → Requests + Duration),
// they are rendered under a single resource header.
type resultGroup struct {
	results []resources.EstimateResult
}

// groupResults groups consecutive results that share the same ResourceName
// and have a non-empty SubLabel. Ungrouped results (SubLabel == "") get their
// own single-element group.
func groupResults(results []resources.EstimateResult) []resultGroup {
	var groups []resultGroup
	i := 0
	for i < len(results) {
		r := results[i]
		if r.SubLabel == "" {
			groups = append(groups, resultGroup{results: []resources.EstimateResult{r}})
			i++
			continue
		}
		// Collect consecutive results with the same ResourceName.
		j := i + 1
		for j < len(results) && results[j].ResourceName == r.ResourceName && results[j].SubLabel != "" {
			j++
		}
		groups = append(groups, resultGroup{results: results[i:j]})
		i = j
	}
	return groups
}

// PrintResults renders per-resource pricing tables and a final cost summary.
// Shared by `cloudcent pulumi estimate` and `cloudcent diagram estimate`.
func PrintResults(results []resources.EstimateResult) {
	titleSt := lipgloss.NewStyle().Foreground(colTitle).Bold(true)
	mutedSt := lipgloss.NewStyle().Foreground(colMuted)
	warnSt := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
	subLabelSt := lipgloss.NewStyle().Foreground(colHeader).Bold(true)

	var regionFallbackNames []string

	groups := groupResults(results)

	for i, g := range groups {
		first := g.results[0]
		fmt.Println()
		fmt.Printf("%s  %s\n",
			titleSt.Render(fmt.Sprintf("[%d] %s", i+1, first.ResourceName)),
			mutedSt.Render("("+first.Product+")"),
		)

		if len(first.Props) > 0 {
			propKeys := make([]string, 0, len(first.Props))
			for k := range first.Props {
				propKeys = append(propKeys, k)
			}
			sort.Strings(propKeys)
			for _, k := range propKeys {
				fmt.Printf("    %s %s\n",
					mutedSt.Render(fmt.Sprintf("%-18s", k)),
					first.Props[k],
				)
			}
		}

		if first.InputsJSON != "" {
			fmt.Printf("    %s\n", mutedSt.Render("Input properties:"))
			fmt.Println(indent(first.InputsJSON, "      "))
		}

		if first.RegionFallback {
			regionFallbackNames = append(regionFallbackNames, first.ResourceName)
			fmt.Printf("    %s\n", warnSt.Render("⚠ Region not detected — using us-east-1 as default"))
		}

		for _, r := range g.results {
			if r.SubLabel != "" {
				fmt.Printf("\n    %s\n", subLabelSt.Render("── "+r.SubLabel+" ──"))
			}

			if r.StatusMsg != "" {
				var msgSt lipgloss.Style
				if strings.Contains(r.StatusMsg, "free") {
					msgSt = lipgloss.NewStyle().Foreground(colFree)
				} else {
					msgSt = lipgloss.NewStyle().Foreground(colWarn)
				}
				fmt.Printf("    %s %s\n", mutedSt.Render("Pricing:"), msgSt.Render(r.StatusMsg))
				continue
			}

			if len(r.Prices) == 0 {
				fmt.Printf("    %s no data\n", mutedSt.Render("Pricing:"))
				continue
			}

			fmt.Println(renderPricesTable(r.Prices))

			if r.IsUsageBased {
				if r.UsageDefault {
					defaultQtySt := lipgloss.NewStyle().Foreground(lipgloss.Color("#F59E0B"))
					qtyPart := defaultQtySt.Render(formatUsageQty(r.UsageQty)+" "+r.UsageUnit+"/mo") +
						" " + mutedSt.Render("(default — use --usage to override)")
					fmt.Printf("    %s %s  →  %s\n",
						mutedSt.Render(fmt.Sprintf("%-18s", "Usage estimate")),
						qtyPart,
						titleSt.Render("$"+r.UsageMonthly.StringFixed(10)+" / mo"),
					)
				} else {
					qtyLabel := formatUsageQty(r.UsageQty) + " " + r.UsageUnit + "/mo"
					fmt.Printf("    %s %s  →  %s\n",
						mutedSt.Render(fmt.Sprintf("%-18s", "Usage estimate")),
						mutedSt.Render(qtyLabel),
						titleSt.Render("$"+r.UsageMonthly.StringFixed(10)+" / mo"),
					)
				}
			}
		}
	}

	// Totals
	totalHourly := decimal.Zero
	totalUsageMonthly := decimal.Zero
	hasHourlyCost := false
	hasUsageCost := false

	for _, r := range results {
		if r.OnDemandRate.IsPositive() {
			totalHourly = totalHourly.Add(r.OnDemandRate)
			hasHourlyCost = true
		}
		if r.IsUsageBased && r.UsageMonthly.IsPositive() {
			totalUsageMonthly = totalUsageMonthly.Add(r.UsageMonthly)
			hasUsageCost = true
		}
	}

	fmt.Println()
	if hasHourlyCost || hasUsageCost {
		monthly := totalHourly.Mul(decimal.NewFromInt(hoursPerMonth))
		fmt.Println(renderTotalsBox(totalHourly, monthly, totalUsageMonthly, hasUsageCost))
	} else {
		fmt.Println(mutedSt.Render("Total: no billable resources found"))
	}

	// Region fallback notice
	if len(regionFallbackNames) > 0 {
		fmt.Println()
		fmt.Println(warnSt.Render(" Region fallback notice"))
		fmt.Println(mutedSt.Render("  The following resources had no region detected and were priced using us-east-1:"))
		for _, name := range regionFallbackNames {
			fmt.Printf("    • %s\n", name)
		}
		fmt.Println()
		fmt.Println(mutedSt.Render("  To set a region, use one of:"))
		fmt.Println(mutedSt.Render("    CLI flag:             cloudcent pulumi estimate --config aws:region=us-west-2"))
	}

	fmt.Println()
}

func renderPricesTable(prices []resources.PriceEntry) string {
	// Check if any entry has tiered pricing.
	hasTiers := false
	for _, p := range prices {
		if len(p.Tiers) > 0 {
			hasTiers = true
			break
		}
	}

	if hasTiers {
		return renderTieredPricesTable(prices)
	}
	return renderFlatPricesTable(prices)
}

// renderFlatPricesTable renders the standard single-rate pricing table,
// hiding Purchase Option / Term / Upfront columns when all values are empty.
func renderFlatPricesTable(prices []resources.PriceEntry) string {
	// Detect which optional columns have data.
	hasOption, hasTerm, hasUpfront := false, false, false
	for _, p := range prices {
		if p.PurchaseOption != "" {
			hasOption = true
		}
		if p.Term != "" {
			hasTerm = true
		}
		if p.UpfrontFee != "" && p.UpfrontFee != "0" {
			hasUpfront = true
		}
	}

	currentRow := -1
	rows := make([][]string, 0, len(prices))
	for i, p := range prices {
		if p.IsCurrent && currentRow == -1 {
			currentRow = i
		}
		marker := ""
		if p.IsCurrent {
			marker = "▶"
		}
		row := []string{marker, p.Model}
		if hasOption {
			row = append(row, p.PurchaseOption)
		}
		if hasTerm {
			row = append(row, p.Term)
		}
		if hasUpfront {
			upfront := p.UpfrontFee
			if upfront == "" || upfront == "0" {
				upfront = "-"
			}
			row = append(row, upfront)
		}
		unit := "$/hr"
		if p.Unit != "" && !strings.EqualFold(p.Unit, "Hrs") {
			unit = "$/" + p.Unit
		}
		_ = unit // used in header
		row = append(row, p.RatePerHr.String())
		rows = append(rows, row)
	}

	headers := []string{"", "Model"}
	if hasOption {
		headers = append(headers, "Purchase Option")
	}
	if hasTerm {
		headers = append(headers, "Term")
	}
	if hasUpfront {
		headers = append(headers, "Upfront")
	}
	// Pick the rate column header from the first entry's unit.
	rateHeader := "$/hr"
	if len(prices) > 0 && prices[0].Unit != "" && !strings.EqualFold(prices[0].Unit, "Hrs") {
		rateHeader = "$/" + prices[0].Unit
	}
	headers = append(headers, rateHeader)

	headerSt := lipgloss.NewStyle().
		Foreground(colHeader).
		Bold(true).
		Padding(0, 1)
	cellSt := lipgloss.NewStyle().Padding(0, 1)
	currentSt := lipgloss.NewStyle().
		Foreground(colCurrent).
		Bold(true).
		Padding(0, 1)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colBorder)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerSt
			}
			if row == currentRow {
				return currentSt
			}
			return cellSt
		})

	return indent(t.Render(), "    ")
}

// renderTieredPricesTable renders volume-tiered pricing with one row per tier.
func renderTieredPricesTable(prices []resources.PriceEntry) string {
	headerSt := lipgloss.NewStyle().
		Foreground(colHeader).
		Bold(true).
		Padding(0, 1)
	cellSt := lipgloss.NewStyle().Padding(0, 1)
	currentSt := lipgloss.NewStyle().
		Foreground(colCurrent).
		Bold(true).
		Padding(0, 1)

	rows := make([][]string, 0)
	currentRow := -1

	for _, p := range prices {
		isCurrent := p.IsCurrent

		if len(p.Tiers) == 0 {
			// Single-rate entry among tiered ones — show one row.
			if isCurrent && currentRow == -1 {
				currentRow = len(rows)
			}
			marker := ""
			if isCurrent {
				marker = "▶"
			}
			rows = append(rows, []string{
				marker, p.Model, "-", "-",
				p.RatePerHr.String(),
				p.Unit,
			})
			continue
		}

		for i, tier := range p.Tiers {
			if isCurrent && currentRow == -1 {
				currentRow = len(rows)
			}
			marker := ""
			if isCurrent && i == 0 {
				marker = "▶"
			}
			model := ""
			if i == 0 {
				model = p.Model
			}
			rows = append(rows, []string{
				marker,
				model,
				formatRange(tier.StartRange),
				formatRange(tier.EndRange),
				tier.Price,
				p.Unit,
			})
		}
	}

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(colBorder)).
		Headers("", "Model", "Start Range", "End Range", "Price", "Unit").
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerSt
			}
			if row == currentRow {
				return currentSt
			}
			return cellSt
		})

	return indent(t.Render(), "    ")
}

// formatRange formats a range value for display, turning "Inf" into "∞"
// and adding thousand separators for large numbers.
func formatRange(s string) string {
	if s == "" {
		return "-"
	}
	if strings.EqualFold(s, "inf") || strings.EqualFold(s, "infinity") {
		return "∞"
	}
	return s
}

func renderTotalsBox(hourly, monthly, usageMonthly decimal.Decimal, hasUsage bool) string {
	labelSt := lipgloss.NewStyle().Foreground(colHeader)
	valueSt := lipgloss.NewStyle().Foreground(colTitle).Bold(true)
	mutedSt := lipgloss.NewStyle().Foreground(colMuted)

	lines := []string{}

	if hourly.IsPositive() {
		lines = append(lines,
			fmt.Sprintf("%s  %s",
				labelSt.Render(fmt.Sprintf("%-24s", "Hourly resources")),
				valueSt.Render(fmt.Sprintf("%s / hr", formatDecimal(hourly, 10))),
			),
			fmt.Sprintf("%s  %s",
				labelSt.Render(fmt.Sprintf("%-24s", fmt.Sprintf("  → est. monthly (%dh)", hoursPerMonth))),
				valueSt.Render(fmt.Sprintf("%s / mo", formatDecimal(monthly, 4))),
			),
		)
	}

	if hasUsage {
		if len(lines) > 0 {
			lines = append(lines, mutedSt.Render(strings.Repeat("─", 42)))
		}
		lines = append(lines,
			fmt.Sprintf("%s  %s",
				labelSt.Render(fmt.Sprintf("%-24s", "Usage-based resources")),
				valueSt.Render(fmt.Sprintf("%s / mo", formatDecimal(usageMonthly, 4))),
			),
			mutedSt.Render("  (based on supplied or default quantities)"),
		)
	}

	if hourly.IsPositive() && hasUsage {
		total := monthly.Add(usageMonthly)
		lines = append(lines, mutedSt.Render(strings.Repeat("─", 42)))
		lines = append(lines,
			fmt.Sprintf("%s  %s",
				labelSt.Render(fmt.Sprintf("%-24s", "Total estimated monthly")),
				valueSt.Render(fmt.Sprintf("%s / mo", formatDecimal(total, 4))),
			),
		)
	}

	body := strings.Join(lines, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colBorder).
		Padding(0, 2).
		Render(body)
}

// formatDecimal formats a decimal.Decimal for display, trimming trailing zeros
// but keeping at least `minDecimals` decimal places.
func formatDecimal(d decimal.Decimal, minDecimals int32) string {
	// Use enough precision to show meaningful digits, then trim trailing zeros.
	s := d.StringFixed(minDecimals)
	// Trim trailing zeros after decimal point, but keep at least 2 decimal places.
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		// Ensure at least 2 decimal places for currency readability.
		if idx := strings.Index(s, "."); idx >= 0 && len(s)-idx-1 < 2 {
			for len(s)-strings.Index(s, ".")-1 < 2 {
				s += "0"
			}
		}
	}
	return "$" + s
}

func indent(s, prefix string) string {
	out := ""
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out += prefix + s[start:i+1]
			start = i + 1
		}
	}
	if start < len(s) {
		out += prefix + s[start:]
	}
	return out
}
