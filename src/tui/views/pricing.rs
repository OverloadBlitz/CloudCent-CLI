use anyhow::Result;
use crossterm::event::{KeyCode, KeyEvent};
use indexmap::IndexMap;
use ratatui::{
    layout::{Alignment, Constraint, Direction, Layout, Rect},
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, BorderType, Borders, Cell, Paragraph, Row, Table},
    Frame,
};

use crate::api::PricingApiResponse;
use crate::tui::semantic::{
    score_and_suggest_products, suggest_attrs, suggest_regions, SuggestionItem,
};

#[derive(Debug, Clone)]
pub struct PricingDisplayItem {
    pub product: String,
    pub region: String,
    pub attributes: IndexMap<String, Option<String>>,
    pub prices: Vec<PriceInfo>,
    pub min_price: Option<String>,
    pub max_price: Option<String>,
}

#[derive(Debug, Clone)]
pub struct RateInfo {
    pub price: String,
    pub start_range: String,
    pub end_range: String,
}

#[derive(Debug, Clone)]
pub struct PriceInfo {
    pub pricing_model: String,
    pub price: String,
    pub unit: String,
    pub upfront_fee: String,
    pub purchase_option: String,
    pub year: String,
    /// Spot-specific: max interruption probability percentage
    pub interruption_max_pct: Option<String>,
    /// All rate tiers (empty if single flat price)
    pub rates: Vec<RateInfo>,
}

// ── Focus within the command area ────────────────────────────────────────────

/// Tracks whether keyboard focus is on the field row or the suggestion list.
#[derive(Clone, PartialEq)]
pub enum BuilderFocus {
    Field,
    Suggestions,
}

/// Whether the command bar is in raw-text mode or structured builder mode.
#[cfg(feature = "raw_command")]
#[derive(Clone, PartialEq)]
pub enum CommandMode {
    /// Structured builder: 4 field slots, suggestions always visible.
    Builder,
    /// Free-text input: single text box, suggestions only shown after Tab.
    Raw,
}

/// State for the structured command builder.
/// Each field holds a list of selected tag values plus a live search input.
#[derive(Clone, Default)]
pub struct CommandBuilderState {
    /// 0 = Product, 1 = Region, 2 = Attrs, 3 = Price
    pub selected_field: usize,
    pub product_tags: Vec<String>,
    pub region_tags: Vec<String>,
    pub attribute_tags: Vec<String>,
    pub price_tags: Vec<String>,
    /// Text being typed in the currently active field.
    pub search_input: String,
}

impl CommandBuilderState {
    pub fn new() -> Self {
        Self::default()
    }

    #[allow(dead_code)]
    pub fn current_tags(&self) -> &Vec<String> {
        match self.selected_field {
            0 => &self.product_tags,
            1 => &self.region_tags,
            2 => &self.attribute_tags,
            _ => &self.price_tags,
        }
    }

    pub fn current_tags_mut(&mut self) -> &mut Vec<String> {
        match self.selected_field {
            0 => &mut self.product_tags,
            1 => &mut self.region_tags,
            2 => &mut self.attribute_tags,
            _ => &mut self.price_tags,
        }
    }

    #[allow(dead_code)]
    pub fn is_empty(&self) -> bool {
        self.product_tags.is_empty()
            && self.region_tags.is_empty()
            && self.attribute_tags.is_empty()
            && self.price_tags.is_empty()
    }
}

// ── View events / sections ───────────────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PricingSection {
    Header,
    Command,
    Results,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum PricingEvent {
    None,
    Quit,
    PrevView,
    NextView,
    SubmitQuery,
    #[cfg(feature = "estimate")]
    AddToEstimate,
}

// ── Main view struct ─────────────────────────────────────────────────────────

#[derive(Clone)]
pub struct PricingView {
    pub command_builder: CommandBuilderState,
    /// Whether keyboard focus is on the field row or the suggestion list.
    pub builder_focus: BuilderFocus,
    /// Current command input mode.
    #[cfg(feature = "raw_command")]
    pub command_mode: CommandMode,
    /// Raw text input (used in Raw mode).
    #[cfg(feature = "raw_command")]
    pub raw_input: String,
    /// Cursor byte position within raw_input.
    #[cfg(feature = "raw_command")]
    pub raw_cursor: usize,
    /// Whether to show suggestions in Raw mode (toggled by Tab).
    #[cfg(feature = "raw_command")]
    pub raw_show_suggestions: bool,
    /// In raw mode: which command keyword is highlighted in the left suggestion column (0-3).
    #[cfg(feature = "raw_command")]
    pub raw_cmd_index: usize,
    /// In raw mode: whether keyboard focus is on the params (right) column vs command (left) column.
    #[cfg(feature = "raw_command")]
    pub raw_param_focus: bool,
    pub items: Vec<PricingDisplayItem>,
    pub filtered_items: Vec<PricingDisplayItem>,
    pub active_section: PricingSection,
    pub selected: usize,
    pub results_page: usize,
    pub results_per_page: usize,
    pub loading: bool,
    pub error_message: Option<String>,
    pub options: Option<std::sync::Arc<crate::commands::pricing::PricingOptions>>,
    /// Current suggestion list.
    pub suggestions_cache: Vec<SuggestionItem>,
    /// Highlighted row in the suggestion list (None = no highlight).
    pub suggestion_index: Option<usize>,
    /// Horizontal scroll offset for the results table (number of scrollable columns to skip).
    pub h_scroll_offset: usize,
    /// Total number of scrollable columns (updated on render).
    pub total_scrollable_cols: std::cell::Cell<usize>,
    /// Number of visible scrollable columns that fit in the table width (updated on render).
    pub visible_scrollable_cols: std::cell::Cell<usize>,
    /// Number of columns in the suggestion panel (updated on render, used for keyboard nav)
    pub suggestion_cols: std::cell::Cell<usize>,
}

impl PricingView {
    pub fn new() -> Self {
        Self {
            command_builder: CommandBuilderState::new(),
            builder_focus: BuilderFocus::Field,
            #[cfg(feature = "raw_command")]
            command_mode: CommandMode::Builder,
            #[cfg(feature = "raw_command")]
            raw_input: String::new(),
            #[cfg(feature = "raw_command")]
            raw_cursor: 0,
            #[cfg(feature = "raw_command")]
            raw_show_suggestions: false,
            #[cfg(feature = "raw_command")]
            raw_cmd_index: 0,
            #[cfg(feature = "raw_command")]
            raw_param_focus: false,
            items: Vec::new(),
            filtered_items: Vec::new(),
            active_section: PricingSection::Command,
            selected: 0,
            results_page: 0,
            results_per_page: 15,
            loading: false,
            error_message: None,
            options: None,
            suggestions_cache: Vec::new(),
            suggestion_index: None,
            h_scroll_offset: 0,
            total_scrollable_cols: std::cell::Cell::new(0),
            visible_scrollable_cols: std::cell::Cell::new(0),
            suggestion_cols: std::cell::Cell::new(1),
        }
    }



    // ── Key handling ─────────────────────────────────────────────────────────

    pub fn handle_key(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        // Global shortcuts active regardless of section
        match key.code {
            KeyCode::F(2) => {
                self.command_builder = CommandBuilderState::new();
                self.builder_focus = BuilderFocus::Field;
                #[cfg(feature = "raw_command")]
                {
                    self.raw_input.clear();
                    self.raw_cursor = 0;
                    self.raw_show_suggestions = false;
                    self.raw_cmd_index = 0;
                    self.raw_param_focus = false;
                }
                self.suggestions_cache.clear();
                self.suggestion_index = None;
                self.filter_items();
                if self.active_section == PricingSection::Command {
                    self.update_suggestions();
                }
                return Ok(PricingEvent::None);
            }
            _ => {}
        }

        match self.active_section {
            PricingSection::Header => self.handle_key_header(key),
            PricingSection::Command => self.handle_key_command(key),
            PricingSection::Results => self.handle_key_results(key),
        }
    }

    fn handle_key_header(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        match key.code {
            KeyCode::Left => return Ok(PricingEvent::PrevView),
            KeyCode::Right => return Ok(PricingEvent::NextView),
            KeyCode::Down => {
                self.active_section = PricingSection::Command;
                self.update_suggestions();
            }
            KeyCode::Esc => return Ok(PricingEvent::Quit),
            _ => {}
        }
        Ok(PricingEvent::None)
    }

    fn handle_key_command(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        match key.code {
            KeyCode::Esc => return Ok(PricingEvent::Quit),
            // F1: toggle between Builder and Raw mode
            #[cfg(feature = "raw_command")]
            KeyCode::F(1) => {
                match self.command_mode {
                    CommandMode::Builder => {
                        self.raw_input = self.builder_to_raw();
                        self.raw_cursor = self.raw_input.len();
                        self.command_mode = CommandMode::Raw;
                    }
                    CommandMode::Raw => {
                        self.apply_raw_to_builder();
                        self.command_mode = CommandMode::Builder;
                    }
                }
                self.builder_focus = BuilderFocus::Field;
                self.raw_show_suggestions = false;
                self.raw_cmd_index = 0;
                self.raw_param_focus = false;
                self.suggestion_index = None;
                self.update_suggestions();
                return Ok(PricingEvent::None);
            }
            KeyCode::Tab => {
                #[cfg(feature = "raw_command")]
                match self.command_mode {
                    CommandMode::Builder => {
                        self.handle_tab_builder();
                    }
                    CommandMode::Raw => {
                        if !self.raw_show_suggestions {
                            self.raw_show_suggestions = true;
                            self.update_suggestions_raw();
                            self.raw_param_focus = false;
                        } else {
                            self.raw_show_suggestions = false;
                            self.raw_param_focus = false;
                            self.suggestion_index = None;
                            self.builder_focus = BuilderFocus::Field;
                        }
                    }
                }
                #[cfg(not(feature = "raw_command"))]
                self.handle_tab_builder();
                return Ok(PricingEvent::None);
            }
            _ => {}
        }

        match self.builder_focus {
            BuilderFocus::Field => {
                #[cfg(feature = "raw_command")]
                if self.command_mode == CommandMode::Raw {
                    return self.handle_key_raw_field(key);
                }
                self.handle_key_builder_field(key)
            }
            BuilderFocus::Suggestions => {
                #[cfg(feature = "raw_command")]
                if self.command_mode == CommandMode::Raw {
                    return self.handle_key_raw_suggestions(key);
                }
                self.handle_key_builder_suggestions(key)
            }
        }
    }

    fn handle_tab_builder(&mut self) {
        let input = self.command_builder.search_input.clone();
        if !input.is_empty() && !self.suggestions_cache.is_empty() {
            if self.suggestions_cache.len() == 1 {
                self.toggle_suggestion(0);
            } else {
                self.suggestion_index = Some(0);
            }
        } else if !self.suggestions_cache.is_empty() {
            self.suggestion_index = Some(0);
        }
    }

