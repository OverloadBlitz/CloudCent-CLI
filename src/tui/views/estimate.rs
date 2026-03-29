use anyhow::Result;
use crossterm::event::{KeyCode, KeyEvent};
use ratatui::{
    layout::{Alignment, Constraint, Direction, Layout, Rect},
    style::{Color, Modifier, Style},
    text::{Line, Span},
    widgets::{Block, BorderType, Borders, Paragraph},
    Frame,
};

use super::pricing::{
    BuilderFocus, CommandBuilderState, PricingDisplayItem,
};
use crate::tui::semantic::SuggestionItem;

// ── Resource Types & Templates ───────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq, Eq, serde::Serialize, serde::Deserialize)]
pub enum ResourceType {
    Archetype(String),
    Custom,
}

impl ResourceType {
    pub fn label(&self) -> String {
        match self {
            ResourceType::Archetype(id) => {
                // Capitalize first letter and replace underscores with spaces
                let s = id.replace('_', " ");
                let mut c = s.chars();
                match c.next() {
                    None => String::new(),
                    Some(f) => f.to_uppercase().collect::<String>() + c.as_str(),
                }
            }
            ResourceType::Custom => "Custom Resource".to_string(),
        }
    }

    /// Pull product templates from pricing options
    pub fn get_templates(&self, _options: &crate::commands::pricing::PricingOptions) -> Vec<String> {
        match self {
            ResourceType::Archetype(_id) => {
                Vec::new()
            }
            ResourceType::Custom => vec!["New Product".to_string()],
        }
    }
}

// ── Data Structures ──────────────────────────────────────────────────────────

#[derive(Debug, Clone, serde::Serialize, serde::Deserialize)]
pub struct SelectedPrice {
    pub product: String,
    pub region: String,
    pub price_display: String,
    pub unit: String,
    pub monthly_cost: f64,
}

#[derive(Clone)]
pub struct EstimateProduct {
    pub label: String,
    pub builder: CommandBuilderState,
    pub quantity: u32,
    pub results: Vec<PricingDisplayItem>,
    pub selected_price: Option<SelectedPrice>,
    pub result_selected: usize,
    pub fixed: bool,
}

impl EstimateProduct {
    pub fn new(label: &str, fixed: bool) -> Self {
        let mut builder = CommandBuilderState::new();
        if fixed {
            // Automatically add the product tag
            builder.product_tags.push(label.to_string());
        }
        Self {
            label: label.to_string(),
            builder,
            quantity: 1,
            results: Vec::new(),
            selected_price: None,
            result_selected: 0,
            fixed,
        }
    }

    pub fn monthly_total(&self) -> f64 {
        self.selected_price
            .as_ref()
            .map(|p| p.monthly_cost * self.quantity as f64)
            .unwrap_or(0.0)
    }
}

#[derive(Clone)]
pub struct EstimateResource {
    pub resource_type: ResourceType,
    pub name: String,
    pub products: Vec<EstimateProduct>,
    pub collapsed: bool,
}

impl EstimateResource {
    pub fn new(resource_type: ResourceType, name: String, options: Option<&crate::commands::pricing::PricingOptions>) -> Self {
        let mut products = Vec::new();
        if let Some(opts) = options {
            for t in resource_type.get_templates(opts) {
                products.push(EstimateProduct::new(&t, true));
            }
        }
        Self {
            resource_type,
            name,
            products,
            collapsed: false,
        }
    }

    pub fn subtotal(&self) -> f64 {
        self.products.iter().map(|p| p.monthly_total()).sum()
    }
}

