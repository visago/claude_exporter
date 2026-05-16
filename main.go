package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	apiURL    = "https://api.anthropic.com/v1/messages"
	namespace = "claude_code"
)

const defaultModel = "claude-haiku-4-5-20251001"

func buildReqBody(model string) []byte {
	return []byte(`{"model":"` + model + `","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}`)
}

type oauthCredentials struct {
	ClaudeAiOauth struct {
		AccessToken string `json:"accessToken"`
	} `json:"claudeAiOauth"`
}

type headerMetric struct {
	header string
	gauge  prometheus.Gauge
}

type exporter struct {
	credsPath string
	reqBody   []byte
	metrics   []headerMetric

	scrapeCount        prometheus.Counter
	scrapeSuccessCount prometheus.Counter
	scrapeSuccess      prometheus.Gauge
	scrapeDuration     prometheus.Gauge
	scrapeTimestamp    prometheus.Gauge
}

func newExporter(credsPath, model string, reg prometheus.Registerer) *exporter {
	g := func(name, help string) prometheus.Gauge {
		gauge := prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: namespace,
			Name:      name,
			Help:      help,
		})
		reg.MustRegister(gauge)
		return gauge
	}
	c := func(name, help string) prometheus.Counter {
		counter := prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: namespace,
			Name:      name,
			Help:      help,
		})
		reg.MustRegister(counter)
		return counter
	}

	return &exporter{
		credsPath: credsPath,
		reqBody:   buildReqBody(model),
		metrics: []headerMetric{
			// Unified (Claude Code / Pro subscription) rate limits
			{"anthropic-ratelimit-unified-5h-utilization", g("ratelimit_unified_5h_utilization_ratio", "Unified 5-hour rate limit utilization (0–1)")},
			{"anthropic-ratelimit-unified-5h-reset", g("ratelimit_unified_5h_reset_timestamp_seconds", "Unix timestamp when the unified 5-hour window resets")},
			{"anthropic-ratelimit-unified-7d-utilization", g("ratelimit_unified_7d_utilization_ratio", "Unified 7-day rate limit utilization (0–1)")},
			{"anthropic-ratelimit-unified-7d-reset", g("ratelimit_unified_7d_reset_timestamp_seconds", "Unix timestamp when the unified 7-day window resets")},
			// Standard per-window limits (requests)
			{"anthropic-ratelimit-requests-limit", g("ratelimit_requests_limit", "Maximum requests allowed in the current window")},
			{"anthropic-ratelimit-requests-remaining", g("ratelimit_requests_remaining", "Requests remaining in the current window")},
			{"anthropic-ratelimit-requests-reset", g("ratelimit_requests_reset_timestamp_seconds", "Unix timestamp when the request limit resets")},
			// Standard per-window limits (tokens)
			{"anthropic-ratelimit-tokens-limit", g("ratelimit_tokens_limit", "Maximum tokens allowed in the current window")},
			{"anthropic-ratelimit-tokens-remaining", g("ratelimit_tokens_remaining", "Tokens remaining in the current window")},
			{"anthropic-ratelimit-tokens-reset", g("ratelimit_tokens_reset_timestamp_seconds", "Unix timestamp when the token limit resets")},
			// Input tokens
			{"anthropic-ratelimit-input-tokens-limit", g("ratelimit_input_tokens_limit", "Maximum input tokens allowed in the current window")},
			{"anthropic-ratelimit-input-tokens-remaining", g("ratelimit_input_tokens_remaining", "Input tokens remaining in the current window")},
			{"anthropic-ratelimit-input-tokens-reset", g("ratelimit_input_tokens_reset_timestamp_seconds", "Unix timestamp when the input token limit resets")},
			// Output tokens
			{"anthropic-ratelimit-output-tokens-limit", g("ratelimit_output_tokens_limit", "Maximum output tokens allowed in the current window")},
			{"anthropic-ratelimit-output-tokens-remaining", g("ratelimit_output_tokens_remaining", "Output tokens remaining in the current window")},
			{"anthropic-ratelimit-output-tokens-reset", g("ratelimit_output_tokens_reset_timestamp_seconds", "Unix timestamp when the output token limit resets")},
		},
		scrapeCount:        c("scrape_count", "Total number of scrapes attempted"),
		scrapeSuccessCount: c("scrape_success_count", "Total number of scrapes that succeeded"),
		scrapeSuccess:      g("scrape_success", "1 if the last scrape succeeded, 0 otherwise"),
		scrapeDuration:     g("scrape_duration_seconds", "Duration of the last scrape in seconds"),
		scrapeTimestamp:    g("scrape_last_success_timestamp_seconds", "Unix timestamp of the last successful scrape"),
	}
}