    #[cfg(feature = "raw_command")]
    fn handle_key_raw_field(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        match key.code {
            KeyCode::Up => {
                self.active_section = PricingSection::Header;
            }
            KeyCode::Down => {
                if self.raw_show_suggestions {
                    // Move focus into suggestion panel (cmd column)
                    self.builder_focus = BuilderFocus::Suggestions;
                    self.raw_param_focus = false;
                } else if !self.filtered_items.is_empty() {
                    self.active_section = PricingSection::Results;
                }
            }
            KeyCode::Left => {
                if self.raw_cursor > 0 {
                    let s = &self.raw_input[..self.raw_cursor];
                    self.raw_cursor = s.char_indices().next_back().map(|(i, _)| i).unwrap_or(0);
                    if self.raw_show_suggestions {
                        self.update_suggestions_raw();
                    }
                } else {
                    return Ok(PricingEvent::PrevView);
                }
            }
            KeyCode::Right => {
                if self.raw_cursor < self.raw_input.len() {
                    let ch = self.raw_input[self.raw_cursor..].chars().next().unwrap();
                    self.raw_cursor += ch.len_utf8();
                    if self.raw_show_suggestions {
                        self.update_suggestions_raw();
                    }
                } else {
                    return Ok(PricingEvent::NextView);
                }
            }
            KeyCode::Enter => {
                self.apply_raw_to_builder();
                return Ok(PricingEvent::SubmitQuery);
            }
            KeyCode::Backspace => {
                if self.raw_cursor > 0 {
                    let s = &self.raw_input[..self.raw_cursor];
                    let new_cursor = s.char_indices().next_back().map(|(i, _)| i).unwrap_or(0);
                    self.raw_input.remove(new_cursor);
                    self.raw_cursor = new_cursor;
                    if self.raw_show_suggestions {
                        self.update_suggestions_raw();
                    }
                }
            }
            KeyCode::Delete => {
                if self.raw_cursor < self.raw_input.len() {
                    self.raw_input.remove(self.raw_cursor);
                    if self.raw_show_suggestions {
                        self.update_suggestions_raw();
                    }
                }
            }
            KeyCode::Char(c) => {
                self.raw_input.insert(self.raw_cursor, c);
                self.raw_cursor += c.len_utf8();
                if self.raw_show_suggestions {
                    self.update_suggestions_raw();
                }
            }
            _ => {}
        }
        Ok(PricingEvent::None)
    }

