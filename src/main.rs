use anyhow::Result;

mod api;
mod commands;
mod config;
mod db;
mod tui;

pub const VERSION: &str = env!("CARGO_PKG_VERSION");

fn main() -> Result<()> {
    tui::run()
}