func (e *exporter) scrape() {
	start := time.Now()
	e.scrapeCount.Inc()

	token, err := e.readToken()
	if err != nil {
		log.Printf("credentials error: %v", err)
		e.scrapeSuccess.Set(0)
		e.scrapeDuration.Set(time.Since(start).Seconds())
		return
	}

	hdrs, status, err := pollAPI(token, e.reqBody)
	if err != nil {
		log.Printf("poll error: %v", err)
		e.scrapeSuccess.Set(0)
		e.scrapeDuration.Set(time.Since(start).Seconds())
		return
	}

	if status != http.StatusOK {
		log.Printf("API returned HTTP %d (still parsing rate-limit headers)", status)
	}

	for _, m := range e.metrics {
		val := hdrs.Get(m.header)
		if val == "" {
			continue
		}
		f, err := parseHeaderValue(val)
		if err != nil {
			log.Printf("parse %s=%q: %v", m.header, val, err)
			continue
		}
		m.gauge.Set(f)
	}

	elapsed := time.Since(start).Seconds()
	e.scrapeSuccessCount.Inc()
	e.scrapeSuccess.Set(1)
	e.scrapeDuration.Set(elapsed)
	e.scrapeTimestamp.Set(float64(time.Now().Unix()))
	log.Printf("scrape OK status=%d (%.3fs)", status, elapsed)
}

// parseHeaderValue handles both plain floats (utilization, Unix timestamps)
// and RFC3339 timestamps (used by some standard rate-limit reset headers).
func parseHeaderValue(val string) (float64, error) {
	if f, err := strconv.ParseFloat(val, 64); err == nil {
		return f, nil
	}
	if t, err := time.Parse(time.RFC3339, val); err == nil {
		return float64(t.Unix()), nil
	}
	return 0, fmt.Errorf("cannot parse %q as float or RFC3339 timestamp", val)
}

func (e *exporter) readToken() (string, error) {
	data, err := os.ReadFile(e.credsPath)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", e.credsPath, err)
	}
	var creds oauthCredentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return "", fmt.Errorf("parse credentials: %w", err)
	}
	if creds.ClaudeAiOauth.AccessToken == "" {
		return "", fmt.Errorf("accessToken is empty in %s", e.credsPath)
	}
	return creds.ClaudeAiOauth.AccessToken, nil
}

func pollAPI(token string, body []byte) (http.Header, int, error) {
	req, err := http.NewRequest(http.MethodPost, apiURL, bytes.NewReader(body))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "claude-code/2.1.5")
	req.Header.Set("Authorization", "Bearer "+token)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP request: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body) //nolint:errcheck

	if resp.StatusCode >= 500 {
		return nil, resp.StatusCode, fmt.Errorf("server error %d", resp.StatusCode)
	}
	return resp.Header, resp.StatusCode, nil
}

// envOr returns the value of the environment variable key, or fallback if unset/empty.
func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envDuration parses a duration from an environment variable, falling back to def on error or absence.
func envDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		log.Printf("warning: invalid %s=%q: %v; using default %s", key, v, err, def)
		return def
	}
	return d
}

func main() {
	addr := flag.String("listen",
		envOr("CLAUDE_EXPORTER_LISTEN", ":25283"),
		"address to listen on [$CLAUDE_EXPORTER_LISTEN]")
	interval := flag.Duration("interval",
		envDuration("CLAUDE_EXPORTER_INTERVAL", 30*time.Minute),
		"how often to poll the API [$CLAUDE_EXPORTER_INTERVAL]")
	credsFile := flag.String("credentials",
		envOr("CLAUDE_EXPORTER_CREDENTIALS", filepath.Join(os.Getenv("HOME"), ".claude", ".credentials.json")),
		"path to Claude Code credentials file [$CLAUDE_EXPORTER_CREDENTIALS]")
	model := flag.String("model",
		envOr("CLAUDE_EXPORTER_MODEL", defaultModel),
		"model to use for the poll request [$CLAUDE_EXPORTER_MODEL]")
	flag.Parse()

	reg := prometheus.NewRegistry()
	exp := newExporter(*credsFile, *model, reg)

	go func() {
		exp.scrape()
		ticker := time.NewTicker(*interval)
		defer ticker.Stop()
		for range ticker.C {
			exp.scrape()
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{Registry: reg}))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintln(w, `<html><head><title>Claude Code Exporter</title></head>`+
			`<body><h1>Claude Code Exporter</h1><a href="/metrics">metrics</a></body></html>`)
	})

	log.Printf("listening on %s, polling every %s", *addr, *interval)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
