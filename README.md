# CloudCent CLI

![License](https://img.shields.io/badge/license-MIT-blue)
![Version](https://img.shields.io/badge/version-0.0.3--beta-orange)

A CLI that estimates cloud costs from Draw.io diagrams and Pulumi code.

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
go build -o cloudcent .
```

## Quick Start

```bash
cloudcent --help
cloudcent init
```

Run `cloudcent init` to authenticate via browser. This sets up a free API key stored at `~/.cloudcent/config.yaml`.

## Demo

### Draw.io
```
cloudcent diagram init aws-simple-architecture.drawio 

cloudcent diagram estimate aws-simple-architecture.drawio
```
![drawio-demo1](/docs/drawio-demo1.png)


### Pulumi
```
cloudcent pulumi estimate
```
![pulumi-demo1](/docs/pulumi-demo1.png)

## Supported Cloud Resources

| Provider | Services                                                      | Pricing Model | Data Source |
|----------|---------------------------------------------------------------|---------------|-------------|
| AWS | EC2, EBS, ECS, S3, ApiGateway, AppSync, DynamoDB, Lambda, SNS | OnDemand, Reserved, SavingPlan, Spot | AWS Pricing API |
| Azure | WIP                                                           | OnDemand, Reserved, SavingPlan (with/without Azure Hybrid Benefit) | Azure Pricing Calculator |
| GCP | WIP                                                           | OnDemand, CommittedUseDiscount, Preemptible | GCP Pricing SDK v1 |
| OCI | WIP                                                           | OnDemand (PAYG) | OCI Cost Estimator |

## CLI Commands

```
cloudcent                 # Show help
cloudcent init            # Authenticate via browser
cloudcent pricing         # Query pricing from the CLI
cloudcent diagram init <file>      # Scaffold a YAML spec next to the diagram
cloudcent diagram estimate <file>  # Estimate costs from the diagram's spec
cloudcent history         # Show past queries
cloudcent cache stats     # Show cache statistics
cloudcent cache clear     # Clear cache and history
cloudcent metadata refresh  # Download latest pricing metadata
cloudcent config          # Show current configuration
cloudcent pulumi estimate  # Estimate costs from pulumi codes
```

## Configuration

Config is stored at `~/.cloudcent/config.yaml` with permissions set to `600` on Unix.

Data files:
- `~/.cloudcent/metadata.json.gz` — compressed pricing metadata
- `~/.cloudcent/cloudcent.db` — SQLite database (history, cache)

## Contributing

1. Create a issue first if you want to change or fix anything
2. Feel free to use AI but need to test and validate changes before raising prs

## Reporting Issues

[Open an issue](https://github.com/OverloadBlitz/cloudcent-cli/issues)


## Honorable Mention
The `0.0.2-beta-legacy` branch includes a deprecated TUI for querying cloud costs across providers. It is no longer supported due to changes in the pricing data model, but remains noted here as an honorable mention.
This CLI also has a TUI mode, I just disabled it for now and am still working on it

## License

[MIT](LICENSE)
