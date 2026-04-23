package ai

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// RetryConfig holds configuration for retry behavior.
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts.
	MaxRetries int
	// InitialBackoff is the initial backoff duration.
	InitialBackoff time.Duration
	// MaxBackoff is the maximum backoff duration.
	MaxBackoff time.Duration
	// BackoffMultiplier is the factor by which backoff increases.
	BackoffMultiplier float64
	// Jitter adds randomness to backoff (0.0 to 1.0).
	Jitter float64
}

// DefaultRetryConfig returns sensible defaults for AI API retries.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    1 * time.Second,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
		Jitter:            0.1,
	}
}

// Retrier provides retry functionality with exponential backoff.
type Retrier struct {
	config RetryConfig
}

// NewRetrier creates a new Retrier with the given configuration.
func NewRetrier(config RetryConfig) *Retrier {
	return &Retrier{config: config}
}

// Do executes a function with retries.
func (r *Retrier) Do(ctx context.Context, operation string, fn func() error) error {
	var lastErr error
	backoff := r.config.InitialBackoff

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		if err := fn(); err != nil {
			lastErr = err

			// Check if error is retryable
			if !isRetryable(err) {
				return err
			}

			// Check if we've exhausted retries
			if attempt >= r.config.MaxRetries {
				return &RetryExhaustedError{
					Operation:  operation,
					Attempts:   attempt + 1,
					LastError:  err,
				}
			}

			// Calculate backoff with jitter
			jitter := time.Duration(float64(backoff) * r.config.Jitter * (rand.Float64()*2 - 1))
			sleepDuration := backoff + jitter

			// Wait before retry
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(sleepDuration):
			}

			// Increase backoff for next attempt
			backoff = time.Duration(float64(backoff) * r.config.BackoffMultiplier)
			if backoff > r.config.MaxBackoff {
				backoff = r.config.MaxBackoff
			}

			continue
		}

		return nil
	}

	return lastErr
}

// DoWithResult executes a function that returns a result with retries.
func DoWithResult[T any](ctx context.Context, r *Retrier, operation string, fn func() (T, error)) (T, error) {
	var result T
	var lastErr error
	backoff := r.config.InitialBackoff

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		var err error
		result, err = fn()
		if err != nil {
			lastErr = err

			if !isRetryable(err) {
				return result, err
			}

			if attempt >= r.config.MaxRetries {
				return result, &RetryExhaustedError{
					Operation:  operation,
					Attempts:   attempt + 1,
					LastError:  err,
				}
			}

			jitter := time.Duration(float64(backoff) * r.config.Jitter * (rand.Float64()*2 - 1))
			select {
			case <-ctx.Done():
				return result, ctx.Err()
			case <-time.After(backoff + jitter):
			}

			backoff = time.Duration(float64(backoff) * r.config.BackoffMultiplier)
			if backoff > r.config.MaxBackoff {
				backoff = r.config.MaxBackoff
			}

			continue
		}

		return result, nil
	}

	return result, lastErr
}

// RetryExhaustedError is returned when all retry attempts have been exhausted.
type RetryExhaustedError struct {
	Operation string
	Attempts  int
	LastError error
}

func (e *RetryExhaustedError) Error() string {
	return "retry exhausted for " + e.Operation + " after " + string(rune('0'+e.Attempts)) + " attempts: " + e.LastError.Error()
}

func (e *RetryExhaustedError) Unwrap() error {
	return e.LastError
}

// isRetryable determines if an error should be retried.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}

	// Context errors are not retryable
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}

	// Check for HTTP status code errors
	var httpErr *HTTPError
	if errors.As(err, &httpErr) {
		switch httpErr.StatusCode {
		case http.StatusTooManyRequests, // 429
			http.StatusInternalServerError,     // 500
			http.StatusBadGateway,              // 502
			http.StatusServiceUnavailable,      // 503
			http.StatusGatewayTimeout:          // 504
			return true
		default:
			return false
		}
	}

	// Assume network errors are retryable
	return true
}

// HTTPError represents an HTTP error with status code.
type HTTPError struct {
	StatusCode int
	Message    string
}

func (e *HTTPError) Error() string {
	return e.Message
}

// ── Circuit Breaker ─────────────────────────────────────────────────────────

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// CircuitBreakerConfig holds configuration for the circuit breaker.
type CircuitBreakerConfig struct {
	// FailureThreshold is the number of failures before opening the circuit.
	FailureThreshold int
	// SuccessThreshold is the number of successes in half-open state to close the circuit.
	SuccessThreshold int
	// Timeout is how long the circuit stays open before transitioning to half-open.
	Timeout time.Duration
}

// DefaultCircuitBreakerConfig returns sensible defaults.
func DefaultCircuitBreakerConfig() CircuitBreakerConfig {
	return CircuitBreakerConfig{
		FailureThreshold: 5,
		SuccessThreshold: 2,
		Timeout:          30 * time.Second,
	}
}

// CircuitBreaker implements the circuit breaker pattern.
type CircuitBreaker struct {
	config       CircuitBreakerConfig
	state        CircuitState
	failures     int
	successes    int
	lastFailure  time.Time
	mu           sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(config CircuitBreakerConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config: config,
		state:  CircuitClosed,
	}
}

// Allow checks if a request should be allowed.
func (cb *CircuitBreaker) Allow() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	switch cb.state {
	case CircuitClosed:
		return true
	case CircuitOpen:
		// Check if timeout has elapsed
		if time.Since(cb.lastFailure) >= cb.config.Timeout {
			// Transition to half-open will happen on next call
			return true
		}
		return false
	case CircuitHalfOpen:
		return true
	default:
		return true
	}
}

// RecordSuccess records a successful operation.
func (cb *CircuitBreaker) RecordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	switch cb.state {
	case CircuitHalfOpen:
		cb.successes++
		if cb.successes >= cb.config.SuccessThreshold {
			cb.state = CircuitClosed
			cb.failures = 0
			cb.successes = 0
		}
	case CircuitClosed:
		cb.failures = 0
	}
}

// RecordFailure records a failed operation.
func (cb *CircuitBreaker) RecordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cb.failures++
	cb.lastFailure = time.Now()

	switch cb.state {
	case CircuitClosed:
		if cb.failures >= cb.config.FailureThreshold {
			cb.state = CircuitOpen
		}
	case CircuitHalfOpen:
		cb.state = CircuitOpen
		cb.successes = 0
	}
}

// State returns the current state of the circuit breaker.
func (cb *CircuitBreaker) State() CircuitState {
	cb.mu.RLock()
	defer cb.mu.RUnlock()

	// Check for transition from open to half-open
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= cb.config.Timeout {
		return CircuitHalfOpen
	}

	return cb.state
}

// Execute runs a function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	if !cb.Allow() {
		return ErrCircuitOpen
	}

	// Check for transition to half-open
	cb.mu.Lock()
	if cb.state == CircuitOpen && time.Since(cb.lastFailure) >= cb.config.Timeout {
		cb.state = CircuitHalfOpen
		cb.successes = 0
	}
	cb.mu.Unlock()

	err := fn()
	if err != nil {
		cb.RecordFailure()
		return err
	}

	cb.RecordSuccess()
	return nil
}

// ErrCircuitOpen is returned when the circuit is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")
