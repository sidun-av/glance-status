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
		ServicesStatusURL:       svc.URL,
		InfraChecks:             []InfraCheck{{Name: "npmplus", CheckURL: infra.URL}},
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
