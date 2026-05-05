//go:build tui

package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/OverloadBlitz/cloudcent-cli/internal/api"
	"github.com/OverloadBlitz/cloudcent-cli/internal/config"
	"github.com/OverloadBlitz/cloudcent-cli/internal/db"
	"github.com/OverloadBlitz/cloudcent-cli/internal/tui/views"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/pkg/browser"
)

// ViewMode tracks which view is active.
type ViewMode int

const (
	ViewModeInitAuth ViewMode = iota
	ViewModePricing
	ViewModeHistory
	ViewModeSettings
)

// AuthState tracks the auth onboarding flow.
type AuthState int

const (
	AuthStatePrompt  AuthState = iota
	AuthStateWaiting           // browser opened, polling
	AuthStateLoading           // downloading metadata
	AuthStateError
)

// ── Messages ─────────────────────────────────────────────────────────────────

type authResultMsg struct {
	cliID  string
	apiKey string
	err    error
}

type metadataReadyMsg struct{ err error }

type pricingResultMsg struct {
	resp *api.PricingAPIResponse
	err  error
}

type tickMsg struct{}

// ── App model ─────────────────────────────────────────────────────────────────

type App struct {
	version string
	width   int
	height  int

	viewMode  ViewMode
	authState AuthState
	errorMsg  string

	client *api.Client
	db     *db.DB

	pricingView  *views.PricingView
	historyView  *views.HistoryView
	settingsView *views.SettingsView

	// pending pricing cache params for saving to DB
	pendingCacheProducts []string
	pendingCacheRegions  []string
	pendingCacheAttrs    map[string]string
	pendingCachePrices   []string

	loadingFrame       int
	metadataRefreshMsg string
	refreshMsgFrames   int

	// token info for auth polling
	exchangeCode string
}

func NewApp(version string) (*App, error) {
	client, err := api.New()
	if err != nil {
		return nil, err
	}

	database, _ := db.New() // non-fatal

	viewMode := ViewModeInitAuth
	if client.IsInitialized() {
		viewMode = ViewModePricing
	}

	// Start with sensible defaults; the real size arrives via WindowSizeMsg
	// on the first frame and updates every view immediately.
	app := &App{
		version:      version,
		client:       client,
		db:           database,
		viewMode:     viewMode,
		authState:    AuthStatePrompt,
		pricingView:  views.NewPricingView(0, 0),
		historyView:  views.NewHistoryView(0, 0),
		settingsView: views.NewSettingsView(0, 0),
	}

	if viewMode == ViewModePricing {
		app.loadMetadataIntoView()
	}

	return app, nil
}

func (a *App) loadMetadataIntoView() {
	meta, err := api.LoadMetadataFromFile()
	if err != nil {
		return
	}
	opts := views.ProcessMetadata(meta)
	a.pricingView.Options = opts
	a.pricingView.UpdateSuggestions()
}

