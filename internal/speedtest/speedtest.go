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
