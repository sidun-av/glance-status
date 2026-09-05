# glance-status Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and publish `glance-status`, a Glance extension widget that renders one compact health-summary card: CPU/RAM/Disk(internal)/Disk(external) usage with hour-over-hour trend arrows, internet download/upload speed (from `speedtest-tracker`) with trend arrows, and an aggregate `All operational`/`Warning`/`Error` status line covering 10 client services plus 3 infrastructure services. Also add a small companion `/status.json` endpoint to the existing `glance-services` repo so this widget can reuse its already-working health checks instead of duplicating them.

**Architecture:** A Go HTTP service matching the conventions of the sibling repos `glance-grafana-sparkline`/`glance-services`: `/widget` returns Glance's custom-widget HTML, `/healthz` for container health. Three independent data sources are fetched concurrently on every render: Grafana's `/api/ds/query` (reusing `glance-grafana-sparkline`'s already-proven request/response format, batched with `offset 1h` PromQL variants so trend needs no stored state), `speedtest-tracker`'s REST API (latest 2 results), and `glance-services`' new `/status.json` plus two direct HTTP checks (npmplus, Home Assistant). Everything is composed into one HTML fragment.

**Tech Stack:** Go 1.23, `gopkg.in/yaml.v3` (only third-party dependency), stdlib `net/http`, manual string-building for HTML (matching sibling repos' `internal/render` style, no `html/template`), Docker multi-stage build, GitHub Actions CI → GHCR.

**Spec:** `/Users/sidun/GIT/glance-status/docs/superpowers/specs/2026-09-05-glance-status-widget-design.md`

## Global Constraints

- Go module path: `github.com/sidun-av/glance-status`. Go 1.23. Only third-party dependency: `gopkg.in/yaml.v3`.
- **This repository is public** (unlike `glance-services`, which is private because real infra credentials/hosts were once committed into its `plan.md`). To keep it safely public: `config.example.yml` contains **no real internal IPs or secrets** — connection-y fields (`grafana.url`, `grafana.token`, `speedtest.url`, `speedtest.token`, `services_status_url`, each `infra_checks[].check_url`) use `${VAR}`-style placeholders expanded from real environment variables at container start via `os.Expand` (exact mechanism already proven in `glance-grafana-sparkline/config.go` — copy it). `grafana.datasource_uid` uses a literal `CHANGE_ME` placeholder (not a secret, just hand-edited, matching `glance-grafana-sparkline`'s own convention). There is **no** `config.docker-default.yml` baking in real values — a real, filled-in `config.yml` is always a mounted volume, never committed.
- Never write real SSH hosts, key filenames, or Komodo/Mongo credential-retrieval commands into any file in this repository. Actual server-side deployment (Komodo stack edit, `glance.yml` wiring) is **explicitly out of scope for this plan document** — it happens afterward, interactively, outside any committed file. This is a direct, deliberate response to what happened with `glance-services`' `plan.md`.
- PromQL queries (not secrets — same convention as `glance-grafana-sparkline`'s own committed example queries):
  - CPU: `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)`
  - RAM: `100 * (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes))`
  - Disk (internal): `100 * (1 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}))`
  - Disk (external): `100 * (1 - (node_filesystem_avail_bytes{mountpoint="/mnt/media-storage"} / node_filesystem_size_bytes{mountpoint="/mnt/media-storage"}))`
- Trend: for each of the 4 Grafana metrics, query the configured expression twice in one batched `/api/ds/query` call — once as-is ("now"), once with literal ` offset 1h` appended to the expression text ("1h ago") — over a `from: "now-5m", to: "now"` window, taking the **last** value of the returned series as the point value (same "last element of `Values[1]`" convention `glance-grafana-sparkline` already uses). For speed, use `speedtest-tracker`'s 2 most recent results (`sort=-created_at&per_page=2`) directly — no Grafana involved.
- Arrow direction always matches the sign of the change (▲ = increased, ▼ = decreased). Arrow **color** is semantic, not directional: for CPU/RAM/Disk(internal)/Disk(external) an increase is bad (`--color-negative`) and a decrease is good (`--color-primary`); for Speed (both download and upload) an increase is good (`--color-primary`) and a decrease is bad (`--color-negative`).
- Arrow is hidden (not just colorless) when the current and prior values are equal after rounding to the nearest whole unit (whole % for CPU/RAM/Disk, whole Mbps for Speed), or when there is no valid prior value to compare against.
- For Speed specifically: if the two results returned by `speedtest-tracker` are more than 2 hours apart, treat the trend as unavailable (hide the arrow, still show the latest value) rather than showing a misleading multi-hour comparison as if it were "the last hour."
- Theme colors: Glance's theme (`main.css`) defines exactly two semantic colors — `--color-primary`/`--color-positive` and `--color-negative`. There is **no** dedicated "warning" color. Status line: `ok` → `--color-primary` filled dot; `error` → `--color-negative` **filled** dot; `warning` → `--color-negative` **outline-only** dot (border, transparent fill) — the same solid/outline distinction Glance's own built-in `.notice-icon-major`/`.notice-icon-minor` uses for exactly this purpose. Never invent a new hex color for "warning."
- Status priority (computed after fetching everything): any tracked service down → `Error: <comma-separated down service/infra names>` (if `glance-services`' `/status.json` call itself fails, include the literal string `service status unavailable` in that list rather than guessing the 10 client services' individual states). Else any of the 4 Grafana metrics has `HasData=true` and `Percent > warning_threshold_percent` (default 90) → `Warning: <comma-separated "Label Percent%">`. Else `All operational`.
- Tracked services for the status line: the 10 existing `glance-services` client services (via its new `/status.json`), plus direct checks for npmplus (`check_url` supplied via config, no baked default) and Home Assistant (`check_url` supplied via config). Prometheus+Grafana health is inferred from the metrics Grafana call's own success/failure — no separate check.
- Health-check semantics for direct infra checks: `GET`, 2xx or 3xx = up, anything else (error, timeout, 4xx/5xx) = down — identical rule to `glance-services/internal/services/check.go`.
- Metric formatting: CPU/RAM/Disk as whole percent (`%.0f%%`). Speed as whole Mbps (`%.0f Mbps`), download and upload as two separate tiles, never converted to a percentage.
- License: MIT, `Copyright (c) 2026 sidun-av`, identical text to the sibling repos.
- CI: GitHub Actions, `test` job (gofmt check, `go vet ./...`, `go test ./...`) gates a `docker` job that builds and pushes `ghcr.io/sidun-av/glance-status:latest` on push to `main` — copy the workflow file verbatim from `github.com/sidun-av/glance-homeassistant`'s `.github/workflows/ci.yml` (already generic).
- Out of scope (do not build): running speedtests ourselves, per-service "degraded" status (only up/down), a per-metric warning threshold (one global threshold for all 4), any infra service beyond npmplus/Prometheus+Grafana/Home Assistant, historical charts (that's `glance-grafana-sparkline`'s job).
- The repo `github.com/sidun-av/glance-status` already exists (created during design, public, with one commit — the design spec). All tasks below commit locally as usual; only Task 10 pushes.

---

### Task 1: Repo scaffolding

**Files:**
- Create: `go.mod`, `go.sum`
- Create: `LICENSE`
- Create: `.dockerignore`
- Create: `.gitignore`

**Interfaces:**
- Produces: the Go module (`github.com/sidun-av/glance-status`) every later task builds on.

- [ ] **Step 1: Initialize the Go module**

```bash
cd /Users/sidun/GIT/glance-status
go mod init github.com/sidun-av/glance-status
go get gopkg.in/yaml.v3
```

- [ ] **Step 2: Write `LICENSE`**

```
MIT License

Copyright (c) 2026 sidun-av

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

- [ ] **Step 3: Write `.dockerignore`**

```
.git
docs/
*.md
```

- [ ] **Step 4: Write `.gitignore`**

```
/glance-status
config.yml
```

- [ ] **Step 5: Verify and commit**

```bash
cd /Users/sidun/GIT/glance-status
cat go.mod
git add go.mod go.sum LICENSE .dockerignore .gitignore
git commit -m "Initialize glance-status Go module"
```

---

### Task 2: Config loading

**Files:**
- Create: `config.go`
- Test: `config_test.go`
- Create: `config.example.yml`

**Interfaces:**
- Produces: `type Config struct { Title string; Grafana GrafanaConfig; Metrics MetricsConfig; Speedtest SpeedtestConfig; ServicesStatusURL string; InfraChecks []InfraCheckConfig; WarningThresholdPercent float64 }` and the nested config types below. `func LoadConfig(path string) (*Config, error)`. Task 8 (`main.go`) calls `LoadConfig`.

- [ ] **Step 1: Write the failing tests**

Create `config_test.go`:

```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTempConfig(t *testing.T, contents string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write temp config: %v", err)
	}
	return path
}

const validConfigYAML = `
grafana:
  url: http://grafana:3000
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`

func TestLoadConfig_Defaults(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Title != "Server Health" {
		t.Errorf("Title = %q, want default %q", cfg.Title, "Server Health")
	}
	if cfg.WarningThresholdPercent != 90 {
		t.Errorf("WarningThresholdPercent = %v, want default 90", cfg.WarningThresholdPercent)
	}
	if cfg.Grafana.URL != "http://grafana:3000" || cfg.Metrics.CPUQuery != "cpu_expr" {
		t.Errorf("unexpected parsed values: %+v", cfg)
	}
}

func TestLoadConfig_TitleEnvOverride(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML)
	t.Setenv("TITLE", "From Env")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Title != "From Env" {
		t.Errorf("Title = %q, want %q", cfg.Title, "From Env")
	}
}

func TestLoadConfig_ExpandsEnvPlaceholdersInConnectionFields(t *testing.T) {
	path := writeTempConfig(t, `
grafana:
  url: ${TEST_GRAFANA_URL}
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
infra_checks:
  - name: npmplus
    check_url: ${TEST_NPMPLUS_URL}
`)
	t.Setenv("TEST_GRAFANA_URL", "http://real-grafana:3000")
	t.Setenv("TEST_NPMPLUS_URL", "http://real-npmplus:81")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Grafana.URL != "http://real-grafana:3000" {
		t.Errorf("Grafana.URL = %q, want expanded value", cfg.Grafana.URL)
	}
	if len(cfg.InfraChecks) != 1 || cfg.InfraChecks[0].CheckURL != "http://real-npmplus:81" {
		t.Errorf("InfraChecks = %+v, want expanded check_url", cfg.InfraChecks)
	}
}

func TestLoadConfig_DoesNotExpandPromQLQueries(t *testing.T) {
	path := writeTempConfig(t, `
grafana:
  url: http://grafana:3000
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: '100 - $__rate_interval'
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Metrics.CPUQuery != "100 - $__rate_interval" {
		t.Errorf("CPUQuery = %q, want untouched literal $ preserved", cfg.Metrics.CPUQuery)
	}
}

func TestLoadConfig_CustomWarningThreshold(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML+"\nwarning_threshold_percent: 80\n")
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.WarningThresholdPercent != 80 {
		t.Errorf("WarningThresholdPercent = %v, want 80", cfg.WarningThresholdPercent)
	}
}

func TestLoadConfig_MissingGrafanaURLRejected(t *testing.T) {
	path := writeTempConfig(t, `
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
  disk_external_query: disk_e_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error for missing grafana.url")
	}
}

func TestLoadConfig_MissingMetricQueryRejected(t *testing.T) {
	path := writeTempConfig(t, `
grafana:
  url: http://grafana:3000
  token: tok
  datasource_uid: uid
metrics:
  cpu_query: cpu_expr
  ram_query: ram_expr
  disk_internal_query: disk_i_expr
speedtest:
  url: http://speedtest:80
  token: stok
services_status_url: http://glance-services:8080/status.json
`)
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error for missing metrics.disk_external_query")
	}
}

func TestLoadConfig_InfraCheckMissingURLRejected(t *testing.T) {
	path := writeTempConfig(t, validConfigYAML+"\ninfra_checks:\n  - name: npmplus\n")
	if _, err := LoadConfig(path); err == nil {
		t.Fatal("want error for infra_checks entry missing check_url")
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	if _, err := LoadConfig("/nonexistent/config.yml"); err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestLoadConfig_ExampleFileIsValid(t *testing.T) {
	// config.example.yml uses ${VAR} placeholders for connection fields —
	// set them so LoadConfig's required-field checks pass, proving the
	// file's structure (not its placeholder values) is valid.
	for _, v := range []string{"GRAFANA_URL", "GRAFANA_TOKEN", "SPEEDTEST_URL", "SPEEDTEST_TOKEN", "SERVICES_STATUS_URL", "INFRA_CHECK_NPMPLUS_URL", "INFRA_CHECK_HOMEASSISTANT_URL"} {
		t.Setenv(v, "http://placeholder")
	}
	cfg, err := LoadConfig("config.example.yml")
	if err != nil {
		t.Fatalf("LoadConfig(config.example.yml): %v", err)
	}
	if len(cfg.InfraChecks) != 2 {
		t.Errorf("len(InfraChecks) = %d, want 2", len(cfg.InfraChecks))
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./... -v`
Expected: FAIL — `config.go` / `Config` / `LoadConfig` don't exist yet, and `config.example.yml` doesn't exist yet.

- [ ] **Step 3: Implement `config.go`**

```go
package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type GrafanaConfig struct {
	URL           string `yaml:"url"`
	Token         string `yaml:"token"`
	DatasourceUID string `yaml:"datasource_uid"`
}

type MetricsConfig struct {
	CPUQuery          string `yaml:"cpu_query"`
	RAMQuery          string `yaml:"ram_query"`
	DiskInternalQuery string `yaml:"disk_internal_query"`
	DiskExternalQuery string `yaml:"disk_external_query"`
}

type SpeedtestConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type InfraCheckConfig struct {
	Name     string `yaml:"name"`
	CheckURL string `yaml:"check_url"`
}

type Config struct {
	Title                   string             `yaml:"title"`
	Grafana                 GrafanaConfig      `yaml:"grafana"`
	Metrics                 MetricsConfig      `yaml:"metrics"`
	Speedtest               SpeedtestConfig    `yaml:"speedtest"`
	ServicesStatusURL       string             `yaml:"services_status_url"`
	InfraChecks             []InfraCheckConfig `yaml:"infra_checks"`
	WarningThresholdPercent float64            `yaml:"warning_threshold_percent"`
}

func LoadConfig(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Only connection-y fields get environment variable expansion (same
	// convention as glance-grafana-sparkline's config.go). PromQL query
	// text is deliberately left untouched so a literal "$" in a query
	// (e.g. a Grafana macro like $__rate_interval) is never silently
	// replaced with an empty string.
	cfg.Grafana.URL = os.Expand(cfg.Grafana.URL, os.Getenv)
	cfg.Grafana.Token = os.Expand(cfg.Grafana.Token, os.Getenv)
	cfg.Grafana.DatasourceUID = os.Expand(cfg.Grafana.DatasourceUID, os.Getenv)
	cfg.Speedtest.URL = os.Expand(cfg.Speedtest.URL, os.Getenv)
	cfg.Speedtest.Token = os.Expand(cfg.Speedtest.Token, os.Getenv)
	cfg.ServicesStatusURL = os.Expand(cfg.ServicesStatusURL, os.Getenv)
	for i := range cfg.InfraChecks {
		cfg.InfraChecks[i].CheckURL = os.Expand(cfg.InfraChecks[i].CheckURL, os.Getenv)
	}

	if v, ok := lookupNonEmptyEnv("TITLE"); ok {
		cfg.Title = v
	}
	if cfg.Title == "" {
		cfg.Title = "Server Health"
	}
	if cfg.WarningThresholdPercent == 0 {
		cfg.WarningThresholdPercent = 90
	}

	if cfg.Grafana.URL == "" {
		return nil, fmt.Errorf("grafana.url is required")
	}
	if cfg.Grafana.Token == "" {
		return nil, fmt.Errorf("grafana.token is required")
	}
	if cfg.Grafana.DatasourceUID == "" {
		return nil, fmt.Errorf("grafana.datasource_uid is required")
	}
	if cfg.Metrics.CPUQuery == "" || cfg.Metrics.RAMQuery == "" || cfg.Metrics.DiskInternalQuery == "" || cfg.Metrics.DiskExternalQuery == "" {
		return nil, fmt.Errorf("metrics: all four queries (cpu_query, ram_query, disk_internal_query, disk_external_query) are required")
	}
	if cfg.Speedtest.URL == "" {
		return nil, fmt.Errorf("speedtest.url is required")
	}
	if cfg.Speedtest.Token == "" {
		return nil, fmt.Errorf("speedtest.token is required")
	}
	if cfg.ServicesStatusURL == "" {
		return nil, fmt.Errorf("services_status_url is required")
	}
	for i, c := range cfg.InfraChecks {
		if c.Name == "" || c.CheckURL == "" {
			return nil, fmt.Errorf("infra_checks[%d]: name and check_url are both required", i)
		}
	}

	return &cfg, nil
}

// lookupNonEmptyEnv returns (value, true) only when the environment variable
// is actually set to a non-empty string — matching the sibling repos'
// convention, so an unfilled-in Komodo stack variable (which arrives as an
// empty string, not unset) doesn't override a real YAML value with "".
func lookupNonEmptyEnv(name string) (string, bool) {
	v, ok := os.LookupEnv(name)
	if !ok || v == "" {
		return "", false
	}
	return v, true
}
```

- [ ] **Step 4: Write `config.example.yml`**

```yaml
# This file documents the config.yml format — copy it, fill in real values
# (or set the referenced environment variables and leave the ${...}
# placeholders as-is), and mount it as /config.yml. Connection fields use
# ${VAR} placeholders expanded from real environment variables at container
# start (see docker-compose.example.yml) — never commit real URLs/tokens
# here. PromQL queries are not secrets and are filled in with real,
# working defaults for a standard node_exporter + Prometheus setup.

title: Server Health   # env: TITLE

grafana:
  url: ${GRAFANA_URL}            # e.g. http://grafana:3000 — reachable from this container
  token: ${GRAFANA_TOKEN}        # Grafana Service Account token (Viewer + Data Source Reader)
  datasource_uid: CHANGE_ME      # Grafana → Connections → Data sources → your datasource → UID is in the URL

metrics:
  cpu_query: '100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100)'
  ram_query: '100 * (1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes))'
  disk_internal_query: '100 * (1 - (node_filesystem_avail_bytes{mountpoint="/"} / node_filesystem_size_bytes{mountpoint="/"}))'
  disk_external_query: '100 * (1 - (node_filesystem_avail_bytes{mountpoint="/mnt/media-storage"} / node_filesystem_size_bytes{mountpoint="/mnt/media-storage"}))'   # adjust the mountpoint to your own second disk

speedtest:
  url: ${SPEEDTEST_URL}          # e.g. http://speedtest-tracker:80 — reachable from this container
  token: ${SPEEDTEST_TOKEN}      # speedtest-tracker Admin → API Tokens, scope: results:read

services_status_url: ${SERVICES_STATUS_URL}   # e.g. http://glance-services:8080/status.json

infra_checks:
  - name: npmplus
    check_url: ${INFRA_CHECK_NPMPLUS_URL}
  - name: Home Assistant
    check_url: ${INFRA_CHECK_HOMEASSISTANT_URL}

warning_threshold_percent: 90
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./... -v`
Expected: PASS, all tests.

- [ ] **Step 6: Commit**

```bash
git add config.go config_test.go config.example.yml
git commit -m "Add config loading with env-expanded connection fields"
```

---

### Task 3: Grafana query client

**Files:**
- Create: `internal/grafana/client.go`
- Test: `internal/grafana/client_test.go`

**Interfaces:**
- Produces: `type PanelQuery struct { ID, Expr string }`, `type SeriesResult struct { Values []float64; Err error }`, `type Client struct { HTTPClient *http.Client; BaseURL, Token, DatasourceUID string }`, `func New(baseURL, token, datasourceUID string) *Client`, `func (c *Client) QueryPanels(ctx context.Context, panels []PanelQuery, from, to string, intervalMs, maxDataPoints int) (map[string]SeriesResult, error)`. Task 4 (`internal/metrics`) is the only consumer.
- This package is a verbatim port of `glance-grafana-sparkline`'s already-working `internal/grafana/client.go` — its request/response shape against the real Grafana instance is already proven in production, so no new API guesswork here.

- [ ] **Step 1: Write the failing tests**

Create `internal/grafana/client_test.go`:

```go
package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueryPanelsSendsExpectedRequest(t *testing.T) {
	var gotBody dsQueryRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/ds/query" {
			t.Errorf("expected path /api/ds/query, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		fmt.Fprint(w, `{"results": {"cpu_now": {"frames": [{"data": {"values": [[1,2],[11,22]]}}]}}}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "prom-uid")
	results, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "up"}}, "now-5m", "now", 60000, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gotBody.Queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(gotBody.Queries))
	}
	q := gotBody.Queries[0]
	if q.RefID != "cpu_now" || q.Expr != "up" || q.Datasource.UID != "prom-uid" || q.Datasource.Type != "prometheus" {
		t.Fatalf("unexpected query sent: %+v", q)
	}
	if gotBody.From != "now-5m" || gotBody.To != "now" {
		t.Fatalf("unexpected time range: from=%q to=%q", gotBody.From, gotBody.To)
	}

	cpu, ok := results["cpu_now"]
	if !ok || cpu.Err != nil {
		t.Fatalf("expected successful cpu_now result, got %+v", cpu)
	}
	if len(cpu.Values) != 2 || cpu.Values[1] != 22 {
		t.Fatalf("expected values [11 22], got %v", cpu.Values)
	}
}

func TestQueryPanelsMarksMissingRefIDAsNoData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results": {}}`)
	}))
	defer server.Close()

	client := New(server.URL, "token", "uid")
	results, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "up"}}, "now-5m", "now", 60000, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cpu := results["cpu_now"]; cpu.Err == nil {
		t.Fatal("expected an error for a panel missing from the response")
	}
}

func TestQueryPanelsReturnsErrorOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, "token", "uid")
	client.HTTPClient.Timeout = time.Second

	_, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "up"}}, "now-5m", "now", 60000, 5)
	if err == nil {
		t.Fatal("expected an error for a 500 response, got nil")
	}
}

func TestQueryPanelsPreservesDatasourceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results": {"cpu_now": {"error": "datasource error: query failed"}}}`)
	}))
	defer server.Close()

	client := New(server.URL, "token", "uid")
	results, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "invalid"}}, "now-5m", "now", 60000, 5)
	if err != nil {
		t.Fatalf("unexpected request error: %v", err)
	}

	cpu := results["cpu_now"]
	if cpu.Err == nil {
		t.Fatal("expected an error for panel with datasource error")
	}
	if cpu.Err.Error() != `panel "cpu_now": datasource error: query failed` {
		t.Errorf("expected error message to contain datasource detail, got %q", cpu.Err.Error())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/grafana/... -v`
Expected: FAIL — package `grafana` doesn't exist yet.

- [ ] **Step 3: Implement `internal/grafana/client.go`**

```go
package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	HTTPClient    *http.Client
	BaseURL       string
	Token         string
	DatasourceUID string
}

func New(baseURL, token, datasourceUID string) *Client {
	return &Client{
		HTTPClient:    &http.Client{Timeout: 5 * time.Second},
		BaseURL:       strings.TrimRight(baseURL, "/"),
		Token:         token,
		DatasourceUID: datasourceUID,
	}
}

