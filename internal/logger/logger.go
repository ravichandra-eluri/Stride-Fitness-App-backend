package logger

import (
	"context"
	"io"
	"log/slog"
	"os"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// RequestIDKey is the context key for request ID.
	RequestIDKey contextKey = "request_id"
	// UserIDKey is the context key for user ID.
	UserIDKey contextKey = "user_id"
)

// Logger wraps slog.Logger with additional context-aware methods.
type Logger struct {
	*slog.Logger
}

// New creates a new Logger with the specified level and output format.
// If isDev is true, uses text format; otherwise uses JSON for production.
func New(level string, isDev bool) *Logger {
	var lvl slog.Level
	switch level {
	case "debug":
		lvl = slog.LevelDebug
	case "warn":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}

	opts := &slog.HandlerOptions{
		Level:     lvl,
		AddSource: lvl == slog.LevelDebug,
	}

	var handler slog.Handler
	if isDev {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return &Logger{slog.New(handler)}
}

// NewWithWriter creates a new Logger that writes to a custom writer (useful for testing).
func NewWithWriter(level string, w io.Writer) *Logger {
	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}
	return &Logger{slog.New(slog.NewJSONHandler(w, opts))}
}

// WithContext returns a new Logger with context values (request_id, user_id) added.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	attrs := make([]any, 0, 4)

	if reqID := ctx.Value(RequestIDKey); reqID != nil {
		attrs = append(attrs, "request_id", reqID)
	}
	if userID := ctx.Value(UserIDKey); userID != nil {
		attrs = append(attrs, "user_id", userID)
	}

	if len(attrs) == 0 {
		return l
	}

	return &Logger{l.Logger.With(attrs...)}
}

// WithRequestID returns a new Logger with the request ID added.
func (l *Logger) WithRequestID(requestID string) *Logger {
	return &Logger{l.Logger.With("request_id", requestID)}
}

// WithUserID returns a new Logger with the user ID added.
func (l *Logger) WithUserID(userID string) *Logger {
	return &Logger{l.Logger.With("user_id", userID)}
}

// WithError returns a new Logger with the error added.
func (l *Logger) WithError(err error) *Logger {
	if err == nil {
		return l
	}
	return &Logger{l.Logger.With("error", err.Error())}
}

// WithFields returns a new Logger with additional fields.
func (l *Logger) WithFields(fields map[string]any) *Logger {
	attrs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}
	return &Logger{l.Logger.With(attrs...)}
}

// HTTPRequest logs an HTTP request with common fields.
func (l *Logger) HTTPRequest(method, path string, status int, latencyMs float64) {
	l.Info("http_request",
		"method", method,
		"path", path,
		"status", status,
		"latency_ms", latencyMs,
	)
}

// AIRequest logs an AI API call with relevant metrics.
func (l *Logger) AIRequest(model string, tokens int, latencyMs float64, err error) {
	if err != nil {
		l.Error("ai_request",
			"model", model,
			"latency_ms", latencyMs,
			"error", err.Error(),
		)
	} else {
		l.Info("ai_request",
			"model", model,
			"tokens", tokens,
			"latency_ms", latencyMs,
		)
	}
}

// DBQuery logs a database query with timing.
func (l *Logger) DBQuery(operation string, table string, latencyMs float64, err error) {
	if err != nil {
		l.Error("db_query",
			"operation", operation,
			"table", table,
			"latency_ms", latencyMs,
			"error", err.Error(),
		)
	} else {
		l.Debug("db_query",
			"operation", operation,
			"table", table,
			"latency_ms", latencyMs,
		)
	}
}

// CronJob logs a cron job execution.
func (l *Logger) CronJob(jobName string, usersProcessed int, durationMs float64, err error) {
	if err != nil {
		l.Error("cron_job",
			"job", jobName,
			"users_processed", usersProcessed,
			"duration_ms", durationMs,
			"error", err.Error(),
		)
	} else {
		l.Info("cron_job",
			"job", jobName,
			"users_processed", usersProcessed,
			"duration_ms", durationMs,
		)
	}
}

// PushNotification logs a push notification attempt.
func (l *Logger) PushNotification(userID string, success bool, err error) {
	if err != nil {
		l.Warn("push_notification",
			"user_id", userID,
			"success", false,
			"error", err.Error(),
		)
	} else {
		l.Debug("push_notification",
			"user_id", userID,
			"success", true,
		)
	}
}

func parseLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
