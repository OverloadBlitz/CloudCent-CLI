//go:build tui

package views

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/semantic"
	"github.com/charmbracelet/lipgloss"
)

// PricingSection tracks keyboard focus within the Pricing view.
type PricingSection int

const (
	PricingSectionHeader PricingSection = iota
	PricingSectionCommand
	PricingSectionResults
)

// BuilderFocus tracks whether focus is on the input field or suggestions.
type BuilderFocus int

const (
	BuilderFocusField BuilderFocus = iota
	BuilderFocusSuggestions
)

// CommandBuilderState holds the structured query builder state.
type CommandBuilderState struct {
	SelectedField int // 0=products, 1=regions, 2=attrs, 3=price
	ProductTags   []string
	RegionTags    []string
	AttributeTags []string
	PriceTags     []string
	SearchInput   string
}

func NewCommandBuilderState() CommandBuilderState {
	return CommandBuilderState{}
}

func (b *CommandBuilderState) CurrentTags() []string {
	switch b.SelectedField {
	case 0:
		return b.ProductTags
	case 1:
		return b.RegionTags
	case 2:
		return b.AttributeTags
	default:
		return b.PriceTags
	}
}

func (b *CommandBuilderState) SetCurrentTags(tags []string) {
	switch b.SelectedField {
	case 0:
		b.ProductTags = tags
	case 1:
		b.RegionTags = tags
	case 2:
		b.AttributeTags = tags
	default:
		b.PriceTags = tags
	}
}

func (b *CommandBuilderState) PopCurrentTag() {
	tags := b.CurrentTags()
	if len(tags) > 0 {
		b.SetCurrentTags(tags[:len(tags)-1])
	}
}

func (b *CommandBuilderState) ClearCurrentTags() {
	b.SetCurrentTags(nil)
}

// PricingDisplayItem is a processed result row for display.
type PricingDisplayItem struct {
	Product  string
	Region   string
	Attrs    map[string]*string // ordered keys from API
	AttrKeys []string
	Prices   []PriceInfo
	MinPrice string
	MaxPrice string
}

type RateInfo struct {
	Price      string
	StartRange string
	EndRange   string
}

type PriceInfo struct {
	PricingModel       string
	Price              string
	Unit               string
	UpfrontFee         string
	PurchaseOption     string
	Year               string
	InterruptionMaxPct string
	Rates              []RateInfo
}

// PricingView holds all state for the pricing screen.
type PricingView struct {
	ActiveSection    PricingSection
	BuilderFocus     BuilderFocus
	CommandBuilder   CommandBuilderState
	Items            []PricingDisplayItem
	FilteredItems    []PricingDisplayItem
	Selected         int
	ResultsPage      int
	ResultsPerPage   int
	Loading          bool
	ErrorMessage     string
	SuggestionsCache []semantic.SuggestionItem
	SuggestionIndex  int // -1 = none
	SuggestionScroll int // first visible row in the suggestion grid
	HScrollOffset    int // index into scrollable attr columns
	scrollColCount   int // total scrollable attr columns, set each Render
	Options          *PricingOptions
	Width            int
	Height           int
}

// PricingOptions mirrors the Rust PricingOptions struct.
type PricingOptions struct {
	Products        []string
	Regions         []string
	ProductRegions  map[string][]string
	ProductAttrs    map[string][]string
	AttributeValues map[string]map[string][]string
	ProductGroups   map[string]uint64
}

// ProcessMetadata builds PricingOptions from API metadata.
func ProcessMetadata(meta *api.MetadataResponse) *PricingOptions {
	products := make([]string, 0, len(meta.ProductRegions))
	for k := range meta.ProductRegions {
		products = append(products, k)
	}
	sort.Strings(products)

	regionSet := map[string]struct{}{}
	for _, regs := range meta.ProductRegions {
		for _, r := range regs {
			if r != "" {
				regionSet[r] = struct{}{}
			}
		}
	}
	regions := make([]string, 0, len(regionSet))
	for r := range regionSet {
		regions = append(regions, r)
	}
	sort.Strings(regions)

	return &PricingOptions{
		Products:        products,
		Regions:         regions,
		ProductRegions:  meta.ProductRegions,
		ProductAttrs:    meta.ProductAttrs,
		AttributeValues: meta.AttributeValues,
		ProductGroups:   meta.ProductGroups,
	}
}

// pricingSplitThreshold is the max number of prices before a two-column layout.
const pricingSplitThreshold = 7

func NewPricingView(width, height int) *PricingView {
	return &PricingView{
		ActiveSection:   PricingSectionCommand,
		BuilderFocus:    BuilderFocusField,
		CommandBuilder:  NewCommandBuilderState(),
		ResultsPerPage:  15,
		SuggestionIndex: -1,
		Width:           width,
		Height:          height,
	}
}

// PricingEvent is returned from HandleKey.
type PricingEvent int

const (
	PricingEventNone PricingEvent = iota
	PricingEventQuit
	PricingEventPrevView
	PricingEventNextView
	PricingEventSubmitQuery
)

func (v *PricingView) HandleKey(key string) PricingEvent {
	if key == "f2" {
		v.CommandBuilder = NewCommandBuilderState()
		v.BuilderFocus = BuilderFocusField
		v.SuggestionsCache = nil
		v.SuggestionIndex = -1
		v.SuggestionScroll = 0
		v.FilterItems()
		if v.ActiveSection == PricingSectionCommand {
			v.UpdateSuggestions()
		}
		return PricingEventNone
	}

	switch v.ActiveSection {
	case PricingSectionHeader:
		return v.handleKeyHeader(key)
	case PricingSectionCommand:
		return v.handleKeyCommand(key)
	case PricingSectionResults:
		return v.handleKeyResults(key)
	}
	return PricingEventNone
}

