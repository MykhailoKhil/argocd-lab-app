// demo-api: a tiny HTTP service for the Argo CD lab.
//
// It exposes Prometheus metrics and can be made deliberately "bad" through
// environment variables, so canary analysis (T15–T17) has something to catch:
//
//	FAULT_LATENCY_MS  extra latency added to every request on /
//	FAULT_ERROR_RATE  share of requests on / that return 500 (0.0–1.0)
//
// Metrics are written in the Prometheus text format by metrics.go (stdlib only,
// no external dependencies).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

// version is set at build time: -ldflags "-X main.version=..."
var version = "dev"

type faults struct {
	latency   time.Duration
	errorRate float64
}

func loadFaults() faults {
	ms, _ := strconv.Atoi(os.Getenv("FAULT_LATENCY_MS"))
	rate, _ := strconv.ParseFloat(os.Getenv("FAULT_ERROR_RATE"), 64)
	return faults{latency: time.Duration(ms) * time.Millisecond, errorRate: rate}
}

// statusRecorder captures the status code for metrics.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

func instrument(path string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		h(rec, r)
		metrics.observe(path, rec.code, time.Since(start).Seconds())
	}
}

func rootHandler(f faults) http.HandlerFunc {
	host, _ := os.Hostname()
	return func(w http.ResponseWriter, r *http.Request) {
		if f.latency > 0 {
			time.Sleep(f.latency)
		}
		w.Header().Set("Content-Type", "application/json")
		if f.errorRate > 0 && rand.Float64() < f.errorRate {
			w.WriteHeader(http.StatusInternalServerError)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "injected failure", "version": version})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"message": "hello from demo-api", "version": version, "pod": host})
	}
}

func newMux(f faults) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", instrument("/", rootHandler(f)))
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /metrics", metrics.handler)
	return mux
}

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	f := loadFaults()

	addr := ":8080"
	if p := os.Getenv("PORT"); p != "" {
		addr = ":" + p
	}
	srv := &http.Server{Addr: addr, Handler: newMux(f), ReadHeaderTimeout: 5 * time.Second}

	go func() {
		log.Info("starting", "addr", addr, "version", version, "fault_latency", f.latency.String(), "fault_error_rate", f.errorRate)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("server failed", "err", err)
			os.Exit(1)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	log.Info("stopped")
}
