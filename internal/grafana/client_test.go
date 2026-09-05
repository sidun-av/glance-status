package grafana

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQueryPanelsSendsExpectedRequest(t *testing.T) {
	var gotBody dsQueryRequest

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/ds/query" {
			t.Errorf("expected path /api/ds/query, got %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("expected Bearer test-token, got %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		fmt.Fprint(w, `{"results": {"cpu_now": {"frames": [{"data": {"values": [[1,2],[11,22]]}}]}}}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token", "prom-uid")
	results, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "up"}}, "now-5m", "now", 60000, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(gotBody.Queries) != 1 {
		t.Fatalf("expected 1 query, got %d", len(gotBody.Queries))
	}
	q := gotBody.Queries[0]
	if q.RefID != "cpu_now" || q.Expr != "up" || q.Datasource.UID != "prom-uid" || q.Datasource.Type != "prometheus" {
		t.Fatalf("unexpected query sent: %+v", q)
	}
	if gotBody.From != "now-5m" || gotBody.To != "now" {
		t.Fatalf("unexpected time range: from=%q to=%q", gotBody.From, gotBody.To)
	}

	cpu, ok := results["cpu_now"]
	if !ok || cpu.Err != nil {
		t.Fatalf("expected successful cpu_now result, got %+v", cpu)
	}
	if len(cpu.Values) != 2 || cpu.Values[1] != 22 {
		t.Fatalf("expected values [11 22], got %v", cpu.Values)
	}
}

func TestQueryPanelsMarksMissingRefIDAsNoData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results": {}}`)
	}))
	defer server.Close()

	client := New(server.URL, "token", "uid")
	results, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "up"}}, "now-5m", "now", 60000, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cpu := results["cpu_now"]; cpu.Err == nil {
		t.Fatal("expected an error for a panel missing from the response")
	}
}

func TestQueryPanelsReturnsErrorOnNonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(server.URL, "token", "uid")
	client.HTTPClient.Timeout = time.Second

	_, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "up"}}, "now-5m", "now", 60000, 5)
	if err == nil {
		t.Fatal("expected an error for a 500 response, got nil")
	}
}

func TestQueryPanelsPreservesDatasourceError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"results": {"cpu_now": {"error": "datasource error: query failed"}}}`)
	}))
	defer server.Close()

	client := New(server.URL, "token", "uid")
	results, err := client.QueryPanels(context.Background(), []PanelQuery{{ID: "cpu_now", Expr: "invalid"}}, "now-5m", "now", 60000, 5)
	if err != nil {
		t.Fatalf("unexpected request error: %v", err)
	}

	cpu := results["cpu_now"]
	if cpu.Err == nil {
		t.Fatal("expected an error for panel with datasource error")
	}
	if cpu.Err.Error() != `panel "cpu_now": datasource error: query failed` {
		t.Errorf("expected error message to contain datasource detail, got %q", cpu.Err.Error())
	}
}