type PanelQuery struct {
	ID   string
	Expr string
}

type SeriesResult struct {
	Values []float64
	Err    error
}

type dsQueryRequest struct {
	Queries []dsQuery `json:"queries"`
	From    string    `json:"from"`
	To      string    `json:"to"`
}

type dsQuery struct {
	RefID         string            `json:"refId"`
	Datasource    dsQueryDatasource `json:"datasource"`
	Expr          string            `json:"expr"`
	Range         bool              `json:"range"`
	Instant       bool              `json:"instant"`
	IntervalMs    int               `json:"intervalMs"`
	MaxDataPoints int               `json:"maxDataPoints"`
}

type dsQueryDatasource struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

type dsQueryResponse struct {
	Results map[string]dsResult `json:"results"`
}

type dsResult struct {
	Frames []dsFrame `json:"frames"`
	Error  string    `json:"error,omitempty"`
}

type dsFrame struct {
	Data dsFrameData `json:"data"`
}

type dsFrameData struct {
	Values [][]float64 `json:"values"`
}

// QueryPanels sends one batched /api/ds/query request (one query per panel,
// refId == panel ID) and returns each panel's parsed time series, keyed by
// panel ID. A panel missing from the response (or with a datasource-level
// error) gets a SeriesResult with Err set; other panels are unaffected. The
// returned top-level error is only for request-level failures (network,
// non-200 status, malformed response) that affect every panel at once.
func (c *Client) QueryPanels(ctx context.Context, panels []PanelQuery, from, to string, intervalMs, maxDataPoints int) (map[string]SeriesResult, error) {
	out := make(map[string]SeriesResult, len(panels))
	if len(panels) == 0 {
		return out, nil
	}

	queries := make([]dsQuery, len(panels))
	for i, p := range panels {
		queries[i] = dsQuery{
			RefID:         p.ID,
			Datasource:    dsQueryDatasource{Type: "prometheus", UID: c.DatasourceUID},
			Expr:          p.Expr,
			Range:         true,
			Instant:       false,
			IntervalMs:    intervalMs,
			MaxDataPoints: maxDataPoints,
		}
	}

	payload, err := json.Marshal(dsQueryRequest{Queries: queries, From: from, To: to})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ds/query", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request grafana: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grafana returned status %d", resp.StatusCode)
	}

	var parsed dsQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	for _, p := range panels {
		result, ok := parsed.Results[p.ID]
		if !ok {
			out[p.ID] = SeriesResult{Err: fmt.Errorf("no data for panel %q", p.ID)}
			continue
		}
		if result.Error != "" {
			out[p.ID] = SeriesResult{Err: fmt.Errorf("panel %q: %s", p.ID, result.Error)}
			continue
		}
		if len(result.Frames) == 0 || len(result.Frames[0].Data.Values) < 2 {
			out[p.ID] = SeriesResult{Err: fmt.Errorf("no data for panel %q", p.ID)}
			continue
		}
		out[p.ID] = SeriesResult{Values: result.Frames[0].Data.Values[1]}
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/grafana/... -v`
Expected: PASS, all 4 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/grafana/client.go internal/grafana/client_test.go
git commit -m "Add Grafana ds/query client (ported from glance-grafana-sparkline)"
```

---

### Task 4: Metrics fetching (CPU/RAM/Disk with trend)

**Files:**
- Create: `internal/metrics/metrics.go`
- Test: `internal/metrics/metrics_test.go`

**Interfaces:**
- Consumes: `grafana.Client`, `grafana.PanelQuery`, `grafana.SeriesResult` (Task 3).
- Produces: `type Metric struct { Label string; Percent float64; HasData bool; ShowArrow bool; ArrowUp bool; ArrowGood bool }`, `type Config struct { CPUQuery, RAMQuery, DiskInternalQuery, DiskExternalQuery string }`, `func Fetch(ctx context.Context, client *grafana.Client, cfg Config) ([]Metric, error)` — always returns exactly 4 metrics in order [CPU, RAM, Disk (internal → label "DISK"), Disk (external → label "DISK (EXT)")] when the request itself succeeds; a per-metric data problem sets `HasData=false` on that one entry rather than failing the whole call. Task 8 (`main.go`) calls `Fetch` and only treats a non-nil `error` (whole-request failure) as "Grafana unavailable."

- [ ] **Step 1: Write the failing tests**

Create `internal/metrics/metrics_test.go`:

```go
package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sidun-av/glance-status/internal/grafana"
)

func TestFetch_ArrowDirectionAndSemantics(t *testing.T) {
	// cpu: now=50, 1h ago=40 -> increased -> usage metric -> bad (red) arrow up
	// ram: now=30, 1h ago=30 -> unchanged -> no arrow
	// disk_internal: now=20, 1h ago=25 -> decreased -> usage metric -> good (green) arrow down
	// disk_external: both queries missing from response -> no data
	values := map[string]float64{
		"cpu_now": 50, "cpu_1h": 40,
		"ram_now": 30, "ram_1h": 30,
		"disk_internal_now": 20, "disk_internal_1h": 25,
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				RefID string `json:"refId"`
			} `json:"queries"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		results := map[string]any{}
		for _, q := range req.Queries {
			if v, ok := values[q.RefID]; ok {
				results[q.RefID] = map[string]any{
					"frames": []any{map[string]any{"data": map[string]any{"values": [][]float64{{0}, {v}}}}},
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer server.Close()

	client := grafana.New(server.URL, "token", "uid")
	cfg := Config{
		CPUQuery:          "cpu_q",
		RAMQuery:          "ram_q",
		DiskInternalQuery: "disk_internal_q",
		DiskExternalQuery: "disk_external_q",
	}

	got, err := Fetch(context.Background(), client, cfg)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("len(got) = %d, want 4", len(got))
	}

	cpu := got[0]
	if cpu.Label != "CPU" || !cpu.HasData || cpu.Percent != 50 || !cpu.ShowArrow || !cpu.ArrowUp || cpu.ArrowGood {
		t.Errorf("CPU metric = %+v, want increased/bad arrow", cpu)
	}

	ram := got[1]
	if ram.Label != "RAM" || !ram.HasData || ram.ShowArrow {
		t.Errorf("RAM metric = %+v, want no arrow (unchanged)", ram)
	}

	diskInt := got[2]
	if diskInt.Label != "DISK" || !diskInt.HasData || diskInt.Percent != 20 || !diskInt.ShowArrow || diskInt.ArrowUp || !diskInt.ArrowGood {
		t.Errorf("Disk(internal) metric = %+v, want decreased/good arrow", diskInt)
	}

	diskExt := got[3]
	if diskExt.Label != "DISK (EXT)" || diskExt.HasData {
		t.Errorf("Disk(external) metric = %+v, want HasData=false (missing from response)", diskExt)
	}
}

func TestFetch_RoundingHidesArrowForSubUnitChange(t *testing.T) {
	// 50.4 rounds to 50, 49.6 rounds to 50 -> same rounded value -> no arrow
	values := map[string]float64{
		"cpu_now": 50.4, "cpu_1h": 49.6,
		"ram_now": 30, "ram_1h": 30,
		"disk_internal_now": 20, "disk_internal_1h": 20,
		"disk_external_now": 10, "disk_external_1h": 10,
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				RefID string `json:"refId"`
			} `json:"queries"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		results := map[string]any{}
		for _, q := range req.Queries {
			if v, ok := values[q.RefID]; ok {
				results[q.RefID] = map[string]any{
					"frames": []any{map[string]any{"data": map[string]any{"values": [][]float64{{0}, {v}}}}},
				}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer server.Close()

	client := grafana.New(server.URL, "token", "uid")
	cfg := Config{CPUQuery: "cpu_q", RAMQuery: "ram_q", DiskInternalQuery: "disk_internal_q", DiskExternalQuery: "disk_external_q"}

	got, err := Fetch(context.Background(), client, cfg)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if got[0].ShowArrow {
		t.Errorf("CPU = %+v, want no arrow (rounds to same value)", got[0])
	}
}

func TestFetch_RequestFailureReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := grafana.New(server.URL, "token", "uid")
	client.HTTPClient.Timeout = 1e9 // 1s, avoid slow test on hang
	cfg := Config{CPUQuery: "cpu_q", RAMQuery: "ram_q", DiskInternalQuery: "disk_internal_q", DiskExternalQuery: "disk_external_q"}

	if _, err := Fetch(context.Background(), client, cfg); err == nil {
		t.Fatal("want error when the whole Grafana request fails")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/metrics/... -v`
Expected: FAIL — package `metrics` doesn't exist yet.

- [ ] **Step 3: Implement `internal/metrics/metrics.go`**

```go
package metrics

import (
	"context"

	"github.com/sidun-av/glance-status/internal/grafana"
)

type Config struct {
	CPUQuery          string
	RAMQuery          string
	DiskInternalQuery string
	DiskExternalQuery string
}

// Metric is a single usage stat (CPU, RAM, or one of the two disks). All
// four are "usage" metrics where an increase is bad — ArrowGood is false
// when the value went up. This is intentionally not configurable per
// metric (YAGNI): only Speed (a different package) has the opposite
// semantics, and it has its own type for that reason.
type Metric struct {
	Label     string
	Percent   float64
	HasData   bool
	ShowArrow bool
	ArrowUp   bool
	ArrowGood bool
}

type querySpec struct {
	id, label, query string
}

// Fetch queries Grafana for the current and 1-hour-ago value of all four
// configured metrics in a single batched call (8 panel queries: 4 metrics x
// {now, now offset 1h}) and returns one Metric per metric, always in the
// fixed order [CPU, RAM, Disk (internal), Disk (external)]. A non-nil error
// means the whole Grafana request failed (network error, non-200, malformed
// response) — the caller should treat that as "Grafana unavailable" for all
// four. A single metric missing its own data (e.g. a mountpoint that no
// longer exists) does not fail the call — that Metric just has
// HasData=false.
func Fetch(ctx context.Context, client *grafana.Client, cfg Config) ([]Metric, error) {
	specs := []querySpec{
		{"cpu", "CPU", cfg.CPUQuery},
		{"ram", "RAM", cfg.RAMQuery},
		{"disk_internal", "DISK", cfg.DiskInternalQuery},
		{"disk_external", "DISK (EXT)", cfg.DiskExternalQuery},
	}

	panels := make([]grafana.PanelQuery, 0, len(specs)*2)
	for _, s := range specs {
		panels = append(panels,
			grafana.PanelQuery{ID: s.id + "_now", Expr: s.query},
			grafana.PanelQuery{ID: s.id + "_1h", Expr: s.query + " offset 1h"},
		)
	}

	results, err := client.QueryPanels(ctx, panels, "now-5m", "now", 60000, 5)
	if err != nil {
		return nil, err
	}

	metrics := make([]Metric, len(specs))
	for i, s := range specs {
		now, nowOK := lastValue(results[s.id+"_now"])
		if !nowOK {
			metrics[i] = Metric{Label: s.label}
			continue
		}
		m := Metric{Label: s.label, HasData: true, Percent: now}
		if prev, prevOK := lastValue(results[s.id+"_1h"]); prevOK && roundToInt(now) != roundToInt(prev) {
			m.ShowArrow = true
			m.ArrowUp = now > prev
			m.ArrowGood = !m.ArrowUp // usage metric: up = bad, down = good
		}
		metrics[i] = m
	}
	return metrics, nil
}

func lastValue(r grafana.SeriesResult) (float64, bool) {
	if r.Err != nil || len(r.Values) == 0 {
		return 0, false
	}
	return r.Values[len(r.Values)-1], true
}

func roundToInt(v float64) int {
	return int(v + 0.5) // metric values are always >= 0 (percentages)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/metrics/... -v`
Expected: PASS, all 3 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/metrics/metrics.go internal/metrics/metrics_test.go
git commit -m "Add CPU/RAM/Disk metric fetching with hour-over-hour trend"
```

---

### Task 5: Speedtest fetching (download/upload with trend)

**Files:**
- Create: `internal/speedtest/speedtest.go`
- Test: `internal/speedtest/speedtest_test.go`

**Interfaces:**
- Produces: `type Reading struct { Mbps float64; HasData bool; ShowArrow bool; ArrowUp bool; ArrowGood bool }`, `type Trend struct { Download, Upload Reading }`, `type Client struct { HTTPClient *http.Client; BaseURL, Token string }`, `func New(baseURL, token string) *Client`, `func (c *Client) Fetch(ctx context.Context) (Trend, error)`. Task 8 (`main.go`) is the only consumer.

- [ ] **Step 1: Write the failing tests**

Create `internal/speedtest/speedtest_test.go`:

```go
package speedtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetch_TrendComputation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		if r.URL.Query().Get("sort") != "-created_at" || r.URL.Query().Get("per_page") != "2" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"data": [
			{"download": 150, "upload": 20, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 100, "upload": 22, "created_at": "2026-09-05T11:00:00Z"}
		]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !trend.Download.HasData || trend.Download.Mbps != 150 || !trend.Download.ShowArrow || !trend.Download.ArrowUp || !trend.Download.ArrowGood {
		t.Errorf("Download = %+v, want increased/good arrow at 150", trend.Download)
	}
	if !trend.Upload.HasData || trend.Upload.Mbps != 20 || !trend.Upload.ShowArrow || trend.Upload.ArrowUp || trend.Upload.ArrowGood {
		t.Errorf("Upload = %+v, want decreased/bad arrow at 20", trend.Upload)
	}
}

func TestFetch_StaleComparisonHidesArrow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": [
			{"download": 150, "upload": 20, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 100, "upload": 22, "created_at": "2026-09-05T08:00:00Z"}
		]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if trend.Download.ShowArrow || trend.Upload.ShowArrow {
		t.Errorf("expected no arrows for a >2h-old comparison, got %+v", trend)
	}
	if trend.Download.Mbps != 150 || !trend.Download.HasData {
		t.Errorf("expected latest value to still show: %+v", trend.Download)
	}
}

