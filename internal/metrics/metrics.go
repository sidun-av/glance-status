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
