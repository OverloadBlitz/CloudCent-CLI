use ratatui::{
    layout::{Alignment, Constraint, Direction, Layout},
    style::{Color, Style, Stylize},
    text::{Line, Span},
    widgets::{Block, Borders, Paragraph},
    Frame,
};

use super::app::{App, AuthState, ViewMode};

pub fn render(f: &mut Frame, app: &App) {
    match app.view_mode {
        ViewMode::InitAuth => render_init_auth(f, app),
        ViewMode::Pricing => app.pricing_view.render(f, true),
        #[cfg(feature = "estimate")]
        ViewMode::Estimate => app.estimate_view.render(f, true),
        ViewMode::Settings => {
            let config = app.user_command.client().get_config();
            let config_path = crate::config::get_config_path()
                .ok()
                .and_then(|p| p.to_str().map(|s| s.to_string()))
                .unwrap_or_else(|| "Unknown".to_string());
            app.settings_view.render(f, config, &config_path, true);
        }
        ViewMode::History => {
            let stats = if let Some(ref db) = app.db {
                db.get_cache_stats().unwrap_or((0, 0))
            } else {
                (0, 0)
            };
            app.history_view.render(f, true, stats);
        }
    }

    if app.metadata_refresh_msg.is_some() {
        render_metadata_status(f, app);
    }
}

fn render_init_auth(f: &mut Frame, app: &App) {
    match app.auth_state {
        AuthState::Prompt => render_auth_prompt(f),
        AuthState::Waiting => render_auth_waiting(f, app),
        AuthState::Success => render_auth_success(f),
        AuthState::Loading => render_auth_loading(f, app),
        AuthState::Error => render_auth_error(f, app),
    }
}

fn render_auth_prompt(f: &mut Frame) {
    let area = f.area();
    
    let vertical_chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Percentage(30),
            Constraint::Length(12),
            Constraint::Percentage(30),
        ])
        .split(area);
    
    let horizontal_chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(20),
            Constraint::Percentage(60),
            Constraint::Percentage(20),
        ])
        .split(vertical_chunks[1]);
    
    let content_area = horizontal_chunks[1];
    
    let content = vec![
        Line::from(""),
        Line::from(Span::styled(
            "Welcome! CloudCent requires a free API Key to fetch",
            Style::default().fg(Color::White),
        )),
        Line::from(Span::styled(
            "live cloud pricing data.",
            Style::default().fg(Color::White),
        )),
        Line::from(""),
        Line::from(vec![
            Span::raw("Press "),
            Span::styled("Enter", Style::default().fg(Color::Yellow).bold()),
            Span::raw(" to authenticate in browser..."),
        ]),
        Line::from(vec![
            Span::raw("(Or press "),
            Span::styled("Esc", Style::default().fg(Color::Red)),
            Span::raw(" to quit)"),
        ]),
        Line::from(""),
    ];
    
    let block = Block::default()
        .borders(Borders::ALL)
        .title(format!(" CloudCent CLI v{} ", crate::VERSION))
        .title_alignment(Alignment::Center)
        .border_style(Style::default().fg(Color::Cyan));
    
    let paragraph = Paragraph::new(content)
        .block(block)
        .alignment(Alignment::Center);
    
    f.render_widget(paragraph, content_area);
}

fn render_auth_waiting(f: &mut Frame, _app: &App) {
    let area = f.area();
    
    let vertical_chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Percentage(30),
            Constraint::Length(15),
            Constraint::Percentage(30),
        ])
        .split(area);
    
    let horizontal_chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(20),
            Constraint::Percentage(60),
            Constraint::Percentage(20),
        ])
        .split(vertical_chunks[1]);
    
    let content_area = horizontal_chunks[1];
    
    let spinner_frames = ["-", "\\", "|", "/"];
    let frame_idx = (std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap()
        .as_millis() / 100) as usize % spinner_frames.len();
    
    let content = vec![
        Line::from(""),
        Line::from(Span::styled(
            format!("{} Waiting for browser authorization...", spinner_frames[frame_idx]),
            Style::default().fg(Color::Cyan).bold(),
        )),
        Line::from(""),
        Line::from(Span::styled(
            "A browser window should have opened.",
            Style::default().fg(Color::White),
        )),
        Line::from(Span::styled(
            "Please complete the verification there.",
            Style::default().fg(Color::White),
        )),
        Line::from(""),
        Line::from(vec![
            Span::raw("Press "),
            Span::styled("Esc", Style::default().fg(Color::Red)),
            Span::raw(" to cancel"),
        ]),
    ];
    
    let block = Block::default()
        .borders(Borders::ALL)
        .title(" Authenticating ")
        .title_alignment(Alignment::Center)
        .border_style(Style::default().fg(Color::Yellow));
    
    let paragraph = Paragraph::new(content)
        .block(block)
        .alignment(Alignment::Center);
    
    f.render_widget(paragraph, content_area);
}

