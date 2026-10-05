package main

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Minimal Prometheus exposition: a request counter and a latency histogram.
// Enough for PromQL like:
//   sum(rate(http_requests_total{code=~"5.."}[1m])) / sum(rate(http_requests_total[1m]))
//   histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[1m])))

var buckets = []float64{.005, .01, .025, .05, .1, .2, .3, .5, .75, 1, 2, 5}

type histogram struct {
	counts []uint64 // cumulative per bucket
	sum    float64
	count  uint64
}

type registry struct {
	mu       sync.Mutex
	requests map[[2]string]uint64 // {path, code} -> count
	latency  map[string]*histogram
}

var metrics = &registry{requests: map[[2]string]uint64{}, latency: map[string]*histogram{}}

func (r *registry) observe(path string, code int, seconds float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requests[[2]string{path, strconv.Itoa(code)}]++
	h, ok := r.latency[path]
	if !ok {
		h = &histogram{counts: make([]uint64, len(buckets))}
		r.latency[path] = h
	}
	for i, le := range buckets {
		if seconds <= le {
			h.counts[i]++
		}
	}
	h.sum += seconds
	h.count++
}

func fmtFloat(f float64) string { return strconv.FormatFloat(f, 'g', -1, 64) }

func (r *registry) handler(w http.ResponseWriter, _ *http.Request) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder

	fmt.Fprintf(&b, "# HELP demo_api_build_info Build information; value is always 1.\n# TYPE demo_api_build_info gauge\n")
	fmt.Fprintf(&b, "demo_api_build_info{version=%q} 1\n", version)

	fmt.Fprintf(&b, "# HELP http_requests_total HTTP requests by path and status code.\n# TYPE http_requests_total counter\n")
	keys := make([][2]string, 0, len(r.requests))
	for k := range r.requests {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0]+keys[i][1] < keys[j][0]+keys[j][1] })
	for _, k := range keys {
		fmt.Fprintf(&b, "http_requests_total{path=%q,code=%q} %d\n", k[0], k[1], r.requests[k])
	}

	fmt.Fprintf(&b, "# HELP http_request_duration_seconds HTTP request latency.\n# TYPE http_request_duration_seconds histogram\n")
	paths := make([]string, 0, len(r.latency))
	for p := range r.latency {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		h := r.latency[p]
		for i, le := range buckets {
			fmt.Fprintf(&b, "http_request_duration_seconds_bucket{path=%q,le=%q} %d\n", p, fmtFloat(le), h.counts[i])
		}
		fmt.Fprintf(&b, "http_request_duration_seconds_bucket{path=%q,le=\"+Inf\"} %d\n", p, h.count)
		fmt.Fprintf(&b, "http_request_duration_seconds_sum{path=%q} %s\n", p, fmtFloat(h.sum))
		fmt.Fprintf(&b, "http_request_duration_seconds_count{path=%q} %d\n", p, h.count)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
