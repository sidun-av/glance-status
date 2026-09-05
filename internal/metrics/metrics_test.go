package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/sidun-av/glance-status/internal/grafana"
)

func TestFetch_ArrowDirectionAndSemantics(t *testing.T) {
	// cpu: now=50, 1h ago=40 -> increased -> usage metric -> bad (red) arrow up
	// ram: now=30, 1h ago=30 -> unchanged -> no arrow
	// disk_internal: now=20, 1h ago=25 -> decreased -> usage metric -> good (green) arrow down
	// disk_external: absent from both -> no data
	nowValues := map[string]float64{"cpu": 50, "ram": 30, "disk_internal": 20}
	pastValues := map[string]float64{"cpu": 40, "ram": 30, "disk_internal": 25}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				RefID string `json:"refId"`
			} `json:"queries"`
			From string `json:"from"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		values := nowValues
		if req.From == "now-1h5m" {
			values = pastValues
		}

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
		t.Errorf("Disk(external) metric = %+v, want HasData=false", diskExt)
	}
}

func TestFetch_RoundingHidesArrowForSubUnitChange(t *testing.T) {
	// 50.4 rounds to 50, 49.6 rounds to 50 -> same rounded value -> no arrow
	nowValues := map[string]float64{"cpu": 50.4, "ram": 30, "disk_internal": 20, "disk_external": 10}
	pastValues := map[string]float64{"cpu": 49.6, "ram": 30, "disk_internal": 20, "disk_external": 10}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				RefID string `json:"refId"`
			} `json:"queries"`
			From string `json:"from"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		values := nowValues
		if req.From == "now-1h5m" {
			values = pastValues
		}

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

func TestFetch_SendsUnmodifiedQueryTwiceWithDifferentTimeRanges(t *testing.T) {
	var gotFromValues []string
	var gotExprs []string
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Queries []struct {
				Expr string `json:"expr"`
			} `json:"queries"`
			From string `json:"from"`
			To   string `json:"to"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		mu.Lock()
		gotFromValues = append(gotFromValues, req.From)
		for _, q := range req.Queries {
			gotExprs = append(gotExprs, q.Expr)
		}
		mu.Unlock()

		json.NewEncoder(w).Encode(map[string]any{"results": map[string]any{}})
	}))
	defer server.Close()

	client := grafana.New(server.URL, "token", "uid")
	cfg := Config{CPUQuery: "cpu_expr", RAMQuery: "ram_expr", DiskInternalQuery: "disk_i_expr", DiskExternalQuery: "disk_e_expr"}
	Fetch(context.Background(), client, cfg)

	for _, expr := range gotExprs {
		if strings.Contains(expr, "offset") {
			t.Errorf("query expr %q must never contain a textual offset — got one anyway", expr)
		}
	}
	wantFrom := map[string]bool{"now-5m": false, "now-1h5m": false}
	for _, f := range gotFromValues {
		if _, ok := wantFrom[f]; ok {
			wantFrom[f] = true
		}
	}
	for f, seen := range wantFrom {
		if !seen {
			t.Errorf("expected a call with from=%q, never saw one (got: %v)", f, gotFromValues)
		}
	}
}