func (v *PricingView) handleKeyHeader(key string) PricingEvent {
	switch key {
	case "left":
		return PricingEventPrevView
	case "right":
		return PricingEventNextView
	case "down":
		v.ActiveSection = PricingSectionCommand
		v.UpdateSuggestions()
	case "esc":
		return PricingEventQuit
	}
	return PricingEventNone
}

func (v *PricingView) handleKeyCommand(key string) PricingEvent {
	switch key {
	case "esc":
		return PricingEventQuit
	case "tab":
		v.handleTabBuilder()
		return PricingEventNone
	}
	if v.BuilderFocus == BuilderFocusField {
		return v.handleKeyBuilderField(key)
	}
	return v.handleKeyBuilderSuggestions(key)
}

func (v *PricingView) handleTabBuilder() {
	input := v.CommandBuilder.SearchInput
	if input != "" && len(v.SuggestionsCache) > 0 {
		if len(v.SuggestionsCache) == 1 {
			v.toggleSuggestion(0)
		} else {
			v.SuggestionIndex = 0
		}
	} else if len(v.SuggestionsCache) > 0 {
		v.SuggestionIndex = 0
	}
}

func (v *PricingView) handleKeyBuilderField(key string) PricingEvent {
	switch key {
	case "left":
		if v.CommandBuilder.SelectedField > 0 {
			v.CommandBuilder.SelectedField--
			v.CommandBuilder.SearchInput = ""
			v.SuggestionIndex = -1
			v.SuggestionScroll = 0
			v.UpdateSuggestions()
		} else {
			return PricingEventPrevView
		}
	case "right":
		if v.CommandBuilder.SelectedField < 3 {
			v.CommandBuilder.SelectedField++
			v.CommandBuilder.SearchInput = ""
			v.SuggestionIndex = -1
			v.SuggestionScroll = 0
			v.UpdateSuggestions()
		} else {
			return PricingEventNextView
		}
	case "up":
		v.ActiveSection = PricingSectionHeader
	case "down":
		if len(v.SuggestionsCache) > 0 {
			v.BuilderFocus = BuilderFocusSuggestions
			if v.SuggestionIndex < 0 {
				v.SuggestionIndex = 0
			}
		} else if len(v.FilteredItems) > 0 {
			v.ActiveSection = PricingSectionResults
		}
	case "enter":
		return PricingEventSubmitQuery
	case "backspace":
		if v.CommandBuilder.SearchInput == "" {
			v.CommandBuilder.PopCurrentTag()
			v.FilterItems()
			v.UpdateSuggestions()
		} else {
			runes := []rune(v.CommandBuilder.SearchInput)
			if len(runes) > 0 {
				v.CommandBuilder.SearchInput = string(runes[:len(runes)-1])
			}
			v.SuggestionIndex = -1
			v.UpdateSuggestions()
			v.FilterItems()
		}
	case "delete":
		if v.CommandBuilder.SearchInput == "" {
			v.CommandBuilder.ClearCurrentTags()
			v.FilterItems()
			v.UpdateSuggestions()
		} else {
			v.CommandBuilder.SearchInput = ""
			v.SuggestionIndex = -1
			v.UpdateSuggestions()
			v.FilterItems()
		}
	default:
		if len(key) == 1 {
			v.CommandBuilder.SearchInput += key
			v.SuggestionIndex = -1
			v.UpdateSuggestions()
			v.FilterItems()
		}
	}
	return PricingEventNone
}

func (v *PricingView) handleKeyBuilderSuggestions(key string) PricingEvent {
	cols := v.suggestionCols()
	if cols < 1 {
		cols = 1
	}
	total := len(v.SuggestionsCache)

	switch key {
	case "up":
		atTop := v.SuggestionIndex < cols
		if atTop {
			v.BuilderFocus = BuilderFocusField
			return PricingEventNone
		}
		if total > 0 {
			v.SuggestionIndex -= cols
			if v.SuggestionIndex < 0 {
				v.SuggestionIndex = 0
			}
		}
	case "down":
		if total > 0 {
			next := v.SuggestionIndex + cols
			if next >= total {
				if len(v.FilteredItems) > 0 {
					v.BuilderFocus = BuilderFocusField
					v.ActiveSection = PricingSectionResults
				}
			} else {
				v.SuggestionIndex = next
			}
		} else if len(v.FilteredItems) > 0 {
			v.BuilderFocus = BuilderFocusField
			v.ActiveSection = PricingSectionResults
		}
	case "right":
		if total > 0 {
			v.SuggestionIndex = min(v.SuggestionIndex+1, total-1)
		}
	case "left":
		if v.SuggestionIndex%cols == 0 {
			v.BuilderFocus = BuilderFocusField
			return PricingEventNone
		}
		if total > 0 && v.SuggestionIndex > 0 {
			v.SuggestionIndex--
		}
	case " ":
		if v.SuggestionIndex >= 0 && v.SuggestionIndex < total {
			v.toggleSuggestion(v.SuggestionIndex)
		}
	case "enter":
		return PricingEventSubmitQuery
	}
	return PricingEventNone
}

func (v *PricingView) handleKeyResults(key string) PricingEvent {
	switch key {
	case "up":
		pageStart := v.ResultsPage * v.ResultsPerPage
		if v.Selected > pageStart {
			v.Selected--
		} else {
			v.ActiveSection = PricingSectionCommand
			v.BuilderFocus = BuilderFocusField
			v.UpdateSuggestions()
		}
	case "down":
		if len(v.FilteredItems) > 0 {
			pageEnd := ((v.ResultsPage + 1) * v.ResultsPerPage)
			if pageEnd > len(v.FilteredItems) {
				pageEnd = len(v.FilteredItems)
			}
			pageEnd--
			if v.Selected < pageEnd {
				v.Selected++
			}
		}
	case "j":
		if v.ResultsPage > 0 {
			v.ResultsPage--
			v.Selected = v.ResultsPage * v.ResultsPerPage
		}
	case "k":
		totalPages := (len(v.FilteredItems) + v.ResultsPerPage - 1) / v.ResultsPerPage
		if v.ResultsPage+1 < totalPages {
			v.ResultsPage++
			v.Selected = v.ResultsPage * v.ResultsPerPage
		}
	case "pgdown":
		totalPages := (len(v.FilteredItems) + v.ResultsPerPage - 1) / v.ResultsPerPage
		if v.ResultsPage+1 < totalPages {
			v.ResultsPage++
			v.Selected = v.ResultsPage * v.ResultsPerPage
		}
	case "pgup":
		if v.ResultsPage > 0 {
			v.ResultsPage--
			v.Selected = v.ResultsPage * v.ResultsPerPage
		}
	case "left":
		if v.HScrollOffset > 0 {
			v.HScrollOffset--
		}
	case "right":
		// Allow scrolling right only if there are more attr columns to reveal.
		if v.scrollColCount > 0 && v.HScrollOffset+1 < v.scrollColCount {
			v.HScrollOffset++
		}
	case "esc":
		return PricingEventQuit
	}
	return PricingEventNone
}