fn render_auth_success(f: &mut Frame) {
    let area = f.area();
    
    let vertical_chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Percentage(30),
            Constraint::Length(10),
            Constraint::Percentage(30),
        ])
        .split(area);
    
    let horizontal_chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(20),
            Constraint::Percentage(60),
            Constraint::Percentage(20),
        ])
        .split(vertical_chunks[1]);
    
    let content_area = horizontal_chunks[1];
    
    let content = vec![
        Line::from(""),
        Line::from(Span::styled(
            "✓ Authentication Successful!",
            Style::default().fg(Color::Green).bold(),
        )),
        Line::from(""),
        Line::from(Span::styled(
            "Your API key has been saved.",
            Style::default().fg(Color::White),
        )),
        Line::from(""),
        Line::from(vec![
            Span::raw("Press "),
            Span::styled("Enter", Style::default().fg(Color::Yellow).bold()),
            Span::raw(" to continue..."),
        ]),
    ];
    
    let block = Block::default()
        .borders(Borders::ALL)
        .title(" Success ")
        .title_alignment(Alignment::Center)
        .border_style(Style::default().fg(Color::Green));
    
    let paragraph = Paragraph::new(content)
        .block(block)
        .alignment(Alignment::Center);
    
    f.render_widget(paragraph, content_area);
}

fn render_auth_loading(f: &mut Frame, app: &App) {
    let area = f.area();
    
    let vertical_chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Percentage(30),
            Constraint::Length(10),
            Constraint::Percentage(30),
        ])
        .split(area);
    
    let horizontal_chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(20),
            Constraint::Percentage(60),
            Constraint::Percentage(20),
        ])
        .split(vertical_chunks[1]);
    
    let content_area = horizontal_chunks[1];
    
    let spinner_frames = ["⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧"];
    let spinner = spinner_frames[app.loading_frame % spinner_frames.len()];
    
    let content = vec![
        Line::from(""),
        Line::from(vec![
            Span::styled(
                spinner,
                Style::default().fg(Color::Cyan).bold(),
            ),
            Span::raw("  "),
            Span::styled(
                "Loading metadata...",
                Style::default().fg(Color::Cyan),
            ),
        ]),
        Line::from(""),
        Line::from(Span::styled(
            "Please wait while we download pricing data",
            Style::default().fg(Color::Gray),
        )),
        Line::from(""),
        Line::from(vec![
            Span::raw("Press "),
            Span::styled("Esc", Style::default().fg(Color::Red)),
            Span::raw(" to cancel"),
        ]),
    ];
    
    let block = Block::default()
        .borders(Borders::ALL)
        .title(" Loading ")
        .title_alignment(Alignment::Center)
        .border_style(Style::default().fg(Color::Cyan));
    
    let paragraph = Paragraph::new(content)
        .block(block)
        .alignment(Alignment::Center);
    
    f.render_widget(paragraph, content_area);
}

fn render_auth_error(f: &mut Frame, app: &App) {
    let area = f.area();
    
    let vertical_chunks = Layout::default()
        .direction(Direction::Vertical)
        .constraints([
            Constraint::Percentage(30),
            Constraint::Length(12),
            Constraint::Percentage(30),
        ])
        .split(area);
    
    let horizontal_chunks = Layout::default()
        .direction(Direction::Horizontal)
        .constraints([
            Constraint::Percentage(20),
            Constraint::Percentage(60),
            Constraint::Percentage(20),
        ])
        .split(vertical_chunks[1]);
    
    let content_area = horizontal_chunks[1];
    
    let error_msg = app.error_message.as_deref().unwrap_or("Unknown error");
    
    let content = vec![
        Line::from(""),
        Line::from(Span::styled(
            "[ERROR] Authentication Failed",
            Style::default().fg(Color::Red).bold(),
        )),
        Line::from(""),
        Line::from(Span::styled(
            error_msg,
            Style::default().fg(Color::White),
        )),
        Line::from(""),
        Line::from(vec![
            Span::raw("Press "),
            Span::styled("Enter", Style::default().fg(Color::Yellow).bold()),
            Span::raw(" to retry or "),
            Span::styled("Esc", Style::default().fg(Color::Red)),
            Span::raw(" to quit"),
        ]),
    ];
    
    let block = Block::default()
        .borders(Borders::ALL)
        .title(" Error ")
        .title_alignment(Alignment::Center)
        .border_style(Style::default().fg(Color::Red));
    
    let paragraph = Paragraph::new(content)
        .block(block)
        .alignment(Alignment::Center);
    
    f.render_widget(paragraph, content_area);
}

fn render_metadata_status(f: &mut Frame, app: &App) {
    if let Some((msg, is_success)) = &app.metadata_refresh_msg {
        let area = f.area();
        let chunks = Layout::default()
            .direction(Direction::Vertical)
            .constraints([
                Constraint::Min(0),
                Constraint::Length(1),
            ])
            .split(area);
        
        let color = if msg.contains("Refreshing") {
            Color::Cyan
        } else if *is_success {
            Color::Green
        } else {
            Color::Red
        };
        
        let status = Paragraph::new(Line::from(vec![
            Span::styled(" ● ", Style::default().fg(color)),
            Span::styled(msg.as_str(), Style::default().fg(Color::White).bold()),
        ]))
        .alignment(Alignment::Center);

        f.render_widget(status, chunks[1]);
    }
}
