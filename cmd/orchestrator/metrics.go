package main

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// jobsTotal counts completed jobs by scenario and terminal status (done|failed).
	jobsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orchestrator_jobs_total",
		Help: "Total completed jobs by scenario and status.",
	}, []string{"scenario", "status"})

	// jobDuration tracks how long jobs take to complete.
	jobDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orchestrator_job_duration_seconds",
		Help:    "Job wall-clock time from submit to completion.",
		Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600},
	}, []string{"scenario", "status"})

	// jobsActive is the current count of pending+running jobs.
	jobsActive = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "orchestrator_jobs_active",
		Help: "Current number of jobs in pending or running state.",
	})

	// scheduleLastStatus reports the outcome of a schedule's last run:
	// 1 = the run finished, 0 = it failed, was cancelled, or the scheduler
	// refused to start it.
	//
	// A schedule that has not produced an outcome yet has no series at all,
	// which is how Prometheus says "no data" — better than a sentinel value
	// that has to be explained and that alerting rules have to special-case.
	scheduleLastStatus = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "orchestrator_schedule_last_status",
		Help: "Outcome of a schedule's last run: 1=done, 0=failed/cancelled/refused. Absent until the first outcome.",
	}, []string{"id", "scenario"})

	httpRequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "orchestrator_http_requests_total",
		Help: "HTTP requests by method, route pattern, and status code.",
	}, []string{"method", "path", "status"})

	httpRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "orchestrator_http_request_duration_seconds",
		Help:    "HTTP request latency.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "path"})
)

// RecordJobSubmit bumps the active-job gauge when a job is queued.
func RecordJobSubmit() { jobsActive.Inc() }

// RecordJobDone updates counters/histogram and decrements active when a job finishes.
func RecordJobDone(scenario, status string, started time.Time) {
	jobsTotal.WithLabelValues(scenario, status).Inc()
	jobDuration.WithLabelValues(scenario, status).Observe(time.Since(started).Seconds())
	jobsActive.Dec()
}

// RecordScheduleOutcome sets the gauge once a schedule's run has an outcome.
//
// Only outcomes reach here. It used to be called at dispatch too, with the
// status the scheduler writes at that moment — "running" — which mapped to the
// sentinel for "never run". Every healthy schedule therefore reported that it
// had never run, permanently: an alert on the sentinel fired constantly, and
// one on failure never fired at all.
func RecordScheduleOutcome(id, scenario, status string) {
	v := 0.0
	if status == "done" {
		v = 1
	}
	scheduleLastStatus.WithLabelValues(id, scenario).Set(v)
}

// SyncActiveJobs seeds the active-job gauge from the DB at startup.
func SyncActiveJobs(db *OrchestratorDB) {
	if n, err := db.CountActiveJobs(); err == nil {
		jobsActive.Set(float64(n))
	}
}

// prometheusMiddleware records per-request counters and latency.
// Uses the chi route pattern (e.g. /jobs/{id}) to avoid high label cardinality.
func prometheusMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)

		path := r.URL.Path
		if rctx := chi.RouteContext(r.Context()); rctx != nil {
			if p := rctx.RoutePattern(); p != "" {
				path = p
			}
		}

		elapsed := time.Since(start).Seconds()
		httpRequestsTotal.WithLabelValues(r.Method, path, strconv.Itoa(sw.status)).Inc()
		httpRequestDuration.WithLabelValues(r.Method, path).Observe(elapsed)
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

// MetricsHandler returns the Prometheus scrape handler for GET /metrics.
func MetricsHandler() http.Handler { return promhttp.Handler() }