func (v *PricingView) toggleSuggestion(idx int) {
	if idx >= len(v.SuggestionsCache) {
		return
	}
	value := v.SuggestionsCache[idx].Value
	if value == "" {
		return
	}

	// Attrs phase 1: key name without '=' → transition to value selection
	if v.CommandBuilder.SelectedField == 2 && !strings.Contains(value, "=") {
		v.CommandBuilder.SearchInput = value + "="
		v.UpdateSuggestions()
		return
	}
	// Price field: set input directly
	if v.CommandBuilder.SelectedField == 3 {
		v.CommandBuilder.SearchInput = value
		v.UpdateSuggestions()
		v.SuggestionIndex = -1
		return
	}

	tags := v.CommandBuilder.CurrentTags()
	found := -1
	for i, t := range tags {
		if t == value {
			found = i
			break
		}
	}
	if found >= 0 {
		tags = append(tags[:found], tags[found+1:]...)
	} else {
		tags = append(tags, value)
	}
	v.CommandBuilder.SetCurrentTags(tags)
	v.CommandBuilder.SearchInput = ""
	v.UpdateSuggestions()
	// Keep cursor on same item
	for i, s := range v.SuggestionsCache {
		if s.Value == value {
			v.SuggestionIndex = i
			return
		}
	}
	if v.SuggestionIndex >= len(v.SuggestionsCache) && len(v.SuggestionsCache) > 0 {
		v.SuggestionIndex = len(v.SuggestionsCache) - 1
	}
	v.FilterItems()
}

// UpdateSuggestions rebuilds the suggestion cache.
func (v *PricingView) UpdateSuggestions() {
	if v.Options == nil {
		v.SuggestionsCache = nil
		return
	}
	opts := v.Options
	q := v.CommandBuilder.SearchInput

	switch v.CommandBuilder.SelectedField {
	case 0:
		v.SuggestionsCache = semantic.ScoreAndSuggestProducts(
			q, opts.Products, opts.AttributeValues, opts.ProductGroups, v.CommandBuilder.ProductTags,
		)
	case 1:
		v.SuggestionsCache = semantic.SuggestRegions(
			q, opts.Regions, opts.ProductRegions, v.CommandBuilder.ProductTags, v.CommandBuilder.RegionTags,
		)
	case 2:
		v.SuggestionsCache = semantic.SuggestAttrs(
			q, v.CommandBuilder.ProductTags, opts.ProductAttrs, opts.AttributeValues, v.CommandBuilder.AttributeTags,
		)
	default:
		ops := []string{">", "<", ">=", "<="}
		var items []semantic.SuggestionItem
		for _, op := range ops {
			if q == "" || strings.HasPrefix(op, q) {
				items = append(items, semantic.SuggestionItem{
					Value:   op,
					Display: fmt.Sprintf("%s (price operator)", op),
					Reason:  "operator",
				})
			}
		}
		v.SuggestionsCache = items
	}
}

// FilterItems filters items based on current builder tags.
func (v *PricingView) FilterItems() {
	rTags := lowered(v.CommandBuilder.RegionTags)
	pTags := lowered(v.CommandBuilder.ProductTags)
	aTags := lowered(v.CommandBuilder.AttributeTags)
	priceTags := lowered(v.CommandBuilder.PriceTags)

	if len(rTags) == 0 && len(pTags) == 0 && len(aTags) == 0 && len(priceTags) == 0 {
		v.FilteredItems = append([]PricingDisplayItem{}, v.Items...)
		v.Selected = 0
		return
	}

	var filtered []PricingDisplayItem
	for _, item := range v.Items {
		regionOK := len(rTags) == 0 || containsAny(strings.ToLower(item.Region), rTags)
		productOK := len(pTags) == 0 || containsAny(strings.ToLower(item.Product), pTags)
		attrsOK := attrMatch(item, aTags)
		priceOK := priceMatch(item, priceTags)
		if regionOK && productOK && attrsOK && priceOK {
			filtered = append(filtered, item)
		}
	}
	v.FilteredItems = filtered
	v.Selected = 0
}

func lowered(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = strings.ToLower(s)
	}
	return out
}

func containsAny(s string, tags []string) bool {
	for _, t := range tags {
		if strings.Contains(s, t) {
			return true
		}
	}
	return false
}

func attrMatch(item PricingDisplayItem, tags []string) bool {
	if len(tags) == 0 {
		return true
	}
	parts := make([]string, 0, len(item.Attrs))
	for k, v := range item.Attrs {
		val := ""
		if v != nil {
			val = *v
		}
		parts = append(parts, strings.ToLower(k)+"="+strings.ToLower(val))
	}
	flat := strings.Join(parts, " ")
	for _, t := range tags {
		if !strings.Contains(flat, t) {
			return false
		}
	}
	return true
}