    /// The keywords recognised in raw command input.
    #[cfg(feature = "raw_command")]
    const RAW_KEYWORDS: [&'static str; 4] = ["products", "regions", "specs", "price"];

    /// If the token immediately before the cursor is a unique prefix of a RAW_KEYWORD,
    /// complete it in-place and return true. Otherwise return false.
    #[cfg(feature = "raw_command")]
    fn try_complete_raw_keyword(&mut self) -> bool {
        let text = &self.raw_input[..self.raw_cursor];
        let token = match text.split_whitespace().last() {
            Some(t) => t.to_string(),
            None => return false,
        };
        if token.is_empty() {
            return false;
        }
        let tl = token.to_lowercase();
        // Already a complete keyword — don't re-complete, just show suggestions
        if Self::RAW_KEYWORDS.contains(&tl.as_str()) {
            self.raw_show_suggestions = true;
            self.update_suggestions_raw();
            self.suggestion_index = if self.suggestions_cache.is_empty() { None } else { Some(0) };
            self.builder_focus = BuilderFocus::Suggestions;
            return true;
        }
        let matches: Vec<&str> = Self::RAW_KEYWORDS.iter()
            .filter(|kw| kw.starts_with(tl.as_str()))
            .copied()
            .collect();
        if matches.len() == 1 {
            let kw = matches[0];
            let token_start = self.raw_cursor - token.len();
            self.raw_input.drain(token_start..self.raw_cursor);
            let insert = format!("{} ", kw);
            for (i, ch) in insert.char_indices() {
                self.raw_input.insert(token_start + i, ch);
            }
            self.raw_cursor = token_start + insert.len();
            self.raw_show_suggestions = true;
            self.update_suggestions_raw();
            self.suggestion_index = if self.suggestions_cache.is_empty() { None } else { Some(0) };
            self.builder_focus = BuilderFocus::Suggestions;
            return true;
        }
        false
    }

    /// Serialize the current builder state into a raw command string.
    #[cfg(feature = "raw_command")]
    fn builder_to_raw(&self) -> String {
        let mut parts: Vec<String> = Vec::new();
        if !self.command_builder.product_tags.is_empty() {
            parts.push(format!("products {}", self.command_builder.product_tags.join(",")));
        }
        if !self.command_builder.region_tags.is_empty() {
            parts.push(format!("regions {}", self.command_builder.region_tags.join(",")));
        }
        if !self.command_builder.attribute_tags.is_empty() {
            parts.push(format!("attrs {}", self.command_builder.attribute_tags.join(",")));
        }
        if !self.command_builder.price_tags.is_empty() {
            parts.push(format!("price {}", self.command_builder.price_tags.join(",")));
        }
        parts.join(" ")
    }

    /// Parse raw input into (keyword, token_before_cursor).
    /// Returns the active keyword (0=products,1=regions,2=attrs,3=price) and
    /// the partial token the user is currently typing after that keyword.
    #[cfg(feature = "raw_command")]
    fn raw_active_field(&self) -> (usize, String) {
        let text = &self.raw_input[..self.raw_cursor];
        // Find the last keyword that appears before the cursor
        let mut best_kw: Option<(usize, usize)> = None; // (field_idx, byte_pos)
        for (i, kw) in Self::RAW_KEYWORDS.iter().enumerate() {
            // Search for keyword followed by whitespace or end
            let mut search = text;
            let mut offset = 0usize;
            while let Some(pos) = search.find(kw) {
                let abs = offset + pos;
                let after = abs + kw.len();
                // keyword must be at start or preceded by whitespace
                let preceded_ok = abs == 0 || text.as_bytes().get(abs - 1).map(|b| b.is_ascii_whitespace()).unwrap_or(false);
                // keyword must be followed by whitespace or end of text-before-cursor
                let followed_ok = after >= text.len() || text.as_bytes().get(after).map(|b| b.is_ascii_whitespace()).unwrap_or(false);
                if preceded_ok && followed_ok {
                    if best_kw.map(|(_, p)| abs > p).unwrap_or(true) {
                        best_kw = Some((i, abs));
                    }
                }
                offset += pos + 1;
                search = &search[pos + 1..];
            }
        }
        match best_kw {
            None => (0, String::new()),
            Some((field, kw_pos)) => {
                let kw_len = Self::RAW_KEYWORDS[field].len();
                let after_kw = &text[kw_pos + kw_len..];
                // Tags are comma-separated; the current token is after the last comma (or after the keyword space)
                let token = after_kw.split(',').last().unwrap_or("").trim_start().to_string();
                (field, token)
            }
        }
    }

    /// Accept the currently highlighted suggestion into the raw input at cursor.
    #[cfg(feature = "raw_command")]
    fn accept_raw_suggestion(&mut self, idx: usize) {
        if idx >= self.suggestions_cache.len() {
            return;
        }
        let value = self.suggestions_cache[idx].value.clone();
        if value.is_empty() {
            return;
        }
        let (_, token) = self.raw_active_field();
        // Remove the partial token before cursor (after last comma or keyword space)
        let remove_len = token.len();
        let new_cursor = self.raw_cursor - remove_len;
        self.raw_input.drain(new_cursor..self.raw_cursor);
        // Insert the completed value; append comma so user can type another tag
        let insert = format!("{},", value);
        for (i, ch) in insert.char_indices() {
            self.raw_input.insert(new_cursor + i, ch);
        }
        self.raw_cursor = new_cursor + insert.len();
        self.suggestion_index = None;
        self.update_suggestions_raw();
    }

    /// Parse the full raw input and populate command_builder tags for submission.
    #[cfg(feature = "raw_command")]
    fn apply_raw_to_builder(&mut self) {
        let text = self.raw_input.clone();
        self.command_builder = CommandBuilderState::new();
        // Split on keywords
        let remaining = text.as_str();
        // Collect (keyword_idx, value_str) pairs
        let mut segments: Vec<(usize, &str)> = Vec::new();
        // Find all keyword positions
        let mut positions: Vec<(usize, usize)> = Vec::new(); // (byte_pos, field_idx)
        for (i, kw) in Self::RAW_KEYWORDS.iter().enumerate() {
            let mut search = remaining;
            let mut offset = 0usize;
            while let Some(pos) = search.find(kw) {
                let abs = offset + pos;
                let after = abs + kw.len();
                let preceded_ok = abs == 0 || remaining.as_bytes().get(abs - 1).map(|b| b.is_ascii_whitespace()).unwrap_or(false);
                let followed_ok = after >= remaining.len() || remaining.as_bytes().get(after).map(|b| b.is_ascii_whitespace()).unwrap_or(false);
                if preceded_ok && followed_ok {
                    positions.push((abs, i));
                }
                offset += pos + 1;
                search = &search[pos + 1..];
            }
        }
        positions.sort_by_key(|(p, _)| *p);
        for (seg_idx, &(pos, field)) in positions.iter().enumerate() {
            let kw_len = Self::RAW_KEYWORDS[field].len();
            let value_start = pos + kw_len;
            let value_end = positions.get(seg_idx + 1).map(|(p, _)| *p).unwrap_or(remaining.len());
            let value_str = remaining[value_start..value_end].trim();
            segments.push((field, value_str));
        }
        for (field, value_str) in segments {
            if value_str.is_empty() {
                continue;
            }
            // Tags are comma-separated; each tag may contain spaces (e.g. "anthropic llm api")
            let tags: Vec<String> = value_str.split(',')
                .map(|s| s.trim().to_string())
                .filter(|s| !s.is_empty())
                .collect();
            match field {
                0 => self.command_builder.product_tags = tags,
                1 => self.command_builder.region_tags = tags,
                2 => self.command_builder.attribute_tags = tags,
                _ => self.command_builder.price_tags = tags,
            }
        }
        self.filter_items();
    }

    #[cfg(feature = "raw_command")]
    fn handle_key_raw_suggestions(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        let total_params = self.suggestions_cache.len();
        match key.code {
            KeyCode::Up => {
                if self.raw_param_focus {
                    if self.suggestion_index.map(|i| i == 0).unwrap_or(true) {
                        // At top of params: move back to cmd column
                        self.raw_param_focus = false;
                        self.suggestion_index = None;
                    } else {
                        self.suggestion_index = self.suggestion_index.map(|i| i.saturating_sub(1));
                    }
                } else {
                    if self.raw_cmd_index == 0 {
                        // At top of cmd column: back to input
                        self.builder_focus = BuilderFocus::Field;
                    } else {
                        self.raw_cmd_index -= 1;
                        // Update params for newly highlighted keyword
                        self.update_suggestions_for_cmd_index();
                    }
                }
            }
            KeyCode::Down => {
                if self.raw_param_focus {
                    if total_params > 0 {
                        let next = self.suggestion_index.map(|i| (i + 1).min(total_params - 1)).unwrap_or(0);
                        if next == self.suggestion_index.unwrap_or(usize::MAX) {
                            // At bottom: go to results
                            if !self.filtered_items.is_empty() {
                                self.builder_focus = BuilderFocus::Field;
                                self.active_section = PricingSection::Results;
                            }
                        } else {
                            self.suggestion_index = Some(next);
                        }
                    }
                } else {
                    if self.raw_cmd_index < 3 {
                        self.raw_cmd_index += 1;
                        self.update_suggestions_for_cmd_index();
                    } else {
                        // At bottom of cmd column: go to results
                        if !self.filtered_items.is_empty() {
                            self.builder_focus = BuilderFocus::Field;
                            self.active_section = PricingSection::Results;
                        }
                    }
                }
            }
            KeyCode::Right => {
                // Move from cmd column to params column
                if !self.raw_param_focus && total_params > 0 {
                    self.raw_param_focus = true;
                    self.suggestion_index = Some(0);
                }
            }
            KeyCode::Left => {
                if self.raw_param_focus {
                    // Move back to cmd column
                    self.raw_param_focus = false;
                    self.suggestion_index = None;
                } else {
                    // Back to input
                    self.builder_focus = BuilderFocus::Field;
                }
            }
            KeyCode::Enter => {
                if self.raw_param_focus {
                    // Accept param suggestion
                    if let Some(idx) = self.suggestion_index {
                        self.accept_raw_suggestion(idx);
                        self.builder_focus = BuilderFocus::Field;
                        self.raw_show_suggestions = false;
                    }
                } else {
                    // Accept keyword: insert it into input
                    self.accept_raw_keyword(self.raw_cmd_index);
                    self.raw_param_focus = true;
                    self.suggestion_index = if self.suggestions_cache.is_empty() { None } else { Some(0) };
                }
            }
            KeyCode::Char(' ') => {
                // Space also accepts
                if self.raw_param_focus {
                    if let Some(idx) = self.suggestion_index {
                        self.accept_raw_suggestion(idx);
                        self.builder_focus = BuilderFocus::Field;
                        self.raw_show_suggestions = false;
                    }
                } else {
                    self.accept_raw_keyword(self.raw_cmd_index);
                    self.raw_param_focus = true;
                    self.suggestion_index = if self.suggestions_cache.is_empty() { None } else { Some(0) };
                }
            }
            KeyCode::Esc => {
                self.raw_show_suggestions = false;
                self.builder_focus = BuilderFocus::Field;
                self.raw_param_focus = false;
                self.suggestion_index = None;
            }
            _ => {}
        }
        Ok(PricingEvent::None)
    }

    /// Update params suggestions for the keyword currently highlighted in the cmd column.
    #[cfg(feature = "raw_command")]
    fn update_suggestions_for_cmd_index(&mut self) {
        let opts = match self.options.clone() {
            Some(o) => o,
            None => { self.suggestions_cache.clear(); return; }
        };
        self.suggestions_cache = match self.raw_cmd_index {
            0 => score_and_suggest_products("", &opts.products, &opts.attribute_values, &opts.product_groups, &[]),
            1 => suggest_regions("", &opts.regions, &opts.product_regions, &[], &[]),
            2 => suggest_attrs("", &[], &opts.product_attrs, &opts.attribute_values, &[]),
            _ => [">", "<", ">=", "<="].iter().map(|op| crate::tui::semantic::SuggestionItem {
                value: op.to_string(),
                display: format!("{} (price operator)", op),
                reason: "operator".to_string(),
                is_semantic: false,
                already_selected: false,
            }).collect(),
        };
        self.suggestion_index = None;
    }

    /// Insert or replace the keyword at cursor position in raw_input.
    #[cfg(feature = "raw_command")]
    fn accept_raw_keyword(&mut self, kw_idx: usize) {
        let kw = Self::RAW_KEYWORDS[kw_idx];
        // Find if there's already a keyword token being typed before cursor
        let text_before = &self.raw_input[..self.raw_cursor].to_string();
        let last_token = text_before.split_whitespace().last().map(|s| s.to_string()).unwrap_or_default();
        // Check if last token is a prefix of any keyword (to replace it)
        let is_kw_prefix = Self::RAW_KEYWORDS.iter().any(|k| k.starts_with(last_token.as_str()) && !last_token.is_empty());
        if is_kw_prefix {
            let remove_start = self.raw_cursor - last_token.len();
            self.raw_input.drain(remove_start..self.raw_cursor);
            let insert = format!("{} ", kw);
            for (i, ch) in insert.char_indices() {
                self.raw_input.insert(remove_start + i, ch);
            }
            self.raw_cursor = remove_start + insert.len();
        } else {
            // Append keyword with leading space if needed
            let needs_space = self.raw_cursor > 0
                && !self.raw_input[..self.raw_cursor].ends_with(' ');
            let insert = if needs_space { format!(" {} ", kw) } else { format!("{} ", kw) };
            for (i, ch) in insert.char_indices() {
                self.raw_input.insert(self.raw_cursor + i, ch);
            }
            self.raw_cursor += insert.len();
        }
        // Update params suggestions for this keyword
        self.raw_cmd_index = kw_idx;
        self.update_suggestions_for_cmd_index();
    }

    fn handle_key_builder_field(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        match key.code {
            // Left/Right: move between parameter slots horizontally
            KeyCode::Left => {
                if self.command_builder.selected_field > 0 {
                    self.command_builder.selected_field -= 1;
                    self.command_builder.search_input.clear();
                    self.suggestion_index = None;
                    self.update_suggestions();
                } else {
                    return Ok(PricingEvent::PrevView);
                }
            }
            KeyCode::Right => {
                if self.command_builder.selected_field < 3 {
                    self.command_builder.selected_field += 1;
                    self.command_builder.search_input.clear();
                    self.suggestion_index = None;
                    self.update_suggestions();
                } else {
                    return Ok(PricingEvent::NextView);
                }
            }
            // Up/Down: move between sections
            KeyCode::Up => {
                self.active_section = PricingSection::Header;
            }
            KeyCode::Down => {
                // If suggestions are visible and there are items, move focus into suggestion panel
                if !self.suggestions_cache.is_empty() {
                    self.builder_focus = BuilderFocus::Suggestions;
                    if self.suggestion_index.is_none() {
                        self.suggestion_index = Some(0);
                    }
                } else if !self.filtered_items.is_empty() {
                    self.active_section = PricingSection::Results;
                }
            }
            // Enter: submit query to API
            KeyCode::Enter => {
                return Ok(PricingEvent::SubmitQuery);
            }
            // Backspace: delete last char from search_input; if empty, pop last tag
            KeyCode::Backspace => {
                if self.command_builder.search_input.is_empty() {
                    self.command_builder.current_tags_mut().pop();
                    self.filter_items();
                    self.update_suggestions();
                } else {
                    self.command_builder.search_input.pop();
                    self.suggestion_index = None;
                    self.update_suggestions();
                    self.filter_items();
                }
            }
            // Delete: clear search_input or all tags for the current field
            KeyCode::Delete => {
                if self.command_builder.search_input.is_empty() {
                    self.command_builder.current_tags_mut().clear();
                    self.filter_items();
                    self.update_suggestions();
                } else {
                    self.command_builder.search_input.clear();
                    self.suggestion_index = None;
                    self.update_suggestions();
                    self.filter_items();
                }
            }
            // Any printable character: type into the current field's search buffer
            KeyCode::Char(c) => {
                self.command_builder.search_input.push(c);
                self.suggestion_index = None;
                self.update_suggestions();
                self.filter_items();
            }
            _ => {}
        }
        Ok(PricingEvent::None)
    }

    fn handle_key_builder_suggestions(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        let cols = self.suggestion_cols.get().max(1);
        let total = self.suggestions_cache.len();
        match key.code {
            // Up: navigate up, or return to field input if at top row
            KeyCode::Up => {
                let at_top = self.suggestion_index.map(|i| i < cols).unwrap_or(true);
                if at_top {
                    self.builder_focus = BuilderFocus::Field;
                    return Ok(PricingEvent::None);
                }
                if total > 0 {
                    self.suggestion_index = Some(
                        self.suggestion_index.map(|i| i.saturating_sub(cols)).unwrap_or(0)
                    );
                }
            }
            // Down: navigate down, or go to results if at bottom
            KeyCode::Down => {
                if total > 0 {
                    let cur = self.suggestion_index.unwrap_or(0);
                    let next = (cur + cols).min(total - 1);
                    if next == cur {
                        // Already at last row — go to results
                        if !self.filtered_items.is_empty() {
                            self.builder_focus = BuilderFocus::Field;
                            self.active_section = PricingSection::Results;
                        }
                    } else {
                        self.suggestion_index = Some(next);
                    }
                } else if !self.filtered_items.is_empty() {
                    self.builder_focus = BuilderFocus::Field;
                    self.active_section = PricingSection::Results;
                }
            }
            // Left/Right: navigate columns
            KeyCode::Right => {
                if total > 0 {
                    let next = self.suggestion_index.map(|i| (i + 1).min(total - 1)).unwrap_or(0);
                    self.suggestion_index = Some(next);
                }
            }
            KeyCode::Left if self.suggestion_index.map(|i| i % cols == 0).unwrap_or(true) => {
                // At leftmost column: go back to field focus
                self.builder_focus = BuilderFocus::Field;
                return Ok(PricingEvent::None);
            }
            KeyCode::Left => {
                if total > 0 {
                    let prev = self.suggestion_index.map(|i| i.saturating_sub(1)).unwrap_or(0);
                    self.suggestion_index = Some(prev);
                }
            }
            // Space: toggle selection
            KeyCode::Char(' ') => {
                if let Some(idx) = self.suggestion_index {
                    if idx < total {
                        #[cfg(feature = "raw_command")]
                        if self.command_mode == CommandMode::Raw {
                            self.accept_raw_suggestion(idx);
                            return Ok(PricingEvent::None);
                        }
                        self.toggle_suggestion(idx);
                    }
                }
            }
            // Enter: submit query
            KeyCode::Enter => {
                #[cfg(feature = "raw_command")]
                if self.command_mode == CommandMode::Raw {
                    if let Some(idx) = self.suggestion_index {
                        if idx < total {
                            self.accept_raw_suggestion(idx);
                            self.builder_focus = BuilderFocus::Field;
                        }
                    }
                    return Ok(PricingEvent::None);
                }
                return Ok(PricingEvent::SubmitQuery);
            }
            _ => {}
        }
        Ok(PricingEvent::None)
    }
    fn toggle_suggestion(&mut self, idx: usize) {
        if idx >= self.suggestions_cache.len() {
            return;
        }
        let value = self.suggestions_cache[idx].value.clone();
        if value.is_empty() {
            return;
        }
        
        // Attrs phase 1: selected value is a key name (no '=').
        // Transition to value selection by setting search_input to "key=".
        if self.command_builder.selected_field == 2 && !value.contains('=') {
            self.command_builder.search_input = format!("{}=", value);
            self.update_suggestions();
            if idx < self.suggestions_cache.len() {
                self.suggestion_index = Some(idx);
            } else if !self.suggestions_cache.is_empty() {
                self.suggestion_index = Some(0);
            }
        } else if self.command_builder.selected_field == 3 {
            // Price field: append operator and let user type the value
            self.command_builder.search_input = value;
            self.update_suggestions();
            self.suggestion_index = None;
        } else {
            let tags = self.command_builder.current_tags_mut();
            if let Some(pos) = tags.iter().position(|t| t == &value) {
                tags.remove(pos);
            } else {
                tags.push(value.clone());
            }
            self.command_builder.search_input.clear();
            self.update_suggestions();
            // Keep cursor on the same item by finding it in the new list
            self.suggestion_index = self.suggestions_cache
                .iter()
                .position(|s| s.value == value)
                .or_else(|| {
                    // Item may have been removed from list; stay at same index clamped
                    self.suggestion_index.map(|i| i.min(self.suggestions_cache.len().saturating_sub(1)))
                        .filter(|_| !self.suggestions_cache.is_empty())
                });
            self.filter_items();
        }
    }


    fn handle_key_results(&mut self, key: KeyEvent) -> Result<PricingEvent> {
        match key.code {
            KeyCode::Up => {
                // Move up within current page, or return to Command from top row
                let page_start = self.results_page * self.results_per_page;
                if self.selected > page_start {
                    self.selected -= 1;
                } else {
                    // At top of page: go back to Command
                    self.active_section = PricingSection::Command;
                    self.builder_focus = BuilderFocus::Field;
                    self.update_suggestions();
                }
            }
            KeyCode::Down => {
                // Move down within current page
                if !self.filtered_items.is_empty() {
                    let page_end = ((self.results_page + 1) * self.results_per_page)
                        .min(self.filtered_items.len())
                        .saturating_sub(1);
                    if self.selected < page_end {
                        self.selected += 1;
                    }
                }
            }
            KeyCode::Char('j') => {
                // j: previous page (intentionally inverted from vim convention)
                if self.results_page > 0 {
                    self.results_page -= 1;
                    self.selected = self.results_page * self.results_per_page;
                }
            }
            KeyCode::Char('k') => {
                // k: next page (intentionally inverted from vim convention)
                let total_pages = (self.filtered_items.len() + self.results_per_page - 1) / self.results_per_page;
                if self.results_page + 1 < total_pages {
                    self.results_page += 1;
                    self.selected = self.results_page * self.results_per_page;
                }
            }
            KeyCode::PageDown => {
                // Explicit page down
                let total_pages = (self.filtered_items.len() + self.results_per_page - 1) / self.results_per_page;
                if self.results_page + 1 < total_pages {
                    self.results_page += 1;
                    self.selected = self.results_page * self.results_per_page;
                }
            }
            KeyCode::PageUp => {
                // Explicit page up
                if self.results_page > 0 {
                    self.results_page -= 1;
                    self.selected = self.results_page * self.results_per_page;
                }
            }
            #[cfg(feature = "estimate")]
            KeyCode::Char('a') => return Ok(PricingEvent::AddToEstimate),
            KeyCode::Left => {
                if self.h_scroll_offset > 0 {
                    self.h_scroll_offset -= 1;
                }
            }
            KeyCode::Right => {
                let total = self.total_scrollable_cols.get();
                let visible = self.visible_scrollable_cols.get();
                if total > visible {
                    let max_offset = total.saturating_sub(visible);
                    if self.h_scroll_offset < max_offset {
                        self.h_scroll_offset += 1;
                    }
                }
            }
            KeyCode::Esc => return Ok(PricingEvent::Quit),
            _ => {}
        }
        Ok(PricingEvent::None)
    }


    // ── Suggestion update ─────────────────────────────────────────────────────

    /// Rebuild `suggestions_cache` based on the current mode and input state.
    /// Must be called whenever the input changes or the active field changes.
    pub fn update_suggestions(&mut self) {
        #[cfg(feature = "raw_command")]
        if self.command_mode == CommandMode::Raw {
            if self.raw_show_suggestions {
                self.update_suggestions_raw();
            }
            return;
        }
        self.update_suggestions_builder();
    }

    #[cfg(feature = "raw_command")]
    fn update_suggestions_raw(&mut self) {
        let opts = match self.options.clone() {
            Some(o) => o,
            None => {
                self.suggestions_cache.clear();
                return;
            }
        };
        let (field, token) = self.raw_active_field();
        // Sync the command keyword highlight to match cursor position
        self.raw_cmd_index = field;
        self.suggestions_cache = match field {
            0 => score_and_suggest_products(
                &token,
                &opts.products,
                &opts.attribute_values,
                &opts.product_groups,
                &[],
            ),
            1 => suggest_regions(
                &token,
                &opts.regions,
                &opts.product_regions,
                &[],
                &[],
            ),
            2 => suggest_attrs(
                &token,
                &[],
                &opts.product_attrs,
                &opts.attribute_values,
                &[],
            ),
            _ => {
                [">", "<", ">=", "<="].iter()
                    .filter(|op| token.is_empty() || op.starts_with(token.as_str()))
                    .map(|op| crate::tui::semantic::SuggestionItem {
                        value: op.to_string(),
                        display: format!("{} (price operator)", op),
                        reason: "operator".to_string(),
                        is_semantic: false,
                        already_selected: false,
                    }).collect()
            }
        };
    }

    fn update_suggestions_builder(&mut self) {
        let opts = match self.options.clone() {
            Some(o) => o,
            None => {
                self.suggestions_cache.clear();
                return;
            }
        };

        let q = self.command_builder.search_input.clone();
        self.suggestions_cache = match self.command_builder.selected_field {
            0 => score_and_suggest_products(
                &q,
                &opts.products,
                &opts.attribute_values,
                &opts.product_groups,
                &self.command_builder.product_tags,
            ),
            1 => suggest_regions(
                &q,
                &opts.regions,
                &opts.product_regions,
                &self.command_builder.product_tags,
                &self.command_builder.region_tags,
            ),
            2 => suggest_attrs(
                &q,
                &self.command_builder.product_tags,
                &opts.product_attrs,
                &opts.attribute_values,
                &self.command_builder.attribute_tags,
            ),
            _ => {
                // Suggest price operators
                [">", "<", ">=", "<="].iter()
                    .filter(|op| q.is_empty() || op.starts_with(&q))
                    .map(|op| crate::tui::semantic::SuggestionItem {
                        value: op.to_string(),
                        display: format!("{} (price operator)", op),
                        reason: "operator".to_string(),
                        is_semantic: false,
                        already_selected: false,
                    }).collect()
            }
        };
    }

    // ── Rendering ────────────────────────────────────────────────────────────

    pub fn render(&self, f: &mut Frame, area: Rect, active: bool) {
        // Suggestions panel: show when command section is active
        #[cfg(feature = "raw_command")]
        let show_suggestions = match self.command_mode {
            CommandMode::Builder => active && self.active_section == PricingSection::Command,
            CommandMode::Raw => self.raw_show_suggestions,
        };
        #[cfg(not(feature = "raw_command"))]
        let show_suggestions = active && self.active_section == PricingSection::Command;
        let suggestion_h: u16 = if show_suggestions { 8 } else { 0 };

        // Price details height: enough rows for all models split across 2 columns,
        // plus borders(2) + header(1) = 3 overhead per column.
        let price_detail_h: u16 = if let Some(item) = self.filtered_items.get(self.selected) {
            let models = item.prices.len();
            if models == 0 {
                3 // just the empty box
            } else {
                let rows_per_col = (models + 1) / 2; // ceil(models/2)
                (rows_per_col as u16 + 3).max(5).min(20) // 3 overhead, min 5, max 20
            }
        } else {
            3
        };

        let main_chunks = Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Length(3),              // Header
                Constraint::Length(3),              // Command bar (full width, single line)
                Constraint::Length(suggestion_h),   // Suggestions (full width, below command)
                Constraint::Min(6),                 // Results
                Constraint::Length(price_detail_h), // Price details (dynamic)
                Constraint::Length(3),              // Help
            ])
            .split(area);

