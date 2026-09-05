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
		// download: 150 Mbps -> 150 * 1_000_000 / 8 = 18750000 bytes/s
		// upload:   20 Mbps  -> 20  * 1_000_000 / 8 = 2500000 bytes/s
		// download: 100 Mbps -> 12500000 bytes/s
		// upload:   22 Mbps  -> 2750000 bytes/s
		fmt.Fprint(w, `{"data": [
			{"download": 18750000, "upload": 2500000, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 12500000, "upload": 2750000, "created_at": "2026-09-05T11:00:00Z"}
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
		// download: 150 Mbps -> 18750000 bytes/s, 100 Mbps -> 12500000 bytes/s
		// upload:   20 Mbps  -> 2500000 bytes/s,  22 Mbps  -> 2750000 bytes/s
		fmt.Fprint(w, `{"data": [
			{"download": 18750000, "upload": 2500000, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 12500000, "upload": 2750000, "created_at": "2026-09-05T08:00:00Z"}
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
		// 150.4 Mbps -> 150.4 * 1_000_000 / 8 = 18800000 bytes/s
		// 149.6 Mbps -> 149.6 * 1_000_000 / 8 = 18700000 bytes/s
		// upload: 20 Mbps -> 2500000 bytes/s (both rows, unchanged)
		fmt.Fprint(w, `{"data": [
			{"download": 18800000, "upload": 2500000, "created_at": "2026-09-05T12:00:00Z"},
			{"download": 18700000, "upload": 2500000, "created_at": "2026-09-05T11:00:00Z"}
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
		// download: 150 Mbps -> 18750000 bytes/s, upload: 20 Mbps -> 2500000 bytes/s
		fmt.Fprint(w, `{"data": [{"download": 18750000, "upload": 2500000, "created_at": "2026-09-05T12:00:00Z"}]}`)
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

func TestFetch_MalformedTimestampStillReturnsValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// download: 150 Mbps -> 18750000 bytes/s, 100 Mbps -> 12500000 bytes/s
		// upload:   20 Mbps  -> 2500000 bytes/s,  22 Mbps  -> 2750000 bytes/s
		// latest row's created_at is malformed -> timestamp unknown, but the
		// download/upload values must still come through.
		fmt.Fprint(w, `{"data": [
			{"download": 18750000, "upload": 2500000, "created_at": "not-a-date"},
			{"download": 12500000, "upload": 2750000, "created_at": "2026-09-05T11:00:00Z"}
		]}`)
	}))
	defer server.Close()

	client := New(server.URL, "test-token")
	trend, err := client.Fetch(context.Background())
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if !trend.Download.HasData || trend.Download.Mbps != 150 {
		t.Errorf("Download = %+v, want HasData=true, Mbps=150 despite malformed created_at", trend.Download)
	}
	if !trend.Upload.HasData || trend.Upload.Mbps != 20 {
		t.Errorf("Upload = %+v, want HasData=true, Mbps=20 despite malformed created_at", trend.Upload)
	}
	if trend.Download.ShowArrow || trend.Upload.ShowArrow {
		t.Errorf("expected no arrows when the latest result's timestamp is unknown, got %+v", trend)
	}
}