// Init implements tea.Model.
func (a App) Init() tea.Cmd {
	return tickCmd()
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

// Update implements tea.Model.
func (a App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.pricingView.Width = msg.Width
		a.pricingView.Height = msg.Height
		a.historyView.Width = msg.Width
		a.historyView.Height = msg.Height
		a.settingsView.Width = msg.Width
		a.settingsView.Height = msg.Height

	case tickMsg:
		a.loadingFrame = (a.loadingFrame + 1) % 8
		if a.refreshMsgFrames > 0 {
			a.refreshMsgFrames--
			if a.refreshMsgFrames == 0 {
				a.metadataRefreshMsg = ""
			}
		}
		return a, tickCmd()

	case tea.KeyMsg:
		return a.handleKey(msg)

	case authResultMsg:
		if msg.err != nil {
			a.errorMsg = msg.err.Error()
			a.authState = AuthStateError
			return a, nil
		}
		cfg := &config.Config{CliID: msg.cliID, APIKey: &msg.apiKey}
		if err := a.client.SaveConfig(cfg); err != nil {
			a.errorMsg = err.Error()
			a.authState = AuthStateError
			return a, nil
		}
		a.authState = AuthStateLoading
		a.loadingFrame = 0
		return a, downloadMetadataCmd(a.client)

	case metadataReadyMsg:
		if msg.err != nil {
			if a.viewMode == ViewModeInitAuth {
				a.errorMsg = msg.err.Error()
				a.authState = AuthStateError
			} else {
				a.metadataRefreshMsg = "Refresh Error: " + msg.err.Error()
				a.refreshMsgFrames = 120
				a.authState = AuthStatePrompt
			}
			return a, nil
		}
		a.loadMetadataIntoView()
		if a.viewMode == ViewModeInitAuth {
			a.viewMode = ViewModePricing
			a.authState = AuthStatePrompt
		} else {
			a.metadataRefreshMsg = "Refresh Succeeded"
			a.refreshMsgFrames = 40
			a.authState = AuthStatePrompt
		}

	case pricingResultMsg:
		a.pricingView.Loading = false
		if msg.err != nil {
			a.pricingView.ErrorMessage = msg.err.Error()
			return a, nil
		}
		a.pricingView.Items = views.ConvertResponse(msg.resp)
		a.pricingView.FilteredItems = append([]views.PricingDisplayItem{}, a.pricingView.Items...)
		a.pricingView.Selected = 0
		a.pricingView.HScrollOffset = 0
		a.pricingView.ResultsPage = 0
		a.pricingView.ErrorMessage = ""
		if len(a.pricingView.FilteredItems) > 0 {
			a.pricingView.ActiveSection = views.PricingSectionResults
		}

		// Save to cache and history
		if a.db != nil && len(a.pendingCacheProducts) > 0 {
			cacheKey := db.MakeCacheKey(nil, a.pendingCacheRegions, nil, a.pendingCacheProducts, a.pendingCacheAttrs, a.pendingCachePrices)
			a.db.SetCache(cacheKey, msg.resp)
			attrList := make([]string, 0, len(a.pendingCacheAttrs))
			for k, v := range a.pendingCacheAttrs {
				attrList = append(attrList, k+"="+v)
			}
			a.db.AddHistory(nil, a.pendingCacheRegions, nil, a.pendingCacheProducts, attrList, a.pendingCachePrices,
				int64(len(a.pricingView.Items)), cacheKey)
			a.historyView.History, _ = a.db.GetHistory(100)
		}
	}
	return a, nil
}

func (a App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	if a.viewMode == ViewModeInitAuth {
		return a.handleInitAuthKey(key)
	}

	// F3: refresh metadata from any view
	if key == "f3" {
		if a.authState != AuthStateLoading {
			a.authState = AuthStateLoading
			a.loadingFrame = 0
			a.metadataRefreshMsg = "Refreshing metadata..."
			a.refreshMsgFrames = 60
			return a, downloadMetadataCmd(a.client)
		}
		return a, nil
	}

	switch a.viewMode {
	case ViewModePricing:
		event := a.pricingView.HandleKey(key)
		return a.handlePricingEvent(event)
	case ViewModeHistory:
		event, idx := a.historyView.HandleKey(key)
		return a.handleHistoryEvent(event, idx)
	case ViewModeSettings:
		event := a.settingsView.HandleKey(key)
		return a.handleSettingsEvent(event)
	}
	return a, nil
}

func (a App) handleInitAuthKey(key string) (tea.Model, tea.Cmd) {
	switch a.authState {
	case AuthStatePrompt:
		switch key {
		case "enter":
			a.authState = AuthStateWaiting
			return a, startAuthCmd(a.client)
		case "esc", "ctrl+c", "q":
			return a, tea.Quit
		}
	case AuthStateWaiting:
		switch key {
		case "esc", "ctrl+c":
			return a, tea.Quit
		case "r", "R":
			a.authState = AuthStateWaiting
			return a, startAuthCmd(a.client)
		}
	case AuthStateLoading:
		if key == "esc" || key == "ctrl+c" {
			return a, tea.Quit
		}
	case AuthStateError:
		switch key {
		case "enter":
			a.authState = AuthStatePrompt
			a.errorMsg = ""
		case "esc", "ctrl+c":
			return a, tea.Quit
		}
	}
	return a, nil
}

func (a App) handlePricingEvent(event views.PricingEvent) (tea.Model, tea.Cmd) {
	switch event {
	case views.PricingEventQuit:
		return a, tea.Quit
	case views.PricingEventNextView:
		a.switchViewNext()
	case views.PricingEventPrevView:
		a.switchViewPrev()
	case views.PricingEventSubmitQuery:
		return a.submitPricingQuery()
	}
	return a, nil
}

