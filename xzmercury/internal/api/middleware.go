package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/rs/zerolog/log"
)

var (
	httpRequestsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "xzmercury_http_requests_total",
			Help: "Total number of HTTP requests by method, path, and status code",
		},
		[]string{"method", "path", "status"},
	)

	httpRequestDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "xzmercury_http_request_duration_seconds",
			Help:    "HTTP request latency in seconds",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "path"},
	)
)

// zerologMiddleware logs every HTTP request with method, path, status, and latency.
func zerologMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Info().
			Str("method", r.Method).
			Str("path", r.URL.Path).
			Int("status", rw.status).
			Dur("latency_ms", time.Since(start)).
			Str("remote", r.RemoteAddr).
			Msg("request")
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

// rateLimitMiddleware is a simple Redis token-bucket rate limiter.
// If rateLimit <= 0 it is a no-op.
func rateLimitMiddleware(rateLimit int) func(http.Handler) http.Handler {
	if rateLimit <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	// TODO(v2): implement sliding-window rate limiter via Redis Lua script.
	// For v1, this is a pass-through placeholder.
	return func(next http.Handler) http.Handler { return next }
}

// prometheusMiddleware records HTTP request counts and latency.
func prometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		elapsed := time.Since(start).Seconds()
		path := r.URL.Path
		httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(rw.status)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(elapsed)
	})
}
