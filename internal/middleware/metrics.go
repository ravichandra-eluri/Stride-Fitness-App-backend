package middleware

import (
	"net/http"
	"strconv"
	"time"

	"stride/backend/internal/metrics"
)

// Metrics returns a middleware that records Prometheus metrics for HTTP requests.
func Metrics(m *metrics.Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Track active requests
			m.HTTPActiveRequests.Inc()
			defer m.HTTPActiveRequests.Dec()

			// Wrap response writer to capture status and size
			rw := &metricsResponseWriter{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			// Process request
			next.ServeHTTP(rw, r)

			// Record metrics
			duration := time.Since(start)
			path := normalizePath(r.URL.Path)
			m.RecordHTTPRequest(r.Method, path, rw.status, duration, rw.size)
		})
	}
}

// metricsResponseWriter wraps http.ResponseWriter to capture status and size.
type metricsResponseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (w *metricsResponseWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *metricsResponseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

// normalizePath reduces path cardinality for metrics.
// e.g., /api/v1/users/123 -> /api/v1/users/:id
func normalizePath(path string) string {
	// Simple normalization: replace UUIDs and numeric IDs with placeholders
	// This prevents high cardinality in metrics
	result := make([]byte, 0, len(path))
	i := 0
	for i < len(path) {
		if path[i] == '/' {
			result = append(result, '/')
			i++
			// Check if next segment looks like an ID
			start := i
			for i < len(path) && path[i] != '/' {
				i++
			}
			segment := path[start:i]
			if isID(segment) {
				result = append(result, ":id"...)
			} else {
				result = append(result, segment...)
			}
		} else {
			result = append(result, path[i])
			i++
		}
	}
	return string(result)
}

// isID checks if a path segment looks like an ID (UUID or numeric).
func isID(s string) bool {
	if len(s) == 0 {
		return false
	}

	// Check if it's a UUID (36 chars with hyphens)
	if len(s) == 36 && s[8] == '-' && s[13] == '-' && s[18] == '-' && s[23] == '-' {
		return true
	}

	// Check if it's numeric
	_, err := strconv.Atoi(s)
	return err == nil
}
