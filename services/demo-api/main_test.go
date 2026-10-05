package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func get(t *testing.T, mux *http.ServeMux, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestRootOK(t *testing.T) {
	if code := get(t, newMux(faults{}), "/").Code; code != http.StatusOK {
		t.Fatalf("want 200, got %d", code)
	}
}

func TestInjectedErrors(t *testing.T) {
	if code := get(t, newMux(faults{errorRate: 1}), "/").Code; code != http.StatusInternalServerError {
		t.Fatalf("want 500, got %d", code)
	}
}

func TestInjectedLatency(t *testing.T) {
	start := time.Now()
	get(t, newMux(faults{latency: 50 * time.Millisecond}), "/")
	if d := time.Since(start); d < 50*time.Millisecond {
		t.Fatalf("expected at least 50ms, took %s", d)
	}
}

func TestProbesAndMetrics(t *testing.T) {
	mux := newMux(faults{})
	for _, p := range []string{"/healthz", "/readyz", "/metrics"} {
		if code := get(t, mux, p).Code; code != http.StatusOK {
			t.Errorf("%s: want 200, got %d", p, code)
		}
	}
}

func TestMetricsExposition(t *testing.T) {
	mux := newMux(faults{errorRate: 1})
	get(t, mux, "/")
	body := get(t, mux, "/metrics").Body.String()
	for _, want := range []string{
		`http_requests_total{path="/",code="500"}`,
		`http_request_duration_seconds_bucket{path="/",le="+Inf"}`,
		`demo_api_build_info{version="dev"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("metrics missing %s", want)
		}
	}
}
