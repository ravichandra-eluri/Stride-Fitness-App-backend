package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	// Server
	Port            string
	Env             string // "development", "staging", "production"
	LogLevel        string // "debug", "info", "warn", "error"
	ShutdownTimeout time.Duration

	// Database
	DatabaseURL     string
	DBMaxOpenConns  int
	DBMaxIdleConns  int
	DBConnMaxLife   time.Duration

	// Authentication
	JWTSecret           []byte
	JWTAccessTokenTTL   time.Duration
	JWTRefreshTokenTTL  time.Duration
	AppleTeamID         string
	AppleBundleID       string

	// AI
	ClaudeAPIKey    string
	ClaudeModel     string
	AIRequestTimeout time.Duration
	AIMaxRetries    int

	// APNs
	APNsKeyID       string
	APNsTeamID      string
	APNsKeyPath     string
	APNsBundleID    string
	APNsProduction  bool

	// Rate Limiting
	RateLimitAuthPerMin    int
	RateLimitAIPerMin      int
	RateLimitGeneralPerMin int

	// Feature Flags
	EnableMetrics   bool
	EnableRateLimit bool
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		// Server
		Port:            getEnv("PORT", "8080"),
		Env:             getEnv("ENV", "development"),
		LogLevel:        getEnv("LOG_LEVEL", "info"),
		ShutdownTimeout: getDuration("SHUTDOWN_TIMEOUT", 30*time.Second),

		// Database
		DatabaseURL:    getEnv("DATABASE_URL", ""),
		DBMaxOpenConns: getInt("DB_MAX_OPEN_CONNS", 25),
		DBMaxIdleConns: getInt("DB_MAX_IDLE_CONNS", 10),
		DBConnMaxLife:  getDuration("DB_CONN_MAX_LIFE", 5*time.Minute),

		// Authentication
		JWTSecret:          []byte(getEnv("JWT_SECRET", "")),
		JWTAccessTokenTTL:  getDuration("JWT_ACCESS_TOKEN_TTL", 24*time.Hour),
		JWTRefreshTokenTTL: getDuration("JWT_REFRESH_TOKEN_TTL", 30*24*time.Hour),
		AppleTeamID:        getEnv("APPLE_TEAM_ID", ""),
		AppleBundleID:      getEnv("APPLE_BUNDLE_ID", "com.stride.app"),

		// AI
		ClaudeAPIKey:     getEnv("CLAUDE_API_KEY", ""),
		ClaudeModel:      getEnv("CLAUDE_MODEL", "claude-sonnet-4-6"),
		AIRequestTimeout: getDuration("AI_REQUEST_TIMEOUT", 90*time.Second),
		AIMaxRetries:     getInt("AI_MAX_RETRIES", 3),

		// APNs
		APNsKeyID:      getEnv("APNS_KEY_ID", ""),
		APNsTeamID:     getEnv("APNS_TEAM_ID", ""),
		APNsKeyPath:    getEnv("APNS_KEY_PATH", ""),
		APNsBundleID:   getEnv("APNS_BUNDLE_ID", "com.stride.app"),
		APNsProduction: getBool("APNS_PRODUCTION", false),

		// Rate Limiting
		RateLimitAuthPerMin:    getInt("RATE_LIMIT_AUTH_PER_MIN", 10),
		RateLimitAIPerMin:      getInt("RATE_LIMIT_AI_PER_MIN", 20),
		RateLimitGeneralPerMin: getInt("RATE_LIMIT_GENERAL_PER_MIN", 100),

		// Feature Flags
		EnableMetrics:   getBool("ENABLE_METRICS", true),
		EnableRateLimit: getBool("ENABLE_RATE_LIMIT", true),
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Validate checks that required configuration values are set.
func (c *Config) Validate() error {
	var missing []string

	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if len(c.JWTSecret) == 0 {
		missing = append(missing, "JWT_SECRET")
	}
	if c.ClaudeAPIKey == "" {
		missing = append(missing, "CLAUDE_API_KEY")
	}

	if len(missing) > 0 {
		return fmt.Errorf("missing required environment variables: %s", strings.Join(missing, ", "))
	}

	// Validate enum values
	validEnvs := map[string]bool{"development": true, "staging": true, "production": true}
	if !validEnvs[c.Env] {
		return fmt.Errorf("invalid ENV value: %s (must be development, staging, or production)", c.Env)
	}

	validLogLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLogLevels[c.LogLevel] {
		return fmt.Errorf("invalid LOG_LEVEL value: %s (must be debug, info, warn, or error)", c.LogLevel)
	}

	return nil
}

// IsDevelopment returns true if running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// IsProduction returns true if running in production mode.
func (c *Config) IsProduction() bool {
	return c.Env == "production"
}

// Masked returns a copy of sensitive fields masked for logging.
func (c *Config) Masked() map[string]any {
	return map[string]any{
		"port":               c.Port,
		"env":                c.Env,
		"log_level":          c.LogLevel,
		"db_max_open_conns":  c.DBMaxOpenConns,
		"db_max_idle_conns":  c.DBMaxIdleConns,
		"database_url":       maskSecret(c.DatabaseURL),
		"jwt_secret":         maskSecret(string(c.JWTSecret)),
		"claude_api_key":     maskSecret(c.ClaudeAPIKey),
		"ai_request_timeout": c.AIRequestTimeout.String(),
		"ai_max_retries":     c.AIMaxRetries,
		"enable_metrics":     c.EnableMetrics,
		"enable_rate_limit":  c.EnableRateLimit,
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}

func getBool(key string, defaultVal bool) bool {
	if val := os.Getenv(key); val != "" {
		if b, err := strconv.ParseBool(val); err == nil {
			return b
		}
	}
	return defaultVal
}

func getDuration(key string, defaultVal time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			return d
		}
	}
	return defaultVal
}

func maskSecret(s string) string {
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "***" + s[len(s)-4:]
}