func priceMatch(item PricingDisplayItem, priceTags []string) bool {
	if len(priceTags) == 0 {
		return true
	}
	var mpVal float64
	if item.MinPrice != "" && item.MinPrice != "-" {
		s := strings.TrimSpace(item.MinPrice)
		if strings.EqualFold(s, "na") || strings.EqualFold(s, "n/a") {
			mpVal = 0
		} else {
			v, _ := strconv.ParseFloat(s, 64)
			mpVal = v
		}
	}
	for _, pt := range priceTags {
		if strings.HasPrefix(pt, ">=") {
			if v, err := strconv.ParseFloat(pt[2:], 64); err == nil && !(mpVal >= v) {
				return false
			}
		} else if strings.HasPrefix(pt, "<=") {
			if v, err := strconv.ParseFloat(pt[2:], 64); err == nil && !(mpVal <= v) {
				return false
			}
		} else if strings.HasPrefix(pt, ">") {
			if v, err := strconv.ParseFloat(pt[1:], 64); err == nil && !(mpVal > v) {
				return false
			}
		} else if strings.HasPrefix(pt, "<") {
			if v, err := strconv.ParseFloat(pt[1:], 64); err == nil && !(mpVal < v) {
				return false
			}
		}
	}
	return true
}

// ConvertResponse transforms an API response into display items.
func ConvertResponse(resp *api.PricingAPIResponse) []PricingDisplayItem {
	items := make([]PricingDisplayItem, 0, len(resp.Data))
	for _, item := range resp.Data {
		product := item.Product
		if item.Provider != "" {
			product = item.Provider + " " + item.Product
		}

		attrKeys := make([]string, 0, len(item.Attributes))
		for k := range item.Attributes {
			attrKeys = append(attrKeys, k)
		}
		attrs := make(map[string]*string, len(item.Attributes))
		for k, v := range item.Attributes {
			if v != nil {
				s := v.String()
				s = normalizeNA(s)
				attrs[k] = &s
			} else {
				attrs[k] = nil
			}
		}

		prices := make([]PriceInfo, 0, len(item.Prices))
		for _, p := range item.Prices {
			displayPrice := "N/A"
			var rateInfos []RateInfo
			if len(p.Rates) > 0 {
				if p.Rates[0].Price != nil {
					displayPrice = p.Rates[0].Price.String()
				}
				for _, r := range p.Rates {
					ri := RateInfo{}
					if r.Price != nil {
						ri.Price = r.Price.String()
					}
					if r.StartRange != nil {
						ri.StartRange = r.StartRange.String()
					}
					if r.EndRange != nil {
						ri.EndRange = r.EndRange.String()
					}
					rateInfos = append(rateInfos, ri)
				}
			}

			model := "OnDemand"
			if p.PricingModel != nil {
				model = *p.PricingModel
			}
			unit := ""
			if p.Unit != nil {
				unit = *p.Unit
			}
			upfront := ""
			if p.UpfrontFee != nil {
				upfront = p.UpfrontFee.String()
			}
			option := ""
			if p.PurchaseOption != nil {
				option = *p.PurchaseOption
			}
			year := ""
			if p.Year != nil {
				year = p.Year.String()
			}
			intMax := ""
			if p.InterruptionMaxPct != nil {
				intMax = p.InterruptionMaxPct.String()
			}

			prices = append(prices, PriceInfo{
				PricingModel: model, Price: displayPrice, Unit: unit,
				UpfrontFee: upfront, PurchaseOption: option, Year: year,
				InterruptionMaxPct: intMax, Rates: rateInfos,
			})
		}

		// Fill empty units from OnDemand
		var onDemandUnit string
		for _, p := range prices {
			if strings.EqualFold(p.PricingModel, "ondemand") && p.Unit != "" {
				onDemandUnit = p.Unit
				break
			}
		}
		if onDemandUnit != "" {
			for i := range prices {
				if prices[i].Unit == "" {
					prices[i].Unit = onDemandUnit
				}
			}
		}

		minPrice := ""
		if item.MinPrice != nil {
			minPrice = normalizePrice(item.MinPrice.String())
		}
		maxPrice := ""
		if item.MaxPrice != nil {
			maxPrice = normalizePrice(item.MaxPrice.String())
		}

		items = append(items, PricingDisplayItem{
			Product: product, Region: item.Region,
			Attrs: attrs, AttrKeys: attrKeys,
			Prices: prices, MinPrice: minPrice, MaxPrice: maxPrice,
		})
	}
	return items
}

func normalizeNA(s string) string {
	t := strings.TrimSpace(s)
	if strings.EqualFold(t, "na") || strings.EqualFold(t, "n/a") {
		return "-"
	}
	return s
}

func normalizePrice(s string) string {
	t := strings.TrimSpace(s)
	if strings.EqualFold(t, "na") || strings.EqualFold(t, "n/a") || t == "-" || t == "" {
		return "0.0"
	}
	return t
}

// ─── Rendering ────────────────────────────────────────────────────────────────

var (
	colorGreen    = lipgloss.Color("2")
	colorCyan     = lipgloss.Color("6")
	colorYellow   = lipgloss.Color("3")
	colorRed      = lipgloss.Color("1")
	colorDarkGray = lipgloss.Color("8")
	colorWhite    = lipgloss.Color("7")
	colorBlack    = lipgloss.Color("0")
	colorMagenta  = lipgloss.Color("5")
	colorGray     = lipgloss.Color("7")
)

func styleActive(active bool, text string) string {
	if active {
		return lipgloss.NewStyle().Foreground(colorCyan).Bold(true).Render(text)
	}
	return lipgloss.NewStyle().Foreground(colorDarkGray).Render(text)
}

func styleFocused(text string) string {
	return lipgloss.NewStyle().Foreground(colorBlack).Background(colorGreen).Bold(true).Render(text)
}