// ── View State ───────────────────────────────────────────────────────────────

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EstimateSection {
    Header,
    Content,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EstimateFocus {
    ResourceTree,
    ProductEditor,
    ResultPicker,
    ResourceTypePicker,
    QuantityInput,
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum EstimateEvent {
    None,
    Quit,
    PrevView,
    NextView,
    SubmitProductQuery,
    OpenInPricing(usize, usize),
}

/// Flattened tree row for navigation
#[derive(Debug, Clone)]
enum TreeRow {
    Resource(usize),
    Product(usize, usize),
    NewResource(ResourceType),
}

#[derive(Clone)]
pub struct EstimateView {
    pub resources: Vec<EstimateResource>,
    pub active_section: EstimateSection,
    pub focus: EstimateFocus,
    /// Index into the flattened tree rows
    pub tree_cursor: usize,
    /// Resource type picker selection
    pub type_picker_index: usize,
    // Suggestion system (shared with ProductEditor)
    pub options: Option<std::sync::Arc<crate::commands::pricing::PricingOptions>>,
    pub suggestions_cache: Vec<SuggestionItem>,
    pub suggestion_index: Option<usize>,
    pub builder_focus: BuilderFocus,
    /// Loading state for product query
    pub loading: bool,
    pub error_message: Option<String>,
    /// Status message from last query (message, is_success)
    pub query_status: Option<(String, bool)>,
    /// Quantity input buffer
    pub qty_input: String,
}

impl EstimateView {
    pub fn new() -> Self {
        Self {
            resources: Vec::new(),
            active_section: EstimateSection::Content,
            focus: EstimateFocus::ResourceTree,
            tree_cursor: 0,
            type_picker_index: 0,
            options: None,
            suggestions_cache: Vec::new(),
            suggestion_index: None,
            builder_focus: BuilderFocus::Field,
            loading: false,
            error_message: None,
            query_status: None,
            qty_input: String::new(),
        }
    }

    /// Build a flat list of tree rows for navigation
    fn tree_rows(&self) -> Vec<TreeRow> {
        let mut rows = Vec::new();
        for (ri, res) in self.resources.iter().enumerate() {
            rows.push(TreeRow::Resource(ri));
            if !res.collapsed {
                for pi in 0..res.products.len() {
                    rows.push(TreeRow::Product(ri, pi));
                }
            }
        }
        
        // Add dynamic archetypes from product groups
        if let Some(opts) = &self.options {
            let mut group_names: Vec<String> = opts.product_groups.keys().cloned().collect();
            group_names.sort();
            for name in group_names {
                rows.push(TreeRow::NewResource(ResourceType::Archetype(name)));
            }
        }
        
        // Always allow a custom resource
        rows.push(TreeRow::NewResource(ResourceType::Custom));
        
        rows
    }

    /// Get the currently selected (resource_idx, product_idx) if on a Product row
    pub fn active_product(&self) -> Option<(usize, usize)> {
        let rows = self.tree_rows();
        if self.tree_cursor < rows.len() {
            if let TreeRow::Product(ri, pi) = rows[self.tree_cursor] {
                return Some((ri, pi));
            }
        }
        None
    }


    pub fn grand_total(&self) -> f64 {
        self.resources.iter().map(|r| r.subtotal()).sum()
    }

    // ── Key Handling ─────────────────────────────────────────────────────────

    pub fn handle_key(&mut self, key: KeyEvent) -> Result<EstimateEvent> {
        match self.active_section {
            EstimateSection::Header => self.handle_key_header(key),
            EstimateSection::Content => match self.focus {
                EstimateFocus::ResourceTree => self.handle_key_tree(key),
                EstimateFocus::ResourceTypePicker => self.handle_key_type_picker(key),
                EstimateFocus::ProductEditor => self.handle_key_editor(key),
                EstimateFocus::ResultPicker => self.handle_key_result_picker(key),
                EstimateFocus::QuantityInput => self.handle_key_qty_input(key),
            },
        }
    }

    fn handle_key_header(&mut self, key: KeyEvent) -> Result<EstimateEvent> {
        match key.code {
            KeyCode::Left => Ok(EstimateEvent::PrevView),
            KeyCode::Right => Ok(EstimateEvent::NextView),
            KeyCode::Down => {
                self.active_section = EstimateSection::Content;
                Ok(EstimateEvent::None)
            }
            KeyCode::Esc => Ok(EstimateEvent::Quit),
            _ => Ok(EstimateEvent::None),
        }
    }

    fn handle_key_tree(&mut self, key: KeyEvent) -> Result<EstimateEvent> {
        let rows = self.tree_rows();
        match key.code {
            KeyCode::Up => {
                if self.tree_cursor == 0 {
                    self.active_section = EstimateSection::Header;
                } else {
                    self.tree_cursor = self.tree_cursor.saturating_sub(1);
                    self.update_suggestions();
                    if let Some((ri, pi)) = self.active_product() {
                        if self.resources[ri].products[pi].results.is_empty() {
                            return Ok(EstimateEvent::SubmitProductQuery);
                        }
                    }
                }
            }
            KeyCode::Down => {
                if self.tree_cursor + 1 < rows.len() {
                    self.tree_cursor += 1;
                    self.update_suggestions();
                    if let Some((ri, pi)) = self.active_product() {
                        if self.resources[ri].products[pi].results.is_empty() {
                            return Ok(EstimateEvent::SubmitProductQuery);
                        }
                    }
                }
            }
            KeyCode::Char(' ') | KeyCode::Right => {
                if let Some(row) = rows.get(self.tree_cursor) {
                    match row {
                        TreeRow::Resource(ri) => {
                            self.resources[*ri].collapsed = !self.resources[*ri].collapsed;
                        }
                        TreeRow::Product(_ri, _pi) => {
                            self.focus = EstimateFocus::ProductEditor;
                            self.builder_focus = BuilderFocus::Field;
                            self.update_suggestions();
                        }
                        TreeRow::NewResource(rt) => {
                            let label = rt.label();
                            let name = format!("{} {}", label, self.resources.len() + 1);
                            let new_res = EstimateResource::new(rt.clone(), name, self.options.as_deref());
                            self.resources.push(new_res);
                            
                            let new_resource_idx = self.resources.len() - 1;
                            let rows = self.tree_rows();
                            if let Some(pos) = rows.iter().position(|r| {
                                if let TreeRow::Product(ri, pi) = r {
                                    *ri == new_resource_idx && *pi == 0
                                } else { false }
                            }) {
                                self.tree_cursor = pos;
                            }
                            
                            self.focus = EstimateFocus::ProductEditor;
                            self.builder_focus = BuilderFocus::Field;
                            self.update_suggestions();
                        }
                    }
                }
            }
            KeyCode::Enter => {
                if let Some(row) = rows.get(self.tree_cursor) {
                    match row {
                        TreeRow::Resource(ri) => {
                            self.resources[*ri].collapsed = !self.resources[*ri].collapsed;
                        }
                        TreeRow::Product(ri, pi) => {
                            return Ok(EstimateEvent::OpenInPricing(*ri, *pi));
                        }
                        TreeRow::NewResource(rt) => {
                            let label = rt.label();
                            let name = format!("{} {}", label, self.resources.len() + 1);
                            let new_res = EstimateResource::new(rt.clone(), name, self.options.as_deref());
                            self.resources.push(new_res);
                            
                            // Re-calculate tree rows and find the first product of the newly added resource
                            let new_resource_idx = self.resources.len() - 1;
                            let rows = self.tree_rows();
                            if let Some(pos) = rows.iter().position(|r| {
                                if let TreeRow::Product(ri, pi) = r {
                                    *ri == new_resource_idx && *pi == 0
                                } else { false }
                            }) {
                                self.tree_cursor = pos;
                            }
                            
                            self.focus = EstimateFocus::ProductEditor;
                            self.builder_focus = BuilderFocus::Field;
                            self.update_suggestions();
                        }
                    }
                }
            }
            KeyCode::Char('d') | KeyCode::Char('D') => {
                if self.tree_cursor < rows.len() {
                    match &rows[self.tree_cursor] {
                        TreeRow::Resource(ri) => {
                            self.resources.remove(*ri);
                        }
                        TreeRow::Product(ri, pi) => {
                            self.resources[*ri].products.remove(*pi);
                        }
                        _ => {}
                    }
                    let new_len = self.tree_rows().len();
                    if self.tree_cursor >= new_len && new_len > 0 {
                        self.tree_cursor = new_len - 1;
                    }
                }
            }
            KeyCode::Char('p') | KeyCode::Char('P') => {
                if let Some(TreeRow::Product(ri, pi)) = rows.get(self.tree_cursor) {
                    return Ok(EstimateEvent::OpenInPricing(*ri, *pi));
                }
            }
            KeyCode::Esc => return Ok(EstimateEvent::Quit),
            _ => {}
        }
        Ok(EstimateEvent::None)
    }

    fn handle_key_type_picker(&mut self, key: KeyEvent) -> Result<EstimateEvent> {
        let mut types = Vec::new();
        if let Some(opts) = &self.options {
            let mut group_names: Vec<String> = opts.product_groups.keys().cloned().collect();
            group_names.sort();
            for name in group_names {
                types.push(ResourceType::Archetype(name));
            }
        }
        types.push(ResourceType::Custom);

        match key.code {
            KeyCode::Up => {
                self.type_picker_index = self.type_picker_index.saturating_sub(1);
            }
            KeyCode::Down => {
                if self.type_picker_index + 1 < types.len() {
                    self.type_picker_index += 1;
                }
            }
            KeyCode::Enter => {
                let rt = types[self.type_picker_index].clone();
                let label = rt.label();
                let name = format!("{} {}", label, self.resources.len() + 1);
                let new_res = EstimateResource::new(rt, name, self.options.as_deref());
                self.resources.push(new_res);
                
                // Focus on the first product of the newly added resource
                let new_resource_idx = self.resources.len() - 1;
                let rows = self.tree_rows();
                if let Some(pos) = rows.iter().position(|r| {
                    if let TreeRow::Product(ri, pi) = r {
                        *ri == new_resource_idx && *pi == 0
                    } else { false }
                }) {
                    self.tree_cursor = pos;
                }
                self.focus = EstimateFocus::ProductEditor;
                self.builder_focus = BuilderFocus::Field;
                self.update_suggestions();
            }
            KeyCode::Left | KeyCode::Esc => {
                self.focus = EstimateFocus::ResourceTree;
            }
            _ => {}
        }
        Ok(EstimateEvent::None)
    }


    fn handle_key_editor(&mut self, key: KeyEvent) -> Result<EstimateEvent> {
        let (ri, pi) = match self.active_product() {
            Some(p) => p,
            None => {
                self.focus = EstimateFocus::ResourceTree;
                return Ok(EstimateEvent::None);
            }
        };

        match self.builder_focus {
            BuilderFocus::Field => {
                match key.code {
                    KeyCode::Left | KeyCode::Esc => {
                        self.focus = EstimateFocus::ResourceTree;
                        self.suggestions_cache.clear();
                    }
                    KeyCode::Up => {
                        let mut field = self.resources[ri].products[pi].builder.selected_field;
                        let is_fixed = self.resources[ri].products[pi].fixed;
                        if field == 0 {
                            self.focus = EstimateFocus::ResourceTree;
                            self.suggestions_cache.clear();
                        } else {
                            field -= 1;
                            if is_fixed && field == 0 {
                                if field == 0 {
                                    self.focus = EstimateFocus::ResourceTree;
                                } else {
                                    field -= 1;
                                }
                            }
                            self.resources[ri].products[pi].builder.selected_field = field;
                            self.resources[ri].products[pi].builder.search_input.clear();
                            self.suggestion_index = None;
                            self.update_suggestions();
                        }
                    }
                    KeyCode::Down => {
                        let mut field = self.resources[ri].products[pi].builder.selected_field;
                        let is_fixed = self.resources[ri].products[pi].fixed;
                        if field >= 3 {
                            // Move to quantity
                            self.qty_input = self.resources[ri].products[pi].quantity.to_string();
                            self.focus = EstimateFocus::QuantityInput;
                        } else {
                            field += 1;
                            if is_fixed && field == 0 {
                                field += 1;
                            }
                            self.resources[ri].products[pi].builder.selected_field = field;
                            self.resources[ri].products[pi].builder.search_input.clear();
                            self.suggestion_index = None;
                            self.update_suggestions();
                        }
                    }
                    KeyCode::Right => {
                        if !self.suggestions_cache.is_empty() {
                            self.builder_focus = BuilderFocus::Suggestions;
                            if self.suggestion_index.is_none() {
                                self.suggestion_index = Some(0);
                            }
                        }
                    }
                    KeyCode::Enter => {
                        return Ok(EstimateEvent::SubmitProductQuery);
                    }
                    KeyCode::Backspace => {
                        let product = &mut self.resources[ri].products[pi];
                        if product.builder.search_input.is_empty() {
                            product.builder.current_tags_mut().pop();
                        } else {
                            product.builder.search_input.pop();
                        }
                        self.suggestion_index = None;
                        self.update_suggestions();
                    }
                    KeyCode::Delete => {
                        let product = &mut self.resources[ri].products[pi];
                        if product.builder.search_input.is_empty() {
                            product.builder.current_tags_mut().clear();
                        } else {
                            product.builder.search_input.clear();
                        }
                        self.suggestion_index = None;
                        self.update_suggestions();
                    }
                    KeyCode::Char(c) => {
                        self.resources[ri].products[pi].builder.search_input.push(c);
                        self.suggestion_index = None;
                        self.update_suggestions();
                    }
                    _ => {}
                }
            }
            BuilderFocus::Suggestions => {
                match key.code {
                    KeyCode::Up => {
                        if !self.suggestions_cache.is_empty() {
                            self.suggestion_index = Some(match self.suggestion_index {
                                None | Some(0) => self.suggestions_cache.len() - 1,
                                Some(i) => i - 1,
                            });
                        }
                    }
                    KeyCode::Down => {
                        if !self.suggestions_cache.is_empty() {
                            let next = self.suggestion_index.map(|i| i + 1).unwrap_or(0);
                            if next >= self.suggestions_cache.len() {
                                self.suggestion_index = Some(0);
                            } else {
                                self.suggestion_index = Some(next);
                            }
                        }
                    }
                    KeyCode::Char(' ') => {
                        if let Some(idx) = self.suggestion_index {
                            if idx < self.suggestions_cache.len() {
                                self.toggle_suggestion(ri, pi, idx);
                            }
                        }
                    }
                    KeyCode::Left => {
                        self.builder_focus = BuilderFocus::Field;
                    }
                    KeyCode::Enter => {
                        return Ok(EstimateEvent::SubmitProductQuery);
                    }
                    KeyCode::Esc => {
                        self.focus = EstimateFocus::ResourceTree;
                        self.suggestions_cache.clear();
                    }
                    _ => {}
                }
            }
        }
        Ok(EstimateEvent::None)
    }

    fn toggle_suggestion(&mut self, ri: usize, pi: usize, idx: usize) {
        if idx >= self.suggestions_cache.len() { return; }
        let value = self.suggestions_cache[idx].value.clone();
        if value.is_empty() { return; }

        let product = &mut self.resources[ri].products[pi];
        let field_idx = product.builder.selected_field;

        if field_idx == 2 && !value.contains('=') {
            product.builder.search_input = format!("{}=", value);
            self.update_suggestions();
            return;
        }
        if field_idx == 3 {
            product.builder.search_input = value;
            self.update_suggestions();
            self.suggestion_index = None;
            return;
        }

        let tags = product.builder.current_tags_mut();
        if let Some(pos) = tags.iter().position(|t| t == &value) {
            tags.remove(pos);
        } else {
            tags.push(value);
        }
        product.builder.search_input.clear();
        self.update_suggestions();
        if idx < self.suggestions_cache.len() {
            self.suggestion_index = Some(idx);
        } else if !self.suggestions_cache.is_empty() {
            self.suggestion_index = Some(self.suggestions_cache.len() - 1);
        }
    }

    fn handle_key_result_picker(&mut self, key: KeyEvent) -> Result<EstimateEvent> {
        let (ri, pi) = match self.active_product() {
            Some(p) => p,
            None => {
                self.focus = EstimateFocus::ResourceTree;
                return Ok(EstimateEvent::None);
            }
        };
        let product = &mut self.resources[ri].products[pi];
        match key.code {
            KeyCode::Up => {
                if product.result_selected == 0 {
                    self.focus = EstimateFocus::ProductEditor;
                } else {
                    product.result_selected = product.result_selected.saturating_sub(1);
                }
            }
            KeyCode::Down => {
                if product.result_selected + 1 < product.results.len() {
                    product.result_selected += 1;
                }
            }
            KeyCode::Char(' ') | KeyCode::Enter => {
                // Select this price
                if let Some(item) = product.results.get(product.result_selected) {
                    let price_str = item.prices.first()
                        .map(|p| p.price.clone())
                        .unwrap_or_else(|| "0".to_string());
                    let price_val: f64 = price_str.parse().unwrap_or(0.0);
                    let unit = item.prices.first()
                        .map(|p| p.unit.clone())
                        .unwrap_or_default();
                    // Rough monthly: assume hourly * 730
                    let monthly = if unit.to_lowercase().contains("hr") || unit.to_lowercase().contains("hour") {
                        price_val * 730.0
                    } else {
                        price_val
                    };
                    product.selected_price = Some(SelectedPrice {
                        product: item.product.clone(),
                        region: item.region.clone(),
                        price_display: price_str,
                        unit,
                        monthly_cost: monthly,
                    });
                    
                    // Auto-advance cursor in tree
                    self.focus = EstimateFocus::ResourceTree;
                    let rows = self.tree_rows();
                    if self.tree_cursor + 1 < rows.len() {
                        self.tree_cursor += 1;
                    }
                }
            }
            KeyCode::Esc | KeyCode::Left => {
                self.focus = EstimateFocus::ProductEditor;
            }
            _ => {}
        }
        Ok(EstimateEvent::None)
    }

    fn handle_key_qty_input(&mut self, key: KeyEvent) -> Result<EstimateEvent> {
        let (ri, pi) = match self.active_product() {
            Some(p) => p,
            None => {
                self.focus = EstimateFocus::ResourceTree;
                return Ok(EstimateEvent::None);
            }
        };
        match key.code {
            KeyCode::Char(c) if c.is_ascii_digit() => {
                self.qty_input.push(c);
            }
            KeyCode::Backspace => { self.qty_input.pop(); }
            KeyCode::Enter | KeyCode::Down => {
                let qty: u32 = self.qty_input.parse().unwrap_or(1).max(1);
                self.resources[ri].products[pi].quantity = qty;
                if key.code == KeyCode::Down && !self.resources[ri].products[pi].results.is_empty() {
                    self.focus = EstimateFocus::ResultPicker;
                } else {
                    self.focus = EstimateFocus::ProductEditor;
                }
            }
            KeyCode::Up => {
                let qty: u32 = self.qty_input.parse().unwrap_or(1).max(1);
                self.resources[ri].products[pi].quantity = qty;
                self.focus = EstimateFocus::ProductEditor;
                self.resources[ri].products[pi].builder.selected_field = 3;
                self.update_suggestions();
            }
            KeyCode::Esc | KeyCode::Left => {
                self.focus = EstimateFocus::ProductEditor;
            }
            _ => {}
        }
        Ok(EstimateEvent::None)
    }

    // ── Suggestion System ────────────────────────────────────────────────────

    pub fn update_suggestions(&mut self) {
        let opts = match self.options.clone() {
            Some(o) => o,
            None => { self.suggestions_cache.clear(); return; }
        };
        let (ri, pi) = match self.active_product() {
            Some(p) => p,
            None => { self.suggestions_cache.clear(); return; }
        };
        let builder = &self.resources[ri].products[pi].builder;
        let q = builder.search_input.clone();

        use crate::tui::semantic::*;
        self.suggestions_cache = match builder.selected_field {
            0 => score_and_suggest_products(&q, &opts.products, &opts.attribute_values, &opts.product_groups, &builder.product_tags),
            1 => suggest_regions(&q, &opts.regions, &opts.product_regions, &builder.product_tags, &builder.region_tags),
            2 => suggest_attrs(&q, &builder.product_tags, &opts.product_attrs, &opts.attribute_values, &builder.attribute_tags),
            _ => {
                [">", "<", ">=", "<="].iter()
                    .filter(|op| q.is_empty() || op.starts_with(&q))
                    .map(|op| SuggestionItem {
                        value: op.to_string(),
                        display: format!("{} (price operator)", op),
                        reason: "operator".to_string(),
                        is_semantic: false,
                        already_selected: false,
                    }).collect()
            }
        };
    }

    // ── Add from Pricing ─────────────────────────────────────────────────────

    /// Add a product from pricing view results into a resource
    pub fn add_from_pricing(&mut self, builder: &CommandBuilderState, items: &[PricingDisplayItem]) {
        // If no resource exists, create a default Instance
        if self.resources.is_empty() {
            self.resources.push(EstimateResource::new(ResourceType::Custom, "Resource 1".to_string(), self.options.as_deref()));
        }
        let ri = self.resources.len() - 1;
        let mut product = EstimateProduct::new("From Pricing", false);
        product.builder = builder.clone();
        product.results = items.to_vec();
        self.resources[ri].products.push(product);
    }

    // ── Rendering ────────────────────────────────────────────────────────────

    pub fn render(&self, f: &mut Frame, active: bool) {
        let area = f.area();

        let main_chunks = Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Length(3),   // header
                Constraint::Min(10),     // content
                Constraint::Length(4),   // summary
                Constraint::Length(1),   // help
            ])
            .split(area);

        self.render_header(f, main_chunks[0], active);
        self.render_content(f, main_chunks[1], active);
        self.render_summary(f, main_chunks[2]);
        self.render_help(f, main_chunks[3]);
    }

    fn render_header(&self, f: &mut Frame, area: Rect, active: bool) {
        let header_active = active && self.active_section == EstimateSection::Header;
        let border_type = if header_active { BorderType::Thick } else { BorderType::Plain };
        let border_color = if header_active { Color::Green } else if active { Color::Cyan } else { Color::DarkGray };

        let estimate_style = if header_active {
            Style::default().fg(Color::Black).bg(Color::Green).add_modifier(Modifier::BOLD)
        } else if active {
            Style::default().fg(Color::Cyan).add_modifier(Modifier::BOLD)
        } else {
            Style::default().fg(Color::DarkGray)
        };

        let header_text = vec![Line::from(vec![
            Span::styled("Pricing", Style::default().fg(Color::DarkGray)),
            Span::raw(" | "),
            Span::styled(if header_active { " > Estimate < " } else { " Estimate " }, estimate_style),
            Span::raw(" | "),
            Span::styled("Settings", Style::default().fg(Color::DarkGray)),
            Span::raw(" | "),
            Span::styled("History", Style::default().fg(Color::DarkGray)),
        ])];

        let title = if header_active {
            Line::from(vec![
                Span::styled(" > ", Style::default().fg(Color::Green).add_modifier(Modifier::BOLD)),
                Span::styled(format!("CloudCent CLI v{}", crate::VERSION), Style::default().fg(Color::Green).add_modifier(Modifier::BOLD)),
                Span::styled(" < ", Style::default().fg(Color::Green).add_modifier(Modifier::BOLD)),
            ])
        } else {
            Line::from(format!(" CloudCent CLI v{} ", crate::VERSION))
        };

        let header = Paragraph::new(header_text).block(
            Block::default()
                .borders(Borders::ALL)
                .border_type(border_type)
                .title(title)
                .title_alignment(Alignment::Center)
                .border_style(Style::default().fg(border_color)),
        );
        f.render_widget(header, area);
    }

    fn render_content(&self, f: &mut Frame, area: Rect, active: bool) {
        // Type picker / name input modals
        if self.focus == EstimateFocus::ResourceTypePicker {
            self.render_type_picker(f, area);
            return;
        }

        // Three-column-ish layout: Tree (45%) | Editor (55%)
        let lr = Layout::default()
            .direction(Direction::Horizontal)
            .constraints([Constraint::Percentage(45), Constraint::Percentage(55)])
            .split(area);
        
        self.render_tree_layout(f, lr[0], active);
        self.render_editor(f, lr[1], active);
    }

    fn render_tree_layout(&self, f: &mut Frame, area: Rect, active: bool) {
        let is_tree_focus = self.focus == EstimateFocus::ResourceTree && self.active_section == EstimateSection::Content;
        let border_color = if is_tree_focus && active { Color::Green } else { Color::DarkGray };
        let border_type = if is_tree_focus { BorderType::Thick } else { BorderType::Plain };
        
        // Two boxes side-by-side: Templates (Left) and Estimates (Right)
        let chunks = Layout::default()
            .direction(Direction::Horizontal)
            .constraints([
                Constraint::Percentage(45),
                Constraint::Percentage(55),
            ])
            .split(area);

        self.render_templates(f, chunks[0], is_tree_focus, border_color, border_type);
        self.render_active_resources(f, chunks[1], is_tree_focus, border_color, border_type);
    }

    fn render_active_resources(&self, f: &mut Frame, area: Rect, is_focus: bool, border_color: Color, border_type: BorderType) {
        let rows = self.tree_rows();
        let mut lines: Vec<Line> = Vec::new();
        
        let mut resource_added = false;
        for (i, row) in rows.iter().enumerate() {
            if let TreeRow::Resource(ri) = row {
                resource_added = true;
                let selected = i == self.tree_cursor && is_focus;
                let cursor = if selected { "> " } else { "  " };
                let res = &self.resources[*ri];
                let arrow = if res.collapsed { ">" } else { "v" };
                let type_label = res.resource_type.label();
                let subtotal = res.subtotal();
                let style = if selected {
                    Style::default().fg(Color::Black).bg(Color::Cyan).add_modifier(Modifier::BOLD)
                } else {
                    Style::default().fg(Color::Gray).add_modifier(Modifier::BOLD)
                };
                lines.push(Line::from(vec![
                    Span::styled(cursor, style),
                    Span::styled(format!("{} {} [{}]", arrow, res.name, type_label), style),
                    Span::styled(format!("  ${:.2}/mo", subtotal), Style::default().fg(Color::Green)),
                ]));
                
                if !res.collapsed {
                    for pi in 0..res.products.len() {
                        // Find the row index for this product
                        if let Some(row_idx) = rows.iter().position(|r| matches!(r, TreeRow::Product(rii, pii) if *rii == *ri && *pii == pi)) {
                            let p_selected = row_idx == self.tree_cursor && is_focus;
                            let product = &res.products[pi];
                            let icon = if product.selected_price.is_some() { "[x]" } else { "[ ]" };
                            let p_cursor = if p_selected { "> " } else { "  " };
                            let price_text = if let Some(sp) = &product.selected_price {
                                format!("${:.2}/mo x{}", sp.monthly_cost, product.quantity)
                            } else {
                                "(not configured)".to_string()
                            };
                            let style = if p_selected {
                                Style::default().fg(Color::Black).bg(Color::Yellow)
                            } else {
                                Style::default().fg(Color::White)
                            };
                            lines.push(Line::from(vec![
                                Span::styled(p_cursor, style),
                                Span::styled(format!("  {} {} ", icon, product.label), style),
                                Span::styled(price_text, Style::default().fg(if product.selected_price.is_some() { Color::Cyan } else { Color::DarkGray })),
                            ]));
                        }
                    }
                }
            }
        }
        
        if !resource_added {
            lines.push(Line::from(vec![
                Span::styled("No resources added yet.", Style::default().fg(Color::DarkGray)),
            ]));
        }

        let tree = Paragraph::new(lines).block(
            Block::default()
                .borders(Borders::ALL)
                .border_type(border_type)
                .title(" Estimate Resources ")
                .border_style(Style::default().fg(border_color)),
        );
        f.render_widget(tree, area);
    }

    fn render_templates(&self, f: &mut Frame, area: Rect, is_focus: bool, border_color: Color, border_type: BorderType) {
        let rows = self.tree_rows();
        let mut lines: Vec<Line> = Vec::new();
        
        for (i, row) in rows.iter().enumerate() {
            if let TreeRow::NewResource(rt) = row {
                let selected = i == self.tree_cursor && is_focus;
                let cursor = if selected { "> " } else { "  " };
                let style = if selected {
                    Style::default().fg(Color::Black).bg(Color::Cyan)
                } else {
                    Style::default().fg(Color::Cyan)
                };
                lines.push(Line::from(vec![
                    Span::styled(cursor, style),
                    Span::styled(format!(" + {}", rt.label()), style),
                ]));
            }
        }

        let tree = Paragraph::new(lines).block(
            Block::default()
                .borders(Borders::ALL)
                .border_type(border_type)
                .title(" Add Resource ")
                .border_style(Style::default().fg(border_color)),
        );
        f.render_widget(tree, area);
    }

    fn render_editor(&self, f: &mut Frame, area: Rect, _active: bool) {
        let (ri, pi) = match self.active_product() {
            Some(p) => p,
            None => {
                let empty = Paragraph::new("Select a product to edit")
                    .block(Block::default().borders(Borders::ALL).title(" Editor "));
                f.render_widget(empty, area);
                return;
            }
        };
        let product = &self.resources[ri].products[pi];
        let resource = &self.resources[ri];
        let is_editor = matches!(self.focus, EstimateFocus::ProductEditor | EstimateFocus::QuantityInput);
        let is_result = self.focus == EstimateFocus::ResultPicker;

        // Side-by-side layout: Left (Config + Results) | Right (Suggestions)
        let main_horiz = Layout::default()
            .direction(Direction::Horizontal)
            .constraints([Constraint::Percentage(60), Constraint::Percentage(40)])
            .split(area);

        let left_vert = Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Length(10), // builder fields + qty
                Constraint::Min(5),    // results
            ])
            .split(main_horiz[0]);

        let config_area = left_vert[0];
        let results_area = left_vert[1];
        let suggestion_area = main_horiz[1];

        // Builder fields
        let field_names = ["Product", "Region", "Attrs", "Price"];
        let mut field_lines: Vec<Line> = Vec::new();
        field_lines.push(Line::from(vec![
            Span::styled(format!(" {} > {} ", resource.name, product.label), Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
        ]));

        for (fi, fname) in field_names.iter().enumerate() {
            let tags = match fi {
                0 => &product.builder.product_tags,
                1 => &product.builder.region_tags,
                2 => &product.builder.attribute_tags,
                _ => &product.builder.price_tags,
            };
            let is_fixed_field = product.fixed && fi == 0;
            let is_active_field = is_editor && product.builder.selected_field == fi && self.focus == EstimateFocus::ProductEditor;
            let marker = if is_active_field { "> " } else { "  " };
            let tag_str = if tags.is_empty() { String::new() } else { tags.join(", ") };
            let search = if is_active_field && !product.builder.search_input.is_empty() {
                format!("|{}", product.builder.search_input)
            } else { String::new() };

            let style = if is_fixed_field {
                Style::default().fg(Color::DarkGray)
            } else if is_active_field {
                Style::default().fg(Color::Cyan)
            } else {
                Style::default().fg(Color::White)
            };
            
            let label_suffix = if is_fixed_field { " (fixed)" } else { "" };
            
            field_lines.push(Line::from(vec![
                Span::styled(format!("{}{}{}: ", marker, fname, label_suffix), style),
                Span::styled(format!("[{}]", tag_str), Style::default().fg(if is_fixed_field { Color::DarkGray } else { Color::Yellow })),
                Span::styled(search, Style::default().fg(Color::Cyan)),
            ]));
        }

        // Quantity line
        let qty_active = self.focus == EstimateFocus::QuantityInput;
        let qty_marker = if qty_active { "> " } else { "  " };
        let qty_val = if qty_active { &self.qty_input } else { &format!("{}", product.quantity) };
        let qty_style = if qty_active { Style::default().fg(Color::Cyan) } else { Style::default().fg(Color::White) };
        field_lines.push(Line::from(vec![
            Span::styled(format!("{}Qty: ", qty_marker), qty_style),
            Span::styled(format!("[{}]", qty_val), Style::default().fg(Color::Yellow)),
        ]));

        let field_border = if is_editor { Color::Green } else { Color::DarkGray };
        let fields = Paragraph::new(field_lines).block(
            Block::default()
                .borders(Borders::ALL)
                .border_type(if is_editor { BorderType::Thick } else { BorderType::Plain })
                .title(" Product Config ")
                .border_style(Style::default().fg(field_border)),
        );
        f.render_widget(fields, config_area);

        // Always show the side-by-side panels
        self.render_results(f, results_area, product, is_result);
        self.render_suggestions(f, suggestion_area);
    }

    fn render_results(&self, f: &mut Frame, area: Rect, product: &EstimateProduct, is_result_focus: bool) {
        if self.loading {
            let loading = Paragraph::new(Line::from(Span::styled("Loading...", Style::default().fg(Color::Cyan))))
                .block(Block::default().borders(Borders::ALL).title(" Results ").border_style(Style::default().fg(Color::DarkGray)));
            f.render_widget(loading, area);
            return;
        }
        if let Some(err) = &self.error_message {
            let err_p = Paragraph::new(Line::from(Span::styled(err.as_str(), Style::default().fg(Color::Red))))
                .block(Block::default().borders(Borders::ALL).title(" Error ").border_style(Style::default().fg(Color::Red)));
            f.render_widget(err_p, area);
            return;
        }
        if product.results.is_empty() {
            let mut text = "Press Enter to search".to_string();
            let mut color = Color::DarkGray;
            
            if let Some((msg, success)) = &self.query_status {
                text = msg.clone();
                color = if *success { Color::Green } else { Color::Red };
            }

            let empty = Paragraph::new(Line::from(Span::styled(
                text, Style::default().fg(color))))
                .block(Block::default().borders(Borders::ALL).title(" Results ").border_style(Style::default().fg(color)));
            f.render_widget(empty, area);
            return;
        }

        // Show status even if results exist
        let mut title = format!(" Results ({}) ", product.results.len());
        let mut border_color = if is_result_focus { Color::Green } else { Color::DarkGray };
        
        if let Some((msg, success)) = &self.query_status {
            title = format!(" Results ({}) - {} ", product.results.len(), msg);
            if !is_result_focus {
                border_color = if *success { Color::Green } else { Color::Red };
            }
        }

        let product_col_w = product.results.iter()
            .map(|i| i.product.len()).max().unwrap_or(7).max(7) + 2;
        let region_col_w = product.results.iter()
            .map(|i| i.region.len()).max().unwrap_or(6).max(6) + 2;

        let mut lines: Vec<Line> = Vec::new();
        let max_show = (area.height as usize).saturating_sub(2).min(product.results.len());
        let start = if product.result_selected >= max_show {
            product.result_selected - max_show + 1
        } else { 0 };

        for i in start..(start + max_show).min(product.results.len()) {
            let item = &product.results[i];
            let selected = i == product.result_selected && is_result_focus;
            let price_str = item.prices.first().map(|p| format!("{} /{}", p.price, p.unit)).unwrap_or_default();
            let model = item.prices.first().map(|p| p.pricing_model.clone()).unwrap_or_default();
            let style = if selected {
                Style::default().fg(Color::Black).bg(Color::Yellow)
            } else {
                Style::default().fg(Color::White)
            };
            let dim = if selected { style } else { Style::default().fg(Color::DarkGray) };
            let marker = if selected { "> " } else { "  " };
            lines.push(Line::from(vec![
                Span::styled(format!("{}{:<pw$}", marker, item.product, pw = product_col_w), style),
                Span::styled(format!("{:<rw$}", item.region, rw = region_col_w), dim),
                Span::styled(format!("{} ", price_str), style),
                Span::styled(format!("{}", model), dim),
            ]));
        }

        let results = Paragraph::new(lines).block(
            Block::default()
                .borders(Borders::ALL)
                .border_type(if is_result_focus { BorderType::Thick } else { BorderType::Plain })
                .title(title)
                .border_style(Style::default().fg(border_color)),
        );
        f.render_widget(results, area);
    }

    fn render_suggestions(&self, f: &mut Frame, area: Rect) {
        let in_sugg = self.builder_focus == BuilderFocus::Suggestions;
        let max_show = (area.height as usize).saturating_sub(2).min(self.suggestions_cache.len());
        let sel = self.suggestion_index.unwrap_or(0);
        let start = if sel >= max_show { sel - max_show + 1 } else { 0 };

        let mut lines: Vec<Line> = Vec::new();
        for i in start..(start + max_show).min(self.suggestions_cache.len()) {
            let s = &self.suggestions_cache[i];
            let selected = Some(i) == self.suggestion_index && in_sugg;
            let check = if s.already_selected { "[x] " } else { "    " };
            let style = if selected {
                Style::default().fg(Color::Black).bg(Color::Cyan)
            } else if s.already_selected {
                Style::default().fg(Color::Cyan)
            } else {
                Style::default().fg(Color::White)
            };
            lines.push(Line::from(Span::styled(format!("{}{}", check, s.display), style)));
        }

        let border = if in_sugg { Color::Green } else { Color::DarkGray };
        let sugg = Paragraph::new(lines).block(
            Block::default()
                .borders(Borders::ALL)
                .border_type(if in_sugg { BorderType::Thick } else { BorderType::Plain })
                .title(" Suggestions ")
                .border_style(Style::default().fg(border)),
        );
        f.render_widget(sugg, area);
    }

    fn render_type_picker(&self, f: &mut Frame, area: Rect) {
        let mut types = Vec::new();
        if let Some(opts) = &self.options {
            let mut group_names: Vec<String> = opts.product_groups.keys().cloned().collect();
            group_names.sort();
            for name in group_names {
                types.push(ResourceType::Archetype(name));
            }
        }
        types.push(ResourceType::Custom);

        let mut lines: Vec<Line> = vec![Line::from(Span::styled("Select resource type:", Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD))), Line::from("")];
        for (i, t) in types.iter().enumerate() {
            let selected = i == self.type_picker_index;
            let style = if selected {
                Style::default().fg(Color::Black).bg(Color::Cyan).add_modifier(Modifier::BOLD)
            } else {
                Style::default().fg(Color::White)
            };
            let marker = if selected { " > " } else { "   " };
            let templates = if let Some(opts) = &self.options {
                t.get_templates(opts).join(", ")
            } else {
                "Custom".to_string()
            };
            lines.push(Line::from(vec![
                Span::styled(format!("{}{}", marker, t.label()), style),
                Span::styled(format!("  ({})", templates), Style::default().fg(Color::DarkGray)),
            ]));
        }

        let v = Layout::default().direction(Direction::Vertical)
            .constraints([Constraint::Percentage(25), Constraint::Length((types.len() as u16) + 5), Constraint::Percentage(25)])
            .split(area);
        let h = Layout::default().direction(Direction::Horizontal)
            .constraints([Constraint::Percentage(20), Constraint::Percentage(60), Constraint::Percentage(20)])
            .split(v[1]);

        let picker = Paragraph::new(lines).block(
            Block::default().borders(Borders::ALL).title(" New Resource ").title_alignment(Alignment::Center)
                .border_style(Style::default().fg(Color::Cyan)));
        f.render_widget(picker, h[1]);
    }


    fn render_summary(&self, f: &mut Frame, area: Rect) {
        let total = self.grand_total();
        let mut breakdown: Vec<String> = Vec::new();
        for res in &self.resources {
            let st = res.subtotal();
            if st > 0.0 { breakdown.push(format!("{}: ${:.2}", res.name, st)); }
        }

        let lines = vec![
            Line::from(vec![
                Span::styled("Total Estimate: ", Style::default().fg(Color::Yellow).add_modifier(Modifier::BOLD)),
                Span::styled(format!("${:.2} / month", total), Style::default().fg(Color::Green).add_modifier(Modifier::BOLD)),
                Span::styled(format!("  ({} resources)", self.resources.len()), Style::default().fg(Color::DarkGray)),
            ]),
            Line::from(Span::styled(
                if breakdown.is_empty() { "- No items configured yet".to_string() } else { format!("- {}", breakdown.join(" | ")) },
                Style::default().fg(Color::DarkGray),
            )),
        ];

        let summary = Paragraph::new(lines).block(
            Block::default().borders(Borders::ALL).border_style(Style::default().fg(Color::White)));
        f.render_widget(summary, area);
    }

    fn render_help(&self, f: &mut Frame, area: Rect) {
        let help = match self.focus {
            EstimateFocus::ResourceTree => Line::from(vec![
                Span::styled("[Enter] ", Style::default().fg(Color::Green)), Span::raw("Pricing  "),
                Span::styled("[Space] ", Style::default().fg(Color::Green)), Span::raw("Edit  "),
                Span::styled("[d] ", Style::default().fg(Color::Red)), Span::raw("Delete  "),
                Span::styled("[Esc] ", Style::default().fg(Color::Red)), Span::raw("Quit"),
            ]),
            EstimateFocus::ProductEditor => Line::from(vec![
                Span::styled("[Enter] ", Style::default().fg(Color::Green)), Span::raw("Search  "),
                Span::styled("[p] ", Style::default().fg(Color::Magenta)), Span::raw("Pricing  "),
                Span::styled("[→] ", Style::default().fg(Color::Cyan)), Span::raw("Sugg  "),
                Span::styled("[↑↓] ", Style::default().fg(Color::Yellow)), Span::raw("Field  "),
                Span::styled("[←] ", Style::default().fg(Color::Red)), Span::raw("Back"),
            ]),
            EstimateFocus::ResultPicker => Line::from(vec![
                Span::styled("[Space/Enter] ", Style::default().fg(Color::Green)), Span::raw("Select  "),
                Span::styled("[↑↓] ", Style::default().fg(Color::Yellow)), Span::raw("Nav  "),
                Span::styled("[←] ", Style::default().fg(Color::Red)), Span::raw("Back"),
            ]),
            EstimateFocus::QuantityInput => Line::from(vec![
                Span::styled("[0-9] ", Style::default().fg(Color::Cyan)), Span::raw("Qty  "),
                Span::styled("[Enter] ", Style::default().fg(Color::Green)), Span::raw("OK  "),
                Span::styled("[←] ", Style::default().fg(Color::Red)), Span::raw("Back"),
            ]),
            _ => Line::from(vec![
                Span::styled("[←] ", Style::default().fg(Color::Red)), Span::raw("Back"),
            ]),
        };
        f.render_widget(Paragraph::new(help), area);
    }
}
