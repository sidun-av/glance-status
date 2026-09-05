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