// Render returns the full string representation of the pricing view.
// It uses a two-pass strategy: render all fixed-height sections first, measure
// their total line count, then give the remaining terminal height to the
// Results section so the UI always fills the screen.
func (v *PricingView) Render(active bool, version string) string {
	showSuggestions := active && v.ActiveSection == PricingSectionCommand

	// Pass 1: render every section except Results.
	headerStr := v.renderHeader(active, version)
	commandStr := v.renderCommand(active)
	var suggestionsStr string
	if showSuggestions {
		suggestionsStr = v.renderSuggestions(active)
	}
	priceDetailsStr := v.renderPriceDetails()
	helpStr := v.renderHelp()

	// Count lines consumed by fixed sections. strings.Join with "\n" does NOT
	// add extra lines beyond the sum of each section's lineCount — the
	// separator merely connects the last line of one section to the first
	// line of the next, so total lines = Σ lineCount(section_i).
	fixedLines := lineCount(headerStr) + lineCount(commandStr) +
		lineCount(priceDetailsStr) + lineCount(helpStr)
	if showSuggestions {
		fixedLines += lineCount(suggestionsStr)
	}

	// Results box must be at least 3 lines tall (top border + 1 content + bottom).
	resultsMinHeight := v.Height - fixedLines
	if resultsMinHeight < 3 {
		resultsMinHeight = 3
	}

	// Derive ResultsPerPage from actual available inner height (subtract 2
	// border lines and 1 table-header row). Cap at 15 rows per page.
	v.ResultsPerPage = resultsMinHeight - 3
	if v.ResultsPerPage > 15 {
		v.ResultsPerPage = 15
	}
	if v.ResultsPerPage < 1 {
		v.ResultsPerPage = 1
	}

	// Keep ResultsPage valid and ensure the selected row stays visible.
	if len(v.FilteredItems) > 0 {
		selPage := v.Selected / v.ResultsPerPage
		if selPage != v.ResultsPage {
			v.ResultsPage = selPage
		}
		totalPages := (len(v.FilteredItems) + v.ResultsPerPage - 1) / v.ResultsPerPage
		if v.ResultsPage >= totalPages {
			v.ResultsPage = totalPages - 1
		}
	}

	// Pass 2: render results with the computed minimum height.
	resultsStr := v.renderResults(active, resultsMinHeight)

	sections := []string{headerStr, commandStr}
	if showSuggestions {
		sections = append(sections, suggestionsStr)
	}
	sections = append(sections, resultsStr, priceDetailsStr, helpStr)
	return strings.Join(sections, "\n")
}

func (v *PricingView) renderHeader(active bool, version string) string {
	isFocused := active && v.ActiveSection == PricingSectionHeader
	return renderNavHeader("Pricing", isFocused, active, version, v.Width)
}

func (v *PricingView) renderCommand(active bool) string {
	cmdActive := active && v.ActiveSection == PricingSectionCommand
	isFocused := cmdActive && v.BuilderFocus == BuilderFocusField
	borderColor := colorDarkGray
	if isFocused {
		borderColor = colorGreen
	} else if cmdActive {
		borderColor = colorCyan
	}

	fieldNames := []string{"products", "regions", "specs", "price"}
	var parts []string
	for i, name := range fieldNames {
		var tags []string
		switch i {
		case 0:
			tags = v.CommandBuilder.ProductTags
		case 1:
			tags = v.CommandBuilder.RegionTags
		case 2:
			tags = v.CommandBuilder.AttributeTags
		default:
			tags = v.CommandBuilder.PriceTags
		}
		isSel := isFocused && i == v.CommandBuilder.SelectedField

		var s string
		if isSel {
			s = lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render(name + " ")
		} else if len(tags) > 0 {
			s = lipgloss.NewStyle().Foreground(colorCyan).Render(name + " ")
		} else {
			s = lipgloss.NewStyle().Foreground(colorDarkGray).Render(name + " ")
		}
		for _, t := range tags {
			s += lipgloss.NewStyle().Foreground(colorGreen).Render("["+t+"]") + " "
		}
		if isSel {
			if v.CommandBuilder.SearchInput != "" {
				s += lipgloss.NewStyle().Foreground(colorWhite).Render(v.CommandBuilder.SearchInput)
			}
			s += lipgloss.NewStyle().Foreground(colorCyan).Render("▌")
		}
		if i < 3 {
			s += lipgloss.NewStyle().Foreground(colorDarkGray).Render("  │  ")
		}
		parts = append(parts, s)
	}

	rawTitle := "Pricing Query"
	if cmdActive {
		rawTitle = "> Pricing Query"
	}
	styledTitle := lipgloss.NewStyle().Foreground(borderColor).Render(rawTitle)
	return boxWithTitle(styledTitle, strings.Join(parts, ""), borderColor, v.Width)
}

func (v *PricingView) suggestionCols() int {
	if v.Width < 10 {
		return 1
	}
	innerW := v.Width - 4
	if len(v.SuggestionsCache) == 0 {
		return 1
	}
	maxDisplay := 10
	maxReason := 0
	for _, s := range v.SuggestionsCache {
		if len(s.Display) > maxDisplay {
			maxDisplay = len(s.Display)
		}
		if len(s.Reason) > maxReason {
			maxReason = len(s.Reason)
		}
	}
	cellW := 4 + maxDisplay + 2 + maxReason
	if cellW < 18 {
		cellW = 18
	}
	if cellW > 38 {
		cellW = 38
	}
	cols := innerW / cellW
	if cols < 1 {
		cols = 1
	}
	return cols
}