func (a App) handleHistoryEvent(event views.HistoryEvent, idx int) (tea.Model, tea.Cmd) {
	switch event {
	case views.HistoryEventQuit:
		return a, tea.Quit
	case views.HistoryEventNextView:
		a.switchViewNext()
	case views.HistoryEventPrevView:
		a.switchViewPrev()
	case views.HistoryEventClearAll:
		if a.db != nil {
			a.db.ClearAll()
			a.historyView.History, _ = a.db.GetHistory(100)
			a.historyView.SelectedResults = nil
		}
	case views.HistoryEventOpenInPricing:
		if idx < len(a.historyView.History) {
			h := a.historyView.History[idx]
			a.pricingView.CommandBuilder.ProductTags = splitNonEmpty(h.ProductFamilies, ",")
			a.pricingView.CommandBuilder.RegionTags = splitNonEmpty(h.Regions, ",")
			a.pricingView.CommandBuilder.AttributeTags = splitNonEmpty(h.Attributes, ",")
			a.pricingView.CommandBuilder.PriceTags = splitNonEmpty(h.Prices, ",")
			a.viewMode = ViewModePricing
			a.pricingView.ActiveSection = views.PricingSectionCommand

			// Try cache first
			if a.db != nil {
				if cached, _ := a.db.GetCache(h.CacheKey); cached != nil {
					a.pricingView.Items = views.ConvertResponse(cached)
					a.pricingView.FilteredItems = append([]views.PricingDisplayItem{}, a.pricingView.Items...)
					a.pricingView.Selected = 0
					a.pricingView.Loading = false
					if len(a.pricingView.FilteredItems) > 0 {
						a.pricingView.ActiveSection = views.PricingSectionResults
					}
					return a, nil
				}
			}
			return a.submitPricingQuery()
		}
	case views.HistoryEventNone:
		// Update preview for selected history item
		if a.db != nil && idx < len(a.historyView.History) {
			h := a.historyView.History[a.historyView.Selected]
			if cached, _ := a.db.GetCache(h.CacheKey); cached != nil {
				a.historyView.SelectedResults = views.ConvertResponse(cached)
			} else {
				a.historyView.SelectedResults = nil
			}
		}
	}
	return a, nil
}

func (a App) handleSettingsEvent(event views.SettingsEvent) (tea.Model, tea.Cmd) {
	switch event {
	case views.SettingsEventQuit:
		return a, tea.Quit
	case views.SettingsEventNextView:
		a.switchViewNext()
	case views.SettingsEventPrevView:
		a.switchViewPrev()
	}
	return a, nil
}

func (a *App) switchViewNext() {
	switch a.viewMode {
	case ViewModePricing:
		a.viewMode = ViewModeHistory
		a.historyView.ActiveSection = views.HistorySectionHeader
		if a.db != nil {
			a.historyView.History, _ = a.db.GetHistory(100)
		}
	case ViewModeHistory:
		a.viewMode = ViewModeSettings
		a.settingsView.ActiveSection = views.SettingsSectionHeader
	case ViewModeSettings:
		a.viewMode = ViewModePricing
		a.pricingView.ActiveSection = views.PricingSectionHeader
	}
}

func (a *App) switchViewPrev() {
	switch a.viewMode {
	case ViewModePricing:
		a.viewMode = ViewModeSettings
		a.settingsView.ActiveSection = views.SettingsSectionHeader
	case ViewModeHistory:
		a.viewMode = ViewModePricing
		a.pricingView.ActiveSection = views.PricingSectionHeader
	case ViewModeSettings:
		a.viewMode = ViewModeHistory
		a.historyView.ActiveSection = views.HistorySectionHeader
		if a.db != nil {
			a.historyView.History, _ = a.db.GetHistory(100)
		}
	}
}

func (a App) submitPricingQuery() (tea.Model, tea.Cmd) {
	if a.pricingView.Loading {
		return a, nil
	}
	b := &a.pricingView.CommandBuilder
	products := append([]string{}, b.ProductTags...)
	regions := append([]string{}, b.RegionTags...)
	prices := append([]string{}, b.PriceTags...)
	attrsMap := map[string]string{}
	for _, tag := range b.AttributeTags {
		if k, v, ok := strings.Cut(tag, "="); ok {
			attrsMap[k] = v
		}
	}

	a.pendingCacheProducts = products
	a.pendingCacheRegions = regions
	a.pendingCacheAttrs = attrsMap
	a.pendingCachePrices = prices
	a.pricingView.Loading = true
	a.pricingView.ErrorMessage = ""

	return a, fetchPricingCmd(a.client, products, regions, attrsMap, prices)
}

