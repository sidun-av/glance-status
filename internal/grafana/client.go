package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	HTTPClient    *http.Client
	BaseURL       string
	Token         string
	DatasourceUID string
}

func New(baseURL, token, datasourceUID string) *Client {
	return &Client{
		HTTPClient:    &http.Client{Timeout: 5 * time.Second},
		BaseURL:       strings.TrimRight(baseURL, "/"),
		Token:         token,
		DatasourceUID: datasourceUID,
	}
}

type PanelQuery struct {
	ID   string
	Expr string
}

type SeriesResult struct {
	Values []float64
	Err    error
}

type dsQueryRequest struct {
	Queries []dsQuery `json:"queries"`
	From    string    `json:"from"`
	To      string    `json:"to"`
}

type dsQuery struct {
	RefID         string            `json:"refId"`
	Datasource    dsQueryDatasource `json:"datasource"`
	Expr          string            `json:"expr"`
	Range         bool              `json:"range"`
	Instant       bool              `json:"instant"`
	IntervalMs    int               `json:"intervalMs"`
	MaxDataPoints int               `json:"maxDataPoints"`
}

type dsQueryDatasource struct {
	Type string `json:"type"`
	UID  string `json:"uid"`
}

type dsQueryResponse struct {
	Results map[string]dsResult `json:"results"`
}

type dsResult struct {
	Frames []dsFrame `json:"frames"`
	Error  string    `json:"error,omitempty"`
}

type dsFrame struct {
	Data dsFrameData `json:"data"`
}

type dsFrameData struct {
	Values [][]float64 `json:"values"`
}

// QueryPanels sends one batched /api/ds/query request (one query per panel,
// refId == panel ID) and returns each panel's parsed time series, keyed by
// panel ID. A panel missing from the response (or with a datasource-level
// error) gets a SeriesResult with Err set; other panels are unaffected. The
// returned top-level error is only for request-level failures (network,
// non-200 status, malformed response) that affect every panel at once.
func (c *Client) QueryPanels(ctx context.Context, panels []PanelQuery, from, to string, intervalMs, maxDataPoints int) (map[string]SeriesResult, error) {
	out := make(map[string]SeriesResult, len(panels))
	if len(panels) == 0 {
		return out, nil
	}

	queries := make([]dsQuery, len(panels))
	for i, p := range panels {
		queries[i] = dsQuery{
			RefID:         p.ID,
			Datasource:    dsQueryDatasource{Type: "prometheus", UID: c.DatasourceUID},
			Expr:          p.Expr,
			Range:         true,
			Instant:       false,
			IntervalMs:    intervalMs,
			MaxDataPoints: maxDataPoints,
		}
	}

	payload, err := json.Marshal(dsQueryRequest{Queries: queries, From: from, To: to})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/ds/query", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request grafana: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("grafana returned status %d", resp.StatusCode)
	}

	var parsed dsQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	for _, p := range panels {
		result, ok := parsed.Results[p.ID]
		if !ok {
			out[p.ID] = SeriesResult{Err: fmt.Errorf("no data for panel %q", p.ID)}
			continue
		}
		if result.Error != "" {
			out[p.ID] = SeriesResult{Err: fmt.Errorf("panel %q: %s", p.ID, result.Error)}
			continue
		}
		if len(result.Frames) == 0 || len(result.Frames[0].Data.Values) < 2 {
			out[p.ID] = SeriesResult{Err: fmt.Errorf("no data for panel %q", p.ID)}
			continue
		}
		out[p.ID] = SeriesResult{Values: result.Frames[0].Data.Values[1]}
	}
	return out, nil
}