func TestFetch_RoundingHidesArrowForSubUnitChange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": [
			{"download": 150.4, "upload": 20, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 149.6, "upload": 20, "created_at": "2026-09-05T11:00:00Z"}
		]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if trend.Download.ShowArrow {
		t.Errorf("Download = %+v, want no arrow (rounds to same value)", trend.Download)
	}
}

func TestFetch_NoData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": []}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if trend.Download.HasData || trend.Upload.HasData {
		t.Errorf("expected HasData=false with no results, got %+v", trend)
	}
}

func TestFetch_OnlyOneResultShowsValueNoArrow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": [{"download": 150, "upload": 20, "created_at": "2026-09-05T12:00:00Z"}]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !trend.Download.HasData || trend.Download.Mbps != 150 || trend.Download.ShowArrow {
		t.Errorf("Download = %+v, want value shown, no arrow", trend.Download)
	}
}

func TestFetch_NonOKStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(server.URL, "bad-token")
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("want error for a 401 response")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/speedtest/... -v`
Expected: FAIL — package `speedtest` doesn't exist yet.

- [ ] **Step 3: Implement `internal/speedtest/speedtest.go`**

```go
package speedtest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	HTTPClient *http.Client
	BaseURL    string
	Token      string
}

func New(baseURL, token string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
		BaseURL:    strings.TrimRight(baseURL, "/"),
		Token:      token,
	}
}

// Reading is one direction's (download or upload) latest value plus its
// hour-over-hour trend.
type Reading struct {
	Mbps      float64
	HasData   bool
	ShowArrow bool
	ArrowUp   bool
	ArrowGood bool // for speed, an increase is good — opposite of internal/metrics.Metric
}

type Trend struct {
	Download Reading
	Upload   Reading
}

// staleAfter bounds how old the "previous" result may be relative to the
// latest one before we stop calling it "the last hour" — if
// speedtest-tracker's own schedule isn't actually hourly yet (or had a
// gap), a multi-hour-old comparison would be misleading.
const staleAfter = 2 * time.Hour

type apiResult struct {
	Download  float64 `json:"download"`
	Upload    float64 `json:"upload"`
	CreatedAt string  `json:"created_at"`
}

type apiResponse struct {
	Data []apiResult `json:"data"`
}

type result struct {
	downloadMbps float64
	uploadMbps   float64
	createdAt    time.Time
}

