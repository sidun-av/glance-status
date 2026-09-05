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

func TestWidgetHandler_SpeedtestDownDoesNotAffectStatus(t *testing.T) {
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
		w.WriteHeader(http.StatusInternalServerError)
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

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "All operational") {
		t.Errorf("want status line to read 'All operational' when speedtest-tracker is down but Grafana and services are healthy (speedtest failure must NOT appear in extraDown), body:\n%s", body)
	}
	if strings.Contains(body, "Speedtest") || strings.Contains(body, "speedtest") {
		t.Error("speedtest failure must NOT appear in status message when Grafana and services are healthy")
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
