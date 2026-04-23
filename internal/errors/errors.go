package errors

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// Error codes that can be returned to clients.
const (
	// Client errors (4xx)
	CodeValidation      = "VALIDATION_ERROR"
	CodeUnauthorized    = "UNAUTHORIZED"
	CodeForbidden       = "FORBIDDEN"
	CodeNotFound        = "NOT_FOUND"
	CodeConflict        = "CONFLICT"
	CodeRateLimited     = "RATE_LIMITED"
	CodePaymentRequired = "PAYMENT_REQUIRED"
	CodeBadRequest      = "BAD_REQUEST"

	// Server errors (5xx)
	CodeInternal    = "INTERNAL_ERROR"
	CodeDatabase    = "DATABASE_ERROR"
	CodeAIService   = "AI_SERVICE_ERROR"
	CodeExternal    = "EXTERNAL_SERVICE_ERROR"
	CodeTimeout     = "TIMEOUT"
	CodeUnavailable = "SERVICE_UNAVAILABLE"
)

// AppError is a structured error type for the application.
type AppError struct {
	// Code is a machine-readable error code.
	Code string `json:"code"`
	// Message is a human-readable error message.
	Message string `json:"message"`
	// Details contains additional context about the error.
	Details map[string]any `json:"details,omitempty"`
	// HTTPStatus is the HTTP status code to return.
	HTTPStatus int `json:"-"`
	// Err is the underlying error (not exposed to clients).
	Err error `json:"-"`
}

// Error implements the error interface.
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s (%v)", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// Unwrap returns the underlying error.
func (e *AppError) Unwrap() error {
	return e.Err
}

// WithDetail adds a detail field to the error.
func (e *AppError) WithDetail(key string, value any) *AppError {
	if e.Details == nil {
		e.Details = make(map[string]any)
	}
	e.Details[key] = value
	return e
}

// WithError wraps an underlying error.
func (e *AppError) WithError(err error) *AppError {
	e.Err = err
	return e
}

// ── Constructors ─────────────────────────────────────────────────────────────

// NewValidationError creates a validation error.
func NewValidationError(message string) *AppError {
	return &AppError{
		Code:       CodeValidation,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
	}
}

// NewValidationErrorWithField creates a validation error for a specific field.
func NewValidationErrorWithField(field, reason string) *AppError {
	return &AppError{
		Code:       CodeValidation,
		Message:    fmt.Sprintf("Invalid %s: %s", field, reason),
		HTTPStatus: http.StatusBadRequest,
		Details:    map[string]any{"field": field, "reason": reason},
	}
}

// NewUnauthorizedError creates an unauthorized error.
func NewUnauthorizedError(message string) *AppError {
	if message == "" {
		message = "Authentication required"
	}
	return &AppError{
		Code:       CodeUnauthorized,
		Message:    message,
		HTTPStatus: http.StatusUnauthorized,
	}
}

// NewForbiddenError creates a forbidden error.
func NewForbiddenError(message string) *AppError {
	if message == "" {
		message = "Access denied"
	}
	return &AppError{
		Code:       CodeForbidden,
		Message:    message,
		HTTPStatus: http.StatusForbidden,
	}
}

// NewNotFoundError creates a not found error.
func NewNotFoundError(resource string) *AppError {
	return &AppError{
		Code:       CodeNotFound,
		Message:    fmt.Sprintf("%s not found", resource),
		HTTPStatus: http.StatusNotFound,
		Details:    map[string]any{"resource": resource},
	}
}

// NewConflictError creates a conflict error.
func NewConflictError(message string) *AppError {
	return &AppError{
		Code:       CodeConflict,
		Message:    message,
		HTTPStatus: http.StatusConflict,
	}
}

// NewRateLimitedError creates a rate limit error.
func NewRateLimitedError(retryAfterSeconds int) *AppError {
	return &AppError{
		Code:       CodeRateLimited,
		Message:    "Too many requests, please try again later",
		HTTPStatus: http.StatusTooManyRequests,
		Details:    map[string]any{"retry_after_seconds": retryAfterSeconds},
	}
}

// NewPaymentRequiredError creates a payment required error.
func NewPaymentRequiredError() *AppError {
	return &AppError{
		Code:       CodePaymentRequired,
		Message:    "Subscription required to access this feature",
		HTTPStatus: http.StatusPaymentRequired,
	}
}

// NewBadRequestError creates a bad request error.
func NewBadRequestError(message string) *AppError {
	return &AppError{
		Code:       CodeBadRequest,
		Message:    message,
		HTTPStatus: http.StatusBadRequest,
	}
}

// NewInternalError creates an internal server error.
// The underlying error is logged but not exposed to clients.
func NewInternalError(err error) *AppError {
	return &AppError{
		Code:       CodeInternal,
		Message:    "An internal error occurred",
		HTTPStatus: http.StatusInternalServerError,
		Err:        err,
	}
}

// NewDatabaseError creates a database error.
func NewDatabaseError(err error) *AppError {
	return &AppError{
		Code:       CodeDatabase,
		Message:    "A database error occurred",
		HTTPStatus: http.StatusInternalServerError,
		Err:        err,
	}
}

// NewAIServiceError creates an AI service error.
func NewAIServiceError(err error) *AppError {
	return &AppError{
		Code:       CodeAIService,
		Message:    "AI service is temporarily unavailable",
		HTTPStatus: http.StatusBadGateway,
		Err:        err,
	}
}

// NewTimeoutError creates a timeout error.
func NewTimeoutError(operation string) *AppError {
	return &AppError{
		Code:       CodeTimeout,
		Message:    fmt.Sprintf("The %s operation timed out", operation),
		HTTPStatus: http.StatusGatewayTimeout,
		Details:    map[string]any{"operation": operation},
	}
}

// NewServiceUnavailableError creates a service unavailable error.
func NewServiceUnavailableError(service string) *AppError {
	return &AppError{
		Code:       CodeUnavailable,
		Message:    fmt.Sprintf("The %s service is temporarily unavailable", service),
		HTTPStatus: http.StatusServiceUnavailable,
		Details:    map[string]any{"service": service},
	}
}

// ── Response helpers ─────────────────────────────────────────────────────────

// ErrorResponse is the JSON structure returned to clients.
type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

// ErrorBody contains the error details.
type ErrorBody struct {
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

// WriteError writes an AppError as a JSON response.
func WriteError(w http.ResponseWriter, appErr *AppError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(appErr.HTTPStatus)

	resp := ErrorResponse{
		Error: ErrorBody{
			Code:    appErr.Code,
			Message: appErr.Message,
			Details: appErr.Details,
		},
	}
	json.NewEncoder(w).Encode(resp)
}

// WriteErrorFromErr converts a standard error to an AppError and writes it.
// If the error is already an AppError, it uses that directly.
// Otherwise, it creates an internal error.
func WriteErrorFromErr(w http.ResponseWriter, err error) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		WriteError(w, appErr)
		return
	}
	WriteError(w, NewInternalError(err))
}

// IsAppError checks if an error is an AppError and returns it.
func IsAppError(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// IsNotFound checks if an error is a not found error.
func IsNotFound(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == CodeNotFound
	}
	return false
}

// IsValidation checks if an error is a validation error.
func IsValidation(err error) bool {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr.Code == CodeValidation
	}
	return false
}
