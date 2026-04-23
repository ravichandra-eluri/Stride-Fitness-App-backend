package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics holds all Prometheus metrics for the application.
type Metrics struct {
	// HTTP metrics
	HTTPRequestsTotal    *prometheus.CounterVec
	HTTPRequestDuration  *prometheus.HistogramVec
	HTTPActiveRequests   prometheus.Gauge
	HTTPResponseSize     *prometheus.HistogramVec

	// Database metrics
	DBQueryTotal         *prometheus.CounterVec
	DBQueryDuration      *prometheus.HistogramVec
	DBConnectionsActive  prometheus.Gauge
	DBConnectionsIdle    prometheus.Gauge

	// AI metrics
	AIRequestsTotal      *prometheus.CounterVec
	AIRequestDuration    *prometheus.HistogramVec
	AITokensUsed         *prometheus.CounterVec

	// Push notification metrics
	PushNotificationsTotal *prometheus.CounterVec

	// Cron job metrics
	CronJobDuration      *prometheus.HistogramVec
	CronJobUsersProcessed *prometheus.GaugeVec

	// Business metrics
	ActiveSubscriptions  *prometheus.GaugeVec
	DailyActiveUsers     prometheus.Gauge
	MealPlansGenerated   prometheus.Counter
	FoodEntriesLogged    prometheus.Counter
}

// New creates and registers all Prometheus metrics.
func New() *Metrics {
	return &Metrics{
		// HTTP metrics
		HTTPRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "http_requests_total",
				Help: "Total number of HTTP requests",
			},
			[]string{"method", "path", "status"},
		),
		HTTPRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_request_duration_seconds",
				Help:    "HTTP request duration in seconds",
				Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
			[]string{"method", "path"},
		),
		HTTPActiveRequests: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "http_active_requests",
				Help: "Number of active HTTP requests",
			},
		),
		HTTPResponseSize: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "http_response_size_bytes",
				Help:    "HTTP response size in bytes",
				Buckets: []float64{100, 500, 1000, 5000, 10000, 50000, 100000},
			},
			[]string{"method", "path"},
		),

		// Database metrics
		DBQueryTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "db_queries_total",
				Help: "Total number of database queries",
			},
			[]string{"operation", "table", "status"},
		),
		DBQueryDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "db_query_duration_seconds",
				Help:    "Database query duration in seconds",
				Buckets: []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1},
			},
			[]string{"operation", "table"},
		),
		DBConnectionsActive: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "db_connections_active",
				Help: "Number of active database connections",
			},
		),
		DBConnectionsIdle: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "db_connections_idle",
				Help: "Number of idle database connections",
			},
		),

		// AI metrics
		AIRequestsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ai_requests_total",
				Help: "Total number of AI API requests",
			},
			[]string{"model", "operation", "status"},
		),
		AIRequestDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "ai_request_duration_seconds",
				Help:    "AI API request duration in seconds",
				Buckets: []float64{.5, 1, 2.5, 5, 10, 25, 50, 100},
			},
			[]string{"model", "operation"},
		),
		AITokensUsed: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "ai_tokens_used_total",
				Help: "Total tokens used in AI requests",
			},
			[]string{"model", "operation", "type"},
		),

		// Push notification metrics
		PushNotificationsTotal: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: "push_notifications_total",
				Help: "Total number of push notifications sent",
			},
			[]string{"status"},
		),

		// Cron job metrics
		CronJobDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "cron_job_duration_seconds",
				Help:    "Cron job execution duration in seconds",
				Buckets: []float64{1, 5, 10, 30, 60, 120, 300, 600},
			},
			[]string{"job"},
		),
		CronJobUsersProcessed: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "cron_job_users_processed",
				Help: "Number of users processed in last cron job run",
			},
			[]string{"job"},
		),

		// Business metrics
		ActiveSubscriptions: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "active_subscriptions",
				Help: "Number of active subscriptions by plan",
			},
			[]string{"plan"},
		),
		DailyActiveUsers: promauto.NewGauge(
			prometheus.GaugeOpts{
				Name: "daily_active_users",
				Help: "Number of daily active users",
			},
		),
		MealPlansGenerated: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "meal_plans_generated_total",
				Help: "Total number of meal plans generated",
			},
		),
		FoodEntriesLogged: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: "food_entries_logged_total",
				Help: "Total number of food entries logged",
			},
		),
	}
}

// Handler returns the Prometheus metrics HTTP handler.
func Handler() http.Handler {
	return promhttp.Handler()
}

// ── Convenience methods ─────────────────────────────────────────────────────

// RecordHTTPRequest records metrics for an HTTP request.
func (m *Metrics) RecordHTTPRequest(method, path string, status int, duration time.Duration, size int) {
	m.HTTPRequestsTotal.WithLabelValues(method, path, strconv.Itoa(status)).Inc()
	m.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration.Seconds())
	m.HTTPResponseSize.WithLabelValues(method, path).Observe(float64(size))
}

// RecordDBQuery records metrics for a database query.
func (m *Metrics) RecordDBQuery(operation, table string, duration time.Duration, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	m.DBQueryTotal.WithLabelValues(operation, table, status).Inc()
	m.DBQueryDuration.WithLabelValues(operation, table).Observe(duration.Seconds())
}

// RecordAIRequest records metrics for an AI API request.
func (m *Metrics) RecordAIRequest(model, operation string, duration time.Duration, inputTokens, outputTokens int, err error) {
	status := "success"
	if err != nil {
		status = "error"
	}
	m.AIRequestsTotal.WithLabelValues(model, operation, status).Inc()
	m.AIRequestDuration.WithLabelValues(model, operation).Observe(duration.Seconds())
	if inputTokens > 0 {
		m.AITokensUsed.WithLabelValues(model, operation, "input").Add(float64(inputTokens))
	}
	if outputTokens > 0 {
		m.AITokensUsed.WithLabelValues(model, operation, "output").Add(float64(outputTokens))
	}
}

// RecordPushNotification records metrics for a push notification.
func (m *Metrics) RecordPushNotification(success bool) {
	status := "success"
	if !success {
		status = "failure"
	}
	m.PushNotificationsTotal.WithLabelValues(status).Inc()
}

// RecordCronJob records metrics for a cron job execution.
func (m *Metrics) RecordCronJob(jobName string, duration time.Duration, usersProcessed int) {
	m.CronJobDuration.WithLabelValues(jobName).Observe(duration.Seconds())
	m.CronJobUsersProcessed.WithLabelValues(jobName).Set(float64(usersProcessed))
}

// UpdateDBConnectionStats updates database connection metrics.
func (m *Metrics) UpdateDBConnectionStats(active, idle int) {
	m.DBConnectionsActive.Set(float64(active))
	m.DBConnectionsIdle.Set(float64(idle))
}