        self.render_header(f, main_chunks[0], active);
        self.render_command(f, main_chunks[1], active);
        if show_suggestions {
            self.render_suggestions(f, main_chunks[2], active);
        }
        self.render_results(f, main_chunks[3], active);
        self.render_price_details(f, main_chunks[4]);
        self.render_help(f, main_chunks[5], active);
    }

    fn render_header(&self, f: &mut Frame, area: Rect, active: bool) {
        let is_focused = active && self.active_section == PricingSection::Header;

        let border_type = if is_focused { BorderType::Thick } else { BorderType::Plain };
        let border_color = if is_focused {
            Color::Green
        } else if active {
            Color::Cyan
        } else {
            Color::DarkGray
        };

        let pricing_style = if is_focused {
            Style::default()
                .fg(Color::Black)
                .bg(Color::Green)
                .add_modifier(Modifier::BOLD)
        } else if active {
            Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD)
        } else {
            Style::default().fg(Color::DarkGray)
        };

        let title = if is_focused {
            Line::from(vec![
                Span::styled(
                    format!(" > CloudCent CLI v{} < ", crate::VERSION),
                    Style::default()
                        .fg(Color::Green)
                        .add_modifier(Modifier::BOLD),
                ),
            ])
        } else {
            Line::from(format!(" CloudCent CLI v{} ", crate::VERSION))
        };

        let mut nav_spans = vec![
            Span::styled(
                if is_focused { " > Pricing < " } else { " Pricing " },
                pricing_style,
            ),
        ];
        #[cfg(feature = "estimate")]
        {
            nav_spans.push(Span::styled(" | ", Style::default().fg(Color::DarkGray)));
            nav_spans.push(Span::styled("Estimate", Style::default().fg(Color::DarkGray)));
        }
        nav_spans.push(Span::styled(" | ", Style::default().fg(Color::DarkGray)));
        nav_spans.push(Span::styled("History", Style::default().fg(Color::DarkGray)));
        nav_spans.push(Span::styled(" | ", Style::default().fg(Color::DarkGray)));
        nav_spans.push(Span::styled("Settings", Style::default().fg(Color::DarkGray)));

        let text = vec![Line::from(nav_spans)];

        f.render_widget(
            Paragraph::new(text).block(
                Block::default()
                    .borders(Borders::ALL)
                    .border_type(border_type)
                    .title(title)
                    .title_alignment(Alignment::Center)
                    .border_style(Style::default().fg(border_color)),
            ),
            area,
        );
    }

    /// Full-width horizontal command bar.
    /// Builder mode: shows 4 field slots inline with tags + cursor.
    /// Raw mode: shows a single text input box.
    fn render_command(&self, f: &mut Frame, area: Rect, active: bool) {
        let cmd_active = active && self.active_section == PricingSection::Command;
        let is_focused = cmd_active && self.builder_focus == BuilderFocus::Field;

        let border_type = if is_focused { BorderType::Thick } else { BorderType::Plain };
        let border_color = if is_focused { Color::Green } else if cmd_active { Color::Cyan } else { Color::DarkGray };
        let title_style = if cmd_active {
            Style::default().fg(if is_focused { Color::Green } else { Color::Cyan }).add_modifier(Modifier::BOLD)
        } else {
            Style::default().fg(Color::DarkGray)
        };

        let mode_label = " Pricing Query ";
        #[cfg(feature = "raw_command")]
        let mode_label = match self.command_mode {
            CommandMode::Builder => " Pricing Query ",
            CommandMode::Raw => " Raw Command ",
        };

        let title = {
            let spans = vec![
                Span::styled(if cmd_active { " > " } else { "   " }, title_style),
                Span::styled(mode_label, title_style),
            ];
            // F1 hint only shown when raw_command feature is enabled
            #[cfg(feature = "raw_command")]
            let mut spans = spans;
            #[cfg(feature = "raw_command")]
            spans.push(Span::styled(" [F1 Switch] ", Style::default().fg(Color::DarkGray)));
            Line::from(spans)
        };

        // Builder content line
        let builder_line = {
            let mut spans = Vec::new();
            for i in 0..4usize {
                let field_name = match i {
                    0 => "products",
                    1 => "regions",
                    2 => "specs",
                    _ => "price",
                };
                let tags = match i {
                    0 => &self.command_builder.product_tags,
                    1 => &self.command_builder.region_tags,
                    2 => &self.command_builder.attribute_tags,
                    _ => &self.command_builder.price_tags,
                };
                let is_sel = is_focused && i == self.command_builder.selected_field;

                spans.push(Span::styled(
                    format!("{} ", field_name),
                    if is_sel {
                        Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)
                    } else if !tags.is_empty() {
                        Style::default().fg(Color::Cyan)
                    } else {
                        Style::default().fg(Color::DarkGray)
                    },
                ));

                for t in tags {
                    spans.push(Span::styled(format!("[{}]", t), Style::default().fg(Color::Green)));
                    spans.push(Span::raw(" "));
                }

                if is_sel {
                    if !self.command_builder.search_input.is_empty() {
                        spans.push(Span::styled(&self.command_builder.search_input, Style::default().fg(Color::White)));
                    }
                    spans.push(Span::styled("▌", Style::default().fg(Color::Cyan)));
                }

                if i < 3 {
                    spans.push(Span::styled("  │  ", Style::default().fg(Color::DarkGray)));
                }
            }
            Line::from(spans)
        };

        #[cfg(feature = "raw_command")]
        let content_line = match self.command_mode {
            CommandMode::Builder => builder_line,
            CommandMode::Raw => {
                if self.raw_input.is_empty() && !is_focused {
                    Line::from(vec![
                        Span::styled("$ ", Style::default().fg(Color::DarkGray)),
                        Span::styled(
                            "products <name> regions <region> specs <key=val> price <op><val>  · comma separates multiple values · Tab for suggestions",
                            Style::default().fg(Color::DarkGray).add_modifier(Modifier::ITALIC),
                        ),
                    ])
                } else {
                    let mut spans: Vec<Span> = vec![
                        Span::styled("$ ", Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                    ];
                    let input = &self.raw_input;
                    let cursor = self.raw_cursor;
                    let kws: &[&str] = &Self::RAW_KEYWORDS;
                    let mut i = 0usize;
                    let mut cursor_inserted = false;
                    while i < input.len() {
                        if i == cursor && is_focused {
                            spans.push(Span::styled("▌", Style::default().fg(Color::Cyan)));
                            cursor_inserted = true;
                        }
                        let mut matched_kw: Option<&str> = None;
                        for kw in kws {
                            if input[i..].starts_with(kw) {
                                let after = i + kw.len();
                                let preceded_ok = i == 0 || input.as_bytes()[i - 1].is_ascii_whitespace();
                                let followed_ok = after >= input.len() || input.as_bytes()[after].is_ascii_whitespace();
                                if preceded_ok && followed_ok {
                                    matched_kw = Some(kw);
                                    break;
                                }
                            }
                        }
                        if let Some(kw) = matched_kw {
                            spans.push(Span::styled(kw.to_string(), Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)));
                            i += kw.len();
                        } else {
                            let ch = input[i..].chars().next().unwrap();
                            let style = if input.as_bytes()[i].is_ascii_whitespace() { Style::default() } else { Style::default().fg(Color::White) };
                            spans.push(Span::styled(ch.to_string(), style));
                            i += ch.len_utf8();
                        }
                    }
                    if !cursor_inserted && is_focused {
                        spans.push(Span::styled("▌", Style::default().fg(Color::Cyan)));
                    }
                    Line::from(spans)
                }
            }
        };
        #[cfg(not(feature = "raw_command"))]
        let content_line = builder_line;

        f.render_widget(
            Paragraph::new(content_line)
                .block(
                    Block::default()
                        .borders(Borders::ALL)
                        .border_type(border_type)
                        .title(title)
                        .border_style(Style::default().fg(border_color)),
                ),
            area,
        );
    }


    /// Full-width suggestion panel.
    /// Builder mode: field tabs in title + multi-column params list.
    /// Raw mode: left column = command keywords, right column = params list.
    fn render_suggestions(&self, f: &mut Frame, area: Rect, active: bool) {
        let cmd_active = active && self.active_section == PricingSection::Command;
        let suggestion_focused = cmd_active && self.builder_focus == BuilderFocus::Suggestions;

        let border_type = if suggestion_focused { BorderType::Thick } else { BorderType::Plain };
        let border_color = if suggestion_focused { Color::Green } else if cmd_active { Color::Cyan } else { Color::DarkGray };

        #[cfg(feature = "raw_command")]
        if self.command_mode == CommandMode::Raw {
            self.render_suggestions_raw(f, area, cmd_active, suggestion_focused, border_type, border_color);
            return;
        }
        self.render_suggestions_builder(f, area, cmd_active, suggestion_focused, border_type, border_color);
    }

    #[cfg(feature = "raw_command")]
    fn render_suggestions_raw(
        &self, f: &mut Frame, area: Rect,
        _cmd_active: bool, suggestion_focused: bool,
        border_type: BorderType, border_color: Color,
    ) {
        let hint = if suggestion_focused {
            "[↑↓ Navigate · ←→ Switch · Enter Accept · Esc Close]"
        } else {
            "[Tab Open · ↓ Browse]"
        };
        let title = Line::from(vec![
            Span::styled(" commands ", Style::default().fg(Color::DarkGray)),
            Span::styled("│", Style::default().fg(Color::DarkGray)),
            Span::styled(
                format!(" params ({}) ", self.suggestions_cache.len()),
                Style::default().fg(Color::DarkGray),
            ),
            Span::styled(hint, Style::default().fg(Color::DarkGray)),
        ]);

        // Split area into left (commands) and right (params)
        let inner = Block::default()
            .borders(Borders::ALL)
            .border_type(border_type)
            .title(title)
            .border_style(Style::default().fg(border_color));
        let inner_area = inner.inner(area);
        f.render_widget(inner, area);

        let cols = Layout::default()
            .direction(Direction::Horizontal)
            .constraints([Constraint::Length(14), Constraint::Min(1)])
            .split(inner_area);

        // ── Left: command keywords ──
        let kw_names = ["products", "regions", "specs", "price"];
        let kw_lines: Vec<Line> = kw_names.iter().enumerate().map(|(i, name)| {
            let is_active = i == self.raw_cmd_index;
            let focused_cmd = suggestion_focused && !self.raw_param_focus;
            if is_active && focused_cmd {
                Line::from(Span::styled(
                    format!("> {}", name),
                    Style::default().fg(Color::Black).bg(Color::Cyan).add_modifier(Modifier::BOLD),
                ))
            } else if is_active {
                Line::from(Span::styled(
                    format!("  {}", name),
                    Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD),
                ))
            } else {
                Line::from(Span::styled(
                    format!("  {}", name),
                    Style::default().fg(Color::DarkGray),
                ))
            }
        }).collect();
        f.render_widget(Paragraph::new(kw_lines), cols[0]);

        // ── Right: params suggestions ──
        if self.suggestions_cache.is_empty() {
            let msg = if self.options.is_none() { "(sync metadata first)" } else { "(no matches)" };
            f.render_widget(
                Paragraph::new(Span::styled(msg, Style::default().fg(Color::DarkGray).add_modifier(Modifier::ITALIC))),
                cols[1],
            );
            return;
        }

        let inner_w = cols[1].width as usize;
        let inner_h = cols[1].height as usize;
        let max_display = self.suggestions_cache.iter().map(|s| s.display.len()).max().unwrap_or(10);
        let max_reason = self.suggestions_cache.iter().map(|s| s.reason.len()).max().unwrap_or(0);
        let cell_w = (4 + max_display + if max_reason > 0 { 2 + max_reason } else { 0 }).clamp(16, 36);
        let num_cols = (inner_w / cell_w).max(1);
        self.suggestion_cols.set(num_cols);

        let sel_idx = self.suggestion_index.unwrap_or(0);
        let sel_row = sel_idx / num_cols;
        let scroll_row = if sel_row >= inner_h { sel_row - inner_h + 1 } else { 0 };

        let focused_params = suggestion_focused && self.raw_param_focus;
        let mut lines: Vec<Line> = Vec::new();
        for row in scroll_row..(scroll_row + inner_h) {
            let mut spans: Vec<Span> = Vec::new();
            for col in 0..num_cols {
                let i = row * num_cols + col;
                if i >= self.suggestions_cache.len() { break; }
                let item = &self.suggestions_cache[i];
                let is_sel = focused_params && self.suggestion_index == Some(i);

                let (item_style, reason_style, check) = if is_sel {
                    (Style::default().fg(Color::Black).bg(Color::Cyan).add_modifier(Modifier::BOLD),
                     Style::default().fg(Color::Black).bg(Color::Cyan), ">")
                } else if item.is_semantic {
                    (Style::default().fg(Color::Yellow), Style::default().fg(Color::DarkGray), " ")
                } else {
                    (Style::default().fg(Color::Gray), Style::default().fg(Color::DarkGray), " ")
                };

                let display_str = format!("{} {}", check, item.display);
                let reason_str = if !item.reason.is_empty() { format!(" {}", item.reason) } else { String::new() };
                let pad = cell_w.saturating_sub(display_str.len() + reason_str.len());
                spans.push(Span::styled(display_str, item_style));
                if !reason_str.is_empty() { spans.push(Span::styled(reason_str, reason_style)); }
                spans.push(Span::raw(" ".repeat(pad.max(1))));
            }
            if !spans.is_empty() { lines.push(Line::from(spans)); }
        }
        f.render_widget(Paragraph::new(lines), cols[1]);
    }

    fn render_suggestions_builder(
        &self, f: &mut Frame, area: Rect,
        cmd_active: bool, suggestion_focused: bool,
        border_type: BorderType, border_color: Color,
    ) {
        let title: Line = {
            let mut spans = vec![Span::styled(" ", Style::default())];
            for i in 0..4usize {
                let name = match i { 0 => "products", 1 => "regions", 2 => "specs", _ => "price" };
                let is_active_field = i == self.command_builder.selected_field;
                if is_active_field {
                    spans.push(Span::styled(
                        format!(" {} ", name),
                        Style::default().fg(Color::Black).bg(Color::Cyan).add_modifier(Modifier::BOLD),
                    ));
                } else {
                    spans.push(Span::styled(format!(" {} ", name), Style::default().fg(Color::DarkGray)));
                }
                if i < 3 { spans.push(Span::styled(" │ ", Style::default().fg(Color::DarkGray))); }
            }
            spans.push(Span::styled(format!("  ({}) ", self.suggestions_cache.len()), Style::default().fg(Color::DarkGray)));
            if suggestion_focused {
                spans.push(Span::styled("[↑ Back · Space Select]", Style::default().fg(Color::DarkGray)));
            } else if cmd_active {
                spans.push(Span::styled("[Tab Complete · ↓ Browse]", Style::default().fg(Color::DarkGray)));
            }
            Line::from(spans)
        };

        let items_area = area;

        if self.suggestions_cache.is_empty() {
            let msg = if self.options.is_none() { "(Sync metadata to see suggestions)" } else { "(No matches)" };
            f.render_widget(
                Paragraph::new(Line::from(Span::styled(msg, Style::default().fg(Color::DarkGray).add_modifier(Modifier::ITALIC))))
                    .block(Block::default().borders(Borders::ALL).border_type(border_type).title(title).border_style(Style::default().fg(border_color))),
                items_area,
            );
            return;
        }

        let inner_w = items_area.width.saturating_sub(2) as usize;
        let inner_h = items_area.height.saturating_sub(2).max(1) as usize;
        let max_display = self.suggestions_cache.iter().map(|s| s.display.len()).max().unwrap_or(10);
        let max_reason = self.suggestions_cache.iter().map(|s| s.reason.len()).max().unwrap_or(0);
        let cell_w = (4 + max_display + if max_reason > 0 { 2 + max_reason } else { 0 }).clamp(18, 38);
        let num_cols = (inner_w / cell_w).max(1);
        self.suggestion_cols.set(num_cols);

        let sel_idx = self.suggestion_index.unwrap_or(0);
        let sel_row = sel_idx / num_cols;
        let scroll_row = if sel_row >= inner_h { sel_row - inner_h + 1 } else { 0 };

        let mut lines: Vec<Line> = Vec::new();
        for row in scroll_row..(scroll_row + inner_h) {
            let mut spans: Vec<Span> = Vec::new();
            for col in 0..num_cols {
                let i = row * num_cols + col;
                if i >= self.suggestions_cache.len() { break; }
                let item = &self.suggestions_cache[i];
                let is_sel = self.suggestion_index == Some(i);

                let (item_style, reason_style, check) = if is_sel && item.already_selected {
                    (Style::default().fg(Color::Black).bg(Color::Green).add_modifier(Modifier::BOLD),
                     Style::default().fg(Color::Black).bg(Color::Green), "✓")
                } else if is_sel {
                    (Style::default().fg(Color::Black).bg(Color::Cyan).add_modifier(Modifier::BOLD),
                     Style::default().fg(Color::Black).bg(Color::Cyan), ">")
                } else if item.already_selected {
                    (Style::default().fg(Color::Green), Style::default().fg(Color::DarkGray), "✓")
                } else if item.is_semantic {
                    (Style::default().fg(Color::Yellow), Style::default().fg(Color::DarkGray), " ")
                } else {
                    (Style::default().fg(Color::Gray), Style::default().fg(Color::DarkGray), " ")
                };

                let display_str = format!("{} {}", check, item.display);
                let reason_str = if !item.reason.is_empty() { format!(" {}", item.reason) } else { String::new() };
                let pad = cell_w.saturating_sub(display_str.len() + reason_str.len());
                spans.push(Span::styled(display_str, item_style));
                if !reason_str.is_empty() { spans.push(Span::styled(reason_str, reason_style)); }
                spans.push(Span::raw(" ".repeat(pad.max(1))));
            }
            if !spans.is_empty() { lines.push(Line::from(spans)); }
        }

        f.render_widget(
            Paragraph::new(lines).block(
                Block::default().borders(Borders::ALL).border_type(border_type).title(title).border_style(Style::default().fg(border_color)),
            ),
            items_area,
        );
    }

    fn normalize_na(s: &str) -> &str {
        let trimmed = s.trim();
        if trimmed.eq_ignore_ascii_case("na") || trimmed.eq_ignore_ascii_case("n/a") {
            "-"
        } else {
            s
        }
    }

    fn normalize_price(s: &str) -> String {
        let trimmed = s.trim();
        if trimmed.eq_ignore_ascii_case("na") || trimmed.eq_ignore_ascii_case("n/a")
            || trimmed == "-" || trimmed.is_empty()
        {
            "0.0".to_string()
        } else {
            trimmed.to_string()
        }
    }

    fn render_results(&self, f: &mut Frame, area: Rect, active: bool) {
        let is_focused = active && self.active_section == PricingSection::Results;
        let border_type = if is_focused { BorderType::Thick } else { BorderType::Plain };
        let border_color = if is_focused { Color::Green } else { Color::DarkGray };
        let title_style = if is_focused {
            Style::default().fg(Color::Green).add_modifier(Modifier::BOLD)
        } else {
            Style::default().fg(Color::DarkGray)
        };

        if self.loading {
            f.render_widget(
                Paragraph::new(Line::from(Span::styled(
                    "Loading pricing data...",
                    Style::default().fg(Color::Yellow),
                )))
                .alignment(Alignment::Center)
                .block(
                    Block::default()
                        .borders(Borders::ALL)
                        .border_type(border_type)
                        .title(" Results ")
                        .border_style(Style::default().fg(border_color)),
                ),
                area,
            );
            return;
        }

        if let Some(ref error) = self.error_message {
            f.render_widget(
                Paragraph::new(Line::from(Span::styled(
                    format!("Error: {}", error),
                    Style::default().fg(Color::Red),
                )))
                .alignment(Alignment::Center)
                .block(
                    Block::default()
                        .borders(Borders::ALL)
                        .title(" Error ")
                        .border_style(Style::default().fg(Color::Red)),
                ),
                area,
            );
            return;
        }

        if self.filtered_items.is_empty() {
            f.render_widget(
                Paragraph::new(Line::from(Span::styled(
                    "No results found. Press Enter to submit query.",
                    Style::default().fg(Color::DarkGray),
                )))
                .alignment(Alignment::Center)
                .block(
                    Block::default()
                        .borders(Borders::ALL)
                        .border_type(border_type)
                        .title(Line::from(vec![
                            Span::styled(if is_focused { " > " } else { "   " }, title_style),
                            Span::styled(" Results (0) ", title_style),
                        ]))
                        .border_style(Style::default().fg(border_color)),
                ),
                area,
            );
            return;
        }

        // Collect all unique attribute keys preserving API insertion order (from first item)
        let mut attr_keys_ordered: Vec<String> = Vec::new();
        let mut attr_keys_seen: std::collections::HashSet<String> = std::collections::HashSet::new();
        let mut pricing_models: std::collections::HashSet<String> = std::collections::HashSet::new();
        
        for item in &self.filtered_items {
            for key in item.attributes.keys() {
                if attr_keys_seen.insert(key.clone()) {
                    attr_keys_ordered.push(key.clone());
                }
            }
            for price in &item.prices {
                pricing_models.insert(price.pricing_model.clone());
            }
        }

        let attr_keys_sorted = attr_keys_ordered; // keep API order
        
        let mut pricing_models_sorted: Vec<String> = pricing_models.into_iter().collect();
        pricing_models_sorted.sort();

        // Build header: No | Product | Region | [Attributes...] | Min Price | Max Price
        // Frozen columns: No, Product, Region
        // Scrollable columns: attributes + Min Price + Max Price
        // Compute dynamic widths for frozen columns based on actual content
        let product_col_w = self.filtered_items.iter()
            .map(|i| i.product.len())
            .max().unwrap_or(7)
            .max(7) as u16 + 2; // +2 padding
        let region_col_w = self.filtered_items.iter()
            .map(|i| i.region.len())
            .max().unwrap_or(6)
            .max(6) as u16 + 2;

        let frozen_header_cells: Vec<Cell> = vec![
            Cell::from("No."),
            Cell::from("Product"),
            Cell::from("Region"),
        ].into_iter().map(|c| c.style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD))).collect();

        let mut scrollable_headers: Vec<String> = Vec::new();
        for attr_key in &attr_keys_sorted {
            scrollable_headers.push(attr_key.clone());
        }
        scrollable_headers.push("Min Price".to_string());
        scrollable_headers.push("Max Price".to_string());

        // Compute content-based widths for scrollable columns
        let scrollable_col_widths: Vec<u16> = (0..scrollable_headers.len()).map(|i| {
            if i < attr_keys_sorted.len() {
                let header_w = attr_keys_sorted[i].len() as u16;
                let content_w = self.filtered_items.iter()
                    .map(|item| {
                        item.attributes.get(&attr_keys_sorted[i])
                            .and_then(|v| v.as_ref())
                            .map(|v| v.len())
                            .unwrap_or(1) as u16
                    })
                    .max().unwrap_or(1);
                header_w.max(content_w).max(8) + 2
            } else {
                // Min Price / Max Price
                14
            }
        }).collect();

        // Store total scrollable column count for key handler
        self.total_scrollable_cols.set(scrollable_headers.len());

        // Determine which scrollable columns fit in the remaining width (from offset 0 first)
        let frozen_width: u16 = 5 + product_col_w + region_col_w;
        let available_width = area.width.saturating_sub(frozen_width + 2 + 1); // 2 for borders, 1 for separator

        // Check how many columns fit from offset 0 to decide if scrolling is needed
        let mut all_fit_count = 0;
        let mut all_fit_width: u16 = 0;
        for i in 0..scrollable_headers.len() {
            let col_w = scrollable_col_widths[i];
            if all_fit_width + col_w > available_width && all_fit_count > 0 {
                break;
            }
            all_fit_width += col_w;
            all_fit_count += 1;
        }
        let all_columns_fit = all_fit_count >= scrollable_headers.len();

        // Clamp h_scroll_offset locally for rendering (key handler caps the actual value)
        let h_offset = if all_columns_fit {
            0
        } else {
            let max_offset = scrollable_headers.len().saturating_sub(1);
            self.h_scroll_offset.min(max_offset)
        };

        let mut visible_scrollable_count = 0;
        let mut used_width: u16 = 0;
        for i in h_offset..scrollable_headers.len() {
            let col_w = scrollable_col_widths[i];
            if used_width + col_w > available_width && visible_scrollable_count > 0 {
                break;
            }
            used_width += col_w;
            visible_scrollable_count += 1;
        }
        self.visible_scrollable_cols.set(visible_scrollable_count);

        // Build final header row
        let mut header_cells = frozen_header_cells;
        for i in h_offset..(h_offset + visible_scrollable_count).min(scrollable_headers.len()) {
            header_cells.push(
                Cell::from(scrollable_headers[i].clone())
                    .style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD))
            );
        }
        let header_row = Row::new(header_cells).height(1);

        // Calculate how many rows fit in the visible area (minus header + borders)
        let visible_rows = area.height.saturating_sub(3) as usize; // 2 borders + 1 header
        let total_items = self.filtered_items.len();
        let total_pages = (total_items + self.results_per_page - 1) / self.results_per_page;

        // Vertical scroll within the current page: keep selected item visible
        let page_start = self.results_page * self.results_per_page;
        let local_sel = self.selected.saturating_sub(page_start);
        let v_scroll = if local_sel >= visible_rows {
            local_sel - visible_rows + 1
        } else {
            0
        };

        // Build rows for current page only, capped at results_per_page
        let start_idx = page_start;
        let rows: Vec<Row> = self
            .filtered_items
            .iter()
            .enumerate()
            .skip(start_idx + v_scroll)
            .take(self.results_per_page.min(visible_rows))
            .map(|(idx, item)| {
                let style = if idx == self.selected {
                    Style::default().bg(Color::DarkGray).fg(Color::White)
                } else {
                    Style::default()
                };
                
                let mut cells: Vec<Cell> = vec![
                    Cell::from(format!("{}", idx + 1)),
                    Cell::from(item.product.clone()),
                    Cell::from(item.region.clone()),
                ];
                
                // Add only visible scrollable columns
                for i in h_offset..(h_offset + visible_scrollable_count).min(scrollable_headers.len()) {
                    if i < attr_keys_sorted.len() {
                        let attr_key = &attr_keys_sorted[i];
                        let value = item.attributes.get(attr_key)
                            .and_then(|v| v.clone())
                            .unwrap_or_else(|| "-".to_string());
                        cells.push(Cell::from(Self::normalize_na(&value).to_string()));
                    } else if i == scrollable_headers.len() - 2 {
                        // Min Price
                        let v = item.min_price.clone().unwrap_or_else(|| "-".to_string());
                        cells.push(Cell::from(Self::normalize_price(&v)));
                    } else {
                        // Max Price
                        let v = item.max_price.clone().unwrap_or_else(|| "-".to_string());
                        cells.push(Cell::from(Self::normalize_price(&v)));
                    }
                }
                
                Row::new(cells).style(style)
            })
            .collect();

        // Build constraints: frozen columns + visible scrollable columns
        // Distribute remaining space evenly across all columns
        let total_col_count = 3 + visible_scrollable_count; // No + Product + Region + scrollable
        let base_widths: Vec<u16> = {
            let mut w = vec![5u16, product_col_w, region_col_w];
            for i in h_offset..(h_offset + visible_scrollable_count).min(scrollable_headers.len()) {
                w.push(scrollable_col_widths[i]);
            }
            w
        };
        let total_base: u16 = base_widths.iter().sum();
        let table_inner_width = area.width.saturating_sub(2); // borders
        let extra = table_inner_width.saturating_sub(total_base);
        let per_col_extra = if total_col_count > 0 { extra / total_col_count as u16 } else { 0 };

        let constraints: Vec<Constraint> = base_widths.iter().map(|&w| {
            Constraint::Length(w + per_col_extra)
        }).collect();

        f.render_widget(
            Table::new(rows, constraints)
                .header(header_row)
                .block(
                    Block::default()
                        .borders(Borders::ALL)
                        .border_type(border_type)
                        .title(Line::from({
                            let mut spans = vec![
                                Span::styled(if is_focused { " > " } else { "   " }, title_style),
                                Span::styled(
                                    format!(" Results ({} total, page {}/{}) ",
                                        self.filtered_items.len(),
                                        self.results_page + 1,
                                        total_pages.max(1)),
                                    title_style,
                                ),
                            ];
                            // Provider counts
                            let mut provider_counts: std::collections::BTreeMap<String, usize> = std::collections::BTreeMap::new();
                            for item in &self.filtered_items {
                                let provider = item.product.split_whitespace().next().unwrap_or(&item.product).to_string();
                                *provider_counts.entry(provider).or_insert(0) += 1;
                            }
                            if !provider_counts.is_empty() {
                                spans.push(Span::styled("[ ", Style::default().fg(Color::DarkGray)));
                                for (i, (provider, count)) in provider_counts.iter().enumerate() {
                                    if i > 0 {
                                        spans.push(Span::styled("  ", Style::default()));
                                    }
                                    spans.push(Span::styled(
                                        format!("{}: {}", provider, count),
                                        Style::default().fg(Color::Cyan),
                                    ));
                                }
                                spans.push(Span::styled(" ]", Style::default().fg(Color::DarkGray)));
                            }
                            if !all_columns_fit {
                                spans.push(Span::styled(
                                    format!(" ←→ col {}/{} ", h_offset + 1, scrollable_headers.len()),
                                    Style::default().fg(Color::Cyan),
                                ));
                            }
                            spans
                        }))
                        .border_style(Style::default().fg(border_color)),
                ),
            area,
        );
    }

    fn render_price_details(&self, f: &mut Frame, area: Rect) {
        let item = match self.filtered_items.get(self.selected) {
            Some(i) => i,
            None => {
                f.render_widget(
                    Paragraph::new(Line::from(vec![
                        Span::styled("Select a result to see pricing details", Style::default().fg(Color::DarkGray).add_modifier(Modifier::ITALIC)),
                    ]))
                    .block(
                        Block::default()
                            .borders(Borders::ALL)
                            .title(" Price Details ")
                            .border_style(Style::default().fg(Color::DarkGray)),
                    ),
                    area,
                );
                return;
            }
        };

        if item.prices.is_empty() {
            f.render_widget(
                Paragraph::new(Line::from(Span::styled(
                    "No price details available",
                    Style::default().fg(Color::DarkGray).add_modifier(Modifier::ITALIC),
                )))
                .block(
                    Block::default()
                        .borders(Borders::ALL)
                        .title(" Price Details ")
                        .border_style(Style::default().fg(Color::DarkGray)),
                ),
                area,
            );
            return;
        }

        // Check if any price has tiered rates (more than 1 rate)
        let has_tiers = item.prices.iter().any(|p| p.rates.len() > 1);

        if has_tiers {
            self.render_price_details_tiered(f, area, item);
        } else {
            self.render_price_details_flat(f, area, item);
        }
    }

    fn render_price_details_tiered(&self, f: &mut Frame, area: Rect, item: &PricingDisplayItem) {
        let title = Line::from(vec![
            Span::styled(" > Price Details - ", Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD)),
            Span::styled(
                format!("{} / {} ({} models, tiered)", item.region, item.product, item.prices.len()),
                Style::default().fg(Color::White),
            ),
            Span::styled(" ", Style::default()),
        ]);

        // Build rows: for each price, show a header row then rate tiers
        let mut rows: Vec<Row> = Vec::new();
        for p in &item.prices {
            // Price summary row
            let model_label = format!(
                "{}{}{}",
                p.pricing_model,
                if !p.purchase_option.is_empty() { format!(" ({})", p.purchase_option) } else { String::new() },
                if !p.year.is_empty() && p.year != "-" { format!(" {}yr", p.year) } else { String::new() },
            );
            rows.push(Row::new(vec![
                Cell::from(model_label).style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                Cell::from(""),
                Cell::from(""),
                Cell::from(p.unit.clone()).style(Style::default().fg(Color::DarkGray)),
                Cell::from(if p.upfront_fee.is_empty() || p.upfront_fee == "0" { String::new() } else { format!("upfront: {}", p.upfront_fee) })
                    .style(Style::default().fg(Color::DarkGray)),
            ]));

            if p.rates.len() <= 1 {
                // Single rate, show inline
                let price_str = p.rates.first().map(|r| r.price.clone()).unwrap_or(p.price.clone());
                rows.push(Row::new(vec![
                    Cell::from("  flat"),
                    Cell::from(price_str).style(Style::default().fg(Color::Green)),
                    Cell::from(""),
                    Cell::from(""),
                    Cell::from(""),
                ]));
            } else {
                // Multiple tiers
                for r in &p.rates {
                    let range = if r.start_range.is_empty() && r.end_range.is_empty() {
                        String::new()
                    } else {
                        let start = if r.start_range.is_empty() { "0".to_string() } else { r.start_range.clone() };
                        let end = if r.end_range.is_empty() || r.end_range == "Inf" { "∞".to_string() } else { r.end_range.clone() };
                        format!("{} - {}", start, end)
                    };
                    rows.push(Row::new(vec![
                        Cell::from(format!("  {}", range)).style(Style::default().fg(Color::DarkGray)),
                        Cell::from(r.price.clone()).style(Style::default().fg(Color::Green)),
                        Cell::from(""),
                        Cell::from(""),
                        Cell::from(""),
                    ]));
                }
            }
        }

        let constraints = vec![
            Constraint::Min(20),   // Model / Range
            Constraint::Min(12),   // Price
            Constraint::Length(0), // spacer
            Constraint::Min(10),   // Unit
            Constraint::Min(14),   // Upfront
        ];

        f.render_widget(
            Table::new(rows, constraints)
                .block(
                    Block::default()
                        .borders(Borders::ALL)
                        .title(title)
                        .border_style(Style::default().fg(Color::Cyan)),
                ),
            area,
        );
    }

    fn render_price_details_flat(&self, f: &mut Frame, area: Rect, item: &PricingDisplayItem) {
        let cols = Layout::default()
            .direction(Direction::Horizontal)
            .constraints(vec![
                Constraint::Percentage(50),
                Constraint::Percentage(50),
            ])
            .split(area);

        let visible_rows = cols[0].height.saturating_sub(3) as usize; // borders(2) + header(1)
        // Split evenly across two columns
        let split = if item.prices.len() <= visible_rows {
            item.prices.len() // all fit in left, right empty
        } else {
            (item.prices.len() + 1) / 2 // ceil half
        };
        let (left_prices, right_prices) = (&item.prices[..split], &item.prices[split..]);

        let title = Line::from(vec![
            Span::styled(" > Price Details - ", Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD)),
            Span::styled(
                format!("{} / {} ({} models)", item.region, item.product, item.prices.len()),
                Style::default().fg(Color::White),
            ),
            Span::styled(" ", Style::default()),
        ]);

        // Compute dynamic column widths based on actual content
        let all_prices = &item.prices;
        let model_w = all_prices.iter().map(|p| p.pricing_model.len()).max().unwrap_or(5).max(5) as u16 + 2;
        let price_w = all_prices.iter().map(|p| p.price.len()).max().unwrap_or(5).max(5) as u16 + 2;
        let unit_w = all_prices.iter().map(|p| p.unit.len()).max().unwrap_or(4).max(4) as u16 + 2;
        let upfront_w = all_prices.iter().map(|p| p.upfront_fee.len()).max().unwrap_or(7).max(7) as u16 + 2;
        let year_w: u16 = 4;
        let option_w = all_prices.iter().map(|p| {
            let base = p.purchase_option.len();
            let interrupt = p.interruption_max_pct.as_deref().map(|pct| {
                if p.purchase_option.is_empty() { format!("Interrupt {}%", pct).len() }
                else { format!("{} Interrupt {}%", p.purchase_option, pct).len() }
            }).unwrap_or(0);
            base.max(interrupt)
        }).max().unwrap_or(6).max(6) as u16 + 2;

        let make_table = |prices: &[PriceInfo]| -> Table {
            let header_cells = vec![
                Cell::from("Model").style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                Cell::from("Price").style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                Cell::from("Unit").style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                Cell::from("Upfront").style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                Cell::from("Yr").style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                Cell::from("Option").style(Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
            ];
            let header_row = Row::new(header_cells).height(1);

            let rows: Vec<Row> = prices.iter().enumerate().map(|(i, p)| {
                let row_style = if i % 2 == 0 {
                    Style::default().fg(Color::Gray)
                } else {
                    Style::default().fg(Color::White)
                };
                let option_cell = match p.interruption_max_pct.as_deref() {
                    Some(pct) => {
                        let text = if p.purchase_option.is_empty() {
                            format!("Interrupt {}%", pct)
                        } else {
                            format!("{} Interrupt {}%", p.purchase_option, pct)
                        };
                        Cell::from(text).style(Style::default().fg(Color::Yellow))
                    }
                    None => Cell::from(if p.purchase_option.is_empty() { "-".to_string() } else { p.purchase_option.clone() }),
                };
                Row::new(vec![
                    Cell::from(p.pricing_model.clone()),
                    Cell::from(p.price.clone()).style(Style::default().fg(Color::Green)),
                    Cell::from(p.unit.clone()),
                    Cell::from(if p.upfront_fee.is_empty() || p.upfront_fee == "0" { "-".to_string() } else { p.upfront_fee.clone() }),
                    Cell::from(if p.year.is_empty() { "-".to_string() } else { p.year.clone() }),
                    option_cell,
                ]).style(row_style)
            }).collect();

            let constraints = vec![
                Constraint::Min(model_w),
                Constraint::Min(price_w),
                Constraint::Min(unit_w),
                Constraint::Min(upfront_w),
                Constraint::Length(year_w),
                Constraint::Min(option_w),
            ];

            Table::new(rows, constraints).header(header_row)
        };

        // Left table (with title)
        f.render_widget(
            make_table(left_prices).block(
                Block::default()
                    .borders(Borders::ALL)
                    .title(title)
                    .border_style(Style::default().fg(Color::Cyan)),
            ),
            cols[0],
        );

        // Right table
        if right_prices.is_empty() {
            f.render_widget(
                Paragraph::new("").block(
                    Block::default()
                        .borders(Borders::ALL)
                        .border_style(Style::default().fg(Color::Cyan)),
                ),
                cols[1],
            );
        } else {
            f.render_widget(
                make_table(right_prices).block(
                    Block::default()
                        .borders(Borders::ALL)
                        .title(" (cont.) ")
                        .border_style(Style::default().fg(Color::Cyan)),
                ),
                cols[1],
            );
        }
    }

    fn render_help(&self, f: &mut Frame, area: Rect, _active: bool) {
        let text = match self.active_section {
            PricingSection::Header => Line::from(vec![
                Span::styled("[←→] ", Style::default().fg(Color::Cyan)),
                Span::raw("Switch View  "),
                Span::styled("[↓] ", Style::default().fg(Color::Yellow)),
                Span::raw("Command  "),
                Span::styled("[F3] ", Style::default().fg(Color::Green)),
                Span::raw("Refresh Metadata  "),
                Span::styled("[Esc] ", Style::default().fg(Color::Red)),
                Span::raw("Quit"),
            ]),
            PricingSection::Command => match self.builder_focus {
                BuilderFocus::Field => Line::from(vec![
                    Span::styled("[←→] ", Style::default().fg(Color::Cyan)),
                    Span::raw("Switch Field  "),
                    Span::styled("[Tab] ", Style::default().fg(Color::Cyan)),
                    Span::raw("Complete  "),
                    Span::styled("[Enter] ", Style::default().fg(Color::Green)),
                    Span::raw("Submit  "),
                    #[cfg(feature = "raw_command")]
                    Span::styled("[F1] ", Style::default().fg(Color::Yellow)),
                    #[cfg(feature = "raw_command")]
                    Span::raw("Raw Mode  "),
                    Span::styled("[F2] ", Style::default().fg(Color::Yellow)),
                    Span::raw("Reset  "),
                    Span::styled("[F3] ", Style::default().fg(Color::Green)),
                    Span::raw("Refresh Metadata"),
                ]),
                BuilderFocus::Suggestions => Line::from(vec![
                    Span::styled("[↑↓←→] ", Style::default().fg(Color::Cyan)),
                    Span::raw("Navigate  "),
                    Span::styled("[Space] ", Style::default().fg(Color::Green)),
                    Span::raw("Select  "),
                    Span::styled("[↑] ", Style::default().fg(Color::Yellow)),
                    Span::raw("Back to Input  "),
                    Span::styled("[Enter] ", Style::default().fg(Color::Green)),
                    Span::raw("Submit Query"),
                ]),
            },
            PricingSection::Results => Line::from(vec![
                Span::styled("[j/k] ", Style::default().fg(Color::Cyan)),
                Span::raw("Scroll  "),
                Span::styled("[←→] ", Style::default().fg(Color::Cyan)),
                Span::raw("H-Scroll  "),
                Span::styled("[↑] ", Style::default().fg(Color::Yellow)),
                Span::raw("Command  "),
                Span::styled("[Esc] ", Style::default().fg(Color::Red)),
                Span::raw("Quit"),
            ]),
        };

        f.render_widget(
            Paragraph::new(text).block(
                Block::default()
                    .borders(Borders::ALL)
                    .title(" Help ")
                    .border_style(Style::default().fg(Color::DarkGray)),
            ),
            area,
        );
    }

    // ── Filtering ─────────────────────────────────────────────────────────────

    pub fn filter_items(&mut self) {
        let r_tags: Vec<String> = self
            .command_builder
            .region_tags
            .iter()
            .map(|s| s.to_lowercase())
            .collect();
        let prod_tags: Vec<String> = self
            .command_builder
            .product_tags
            .iter()
            .map(|s| s.to_lowercase())
            .collect();
        let attr_tags: Vec<String> = self
            .command_builder
            .attribute_tags
            .iter()
            .map(|s| s.to_lowercase())
            .collect();
        let price_tags: Vec<String> = self
            .command_builder
            .price_tags
            .iter()
            .map(|s| s.to_lowercase())
            .collect();

        if r_tags.is_empty()
            && prod_tags.is_empty()
            && attr_tags.is_empty()
            && price_tags.is_empty()
        {
            self.filtered_items = self.items.clone();
            self.selected = 0;
            return;
        }

        self.filtered_items = self
            .items
            .iter()
            .filter(|item| {
                // Region: OR across selected region tags
                let region_ok = r_tags.is_empty()
                    || r_tags.iter().any(|t| {
                        item.region.to_lowercase().contains(t.as_str())
                    });
                // Product: OR across selected product tags
                let product_ok = prod_tags.is_empty()
                    || prod_tags.iter().any(|t| {
                        item.product.to_lowercase().contains(t.as_str())
                    });
                // Attrs: ALL selected attr tags must be present (AND)
                let attrs_ok = attr_tags.is_empty() || {
                    let flat = item
                        .attributes
                        .iter()
                        .map(|(k, v)| {
                            format!(
                                "{}={}",
                                k.to_lowercase(),
                                v.as_deref().unwrap_or("")
                            )
                        })
                        .collect::<Vec<_>>()
                        .join(" ");
                    attr_tags
                        .iter()
                        .all(|t| flat.contains(t.as_str()))
                };

                // Price: local filtering by comparing with min_price (N/A treated as 0.0)
                let price_ok = price_tags.is_empty() || {
                    let mp_val = item.min_price.as_ref().map(|mp| {
                        let s = mp.trim();
                        if s.eq_ignore_ascii_case("na") || s.eq_ignore_ascii_case("n/a") || s == "-" {
                            0.0f64
                        } else {
                            s.parse::<f64>().unwrap_or(0.0)
                        }
                    }).unwrap_or(0.0);
                    price_tags.iter().all(|pt| {
                        if pt.starts_with(">=") {
                            if let Ok(v) = pt[2..].parse::<f64>() { return mp_val >= v; }
                        } else if pt.starts_with("<=") {
                            if let Ok(v) = pt[2..].parse::<f64>() { return mp_val <= v; }
                        } else if pt.starts_with('>') {
                            if let Ok(v) = pt[1..].parse::<f64>() { return mp_val > v; }
                        } else if pt.starts_with('<') {
                            if let Ok(v) = pt[1..].parse::<f64>() { return mp_val < v; }
                        }
                        true
                    })
                };

                region_ok && product_ok && attrs_ok && price_ok
            })
            .cloned()
            .collect();

        self.selected = 0;
    }



    // ── Data loading ──────────────────────────────────────────────────────────

    pub fn load_options(&mut self) {
        match crate::commands::pricing::load_metadata_async() {
            Ok(options) => {
                self.options = Some(options);
                self.update_suggestions();
                self.filter_items();
            }
            Err(e) => {
                self.error_message = Some(format!("Failed to load metadata: {}", e));
            }
        }
    }


    fn stringify_json(v: &Option<serde_json::Value>) -> Option<String> {
        let s = match v {
            Some(serde_json::Value::String(s)) => s.clone(),
            Some(serde_json::Value::Number(n)) => format!("{}", n),
            Some(serde_json::Value::Bool(b)) => format!("{}", b),
            _ => return None,
        };
        if s.trim().is_empty() { None } else { Some(s) }
    }

    pub fn convert_response(response: PricingApiResponse) -> Vec<PricingDisplayItem> {
        response
            .data
            .into_iter()
            .map(|item| {
                let mut prices: Vec<PriceInfo> = item.prices.iter().map(|p| {
                    let display_price = if let Some(rates) = &p.rates {
                        if let Some(first_rate) = rates.first() {
                            Self::stringify_json(&first_rate.price).unwrap_or_else(|| "N/A".to_string())
                        } else {
                            "N/A".to_string()
                        }
                    } else {
                        "N/A".to_string()
                    };

                    let rate_infos: Vec<RateInfo> = if let Some(rates) = &p.rates {
                        rates.iter().map(|r| RateInfo {
                            price: Self::stringify_json(&r.price).unwrap_or_else(|| "N/A".to_string()),
                            start_range: Self::stringify_json(&r.start_range).unwrap_or_default(),
                            end_range: Self::stringify_json(&r.end_range).unwrap_or_default(),
                        }).collect()
                    } else {
                        Vec::new()
                    };

                    PriceInfo {
                        pricing_model: p.pricing_model.clone().unwrap_or_else(|| "OnDemand".to_string()),
                        price: display_price,
                        unit: p.unit.clone().unwrap_or_default(),
                        upfront_fee: Self::stringify_json(&p.upfront_fee).unwrap_or_default(),
                        purchase_option: p.purchase_option.clone().unwrap_or_default(),
                        year: Self::stringify_json(&p.year).unwrap_or_default(),
                        interruption_max_pct: Self::stringify_json(&p.interruption_max_pct),
                        rates: rate_infos,
                    }
                }).collect();

                // Fill empty units from the OnDemand model's unit
                let ondemand_unit = prices.iter()
                    .find(|p| p.pricing_model.eq_ignore_ascii_case("ondemand") && !p.unit.is_empty())
                    .map(|p| p.unit.clone());
                if let Some(ref fallback) = ondemand_unit {
                    for p in &mut prices {
                        if p.unit.is_empty() {
                            p.unit = fallback.clone();
                        }
                    }
                }

                PricingDisplayItem {
                    product: if item.provider.is_empty() {
                        item.product
                    } else {
                        format!("{} {}", item.provider, item.product)
                    },
                    region: item.region,
                    attributes: item.attributes.into_iter().map(|(k, v)| {
                        (k, v.map(|av| av.to_string()))
                    }).collect(),
                    prices,
                    min_price: Self::stringify_json(&item.min_price),
                    max_price: Self::stringify_json(&item.max_price),
                }
            })
            .collect()
    }
}
