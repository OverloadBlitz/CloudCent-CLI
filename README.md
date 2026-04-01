# CloudCent CLI

A terminal UI for querying and comparing cloud pricing across providers, built with Rust and [Ratatui](https://ratatui.rs).

![License](https://img.shields.io/badge/license-Apache--2.0-blue)
![Version](https://img.shields.io/badge/version-0.0.1--beta-orange)

## Features

- Multi-cloud pricing search — query pricing data across AWS, GCP, Azure and more from a single interface
- Smart suggestions — fuzzy matching and semantic aliases (e.g. type "compute" to find EC2, Compute Engine, VMs)
- Command builder — structured form with product, region, attribute, and price filter fields with autocomplete
- Raw command mode — type queries directly for power users (`product <name> region <region> attrs <key=value>`)
- Attribute filtering — drill into instance types, storage classes, vCPU counts, etc.
- Price operators — filter results with `>`, `<`, `>=`, `<=`
- Query history — browse past queries, preview cached results, and re-run with one keystroke
- Local caching — SQLite-backed cache for pricing data and metadata (3-day TTL)
- Settings view — view your CLI ID, API key, and config path
- Cross-platform — runs on macOS, Linux, and Windows (x64 and ARM64)

## Installation

### npm (recommended)

```bash
npm install -g @cloudcent/cli
```

### Shell script (macOS / Linux)

```bash
curl -fsSL https://raw.githubusercontent.com/OverloadBlitz/cloudcent-cli/main/install.sh | bash
```

### PowerShell (Windows)

```powershell
irm https://raw.githubusercontent.com/OverloadBlitz/cloudcent-cli/main/install.ps1 | iex
```

### Build from source

```bash
git clone https://github.com/OverloadBlitz/cloudcent-cli.git
cd cloudcent-cli
cargo build --release
# Binary at target/release/cloudcent
```

## Quick Start

```bash
cloudcent
```

On first launch you'll be prompted to authenticate via browser. This sets up a free API key stored at `~/.cloudcent/config.yaml`.

## Keyboard Shortcuts

### Navigation

| Key | Action |
|-----|--------|
| `Left` `Right` | Switch between views (Pricing / History / Settings) |
| `Up` `Down` | Move between sections and items |
| `Esc` | Quit |
| `F1` | Toggle between Command Builder and Raw Command mode |
| `F2` | Clear current query |
| `F3` | Refresh metadata from API |

### Pricing View — Command Builder

| Key | Action |
|-----|--------|
| `Up` `Down` | Navigate fields (Product / Region / Attrs / Price) |
| `Right` | Enter suggestion panel |
| `Space` | Toggle suggestion selection |
| `Enter` | Submit query |
| `Backspace` | Delete character or remove last tag |
| `Delete` | Clear search input or all tags for current field |
| Type | Filter suggestions |

### Pricing View — Results

| Key | Action |
|-----|--------|
| `Up` `Down` | Navigate rows |
| `Left` `Right` | Horizontal scroll |
| `j` / `k` | Previous / next page |
| `PageUp` / `PageDown` | Page navigation |

### History View

| Key | Action |
|-----|--------|
| `Up` `Down` | Navigate entries |
| `Enter` | Open query in Pricing view |
| `c` | Clear all history and cache |

## Project Structure

```
src/
├── main.rs              # Entry point
├── config.rs            # YAML config (~/.cloudcent/config.yaml)
├── api/
│   ├── client.rs        # HTTP client (pricing, metadata, auth)
│   └── models.rs        # API request/response types
├── commands/
│   ├── pricing.rs       # Pricing options loading and metadata processing
│   └── user.rs          # Authentication flow (browser OAuth)
├── db/
│   └── mod.rs           # SQLite (history, pricing cache, metadata cache)
└── tui/
    ├── app.rs           # App state and event loop
    ├── ui.rs            # Top-level render dispatch
    ├── semantic.rs      # Fuzzy matching and alias engine
    └── views/
        ├── pricing.rs   # Pricing query builder and results table
        ├── settings.rs  # Config display
        └── history.rs   # Query history and cache stats
```

## Configuration

Config is stored at `~/.cloudcent/config.yaml` with permissions set to `600` on Unix.

Data files:
- `~/.cloudcent/metadata.json.gz` — compressed pricing metadata
- `~/.cloudcent/cloudcent.db` — SQLite database (history, cache)

## License

[Apache License 2.0](LICENSE)