// Fetch retrieves the 2 most recent speedtest-tracker results and derives a
// trend for download and upload independently. A non-nil error means the
// request itself failed (network error, non-200, malformed response) — the
// caller should treat that as "speed data unavailable." Fewer than 2
// results (or a >2h gap between them) is not an error — the returned Trend
// still carries the latest value(s), just with ShowArrow=false.
func (c *Client) Fetch(ctx context.Context) (Trend, error) {
	results, err := c.latestResults(ctx, 2)
	if err != nil {
		return Trend{}, err
	}
	if len(results) == 0 {
		return Trend{}, nil
	}

	latest := results[0]
	trend := Trend{
		Download: Reading{Mbps: latest.downloadMbps, HasData: true},
		Upload:   Reading{Mbps: latest.uploadMbps, HasData: true},
	}

	if len(results) < 2 {
		return trend, nil
	}
	prev := results[1]
	if latest.createdAt.Sub(prev.createdAt) > staleAfter {
		return trend, nil
	}

	if roundToInt(latest.downloadMbps) != roundToInt(prev.downloadMbps) {
		trend.Download.ShowArrow = true
		trend.Download.ArrowUp = latest.downloadMbps > prev.downloadMbps
		trend.Download.ArrowGood = trend.Download.ArrowUp
	}
	if roundToInt(latest.uploadMbps) != roundToInt(prev.uploadMbps) {
		trend.Upload.ShowArrow = true
		trend.Upload.ArrowUp = latest.uploadMbps > prev.uploadMbps
		trend.Upload.ArrowGood = trend.Upload.ArrowUp
	}
	return trend, nil
}

func roundToInt(v float64) int {
	return int(v + 0.5) // speeds are always >= 0
}

func (c *Client) latestResults(ctx context.Context, n int) ([]result, error) {
	url := fmt.Sprintf("%s/api/v1/results?sort=-created_at&per_page=%d", c.BaseURL, n)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request speedtest-tracker: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("speedtest-tracker returned status %d", resp.StatusCode)
	}

	var parsed apiResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	out := make([]result, len(parsed.Data))
	for i, r := range parsed.Data {
		t, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("parse created_at %q: %w", r.CreatedAt, err)
		}
		out[i] = result{downloadMbps: r.Download, uploadMbps: r.Upload, createdAt: t}
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/speedtest/... -v`
Expected: PASS, all 6 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/speedtest/speedtest.go internal/speedtest/speedtest_test.go
git commit -m "Add speedtest-tracker client with hour-over-hour trend"
```

---

### Task 6: Status aggregation

**Files:**
- Create: `internal/status/status.go`
- Test: `internal/status/status_test.go`

**Interfaces:**
- Produces: `type Level int` with constants `OK, Warning, Error`, `type ServiceStatus struct { Name string; Up bool }` (JSON tags `name`/`up` — matches Task 11's `glance-services` `/status.json` shape), `type InfraCheck struct { Name, CheckURL string }`, `type MetricInput struct { Label string; Percent float64; HasData bool }`, `type Config struct { ServicesStatusURL string; InfraChecks []InfraCheck; WarningThresholdPercent float64 }`, `type Result struct { Level Level; Message string }`, `func Compute(ctx context.Context, cfg Config, metrics []MetricInput, extraDown []string) Result` — `extraDown` lets the caller (Task 8) fold in problems this package has no way to detect itself, specifically "Prometheus + Grafana" when the metrics fetch fails entirely; pass `nil` when there's nothing extra to report.
- Consumes (at the call site in Task 8): `[]metrics.Metric` gets mapped to `[]status.MetricInput` field-by-field.

- [ ] **Step 1: Write the failing tests**

Create `internal/status/status_test.go`:

```go
package status

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompute_AllOperational(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"Jellyfin","up":true},{"name":"Kavita","up":true}]`)
	}))
	defer svc.Close()
	infra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer infra.Close()

	cfg := Config{
		ServicesStatusURL:       svc.URL,
		InfraChecks:             []InfraCheck{{Name: "npmplus", CheckURL: infra.URL}},
		WarningThresholdPercent: 90,
	}
	metrics := []MetricInput{{Label: "CPU", Percent: 40, HasData: true}}

	result := Compute(context.Background(), cfg, metrics, nil)
	if result.Level != OK || result.Message != "All operational" {
		t.Errorf("Compute() = %+v, want OK/All operational", result)
	}
}

func TestCompute_DownClientServiceIsError(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"Jellyfin","up":true},{"name":"Kavita","up":false}]`)
	}))
	defer svc.Close()

	cfg := Config{ServicesStatusURL: svc.URL, WarningThresholdPercent: 90}
	result := Compute(context.Background(), cfg, nil, nil)
	if result.Level != Error || result.Message != "Error: Kavita" {
		t.Errorf("Compute() = %+v, want Error: Kavita", result)
	}
}

func TestCompute_DownInfraCheckIsError(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer svc.Close()
	infra := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer infra.Close()

	cfg := Config{
		ServicesStatusURL: svc.URL,
		InfraChecks:       []InfraCheck{{Name: "npmplus", CheckURL: infra.URL}},
		WarningThresholdPercent: 90,
	}
	result := Compute(context.Background(), cfg, nil, nil)
	if result.Level != Error || result.Message != "Error: npmplus" {
		t.Errorf("Compute() = %+v, want Error: npmplus", result)
	}
}

func TestCompute_HighMetricIsWarningWhenNothingDown(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"Jellyfin","up":true}]`)
	}))
	defer svc.Close()

	cfg := Config{ServicesStatusURL: svc.URL, WarningThresholdPercent: 90}
	metrics := []MetricInput{{Label: "DISK (EXT)", Percent: 91, HasData: true}, {Label: "CPU", Percent: 40, HasData: true}}
	result := Compute(context.Background(), cfg, metrics, nil)
	if result.Level != Warning || result.Message != "Warning: DISK (EXT) 91%" {
		t.Errorf("Compute() = %+v, want Warning: DISK (EXT) 91%%", result)
	}
}

func TestCompute_MetricWithoutDataNeverTriggersWarning(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer svc.Close()

	cfg := Config{ServicesStatusURL: svc.URL, WarningThresholdPercent: 90}
	metrics := []MetricInput{{Label: "CPU", Percent: 0, HasData: false}}
	result := Compute(context.Background(), cfg, metrics, nil)
	if result.Level != OK {
		t.Errorf("Compute() = %+v, want OK (no-data metric must not trigger Warning)", result)
	}
}

func TestCompute_ServicesStatusUnavailableIsError(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer svc.Close()

	cfg := Config{ServicesStatusURL: svc.URL, WarningThresholdPercent: 90}
	result := Compute(context.Background(), cfg, nil, nil)
	if result.Level != Error || result.Message != "Error: service status unavailable" {
		t.Errorf("Compute() = %+v, want Error: service status unavailable", result)
	}
}