func (v *PricingView) renderSuggestions(active bool) string {
	cmdActive := active && v.ActiveSection == PricingSectionCommand
	suggFocused := cmdActive && v.BuilderFocus == BuilderFocusSuggestions

	borderColor := colorDarkGray
	if suggFocused {
		borderColor = colorGreen
	} else if cmdActive {
		borderColor = colorCyan
	}

	// Build border title: highlighted field tabs + count + hint
	fieldNames := []string{"products", "regions", "specs", "price"}
	var tabParts []string
	for i, name := range fieldNames {
		if i == v.CommandBuilder.SelectedField && cmdActive {
			tabParts = append(tabParts, lipgloss.NewStyle().
				Foreground(colorBlack).Background(colorGreen).Bold(true).
				Render(" "+name+" "))
		} else {
			tabParts = append(tabParts, lipgloss.NewStyle().Foreground(colorDarkGray).Render(name))
		}
	}
	sep := lipgloss.NewStyle().Foreground(colorDarkGray).Render(" | ")
	tabsStr := strings.Join(tabParts, sep)
	hintStr := lipgloss.NewStyle().Foreground(colorDarkGray).
		Render(fmt.Sprintf("  (%d) [Tab Complete · ↓ Browse]", len(v.SuggestionsCache)))
	styledTitle := tabsStr + hintStr

	if len(v.SuggestionsCache) == 0 {
		msg := "(No matches)"
		if v.Options == nil {
			msg = "(Sync metadata to see suggestions)"
		}
		return boxWithTitle(styledTitle,
			lipgloss.NewStyle().Foreground(colorDarkGray).Italic(true).Render(msg),
			borderColor, v.Width)
	}

	cols := v.suggestionCols()
	innerH := 4 // show ~4 rows max

	// Compute total rows in the suggestion grid.
	totalRows := (len(v.SuggestionsCache) + cols - 1) / cols

	// Ensure the selected item's row is visible by adjusting SuggestionScroll.
	if v.SuggestionIndex >= 0 {
		selRow := v.SuggestionIndex / cols
		if selRow < v.SuggestionScroll {
			v.SuggestionScroll = selRow
		}
		if selRow >= v.SuggestionScroll+innerH {
			v.SuggestionScroll = selRow - innerH + 1
		}
	}
	if v.SuggestionScroll > totalRows-innerH {
		v.SuggestionScroll = totalRows - innerH
	}
	if v.SuggestionScroll < 0 {
		v.SuggestionScroll = 0
	}

	var lines []string
	for row := v.SuggestionScroll; row < v.SuggestionScroll+innerH && row < totalRows; row++ {
		var rowParts []string
		for col := 0; col < cols; col++ {
			i := row*cols + col
			if i >= len(v.SuggestionsCache) {
				break
			}
			item := v.SuggestionsCache[i]
			isSel := v.SuggestionIndex == i

			check := " "
			var itemStyle lipgloss.Style
			if isSel && item.AlreadySelected {
				check = "✓"
				itemStyle = lipgloss.NewStyle().Foreground(colorBlack).Background(colorGreen).Bold(true)
			} else if isSel {
				check = ">"
				itemStyle = lipgloss.NewStyle().Foreground(colorBlack).Background(colorCyan).Bold(true)
			} else if item.AlreadySelected {
				check = "✓"
				itemStyle = lipgloss.NewStyle().Foreground(colorGreen)
			} else if item.IsSemantic {
				itemStyle = lipgloss.NewStyle().Foreground(colorYellow)
			} else {
				itemStyle = lipgloss.NewStyle().Foreground(colorGray)
			}

			displayStr := fmt.Sprintf("%s %s", check, item.Display)
			if item.Reason != "" {
				displayStr += lipgloss.NewStyle().Foreground(colorDarkGray).Render(" " + item.Reason)
			}
			rowParts = append(rowParts, itemStyle.Render(displayStr))
		}
		if len(rowParts) > 0 {
			lines = append(lines, strings.Join(rowParts, "  "))
		}
	}

	return boxWithTitle(styledTitle, strings.Join(lines, "\n"), borderColor, v.Width)
}

func countByProvider(items []PricingDisplayItem) map[string]int {
	counts := make(map[string]int)
	for _, item := range items {
		parts := strings.SplitN(item.Product, " ", 2)
		if len(parts) > 0 && parts[0] != "" {
			counts[strings.ToLower(parts[0])]++
		}
	}
	return counts
}

func (v *PricingView) renderResults(active bool, minHeight int) string {
	isFocused := active && v.ActiveSection == PricingSectionResults
	borderColor := colorDarkGray
	if isFocused {
		borderColor = colorGreen
	}

	box := func(title, content string, color lipgloss.Color) string {
		return boxWithTitleMinHeight(title, content, color, v.Width, minHeight)
	}

	if v.Loading {
		loadTitle := lipgloss.NewStyle().Foreground(colorYellow).Render("Results")
		return box(loadTitle, lipgloss.NewStyle().Foreground(colorYellow).Render("Loading pricing data..."), borderColor)
	}

	if v.ErrorMessage != "" {
		errTitle := lipgloss.NewStyle().Foreground(colorRed).Render("Results")
		return box(errTitle, lipgloss.NewStyle().Foreground(colorRed).Render("Error: "+v.ErrorMessage), colorRed)
	}

	if len(v.FilteredItems) == 0 {
		emptyTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Results (0)")
		return box(emptyTitle, lipgloss.NewStyle().Foreground(colorDarkGray).Render("No results found. Press Enter to submit query."), borderColor)
	}

	totalPages := (len(v.FilteredItems) + v.ResultsPerPage - 1) / v.ResultsPerPage
	pageStart := v.ResultsPage * v.ResultsPerPage
	pageEnd := pageStart + v.ResultsPerPage
	if pageEnd > len(v.FilteredItems) {
		pageEnd = len(v.FilteredItems)
	}

	// Collect attr keys from visible items
	attrKeySeen := map[string]struct{}{}
	var attrKeys []string
	for _, item := range v.FilteredItems {
		for _, k := range item.AttrKeys {
			if _, exists := attrKeySeen[k]; !exists {
				attrKeySeen[k] = struct{}{}
				attrKeys = append(attrKeys, k)
			}
		}
	}

	// Build table header
	colWidths := v.computeColumnWidths(attrKeys)
	header := v.buildResultRow(-1, attrKeys, colWidths, false)
	headerLine := lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render(header)

	var rows []string
	rows = append(rows, headerLine)

	for idx := pageStart; idx < pageEnd; idx++ {
		isSel := idx == v.Selected
		row := v.buildResultRow(idx, attrKeys, colWidths, isSel)
		if isSel {
			rows = append(rows, lipgloss.NewStyle().Background(colorGreen).Foreground(colorBlack).Bold(true).Render(row))
		} else {
			rows = append(rows, row)
		}
	}

	// Build border title with provider counts and column indicator
	prefix := ""
	if isFocused {
		prefix = "> "
	}
	pagePart := fmt.Sprintf("%sResults (%d total, page %d/%d)", prefix, len(v.FilteredItems), v.ResultsPage+1, max(1, totalPages))

	provCounts := countByProvider(v.FilteredItems)
	provKeys := make([]string, 0, len(provCounts))
	for k := range provCounts {
		provKeys = append(provKeys, k)
	}
	sort.Strings(provKeys)
	var provParts []string
	for _, k := range provKeys {
		provParts = append(provParts, fmt.Sprintf("%s: %d", k, provCounts[k]))
	}
	provPart := strings.Join(provParts, "  ")

	// Store scrollable count so handleKeyResults can clamp the right boundary.
	v.scrollColCount = len(attrKeys)

	var colPart string
	if len(attrKeys) == 0 {
		colPart = "no attrs"
	} else {
		colPart = fmt.Sprintf("attr %d/%d [← →]", v.HScrollOffset+1, len(attrKeys))
	}

	rawTitle := pagePart
	if provPart != "" {
		rawTitle += " | " + provPart
	}
	rawTitle += " | " + colPart
	styledTitle := lipgloss.NewStyle().Foreground(borderColor).Render(rawTitle)

	return box(styledTitle, strings.Join(rows, "\n"), borderColor)
}

