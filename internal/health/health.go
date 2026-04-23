package health

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// Status represents the health status of a component.
type Status string

const (
	StatusUp       Status = "up"
	StatusDown     Status = "down"
	StatusDegraded Status = "degraded"
)

// CheckResult represents the result of a health check.
type CheckResult struct {
	Status    Status `json:"status"`
	LatencyMs int64  `json:"latency_ms,omitempty"`
	Message   string `json:"message,omitempty"`
}

// HealthResponse is the response structure for health endpoints.
type HealthResponse struct {
	Status  Status                 `json:"status"`
	Version string                 `json:"version,omitempty"`
	Checks  map[string]CheckResult `json:"checks,omitempty"`
}

// Checker provides health check functionality.
type Checker struct {
	version string
	db      *sql.DB
	checks  map[string]CheckFunc
	mu      sync.RWMutex
}

// CheckFunc is a function that performs a health check.
type CheckFunc func(ctx context.Context) CheckResult

// NewChecker creates a new health checker.
func NewChecker(version string, db *sql.DB) *Checker {
	c := &Checker{
		version: version,
		db:      db,
		checks:  make(map[string]CheckFunc),
	}

	// Register default checks
	if db != nil {
		c.RegisterCheck("database", c.checkDatabase)
	}

	return c
}

// RegisterCheck registers a custom health check.
func (c *Checker) RegisterCheck(name string, check CheckFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checks[name] = check
}

// checkDatabase checks database connectivity.
func (c *Checker) checkDatabase(ctx context.Context) CheckResult {
	start := time.Now()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := c.db.PingContext(ctx); err != nil {
		return CheckResult{
			Status:    StatusDown,
			LatencyMs: time.Since(start).Milliseconds(),
			Message:   "Database connection failed",
		}
	}

	return CheckResult{
		Status:    StatusUp,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

// RunChecks runs all registered health checks.
func (c *Checker) RunChecks(ctx context.Context) HealthResponse {
	c.mu.RLock()
	defer c.mu.RUnlock()

	results := make(map[string]CheckResult)
	overallStatus := StatusUp

	// Run checks concurrently
	var wg sync.WaitGroup
	var resultsMu sync.Mutex

	for name, check := range c.checks {
		wg.Add(1)
		go func(name string, check CheckFunc) {
			defer wg.Done()
			result := check(ctx)

			resultsMu.Lock()
			results[name] = result
			if result.Status == StatusDown {
				overallStatus = StatusDown
			} else if result.Status == StatusDegraded && overallStatus != StatusDown {
				overallStatus = StatusDegraded
			}
			resultsMu.Unlock()
		}(name, check)
	}

	wg.Wait()

	return HealthResponse{
		Status:  overallStatus,
		Version: c.version,
		Checks:  results,
	}
}

// LivenessHandler returns an HTTP handler for liveness probes.
// This is a simple check that the service is running.
func (c *Checker) LivenessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	}
}

// ReadinessHandler returns an HTTP handler for readiness probes.
// This checks that all dependencies are available.
func (c *Checker) ReadinessHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		response := c.RunChecks(ctx)

		w.Header().Set("Content-Type", "application/json")

		if response.Status == StatusUp {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// FullHealthHandler returns an HTTP handler that returns full health information.
func (c *Checker) FullHealthHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()

		response := c.RunChecks(ctx)

		w.Header().Set("Content-Type", "application/json")

		switch response.Status {
		case StatusUp:
			w.WriteHeader(http.StatusOK)
		case StatusDegraded:
			w.WriteHeader(http.StatusOK) // Degraded is still "available"
		default:
			w.WriteHeader(http.StatusServiceUnavailable)
		}

		json.NewEncoder(w).Encode(response)
	}
}

// ── Database stats helper ───────────────────────────────────────────────────

// DBStats holds database connection pool statistics.
type DBStats struct {
	MaxOpenConnections int `json:"max_open_connections"`
	OpenConnections    int `json:"open_connections"`
	InUse              int `json:"in_use"`
	Idle               int `json:"idle"`
	WaitCount          int64 `json:"wait_count"`
	WaitDuration       string `json:"wait_duration"`
}

// GetDBStats returns current database connection pool statistics.
func GetDBStats(db *sql.DB) DBStats {
	stats := db.Stats()
	return DBStats{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDuration:       stats.WaitDuration.String(),
	}
}