// View implements tea.Model.
func (a App) View() string {
	if a.width == 0 {
		return "Loading..."
	}

	// Reserve 1 line for the status bar when a refresh message is active so
	// that sub-views don't render into space that will be taken by the status.
	statusLines := 0
	if a.metadataRefreshMsg != "" {
		statusLines = 1
	}

	// Temporarily adjust the height passed to sub-views so they leave room
	// for the status line.
	effectiveHeight := a.height - statusLines

	var content string
	switch a.viewMode {
	case ViewModeInitAuth:
		content = a.renderInitAuth()
	case ViewModePricing:
		a.pricingView.Height = effectiveHeight
		content = a.pricingView.Render(true, a.version)
	case ViewModeHistory:
		a.historyView.Height = effectiveHeight
		cacheCount, cacheBytes := 0, 0
		cachePath, _ := config.DBPath()
		if a.db != nil {
			cacheCount, cacheBytes, _ = a.db.GetCacheStats()
		}
		content = a.historyView.Render(true, cacheCount, cacheBytes, cachePath, a.version)
	case ViewModeSettings:
		a.settingsView.Height = effectiveHeight
		configPath, _ := config.Path()
		content = a.settingsView.Render(true, a.client.Config, configPath, a.version)
	}

	if a.metadataRefreshMsg != "" {
		color := lipgloss.Color("6")
		if strings.Contains(a.metadataRefreshMsg, "Error") {
			color = lipgloss.Color("1")
		} else if strings.Contains(a.metadataRefreshMsg, "Succeeded") {
			color = lipgloss.Color("2")
		}
		status := lipgloss.NewStyle().Foreground(color).Bold(true).Render(" ● " + a.metadataRefreshMsg)
		content += "\n" + status
	}

	// Ensure the output fills the entire terminal so there is no blank space.
	return lipgloss.Place(a.width, a.height, lipgloss.Left, lipgloss.Top, content)
}

func (a App) renderInitAuth() string {
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"}
	spinner := spinners[a.loadingFrame%len(spinners)]

	var content string
	switch a.authState {
	case AuthStatePrompt:
		content = lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Render(
			fmt.Sprintf("\nWelcome! CloudCent requires a free API Key.\n\nPress %s to authenticate in browser...\n(Or press %s to quit)",
				lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render("Enter"),
				lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Esc"),
			))
	case AuthStateWaiting:
		content = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Render(
			fmt.Sprintf("\n%s Waiting for browser authorization...\n\nA browser window should have opened.\nPlease complete the verification there.\n\nPress %s to cancel  %s to retry",
				spinner,
				lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Esc"),
				lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render("r"),
			))
	case AuthStateLoading:
		content = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true).Render(
			fmt.Sprintf("\n%s  Loading metadata...\n\nPlease wait while we download pricing data.",
				spinner))
	case AuthStateError:
		content = fmt.Sprintf("\n%s\n\n%s\n\nPress %s to retry or %s to quit",
			lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Bold(true).Render("[ERROR] Authentication Failed"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Render(a.errorMsg),
			lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Bold(true).Render("Enter"),
			lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render("Esc"),
		)
	}

	title := fmt.Sprintf(" CloudCent CLI v%s ", a.version)
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("6")).
		Align(lipgloss.Center).
		Width(60).
		Render(title + content)
	return lipgloss.Place(a.width, a.height, lipgloss.Center, lipgloss.Center, box)
}

// ── Commands (async) ──────────────────────────────────────────────────────────

func startAuthCmd(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		tokenResp, err := client.GenerateToken()
		if err != nil {
			return authResultMsg{err: err}
		}

		authURL := fmt.Sprintf("%s?token=%s&exchange=%s", api.CLIBaseURL, tokenResp.AccessToken, tokenResp.ExchangeCode)
		browser.OpenURL(authURL)

		const maxAttempts = 150
		for i := 0; i < maxAttempts; i++ {
			time.Sleep(2 * time.Second)
			resp, err := client.ExchangeToken(tokenResp.ExchangeCode)
			if err != nil {
				continue
			}
			if resp.CliID != nil && resp.APIKey != nil {
				return authResultMsg{cliID: *resp.CliID, apiKey: *resp.APIKey}
			}
			if resp.Status != nil && *resp.Status == "expired" {
				return authResultMsg{err: fmt.Errorf("authentication token expired")}
			}
		}
		return authResultMsg{err: fmt.Errorf("authentication timed out")}
	}
}

func downloadMetadataCmd(client *api.Client) tea.Cmd {
	return func() tea.Msg {
		err := client.DownloadMetadataGz()
		return metadataReadyMsg{err: err}
	}
}

func fetchPricingCmd(client *api.Client, products, regions []string, attrs map[string]string, prices []string) tea.Cmd {
	return func() tea.Msg {
		resp, err := client.FetchPricing(products, regions, attrs, prices)
		return pricingResultMsg{resp: resp, err: err}
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func splitNonEmpty(s, sep string) []string {
	parts := strings.Split(s, sep)
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
