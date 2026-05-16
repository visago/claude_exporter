# claude_exporter

Prometheus exporter for Claude Code API rate-limit metrics. It makes a minimal API request on a configurable interval, reads the rate-limit headers from the response, and exposes them as Prometheus gauges.

## Prerequisites

- Claude Code installed and authenticated (`~/.claude/.credentials.json` must exist)
- A Claude Pro or Max subscription (the exporter uses Claude Code's own OAuth token — no separate API key needed)

## Quick start

```bash
# Build
make build

# Run (defaults: listen :25283, poll every 30m)
./claude_exporter
```

Metrics are available at `http://localhost:25283/metrics`.

## Configuration

All options can be set via flag or environment variable:

| Flag | Env var | Default | Description |
|---|---|---|---|
| `-listen` | `CLAUDE_EXPORTER_LISTEN` | `:25283` | Address to listen on |
| `-interval` | `CLAUDE_EXPORTER_INTERVAL` | `30m` | How often to poll the API |
| `-credentials` | `CLAUDE_EXPORTER_CREDENTIALS` | `~/.claude/.credentials.json` | Path to Claude Code credentials file |
| `-model` | `CLAUDE_EXPORTER_MODEL` | `claude-haiku-4-5-20251001` | Model used for the poll request (cheapest/fastest is fine) |

## Metrics

All metrics are prefixed `claude_code_`.

| Metric | Type | Description |
|---|---|---|
| `ratelimit_unified_5h_utilization_ratio` | Gauge | Unified 5-hour window utilization (0–1) |
| `ratelimit_unified_5h_reset_timestamp_seconds` | Gauge | Unix timestamp when the 5-hour window resets |
| `ratelimit_unified_7d_utilization_ratio` | Gauge | Unified 7-day window utilization (0–1) |
| `ratelimit_unified_7d_reset_timestamp_seconds` | Gauge | Unix timestamp when the 7-day window resets |
| `ratelimit_requests_limit` | Gauge | Max requests in current window |
| `ratelimit_requests_remaining` | Gauge | Requests remaining in current window |
| `ratelimit_requests_reset_timestamp_seconds` | Gauge | Unix timestamp when request limit resets |
| `ratelimit_tokens_limit` | Gauge | Max tokens in current window |
| `ratelimit_tokens_remaining` | Gauge | Tokens remaining in current window |
| `ratelimit_tokens_reset_timestamp_seconds` | Gauge | Unix timestamp when token limit resets |
| `ratelimit_input_tokens_limit` | Gauge | Max input tokens in current window |
| `ratelimit_input_tokens_remaining` | Gauge | Input tokens remaining in current window |
| `ratelimit_input_tokens_reset_timestamp_seconds` | Gauge | Unix timestamp when input token limit resets |
| `ratelimit_output_tokens_limit` | Gauge | Max output tokens in current window |
| `ratelimit_output_tokens_remaining` | Gauge | Output tokens remaining in current window |
| `ratelimit_output_tokens_reset_timestamp_seconds` | Gauge | Unix timestamp when output token limit resets |
| `scrape_count` | Counter | Total scrapes attempted |
| `scrape_success_count` | Counter | Total scrapes that succeeded |
| `scrape_success` | Gauge | 1 if the last scrape succeeded, 0 otherwise |
| `scrape_duration_seconds` | Gauge | Duration of the last scrape |
| `scrape_last_success_timestamp_seconds` | Gauge | Unix timestamp of the last successful scrape |

## Docker

```bash
# Build and run with docker-compose (mounts ~/.claude/.credentials.json read-only)
docker compose up -d
```

For multi-arch builds (linux/amd64 + linux/arm64) and push to registry:

```bash
make docker
```

The image is based on `scratch` — no shell, just the binary and CA certificates.

When running in Docker, set `CLAUDE_EXPORTER_CREDENTIALS` to the in-container path of the mounted credentials file (default in `docker-compose.yaml` is `/.claude/.credentials.json`).

## Credits

The approach for extracting Claude usage data via the API's rate-limit headers was inspired by [Clawdmeter](https://github.com/HermannBjorgvin/Clawdmeter) by HermannBjorgvin.

## Caveats

The exporter authenticates using Claude Code's internal OAuth flow (`anthropic-beta: oauth-2025-04-20`), which is not publicly documented. Anthropic may change the beta header name, credentials file format, or restrict this flow without notice. If scrapes start returning auth errors, check those first.