func (v *PricingView) computeColumnWidths(attrKeys []string) map[string]int {
	w := map[string]int{
		"no":       4,
		"product":  7,
		"region":   6,
		"minprice": 9,
		"maxprice": 9,
	}
	for _, item := range v.FilteredItems {
		if len(item.Product) > w["product"] {
			w["product"] = len(item.Product)
		}
		if len(item.Region) > w["region"] {
			w["region"] = len(item.Region)
		}
		if len(item.MinPrice) > w["minprice"] {
			w["minprice"] = len(item.MinPrice)
		}
		if len(item.MaxPrice) > w["maxprice"] {
			w["maxprice"] = len(item.MaxPrice)
		}
	}

	for _, k := range attrKeys {
		w[k] = len(k)
		for _, item := range v.FilteredItems {
			if val, ok := item.Attrs[k]; ok && val != nil {
				if len(*val) > w[k] {
					w[k] = len(*val)
				}
			}
		}
		if w[k] < 6 {
			w[k] = 6
		}
	}
	return w
}

// buildResultRow renders one table row.
//
// Layout: [No.] [Product] [Region] ... scrollable attrs ... [Min Price] [Max Price]
//
// The "no / product / region" and price columns are always pinned.
// attrKeys are the scrollable middle section; HScrollOffset determines which
// attr column is shown first. Only as many attrs as fit in the remaining inner
// width are rendered, so the row is guaranteed never to exceed v.Width-2 chars.
func (v *PricingView) buildResultRow(idx int, attrKeys []string, colWidths map[string]int, selected bool) string {
	_ = selected
	pad := func(s string, n int) string {
		if len(s) >= n {
			return s
		}
		return s + strings.Repeat(" ", n-len(s))
	}

	// Width consumed by the five always-visible columns (each separated by 1 space).
	// no + sp + product + sp + region + sp + minprice + sp + maxprice
	pinnedWidth := colWidths["no"] + 1 +
		colWidths["product"] + 1 +
		colWidths["region"] + 1 +
		colWidths["minprice"] + 1 +
		colWidths["maxprice"]

	// Available inner width for the scrollable attr section.
	innerWidth := v.Width - 2 // subtract box left/right borders
	attrBudget := innerWidth - pinnedWidth

	// Determine which attr columns are visible starting at HScrollOffset.
	// Try to fill the budget completely — if starting at HScrollOffset leaves
	// empty space on the right, pull the offset back so columns stay full.
	offset := v.HScrollOffset
	if offset >= len(attrKeys) {
		offset = max(0, len(attrKeys)-1)
	}

	// First, find the maximum offset that still fills the budget.
	// Walk backwards from the end to find how many columns fit.
	if len(attrKeys) > 0 {
		maxOffset := len(attrKeys) - 1
		used := 0
		for i := len(attrKeys) - 1; i >= 0; i-- {
			needed := colWidths[attrKeys[i]] + 1
			if used+needed > attrBudget {
				break
			}
			used += needed
			maxOffset = i
		}
		if offset > maxOffset {
			offset = maxOffset
		}
	}

	var visibleAttrs []string
	used := 0
	for i := offset; i < len(attrKeys); i++ {
		needed := colWidths[attrKeys[i]] + 1 // +1 for leading separator
		if used+needed > attrBudget && len(visibleAttrs) > 0 {
			break
		}
		visibleAttrs = append(visibleAttrs, attrKeys[i])
		used += needed
	}

	// Build the row for header (idx < 0) or a data row.
	var cols []string
	if idx < 0 {
		cols = []string{
			pad("No.", colWidths["no"]),
			pad("Product", colWidths["product"]),
			pad("Region", colWidths["region"]),
		}
		for _, k := range visibleAttrs {
			cols = append(cols, pad(k, colWidths[k]))
		}
		cols = append(cols, pad("Min Price", colWidths["minprice"]))
		cols = append(cols, pad("Max Price", colWidths["maxprice"]))
	} else {
		item := v.FilteredItems[idx]
		cols = []string{
			pad(fmt.Sprintf("%d", idx+1), colWidths["no"]),
			pad(item.Product, colWidths["product"]),
			pad(item.Region, colWidths["region"]),
		}
		for _, k := range visibleAttrs {
			val := "-"
			if attrVal, ok := item.Attrs[k]; ok && attrVal != nil {
				val = *attrVal
			}
			cols = append(cols, pad(val, colWidths[k]))
		}
		cols = append(cols, pad(item.MinPrice, colWidths["minprice"]))
		cols = append(cols, pad(item.MaxPrice, colWidths["maxprice"]))
	}

	row := strings.Join(cols, " ")
	// Pad the row to fill the full inner width so highlights span the entire line.
	if len(row) < innerWidth {
		row += strings.Repeat(" ", innerWidth-len(row))
	}
	return row
}

