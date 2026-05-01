package httpapi

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

var appMetrics = newMetrics()

type metricsRegistry struct {
	startedAt time.Time
	mu        sync.Mutex
	requests  map[string]uint64
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func newMetrics() *metricsRegistry {
	return &metricsRegistry{
		startedAt: time.Now(),
		requests:  make(map[string]uint64),
	}
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}

func metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(recorder, r)

		if r.URL.Path == "/metrics" {
			return
		}
		appMetrics.observe(r.Method, sanitizePath(r.URL.Path), recorder.status)
	})
}

func (m *metricsRegistry) observe(method, path string, status int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf(`method="%s",path="%s",status="%d"`, method, path, status)
	m.requests[key]++
}

func (s Server) metrics(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	appMetrics.mu.Lock()
	defer appMetrics.mu.Unlock()

	_, _ = fmt.Fprintf(w, "# HELP bank_api_uptime_seconds API uptime in seconds.\n")
	_, _ = fmt.Fprintf(w, "# TYPE bank_api_uptime_seconds gauge\n")
	_, _ = fmt.Fprintf(w, "bank_api_uptime_seconds %.0f\n", time.Since(appMetrics.startedAt).Seconds())
	_, _ = fmt.Fprintf(w, "# HELP bank_api_http_requests_total Total HTTP requests processed by API.\n")
	_, _ = fmt.Fprintf(w, "# TYPE bank_api_http_requests_total counter\n")
	for labels, value := range appMetrics.requests {
		_, _ = fmt.Fprintf(w, "bank_api_http_requests_total{%s} %d\n", labels, value)
	}
}

func sanitizePath(path string) string {
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if part == "" {
			continue
		}
		allDigits := true
		for _, ch := range part {
			if ch < '0' || ch > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			parts[i] = "{id}"
		}
	}
	return strings.Join(parts, "/")
}
