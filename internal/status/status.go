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

// checkClient is used only for infra up/down pings (checkOne), never for
// fetchClientServices. It never follows redirects: a redirect response
// (3xx) is itself evidence the service is up and listening, and following
// it can lead somewhere this check has no business validating — e.g. many
// self-hosted admin panels (npmplus among them) redirect http->https on
// the same port using a self-signed certificate, which fails Go's default
// TLS verification and would otherwise wrongly report a perfectly healthy
// service as down.
var checkClient = &http.Client{
	Timeout: checkTimeout,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Fetched holds the raw results of the network-bound half of status
// computation. Kept separate from the pure aggregation step so the fetch
// can run concurrently with other unrelated fetches (metrics, speedtest)
// in a shared WaitGroup, instead of waiting for them to finish first.
type Fetched struct {
	ClientServices []ServiceStatus
	ClientErr      error
	InfraResults   []ServiceStatus
}

// Fetch performs the network-bound half of status computation: the
// glance-services status.json call and all configured infra checks,
// concurrently with each other. Call this alongside other independent
// fetches (e.g. in the same sync.WaitGroup as metrics.Fetch), then call
// Aggregate once everything has returned.
func Fetch(ctx context.Context, cfg Config) Fetched {
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

	return Fetched{ClientServices: clientServices, ClientErr: clientErr, InfraResults: infraResults}
}

// Aggregate is the pure half of status computation — no I/O, effectively
// instant. Combines an already-fetched Fetched with already-fetched metric
// values and any caller-supplied extraDown entries into one prioritized
// Result, using the same priority rule Compute always has: any down
// service (or extraDown entry) wins (Error); else any metric over the
// threshold (Warning); else OK.
func Aggregate(fetched Fetched, metrics []MetricInput, warningThresholdPercent float64, extraDown []string) Result {
	down := append([]string{}, extraDown...)
	if fetched.ClientErr != nil {
		down = append(down, "service status unavailable")
	} else {
		for _, s := range fetched.ClientServices {
			if !s.Up {
				down = append(down, s.Name)
			}
		}
	}
	for _, s := range fetched.InfraResults {
		if !s.Up {
			down = append(down, s.Name)
		}
	}

	if len(down) > 0 {
		return Result{Level: Error, Message: "Error: " + strings.Join(down, ", ")}
	}

	var warn []string
	for _, m := range metrics {
		if m.HasData && m.Percent > warningThresholdPercent {
			warn = append(warn, fmt.Sprintf("%s %.0f%%", m.Label, m.Percent))
		}
	}
	if len(warn) > 0 {
		return Result{Level: Warning, Message: "Warning: " + strings.Join(warn, ", ")}
	}

	return Result{Level: OK, Message: "All operational"}
}

// Compute fetches glance-services' aggregate client-service status and this
// widget's own configured infra checks concurrently, then combines that
// with already-fetched metric values and any caller-supplied extraDown
// entries into one prioritized Result: any down service (or extraDown
// entry) wins (Error, listing every down name); else any metric over the
// threshold (Warning, listing "Label Percent%"); else OK. If the
// glance-services status endpoint itself is unreachable, that is reported
// as its own Error entry ("service status unavailable") rather than
// guessing the 10 client services' individual states.
//
// Compute is a convenience wrapper combining Fetch and Aggregate
// sequentially — for callers that don't need the fetch to run concurrently
// with anything else. main.go's widgetHandler calls Fetch and Aggregate
// directly instead (see Fetch's doc comment), so its fetch can join the
// same WaitGroup as the metrics/speedtest fetches.
func Compute(ctx context.Context, cfg Config, metrics []MetricInput, extraDown []string) Result {
	return Aggregate(Fetch(ctx, cfg), metrics, cfg.WarningThresholdPercent, extraDown)
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
	resp, err := checkClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 400
}
