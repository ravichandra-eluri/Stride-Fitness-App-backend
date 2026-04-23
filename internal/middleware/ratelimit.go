package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	apperrors "stride/backend/internal/errors"
)

// RateLimiter provides rate limiting functionality.
type RateLimiter struct {
	visitors map[string]*visitor
	mu       sync.RWMutex
	rate     rate.Limit
	burst    int
	cleanup  time.Duration
}

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// RateLimitConfig holds configuration for rate limiting.
type RateLimitConfig struct {
	// RequestsPerMinute is the number of requests allowed per minute.
	RequestsPerMinute int
	// Burst is the maximum burst size.
	Burst int
	// CleanupInterval is how often to clean up old visitor entries.
	CleanupInterval time.Duration
}

// NewRateLimiter creates a new rate limiter with the given configuration.
func NewRateLimiter(cfg RateLimitConfig) *RateLimiter {
	rl := &RateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate.Limit(float64(cfg.RequestsPerMinute) / 60.0),
		burst:    cfg.Burst,
		cleanup:  cfg.CleanupInterval,
	}

	if rl.burst == 0 {
		rl.burst = cfg.RequestsPerMinute / 2
		if rl.burst < 1 {
			rl.burst = 1
		}
	}

	if rl.cleanup == 0 {
		rl.cleanup = 5 * time.Minute
	}

	go rl.cleanupVisitors()
	return rl
}

// getVisitor returns the rate limiter for a visitor, creating one if needed.
func (rl *RateLimiter) getVisitor(key string) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[key]
	if !exists {
		limiter := rate.NewLimiter(rl.rate, rl.burst)
		rl.visitors[key] = &visitor{limiter: limiter, lastSeen: time.Now()}
		return limiter
	}

	v.lastSeen = time.Now()
	return v.limiter
}

// cleanupVisitors removes old visitors periodically.
func (rl *RateLimiter) cleanupVisitors() {
	for {
		time.Sleep(rl.cleanup)
		rl.mu.Lock()
		for key, v := range rl.visitors {
			if time.Since(v.lastSeen) > rl.cleanup*2 {
				delete(rl.visitors, key)
			}
		}
		rl.mu.Unlock()
	}
}

// Allow checks if a request from the given key is allowed.
func (rl *RateLimiter) Allow(key string) bool {
	limiter := rl.getVisitor(key)
	return limiter.Allow()
}

// Limit returns a middleware that rate limits requests by IP.
func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)
		if !rl.Allow(ip) {
			apperrors.WriteError(w, apperrors.NewRateLimitedError(60))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// LimitByUser returns a middleware that rate limits requests by user ID.
// Must be used after authentication middleware.
func (rl *RateLimiter) LimitByUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := UserIDFromCtx(r.Context())
		if userID == "" {
			// Fall back to IP-based limiting if no user ID
			userID = getClientIP(r)
		}
		if !rl.Allow("user:" + userID) {
			apperrors.WriteError(w, apperrors.NewRateLimitedError(60))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// getClientIP extracts the real client IP from a request.
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (common in proxied setups)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// X-Forwarded-For can contain multiple IPs; take the first one
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return strings.TrimSpace(xff)
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// ── Pre-configured rate limiters ────────────────────────────────────────────

// RateLimiterSet holds multiple rate limiters for different endpoint types.
type RateLimiterSet struct {
	Auth    *RateLimiter
	AI      *RateLimiter
	General *RateLimiter
}

// NewRateLimiterSet creates a set of rate limiters with common configurations.
func NewRateLimiterSet(authPerMin, aiPerMin, generalPerMin int) *RateLimiterSet {
	return &RateLimiterSet{
		Auth: NewRateLimiter(RateLimitConfig{
			RequestsPerMinute: authPerMin,
			Burst:             authPerMin,
		}),
		AI: NewRateLimiter(RateLimitConfig{
			RequestsPerMinute: aiPerMin,
			Burst:             aiPerMin / 2,
		}),
		General: NewRateLimiter(RateLimitConfig{
			RequestsPerMinute: generalPerMin,
			Burst:             generalPerMin / 2,
		}),
	}
}
