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