func (v *PricingView) renderPriceTable(prices []PriceInfo) string {
	// padV pads s to exactly n visual columns using lipgloss.Width so that
	// multi-byte characters like "–" are measured correctly.
	padV := func(s string, n int) string {
		w := lipgloss.Width(s)
		if w >= n {
			return truncate(s, n)
		}
		return s + strings.Repeat(" ", n-w)
	}

	header := lipgloss.NewStyle().Foreground(colorYellow).Bold(true).Render(
		padV("Model", 24) + "  " + padV("Price", 10) + "  " + padV("Unit", 5) + "  " +
			padV("Upfront", 10) + "  " + padV("Yr", 4) + "  " + "Option")

	rows := []string{header}
	for _, p := range prices {
		upfront := p.UpfrontFee
		if upfront == "0" || upfront == "" {
			upfront = "–"
		}
		yr := p.Year
		if yr == "" {
			yr = "–"
		}
		opt := p.PurchaseOption
		if opt == "" {
			opt = "–"
		}
		if p.InterruptionMaxPct != "" {
			if opt == "–" {
				opt = fmt.Sprintf("Interrupt %s%%", p.InterruptionMaxPct)
			} else {
				opt = fmt.Sprintf("%s Interrupt %s%%", opt, p.InterruptionMaxPct)
			}
		}

		isSpot := strings.EqualFold(p.PricingModel, "spot")
		priceColor := colorGreen
		if isSpot {
			priceColor = colorRed
		}

		row := padV(p.PricingModel, 24) + "  " +
			lipgloss.NewStyle().Foreground(priceColor).Render(padV(p.Price, 10)) + "  " +
			padV(p.Unit, 5) + "  " +
			padV(upfront, 10) + "  " +
			padV(yr, 4) + "  " +
			opt

		if isSpot {
			row = lipgloss.NewStyle().Foreground(colorRed).Render(padV(p.PricingModel, 24)) + "  " +
				lipgloss.NewStyle().Foreground(priceColor).Render(padV(p.Price, 10)) + "  " +
				padV(p.Unit, 5) + "  " +
				padV(upfront, 10) + "  " +
				padV(yr, 4) + "  " +
				lipgloss.NewStyle().Foreground(colorRed).Render(opt)
		}
		rows = append(rows, row)
	}
	return strings.Join(rows, "\n")
}

func (v *PricingView) renderPriceDetails() string {
	emptyTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Price Details")
	if v.Selected >= len(v.FilteredItems) {
		return boxWithTitle(emptyTitle,
			lipgloss.NewStyle().Foreground(colorDarkGray).Italic(true).Render("Select a result to see pricing details"),
			colorDarkGray, v.Width)
	}
	item := v.FilteredItems[v.Selected]
	if len(item.Prices) == 0 {
		return boxWithTitle(emptyTitle,
			lipgloss.NewStyle().Foreground(colorDarkGray).Render("No price details available"),
			colorDarkGray, v.Width)
	}

	titleText := fmt.Sprintf("> Price Details – %s / %s (%d models)", item.Region, item.Product, len(item.Prices))
	styledTitle := lipgloss.NewStyle().Foreground(colorCyan).Render(titleText)

	if len(item.Prices) <= pricingSplitThreshold {
		return boxWithTitle(styledTitle, v.renderPriceTable(item.Prices), colorCyan, v.Width)
	}

	mid := (len(item.Prices) + 1) / 2
	leftContent := v.renderPriceTable(item.Prices[:mid])
	rightContent := v.renderPriceTable(item.Prices[mid:])

	halfW := (v.Width - 4) / 2
	contTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("(cont.)")
	leftPanel := boxWithTitle(styledTitle, leftContent, colorCyan, halfW+2)
	rightPanel := boxWithTitle(contTitle, rightContent, colorDarkGray, v.Width-halfW-2)

	return lipgloss.JoinHorizontal(lipgloss.Top, leftPanel, rightPanel)
}

func (v *PricingView) renderHelp() string {
	var text string
	switch v.ActiveSection {
	case PricingSectionHeader:
		text = styleHelpKey("[↔]") + " Switch View  " + styleHelpKey("[↓]") + " Command  " + styleHelpKey("[F3]") + " Refresh  " + styleHelpKey("[Esc]") + " Quit"
	case PricingSectionCommand:
		if v.BuilderFocus == BuilderFocusField {
			text = styleHelpKey("[↔]") + " Switch Field  " + styleHelpKey("[Tab]") + " Complete  " + styleHelpKey("[Enter]") + " Submit  " + styleHelpKey("[F2]") + " Reset  " + styleHelpKey("[F3]") + " Refresh"
		} else {
			text = styleHelpKey("[↑↓↔]") + " Navigate  " + styleHelpKey("[Space]") + " Select  " + styleHelpKey("[Enter]") + " Submit"
		}
	case PricingSectionResults:
		text = styleHelpKey("[j/k]") + " Scroll  " + styleHelpKey("[↔]") + " H-Scroll  " + styleHelpKey("[↑]") + " Command  " + styleHelpKey("[Esc]") + " Quit"
	}
	helpTitle := lipgloss.NewStyle().Foreground(colorDarkGray).Render("Help")
	return boxWithTitle(helpTitle, text, colorDarkGray, v.Width)
}

func styleHelpKey(k string) string {
	return lipgloss.NewStyle().Foreground(colorCyan).Render(k)
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
