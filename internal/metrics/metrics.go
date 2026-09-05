package metrics

import (
	"context"
	"sync"

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
// configured metrics and returns one Metric per metric, always in the fixed
// order [CPU, RAM, Disk (internal), Disk (external)]. The "1 hour ago" value
// is obtained by issuing the SAME unmodified query expressions a second time
// over a time-shifted window (now-1h5m..now-1h) rather than by injecting a
// PromQL `offset` modifier into the expression text — `offset` must
// immediately follow a vector/matrix selector per Prometheus' own docs, so
// appending it to the end of an arbitrary expression (as this widget's
// queries do) is invalid PromQL. The two calls run concurrently since
// they're independent HTTP round-trips. A non-nil error means the "now"
// Grafana request failed (network error, non-200, malformed response) — the
// caller should treat that as "Grafana unavailable" for all four. The "past"
// request failing is not fatal: it just means no trend data, same as any
// other "no prior data" case. A single metric missing its own data (e.g. a
// mountpoint that no longer exists) does not fail the call — that Metric
// just has HasData=false.
func Fetch(ctx context.Context, client *grafana.Client, cfg Config) ([]Metric, error) {
	specs := []querySpec{
		{"cpu", "CPU", cfg.CPUQuery},
		{"ram", "RAM", cfg.RAMQuery},
		{"disk_internal", "DISK", cfg.DiskInternalQuery},
		{"disk_external", "DISK (EXT)", cfg.DiskExternalQuery},
	}

	panels := make([]grafana.PanelQuery, len(specs))
	for i, s := range specs {
		panels[i] = grafana.PanelQuery{ID: s.id, Expr: s.query}
	}

	var nowResults, pastResults map[string]grafana.SeriesResult
	var nowErr, pastErr error
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		nowResults, nowErr = client.QueryPanels(ctx, panels, "now-5m", "now", 60000, 5)
	}()
	go func() {
		defer wg.Done()
		pastResults, pastErr = client.QueryPanels(ctx, panels, "now-1h5m", "now-1h", 60000, 5)
	}()
	wg.Wait()

	// The "now" call failing is fatal (no current values at all — the
	// caller treats a non-nil error as "Grafana unavailable" for
	// everything). The "past" call failing is NOT fatal — we still have
	// valid current values, we just can't compute a trend, same as any
	// other "no prior data" case.
	if nowErr != nil {
		return nil, nowErr
	}

	metrics := make([]Metric, len(specs))
	for i, s := range specs {
		now, nowOK := lastValue(nowResults[s.id])
		if !nowOK {
			metrics[i] = Metric{Label: s.label}
			continue
		}
		m := Metric{Label: s.label, HasData: true, Percent: now}
		if pastErr == nil {
			if prev, prevOK := lastValue(pastResults[s.id]); prevOK && roundToInt(now) != roundToInt(prev) {
				m.ShowArrow = true
				m.ArrowUp = now > prev
				m.ArrowGood = !m.ArrowUp // usage metric: up = bad, down = good
			}
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
