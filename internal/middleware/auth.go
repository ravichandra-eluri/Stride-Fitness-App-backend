package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"stride/backend/internal/errors"
	"stride/backend/internal/logger"

	"github.com/golang-jwt/jwt/v5"
)

// Context keys for user information
type contextKey string

const (
	UserIDKey    contextKey = "user_id"
	UserEmailKey contextKey = "user_email"
	TokenKey     contextKey = "token"
)

// Claims represents the JWT claims structure.
type Claims struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
	jwt.RegisteredClaims
}

// RequireAuth middleware validates JWT tokens and adds user info to context.
func RequireAuth(secret []byte, log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract token from Authorization header
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				errors.WriteJSON(w, errors.Unauthorized("missing authorization header"))
				return
			}

			// Expect "Bearer <token>"
			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				errors.WriteJSON(w, errors.Unauthorized("invalid authorization format"))
				return
			}
			tokenString := parts[1]

			// Parse and validate token
			token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
				// Validate signing method
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
				}
				return secret, nil
			})

			if err != nil {
				log.Debug("token validation failed", "error", err)
				errors.WriteJSON(w, errors.Unauthorized("invalid or expired token"))
				return
			}

			claims, ok := token.Claims.(*Claims)
			if !ok || !token.Valid {
				errors.WriteJSON(w, errors.Unauthorized("invalid token claims"))
				return
			}

			// Check for user_id in custom claim or subject
			userID := claims.UserID
			if userID == "" {
				userID = claims.Subject
			}
			if userID == "" {
				errors.WriteJSON(w, errors.Unauthorized("missing user_id in token"))
				return
			}

			// Add user info to context
			ctx := context.WithValue(r.Context(), UserIDKey, userID)
			ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
			ctx = context.WithValue(ctx, TokenKey, tokenString)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OptionalAuth middleware extracts user info if present but doesn't require it.
func OptionalAuth(secret []byte) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				next.ServeHTTP(w, r)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				next.ServeHTTP(w, r)
				return
			}

			token, err := jwt.ParseWithClaims(parts[1], &Claims{}, func(token *jwt.Token) (interface{}, error) {
				if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
					return nil, fmt.Errorf("unexpected signing method")
				}
				return secret, nil
			})

			if err != nil || !token.Valid {
				next.ServeHTTP(w, r)
				return
			}

			if claims, ok := token.Claims.(*Claims); ok {
				userID := claims.UserID
				if userID == "" {
					userID = claims.Subject
				}
				if userID != "" {
					ctx := context.WithValue(r.Context(), UserIDKey, userID)
					ctx = context.WithValue(ctx, UserEmailKey, claims.Email)
					ctx = context.WithValue(ctx, TokenKey, parts[1])
					r = r.WithContext(ctx)
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// GetUserID retrieves the user ID from context.
func GetUserID(ctx context.Context) (string, bool) {
	userID, ok := ctx.Value(UserIDKey).(string)
	return userID, ok && userID != ""
}

// UserIDFromCtx extracts the userID set by RequireAuth (backwards compatible).
func UserIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(UserIDKey).(string)
	return v
}

// MustGetUserID retrieves the user ID from context or panics.
func MustGetUserID(ctx context.Context) string {
	userID, ok := GetUserID(ctx)
	if !ok {
		panic("user_id not found in context - RequireAuth middleware missing?")
	}
	return userID
}

// GetUserEmail retrieves the user email from context.
func GetUserEmail(ctx context.Context) (string, bool) {
	email, ok := ctx.Value(UserEmailKey).(string)
	return email, ok && email != ""
}

// SubscriptionChecker interface for checking subscription status.
type SubscriptionChecker interface {
	IsSubscribed(ctx context.Context, userID string) (bool, error)
}

// RequireSubscription middleware checks if user has an active subscription.
// Must be used after RequireAuth.
func RequireSubscription(checker SubscriptionChecker, log *logger.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID, ok := GetUserID(r.Context())
			if !ok {
				errors.WriteJSON(w, errors.Unauthorized("authentication required"))
				return
			}

			subscribed, err := checker.IsSubscribed(r.Context(), userID)
			if err != nil {
				log.Error("subscription check failed", "user_id", userID, "error", err)
				errors.WriteJSON(w, errors.Internal("failed to verify subscription"))
				return
			}

			if !subscribed {
				errors.WriteJSON(w, errors.PaymentRequired("active subscription required"))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CORSOptions configures CORS middleware.
type CORSOptions struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           int // seconds
}

// CORS middleware handles Cross-Origin Resource Sharing.
func CORS(opts CORSOptions) func(http.Handler) http.Handler {
	// Build allowed origins map for O(1) lookup
	allowedOrigins := make(map[string]bool)
	allowAll := false
	for _, origin := range opts.AllowedOrigins {
		if origin == "*" {
			allowAll = true
			break
		}
		allowedOrigins[origin] = true
	}

	methods := strings.Join(opts.AllowedMethods, ", ")
	headers := strings.Join(opts.AllowedHeaders, ", ")
	exposed := strings.Join(opts.ExposedHeaders, ", ")
	maxAge := fmt.Sprintf("%d", opts.MaxAge)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// Check if origin is allowed
			if origin != "" {
				if allowAll {
					w.Header().Set("Access-Control-Allow-Origin", "*")
				} else if allowedOrigins[origin] {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Vary", "Origin")
				}
			}

			if opts.AllowCredentials && !allowAll {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}

			if exposed != "" {
				w.Header().Set("Access-Control-Expose-Headers", exposed)
			}

			// Handle preflight requests
			if r.Method == http.MethodOptions {
				w.Header().Set("Access-Control-Allow-Methods", methods)
				w.Header().Set("Access-Control-Allow-Headers", headers)
				if opts.MaxAge > 0 {
					w.Header().Set("Access-Control-Max-Age", maxAge)
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
