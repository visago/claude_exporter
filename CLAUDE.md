# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Prometheus exporter that polls the Anthropic API on a configurable interval, extracts rate-limit response headers, and exposes them as Prometheus metrics at `/metrics` on port 25283. It authenticates using the OAuth token from Claude Code's local credentials file.

## Commands

```bash
make all          # fmt + vet + build
make build        # go build → ./claude_exporter binary
make fmt          # go fmt ./...
make vet          # go vet ./...
make docker       # multi-arch (linux/amd64,linux/arm64) build + push to registry
make docker-load  # single-arch build loaded into local Docker daemon (for testing)
```

Run directly:
```bash
./claude_exporter \
  -listen :25283 \
  -interval 30m \
  -credentials ~/.claude/.credentials.json \
  -model claude-haiku-4-5-20251001
```

All flags have environment variable equivalents: `CLAUDE_EXPORTER_LISTEN`, `CLAUDE_EXPORTER_INTERVAL`, `CLAUDE_EXPORTER_CREDENTIALS`, `CLAUDE_EXPORTER_MODEL`.

## Architecture

Everything is in a single file (`main.go`). The flow is:

1. `main()` parses flags/env vars, creates a `prometheus.Registry`, constructs an `exporter`, fires an immediate scrape, then starts a ticker goroutine for subsequent scrapes.
2. Each `scrape()` call reads the OAuth token from `~/.claude/.credentials.json` (JSON: `claudeAiOauth.accessToken`), calls `pollAPI()` with a minimal 1-token request, then maps each response header to its registered `prometheus.Gauge` via the `headerMetric` slice in `newExporter`.
3. Header values are parsed by `parseHeaderValue()`, which handles both plain floats and RFC3339 timestamps (converting the latter to Unix seconds).
4. The HTTP server exposes `/metrics` (via `promhttp`) and a minimal HTML index at `/`.

The `exporter` struct holds all metric handles; `newExporter` registers every gauge and counter with the provided registry. No external state beyond the credentials file.

## API call mechanics

The exporter sends a minimal 1-token POST to `https://api.anthropic.com/v1/messages` using headers:

| Header | Value |
|---|---|
| `anthropic-version` | `2023-06-01` |
| `anthropic-beta` | `oauth-2025-04-20` |
| `Authorization` | `Bearer <accessToken>` |
| `User-Agent` | `claude-code/2.1.5` |

**Important:** `anthropic-beta: oauth-2025-04-20` and the credential format (`~/.claude/.credentials.json`) are undocumented internal Claude Code implementation details. Anthropic could rotate the beta flag, change the credentials file schema, or restrict this OAuth flow without public notice. If scrapes start failing with auth errors, check these first.

The `anthropic-version: 2023-06-01` header is the current publicly documented API version as of May 2026.

## Docker / deployment

`docker-compose.yaml` mounts `~/.claude/.credentials.json` read-only into the container at `/.claude/.credentials.json`. The `CLAUDE_EXPORTER_CREDENTIALS` env var must point to this path inside the container if it differs from the default (`$HOME/.claude/.credentials.json`). The scratch-based image requires no shell.
