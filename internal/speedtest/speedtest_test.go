package speedtest

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetch_TrendComputation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("Authorization = %q, want Bearer test-token", got)
		}
		if r.URL.Query().Get("sort") != "-created_at" || r.URL.Query().Get("per_page") != "2" {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}
		fmt.Fprint(w, `{"data": [
			{"download": 150, "upload": 20, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 100, "upload": 22, "created_at": "2026-09-05T11:00:00Z"}
		]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !trend.Download.HasData || trend.Download.Mbps != 150 || !trend.Download.ShowArrow || !trend.Download.ArrowUp || !trend.Download.ArrowGood {
		t.Errorf("Download = %+v, want increased/good arrow at 150", trend.Download)
	}
	if !trend.Upload.HasData || trend.Upload.Mbps != 20 || !trend.Upload.ShowArrow || trend.Upload.ArrowUp || trend.Upload.ArrowGood {
		t.Errorf("Upload = %+v, want decreased/bad arrow at 20", trend.Upload)
	}
}

func TestFetch_StaleComparisonHidesArrow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": [
			{"download": 150, "upload": 20, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 100, "upload": 22, "created_at": "2026-09-05T08:00:00Z"}
		]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if trend.Download.ShowArrow || trend.Upload.ShowArrow {
		t.Errorf("expected no arrows for a >2h-old comparison, got %+v", trend)
	}
	if trend.Download.Mbps != 150 || !trend.Download.HasData {
		t.Errorf("expected latest value to still show: %+v", trend.Download)
	}
}

func TestFetch_RoundingHidesArrowForSubUnitChange(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": [
			{"download": 150.4, "upload": 20, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 149.6, "upload": 20, "created_at": "2026-09-05T11:00:00Z"}
		]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if trend.Download.ShowArrow {
		t.Errorf("Download = %+v, want no arrow (rounds to same value)", trend.Download)
	}
}

func TestFetch_NoData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": []}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if trend.Download.HasData || trend.Upload.HasData {
		t.Errorf("expected HasData=false with no results, got %+v", trend)
	}
}

func TestFetch_OnlyOneResultShowsValueNoArrow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data": [{"download": 150, "upload": 20, "created_at": "2026-09-05T12:00:00Z"}]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !trend.Download.HasData || trend.Download.Mbps != 150 || trend.Download.ShowArrow {
		t.Errorf("Download = %+v, want value shown, no arrow", trend.Download)
	}
}

func TestFetch_NonOKStatusReturnsError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	client := New(server.URL, "bad-token")
	if _, err := client.Fetch(context.Background()); err == nil {
		t.Fatal("want error for a 401 response")
	}
}
