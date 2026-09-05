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