func TestCompute_ErrorTakesPriorityOverWarning(t *testing.T) {
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"Kavita","up":false}]`)
	}))
	defer svc.Close()

	cfg := Config{ServicesStatusURL: svc.URL, WarningThresholdPercent: 90}
	metrics := []MetricInput{{Label: "CPU", Percent: 95, HasData: true}}
	result := Compute(context.Background(), cfg, metrics, nil)
	if result.Level != Error || result.Message != "Error: Kavita" {
		t.Errorf("Compute() = %+v, want Error to win over Warning", result)
	}
}

func TestCompute_ExtraDownIncludedInErrorList(t *testing.T) {
	// extraDown is how the caller (main.go, Task 8) reports "Prometheus +
	// Grafana" is down when the metrics fetch itself failed entirely —
	// this package has no way to know that on its own.
	svc := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"Jellyfin","up":true}]`)
	}))
	defer svc.Close()

	cfg := Config{ServicesStatusURL: svc.URL, WarningThresholdPercent: 90}
	result := Compute(context.Background(), cfg, nil, []string{"Prometheus + Grafana"})
	if result.Level != Error || result.Message != "Error: Prometheus + Grafana" {
		t.Errorf("Compute() = %+v, want Error: Prometheus + Grafana", result)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/status/... -v`
Expected: FAIL — package `status` doesn't exist yet.

- [ ] **Step 3: Implement `internal/status/status.go`**

```go
package status

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

type ServiceStatus struct {
	Name string `json:"name"`
	Up   bool   `json:"up"`
}

type InfraCheck struct {
	Name     string
	CheckURL string
}

type MetricInput struct {
	Label   string
	Percent float64
	HasData bool
}

type Config struct {
	ServicesStatusURL       string
	InfraChecks             []InfraCheck
	WarningThresholdPercent float64
}

type Level int

const (
	OK Level = iota
	Warning
	Error
)

type Result struct {
	Level   Level
	Message string
}

const checkTimeout = 3 * time.Second

var httpClient = &http.Client{Timeout: checkTimeout}

// Compute fetches glance-services' aggregate client-service status and this
// widget's own configured infra checks concurrently, then combines that
// with already-fetched metric values and any caller-supplied extraDown
// entries into one prioritized Result: any down service (or extraDown
// entry) wins (Error, listing every down name); else any metric over the
// threshold (Warning, listing "Label Percent%"); else OK. If the
// glance-services status endpoint itself is unreachable, that is reported
// as its own Error entry ("service status unavailable") rather than
// guessing the 10 client services' individual states.
func Compute(ctx context.Context, cfg Config, metrics []MetricInput, extraDown []string) Result {
	var wg sync.WaitGroup
	var clientServices []ServiceStatus
	var clientErr error

	wg.Add(1)
	go func() {
		defer wg.Done()
		clientServices, clientErr = fetchClientServices(ctx, cfg.ServicesStatusURL)
	}()

	infraResults := make([]ServiceStatus, len(cfg.InfraChecks))
	for i, c := range cfg.InfraChecks {
		wg.Add(1)
		go func(i int, c InfraCheck) {
			defer wg.Done()
			infraResults[i] = ServiceStatus{Name: c.Name, Up: checkOne(ctx, c.CheckURL)}
		}(i, c)
	}
	wg.Wait()

	down := append([]string{}, extraDown...)
	if clientErr != nil {
		down = append(down, "service status unavailable")
	} else {
		for _, s := range clientServices {
			if !s.Up {
				down = append(down, s.Name)
			}
		}
	}
	for _, s := range infraResults {
		if !s.Up {
			down = append(down, s.Name)
		}
	}

	if len(down) > 0 {
		return Result{Level: Error, Message: "Error: " + strings.Join(down, ", ")}
	}

	var warn []string
	for _, m := range metrics {
		if m.HasData && m.Percent > cfg.WarningThresholdPercent {
			warn = append(warn, fmt.Sprintf("%s %.0f%%", m.Label, m.Percent))
		}
	}
	if len(warn) > 0 {
		return Result{Level: Warning, Message: "Warning: " + strings.Join(warn, ", ")}
	}

	return Result{Level: OK, Message: "All operational"}
}

func fetchClientServices(ctx context.Context, url string) ([]ServiceStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	var out []ServiceStatus
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

func checkOne(ctx context.Context, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/status/... -v`
Expected: PASS, all 8 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/status/status.go internal/status/status_test.go
git commit -m "Add service+infra status aggregation with Error/Warning priority"
```

---

### Task 7: Renderer

**Files:**
- Create: `internal/render/template.go`
- Test: `internal/render/template_test.go`

**Interfaces:**
- Produces: `type ValueView struct { Label, ValueText string; HasData, ShowArrow, ArrowUp, ArrowGood bool }` (used for all 6 tiles — CPU/RAM/Disk×2/Speed down/up, identical shape, the caller pre-formats `ValueText`), `type StatusView struct { Level, Message string }` (`Level` one of `"ok"`, `"warning"`, `"error"`), `func RenderWidget(title string, values []ValueView, status StatusView) string`.
- Consumes: nothing from other packages — plain structs defined in this package, so it stays independently testable. Task 8 builds `[]ValueView`/`StatusView` from `metrics.Metric`, `speedtest.Trend`, and `status.Result`.

- [ ] **Step 1: Write the failing tests**

Create `internal/render/template_test.go`:

```go
package render

import (
	"strings"
	"testing"
)

func TestRenderWidget_ShowsValuesAndArrows(t *testing.T) {
	values := []ValueView{
		{Label: "CPU", ValueText: "42%", HasData: true, ShowArrow: true, ArrowUp: true, ArrowGood: false},
		{Label: "RAM", ValueText: "67%", HasData: true},
		{Label: "DISK (EXT)", HasData: false},
	}
	status := StatusView{Level: "warning", Message: "Warning: DISK (EXT) 91%"}

	html := RenderWidget("Server Health", values, status)

	if !strings.Contains(html, "42%") {
		t.Error("missing CPU value")
	}
	if !strings.Contains(html, `class="stat-arrow stat-arrow-bad"`) {
		t.Error("missing bad/up arrow for CPU (an 'up' arrow gets no extra direction class — only 'down' rotates via stat-arrow-down)")
	}
	if !strings.Contains(html, "no data") {
		t.Error("missing no-data marker for DISK (EXT)")
	}
	if !strings.Contains(html, `data-level="warning"`) {
		t.Error("missing warning status level")
	}
	if !strings.Contains(html, "Warning: DISK (EXT) 91%") {
		t.Error("missing status message")
	}
}

func TestRenderWidget_NoArrowWhenFlagFalse(t *testing.T) {
	values := []ValueView{{Label: "RAM", ValueText: "67%", HasData: true, ShowArrow: false}}
	html := RenderWidget("Server Health", values, StatusView{Level: "ok", Message: "All operational"})
	if strings.Contains(html, "<svg") {
		t.Error("should not render an arrow svg when ShowArrow is false")
	}
}

func TestRenderWidget_EscapesHTML(t *testing.T) {
	status := StatusView{Level: "error", Message: `<script>alert(1)</script>`}
	html := RenderWidget("Server Health", nil, status)
	if strings.Contains(html, "<script>alert(1)</script>") {
		t.Error("status message must be HTML-escaped")
	}
}

func TestRenderWidget_StatusLevelsUseThemeColors(t *testing.T) {
	for _, level := range []string{"ok", "warning", "error"} {
		html := RenderWidget("Server Health", nil, StatusView{Level: level, Message: "x"})
		if !strings.Contains(html, `data-level="`+level+`"`) {
			t.Errorf("level %q: missing data-level attribute", level)
		}
	}
	if strings.Contains(RenderWidget("t", nil, StatusView{}), "#") {
		t.Error("must not contain any hardcoded hex color — theme variables only")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/render/... -v`
Expected: FAIL — package `render` doesn't exist yet.

- [ ] **Step 3: Implement `internal/render/template.go`**

```go
package render

import (
	"fmt"
	"html"
	"strings"
)

type ValueView struct {
	Label     string
	ValueText string
	HasData   bool
	ShowArrow bool
	ArrowUp   bool
	ArrowGood bool
}

type StatusView struct {
	Level   string // "ok", "warning", "error"
	Message string
}

func styleBlock() string {
	return `<style>
	.stat-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(90px,1fr));gap:10px;margin-bottom:12px}
	.stat-tile{display:flex;flex-direction:column;gap:2px}
	.stat-label{font-size:10.5px;color:var(--color-text-subdue);text-transform:uppercase;letter-spacing:.03em}
	.stat-value-row{display:flex;align-items:center;gap:4px}
	.stat-value{font-size:16px;font-weight:600;color:var(--color-text-highlight)}
	.stat-value-nodata{font-size:12px;color:var(--color-text-subdue)}
	.stat-arrow{width:9px;height:9px}
	.stat-arrow-down{transform:rotate(180deg)}
	.stat-arrow-good{color:var(--color-primary)}
	.stat-arrow-bad{color:var(--color-negative)}
	.status-line{display:flex;align-items:center;gap:8px;padding-top:10px;border-top:1px solid var(--color-widget-content-border)}
	.status-dot{width:8px;height:8px;border-radius:50%;flex-shrink:0}
	.status-dot[data-level="ok"]{background:var(--color-primary)}
	.status-dot[data-level="error"]{background:var(--color-negative)}
	.status-dot[data-level="warning"]{background:transparent;border:1.5px solid var(--color-negative)}
	.status-text{font-size:12px;color:var(--color-text-base)}
</style>`
}

const arrowSVG = `<svg class="stat-arrow %s" viewBox="0 0 10 10" fill="currentColor"><path d="M5 0 L10 8 L0 8 Z"/></svg>`

// RenderWidget renders the widget body HTML. It does not render a heading
// for title — Glance renders the widget's header chrome itself from the
// Widget-Title response header (see main.go), the same convention followed
// by the sibling repos. The title parameter is kept for signature
// stability even though it is currently unused.
func RenderWidget(title string, values []ValueView, status StatusView) string {
	var b strings.Builder
	b.WriteString(styleBlock())
	b.WriteString(`<div class="stat-body"><div class="stat-grid">`)

	for _, v := range values {
		b.WriteString(`<div class="stat-tile">`)
		fmt.Fprintf(&b, `<span class="stat-label">%s</span>`, html.EscapeString(v.Label))
		b.WriteString(`<span class="stat-value-row">`)
		if v.HasData {
			fmt.Fprintf(&b, `<span class="stat-value">%s</span>`, html.EscapeString(v.ValueText))
			if v.ShowArrow {
				dir := ""
				if !v.ArrowUp {
					dir = "stat-arrow-down"
				}
				color := "stat-arrow-bad"
				if v.ArrowGood {
					color = "stat-arrow-good"
				}
				fmt.Fprintf(&b, arrowSVG, strings.TrimSpace(dir+" "+color))
			}
		} else {
			b.WriteString(`<span class="stat-value-nodata">no data</span>`)
		}
		b.WriteString(`</span></div>`)
	}
	b.WriteString(`</div>`)

	fmt.Fprintf(&b, `<div class="status-line"><span class="status-dot" data-level="%s"></span><span class="status-text">%s</span></div>`,
		html.EscapeString(status.Level), html.EscapeString(status.Message))

	b.WriteString(`</div>`)
	return b.String()
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/render/... -v`
Expected: PASS, all 4 tests.

- [ ] **Step 5: Commit**

```bash
git add internal/render/template.go internal/render/template_test.go
git commit -m "Add health-summary renderer (metric tiles + status line)"
```

---

### Task 8: main.go — wire it together

**Files:**
- Create: `main.go`
- Test: `main_test.go`

**Interfaces:**
- Consumes: `LoadConfig`/`Config` (Task 2), `grafana.New`/`grafana.Client` (Task 3), `metrics.Fetch`/`metrics.Config`/`metrics.Metric` (Task 4), `speedtest.New`/`speedtest.Client`/`speedtest.Trend` (Task 5), `status.Compute`/`status.Config`/`status.MetricInput`/`status.InfraCheck`/`status.Level` (Task 6), `render.RenderWidget`/`render.ValueView`/`render.StatusView` (Task 7).
- Produces: the `/widget` and `/healthz` HTTP handlers Task 9's Dockerfile runs.

- [ ] **Step 1: Write the failing test**

Create `main_test.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWidgetHandler_EndToEnd(t *testing.T) {
	grafanaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				RefID string `json:"refId"`
			} `json:"queries"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		results := map[string]any{}
		for _, q := range req.Queries {
			results[q.RefID] = map[string]any{
				"frames": []any{map[string]any{"data": map[string]any{"values": [][]float64{{0}, {42}}}}},
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer grafanaSrv.Close()

	speedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": [{"download": 150, "upload": 20, "created_at": "2026-09-05T12:00:00Z"}]}`)
	}))
	defer speedSrv.Close()

	svcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[{"name":"Jellyfin","up":true}]`)
	}))
	defer svcSrv.Close()

	cfg := &Config{
		Title:                   "Server Health",
		Grafana:                 GrafanaConfig{URL: grafanaSrv.URL, Token: "t", DatasourceUID: "u"},
		Metrics:                 MetricsConfig{CPUQuery: "cpu", RAMQuery: "ram", DiskInternalQuery: "disk_i", DiskExternalQuery: "disk_e"},
		Speedtest:               SpeedtestConfig{URL: speedSrv.URL, Token: "t"},
		ServicesStatusURL:       svcSrv.URL,
		WarningThresholdPercent: 90,
	}
	a := newApp(cfg)

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	w := httptest.NewRecorder()
	a.widgetHandler(w, req)

	resp := w.Result()
	if resp.Header.Get("Widget-Title") != "Server Health" {
		t.Errorf("Widget-Title = %q, want %q", resp.Header.Get("Widget-Title"), "Server Health")
	}
	if resp.Header.Get("Widget-Content-Type") != "html" {
		t.Errorf("Widget-Content-Type = %q, want html", resp.Header.Get("Widget-Content-Type"))
	}
	body := w.Body.String()
	if !strings.Contains(body, "42%") {
		t.Error("missing metric value")
	}
	if !strings.Contains(body, "150 Mbps") {
		t.Error("missing speed value")
	}
	if !strings.Contains(body, "All operational") {
		t.Error("missing status line")
	}
}

func TestWidgetHandler_GrafanaDownStillRendersFourNoDataTiles(t *testing.T) {
	grafanaSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer grafanaSrv.Close()
	speedSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": []}`)
	}))
	defer speedSrv.Close()
	svcSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `[]`)
	}))
	defer svcSrv.Close()

	cfg := &Config{
		Title:                   "Server Health",
		Grafana:                 GrafanaConfig{URL: grafanaSrv.URL, Token: "t", DatasourceUID: "u"},
		Metrics:                 MetricsConfig{CPUQuery: "cpu", RAMQuery: "ram", DiskInternalQuery: "disk_i", DiskExternalQuery: "disk_e"},
		Speedtest:               SpeedtestConfig{URL: speedSrv.URL, Token: "t"},
		ServicesStatusURL:       svcSrv.URL,
		WarningThresholdPercent: 90,
	}
	a := newApp(cfg)

	req := httptest.NewRequest(http.MethodGet, "/widget", nil)
	w := httptest.NewRecorder()
	a.widgetHandler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 even when Grafana is down", w.Code)
	}
	body := w.Body.String()
	if strings.Count(body, "no data") < 4 {
		t.Errorf("want at least 4 no-data tiles when Grafana is unreachable, body:\n%s", body)
	}
	if !strings.Contains(body, "Prometheus + Grafana") {
		t.Error("want status line to list Prometheus + Grafana as down when the metrics fetch fails entirely")
	}
}

func TestHealthzHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	newMux(&app{cfg: &Config{}}).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "ok" {
		t.Errorf("body = %q, want %q", w.Body.String(), "ok")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./... -run 'TestWidgetHandler|TestHealthz' -v`
Expected: FAIL — `app`, `newApp`, `widgetHandler`, `newMux` don't exist yet.

- [ ] **Step 3: Implement `main.go`**

```go
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/sidun-av/glance-status/internal/grafana"
	"github.com/sidun-av/glance-status/internal/metrics"
	"github.com/sidun-av/glance-status/internal/render"
	"github.com/sidun-av/glance-status/internal/speedtest"
	"github.com/sidun-av/glance-status/internal/status"
)

type app struct {
	cfg           *Config
	grafanaClient *grafana.Client
	speedClient   *speedtest.Client
}

func newApp(cfg *Config) *app {
	return &app{
		cfg:           cfg,
		grafanaClient: grafana.New(cfg.Grafana.URL, cfg.Grafana.Token, cfg.Grafana.DatasourceUID),
		speedClient:   speedtest.New(cfg.Speedtest.URL, cfg.Speedtest.Token),
	}
}

func (a *app) widgetHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	var metricList []metrics.Metric
	var metricsErr error
	var speedTrend speedtest.Trend

	wg.Add(2)
	go func() {
		defer wg.Done()
		metricList, metricsErr = metrics.Fetch(ctx, a.grafanaClient, metrics.Config{
			CPUQuery:          a.cfg.Metrics.CPUQuery,
			RAMQuery:          a.cfg.Metrics.RAMQuery,
			DiskInternalQuery: a.cfg.Metrics.DiskInternalQuery,
			DiskExternalQuery: a.cfg.Metrics.DiskExternalQuery,
		})
	}()
	go func() {
		defer wg.Done()
		if trend, err := a.speedClient.Fetch(ctx); err != nil {
			log.Printf("speedtest-tracker unavailable: %v", err)
		} else {
			speedTrend = trend
		}
	}()
	wg.Wait()

	var extraDown []string
	if metricsErr != nil {
		log.Printf("grafana unavailable: %v", metricsErr)
		metricList = []metrics.Metric{{Label: "CPU"}, {Label: "RAM"}, {Label: "DISK"}, {Label: "DISK (EXT)"}}
		extraDown = []string{"Prometheus + Grafana"}
	}

	statusInputs := make([]status.MetricInput, len(metricList))
	for i, m := range metricList {
		statusInputs[i] = status.MetricInput{Label: m.Label, Percent: m.Percent, HasData: m.HasData}
	}
	infraChecks := make([]status.InfraCheck, len(a.cfg.InfraChecks))
	for i, c := range a.cfg.InfraChecks {
		infraChecks[i] = status.InfraCheck{Name: c.Name, CheckURL: c.CheckURL}
	}
	statusResult := status.Compute(ctx, status.Config{
		ServicesStatusURL:       a.cfg.ServicesStatusURL,
		InfraChecks:             infraChecks,
		WarningThresholdPercent: a.cfg.WarningThresholdPercent,
	}, statusInputs, extraDown)

	values := make([]render.ValueView, 0, len(metricList)+2)
	for _, m := range metricList {
		values = append(values, render.ValueView{
			Label:     m.Label,
			ValueText: fmt.Sprintf("%.0f%%", m.Percent),
			HasData:   m.HasData,
			ShowArrow: m.ShowArrow,
			ArrowUp:   m.ArrowUp,
			ArrowGood: m.ArrowGood,
		})
	}
	values = append(values,
		render.ValueView{
			Label:     "↓ Speed",
			ValueText: fmt.Sprintf("%.0f Mbps", speedTrend.Download.Mbps),
			HasData:   speedTrend.Download.HasData,
			ShowArrow: speedTrend.Download.ShowArrow,
			ArrowUp:   speedTrend.Download.ArrowUp,
			ArrowGood: speedTrend.Download.ArrowGood,
		},
		render.ValueView{
			Label:     "↑ Speed",
			ValueText: fmt.Sprintf("%.0f Mbps", speedTrend.Upload.Mbps),
			HasData:   speedTrend.Upload.HasData,
			ShowArrow: speedTrend.Upload.ShowArrow,
			ArrowUp:   speedTrend.Upload.ArrowUp,
			ArrowGood: speedTrend.Upload.ArrowGood,
		},
	)

	statusView := render.StatusView{Level: levelString(statusResult.Level), Message: statusResult.Message}

	w.Header().Set("Widget-Title", a.cfg.Title)
	w.Header().Set("Widget-Content-Type", "html")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, render.RenderWidget(a.cfg.Title, values, statusView))
}

func levelString(l status.Level) string {
	switch l {
	case status.Error:
		return "error"
	case status.Warning:
		return "warning"
	default:
		return "ok"
	}
}

func newMux(a *app) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/widget", a.widgetHandler)
	return mux
}

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {
		configPath = "/config.yml"
	}
	cfg, err := LoadConfig(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	a := newApp(cfg)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, newMux(a)))
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./... -v`
Expected: PASS, every test in the module (all tasks combined).

- [ ] **Step 5: Commit**

```bash
gofmt -l .   # must print nothing
go vet ./...
git add main.go main_test.go
git commit -m "Wire config, metrics, speedtest, and status into HTTP handlers"
```

---

### Task 9: Packaging — Dockerfile, compose example, CI, README

**Files:**
- Create: `Dockerfile`
- Create: `docker-compose.example.yml`
- Create: `.github/workflows/ci.yml`
- Create: `README.md`

**Interfaces:**
- Consumes: everything from Tasks 1-8 (this task packages the finished binary, doesn't change any Go code).

- [ ] **Step 1: Write `Dockerfile`**

```dockerfile
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/glance-status .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/glance-status /glance-status
EXPOSE 8080
ENTRYPOINT ["/glance-status"]
```

(No `COPY config.docker-default.yml /config.yml` — unlike `glance-services`, this repo ships no baked-in defaults; a real `config.yml` must always be mounted, per the Global Constraints.)

- [ ] **Step 2: Write `docker-compose.example.yml`**

```yaml
services:
  glance-status:
    image: ghcr.io/sidun-av/glance-status:latest
    restart: unless-stopped
    environment:
      - TITLE=${TITLE:-}
      - GRAFANA_URL=${GRAFANA_URL}
      - GRAFANA_TOKEN=${GRAFANA_TOKEN}
      - SPEEDTEST_URL=${SPEEDTEST_URL}
      - SPEEDTEST_TOKEN=${SPEEDTEST_TOKEN}
      - SERVICES_STATUS_URL=${SERVICES_STATUS_URL}
      - INFRA_CHECK_NPMPLUS_URL=${INFRA_CHECK_NPMPLUS_URL}
      - INFRA_CHECK_HOMEASSISTANT_URL=${INFRA_CHECK_HOMEASSISTANT_URL}
    volumes:
      - ./glance-status/config.yml:/config.yml:ro
```

- [ ] **Step 3: Copy the CI workflow verbatim from a sibling repo**

```bash
mkdir -p .github/workflows
gh api repos/sidun-av/glance-homeassistant/contents/.github/workflows/ci.yml --jq '.content' | base64 -d > .github/workflows/ci.yml
cat .github/workflows/ci.yml
```

Expected output: a `name: CI` workflow with a `test` job (gofmt/vet/test) and a `docker` job (needs: test, builds+pushes to `ghcr.io/${{ github.repository }}`). It's already generic — no edits needed.

- [ ] **Step 4: Write `README.md`**

```markdown
# glance-status

A [Glance](https://github.com/glanceapp/glance) extension widget: one compact
health-summary card — CPU/RAM/Disk (internal + external) usage and internet
speed, each with an hour-over-hour trend arrow, plus an aggregate
`All operational` / `Warning` / `Error` status line covering your other
services.

## How it works

Serves an HTML fragment at `/widget` (Glance's custom-widget protocol —
`Widget-Title` and `Widget-Content-Type: html` response headers). On every
render it concurrently: queries Grafana's `/api/ds/query` API for CPU/RAM/
Disk (current value + the same expression with `offset 1h`, so trend needs
no stored state on this widget's side), queries `speedtest-tracker`'s REST
API for the 2 most recent results, and queries a `glance-services`-style
`/status.json` endpoint plus any extra `infra_checks` you configure. Trend
arrows are hidden when nothing meaningfully changed (same value after
rounding) or when there's no valid prior value to compare against.

## Setup

### 1. Grafana

Reuse the same Service Account token as `glance-grafana-sparkline` if you
already run it (role **Viewer** + **Data Source Reader** permission on your
Prometheus datasource), or create a new one the same way.

### 2. speedtest-tracker

In its Admin → API Tokens, create a token with the `results:read` scope.
In its Settings, make sure scheduled speedtests actually run about once an
hour — without that, the trend arrows for Speed will never have a valid
"an hour ago" value to compare against.

### 3. Configure

Copy [`config.example.yml`](config.example.yml) to `config.yml`, either
filling in real values directly or leaving the `${VAR}` placeholders and
setting the corresponding environment variables in your compose file (see
`docker-compose.example.yml`).

### 4. Run it alongside Glance

```bash
docker compose -f docker-compose.example.yml up -d
```

### 5. Add the widget to Glance

```yaml
- type: extension
  url: http://glance-status:8080/widget
  cache: 5m
```

## Configuration reference

| Field | Required | Description |
|---|---|---|
| `title` | no (default `Server Health`) | Widget title. Also overridable via `TITLE` env var. |
| `grafana.url` / `.token` / `.datasource_uid` | yes | Same as `glance-grafana-sparkline`'s Grafana config. |
| `metrics.cpu_query` / `.ram_query` / `.disk_internal_query` / `.disk_external_query` | yes | PromQL expressions — the shipped example values work for a standard `node_exporter` setup; adjust mountpoints to your own disks. |
| `speedtest.url` / `.token` | yes | `speedtest-tracker` base URL (reachable from this container) and a `results:read`-scoped API token. |
| `services_status_url` | yes | URL of a `glance-services`-style `/status.json` endpoint (or any endpoint returning `[{"name": "...", "up": true}, ...]`). |
| `infra_checks[].name` / `.check_url` | no | Extra services checked with a plain `GET` (2xx/3xx = up), shown in the same status line as the client services. |
| `warning_threshold_percent` | no (default `90`) | CPU/RAM/Disk usage above this triggers `Warning` in the status line. |

## Environment variable reference

| Variable | Default |
|---|---|
| `CONFIG_PATH` | `/config.yml` |
| `PORT` | `8080` |

`TITLE` and the `${VAR}`-style placeholders referenced from `config.yml`
(see above) are also read from the environment, but only the fields listed
in the config reference table support this — PromQL query text does not (a
literal `$` in a Grafana macro like `$__rate_interval` is preserved as-is).

## Development

```bash
go test ./...
go vet ./...
gofmt -l .
```
```

- [ ] **Step 5: Verify the Docker build works locally**

```bash
docker build -t glance-status:test .
cat > /tmp/glance-status-test-config.yml <<'EOF'
grafana:
  url: http://127.0.0.1:1
  token: t
  datasource_uid: u
metrics:
  cpu_query: 'up'
  ram_query: 'up'
  disk_internal_query: 'up'
  disk_external_query: 'up'
speedtest:
  url: http://127.0.0.1:1
  token: t
services_status_url: http://127.0.0.1:1/status.json
EOF
docker run --rm -p 8080:8080 -v /tmp/glance-status-test-config.yml:/config.yml:ro -d --name glance-status-test glance-status:test
sleep 1
curl -s -D - http://localhost:8080/healthz
curl -s -D - http://localhost:8080/widget | head -30
docker stop glance-status-test
```

Expected: `/healthz` returns `200 ok`; `/widget` returns `200` with
`Widget-Title: Server Health` and `Widget-Content-Type: html` headers, and
HTML containing 4 "no data" metric tiles + 2 "no data" speed tiles (every
upstream in this local smoke test points at an unreachable address on
purpose) plus a status line reading
`Error: Prometheus + Grafana, service status unavailable` (extraDown from
the failed metrics fetch, then the failed glance-services status call —
see Task 6/8's `extraDown` parameter). This confirms the widget degrades
gracefully end-to-end, not that live data flows (that needs the real
Grafana/speedtest-tracker/glance-services — out of scope for this local
check).

- [ ] **Step 6: Commit**

```bash
git add Dockerfile docker-compose.example.yml .github/workflows/ci.yml README.md
git commit -m "Add Dockerfile, compose example, CI workflow, and README"
```

---

### Task 10: Push and confirm CI

**Files:** none (infrastructure action — pushes to the already-existing `origin` remote, `github.com/sidun-av/glance-status`, created during design with just the spec commit)

- [ ] **Step 1: Push**

```bash
cd /Users/sidun/GIT/glance-status
git push
```

- [ ] **Step 2: Wait for CI and verify it's green**

```bash
sleep 15
gh run list --repo sidun-av/glance-status --branch main --limit 1
gh run watch --repo sidun-av/glance-status $(gh run list --repo sidun-av/glance-status --branch main --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status
```

Expected: the `test` and `docker` jobs both succeed, and `ghcr.io/sidun-av/glance-status:latest` is published. If CI fails, fix the issue, push a new commit, and re-check before proceeding.

---

### Task 11: Companion change — `glance-services` `/status.json`

**This task operates in a DIFFERENT git repository:** `/Users/sidun/GIT/glance-services` (already exists, already private, already has its own `origin` remote and CI). It is a small, self-contained addition: a new read-only endpoint that exposes the exact same health-check result the existing `/widget` cards already show, as JSON, so `glance-status` doesn't need to duplicate the 10-service check list.

**Files (in `glance-services`):**
- Modify: `internal/services/check.go`
- Modify: `main.go`
- Modify: `main_test.go`

**Interfaces:**
- Consumes: the already-existing `services.CheckAll(ctx, []services.ServiceCheck) []services.Status` and `a.cfg.Services` (unchanged).
- Produces: `GET /status.json` returning `[{"name": "...", "up": true}, ...]` — this is the exact shape `glance-status`'s `internal/status.ServiceStatus` (Task 6, other repo) already expects.

- [ ] **Step 1: Add JSON tags to `Status` (currently untagged)**

In `/Users/sidun/GIT/glance-services/internal/services/check.go`, change:

```go
type Status struct {
	Name string
	Up   bool
}
```

to:

```go
type Status struct {
	Name string `json:"name"`
	Up   bool   `json:"up"`
}
```

This only affects JSON marshaling — existing field access (`.Name`, `.Up`) and existing tests in `internal/services/check_test.go` are unaffected.

- [ ] **Step 2: Write the failing test**

In `/Users/sidun/GIT/glance-services/main_test.go`, add:

```go
func TestStatusHandler_ReturnsJSON(t *testing.T) {
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer up.Close()

	cfg := &Config{
		Services: []ServiceConfig{
			{Name: "TestSvc", CheckURL: up.URL, LinkURL: "https://example.com", FallbackColor: "#111"},
		},
	}
	a := &app{cfg: cfg}

	req := httptest.NewRequest(http.MethodGet, "/status.json", nil)
	w := httptest.NewRecorder()
	a.statusHandler(w, req)

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}

	var got []struct {
		Name string `json:"name"`
		Up   bool   `json:"up"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got) != 1 || got[0].Name != "TestSvc" || !got[0].Up {
		t.Errorf("got %+v, want [{TestSvc true}]", got)
	}
}
```

Add `"encoding/json"` to `main_test.go`'s import block if not already present.

- [ ] **Step 3: Run test to verify it fails**

```bash
cd /Users/sidun/GIT/glance-services
go test ./... -run TestStatusHandler -v
```

Expected: FAIL — `a.statusHandler` doesn't exist yet.

- [ ] **Step 4: Implement the handler in `main.go`**

Add `"encoding/json"` to `main.go`'s import block, then add this method (near `widgetHandler`):

```go
func (a *app) statusHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	checks := make([]services.ServiceCheck, len(a.cfg.Services))
	for i, s := range a.cfg.Services {
		checks[i] = services.ServiceCheck{Name: s.Name, CheckURL: s.CheckURL}
	}
	statuses := services.CheckAll(ctx, checks)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(statuses)
}
```

Register it in `newMux`:

```go
func newMux(a *app) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "ok")
	})
	mux.HandleFunc("/widget", a.widgetHandler)
	mux.HandleFunc("/status.json", a.statusHandler)
	return mux
}
```

- [ ] **Step 5: Run test to verify it passes**

```bash
go test ./... -v
```

Expected: PASS, every test in the module including the new `TestStatusHandler_ReturnsJSON`.

- [ ] **Step 6: Commit and push**

```bash
gofmt -l .   # must print nothing
go vet ./...
git add internal/services/check.go main.go main_test.go
git commit -m "Add /status.json endpoint for glance-status to consume"
git push
sleep 15
gh run list --repo sidun-av/glance-services --branch main --limit 1
gh run watch --repo sidun-av/glance-services $(gh run list --repo sidun-av/glance-services --branch main --limit 1 --json databaseId --jq '.[0].databaseId') --exit-status
```

Expected: CI green, `ghcr.io/sidun-av/glance-services:latest` republished with the new endpoint baked in.

---

## After this plan: deployment (explicitly not part of this document)

Once Tasks 1-11 are done and both images are published, deployment is a
separate, interactive step — not written here, on purpose (see Global
Constraints): add `glance-status` as a new container in the `glance` Komodo
stack (with its real `config.yml` mounted, real env vars set), add its
`/widget` URL as an `extension` widget in `glance.yml`, and confirm
`speedtest-tracker`'s own scheduled-test interval is actually set to hourly.
This touches live infrastructure and real credentials, and should be
confirmed with the user before running, the same way the `glance-services`
StirlingPDF health-check fix was deployed earlier in this project — live,
outside any committed plan file.
