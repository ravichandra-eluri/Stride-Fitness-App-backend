package push

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/sideshow/apns2"
	"github.com/sideshow/apns2/payload"
	"github.com/sideshow/apns2/token"

	"stride/backend/internal/logger"
	"stride/backend/internal/metrics"
)

// Config holds APNs configuration.
type Config struct {
	// KeyID is the Apple Key ID from App Store Connect.
	KeyID string
	// TeamID is the Apple Team ID.
	TeamID string
	// BundleID is the app's bundle identifier.
	BundleID string
	// KeyPath is the path to the .p8 private key file.
	KeyPath string
	// Production indicates whether to use production or sandbox APNs.
	Production bool
}

// APNsClient wraps the APNs client with retry and metrics.
type APNsClient struct {
	client     *apns2.Client
	bundleID   string
	log        *logger.Logger
	metrics    *metrics.Metrics
	production bool
}

// NewAPNsClient creates a new APNs client.
func NewAPNsClient(cfg Config, log *logger.Logger, m *metrics.Metrics) (*APNsClient, error) {
	// Load the .p8 key file
	keyBytes, err := os.ReadFile(cfg.KeyPath)
	if err != nil {
		return nil, fmt.Errorf("read APNs key file: %w", err)
	}

	// Parse the .p8 key
	block, _ := pem.Decode(keyBytes)
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block from APNs key")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse APNs private key: %w", err)
	}

	ecdsaKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("APNs key is not ECDSA")
	}

	// Create the token
	authToken := &token.Token{
		AuthKey: ecdsaKey,
		KeyID:   cfg.KeyID,
		TeamID:  cfg.TeamID,
	}

	// Create the client
	var client *apns2.Client
	if cfg.Production {
		client = apns2.NewTokenClient(authToken).Production()
	} else {
		client = apns2.NewTokenClient(authToken).Development()
	}

	return &APNsClient{
		client:     client,
		bundleID:   cfg.BundleID,
		log:        log,
		metrics:    m,
		production: cfg.Production,
	}, nil
}

// Notification represents a push notification to be sent.
type Notification struct {
	// DeviceToken is the APNs device token.
	DeviceToken string
	// Title is the alert title.
	Title string
	// Body is the alert body.
	Body string
	// Badge is the badge number (0 to clear).
	Badge *int
	// Sound is the notification sound.
	Sound string
	// Data is custom data to include.
	Data map[string]interface{}
	// Category is the notification category for actions.
	Category string
	// ThreadID groups notifications together.
	ThreadID string
	// CollapseID for notification coalescing.
	CollapseID string
	// Priority (5 = low, 10 = high)
	Priority int
	// Expiration is when the notification expires.
	Expiration time.Time
}

// DefaultNotification returns a notification with sensible defaults.
func DefaultNotification(deviceToken, title, body string) *Notification {
	return &Notification{
		DeviceToken: deviceToken,
		Title:       title,
		Body:        body,
		Sound:       "default",
		Priority:    10,
	}
}

// Send sends a push notification.
func (c *APNsClient) Send(ctx context.Context, n *Notification) error {
	// Build the payload
	p := payload.NewPayload()

	if n.Title != "" || n.Body != "" {
		p.Alert(&payload.Alert{
			Title: n.Title,
			Body:  n.Body,
		})
	}

	if n.Badge != nil {
		p.Badge(*n.Badge)
	}

	if n.Sound != "" {
		p.Sound(n.Sound)
	}

	if n.Category != "" {
		p.Category(n.Category)
	}

	if n.ThreadID != "" {
		p.ThreadID(n.ThreadID)
	}

	// Add custom data
	for key, value := range n.Data {
		p.Custom(key, value)
	}

	// Create the notification
	notification := &apns2.Notification{
		DeviceToken: n.DeviceToken,
		Topic:       c.bundleID,
		Payload:     p,
		Priority:    n.Priority,
	}

	if n.CollapseID != "" {
		notification.CollapseID = n.CollapseID
	}

	if !n.Expiration.IsZero() {
		notification.Expiration = n.Expiration
	}

	// Send the notification
	res, err := c.client.PushWithContext(ctx, notification)
	if err != nil {
		c.log.WithError(err).Error("APNs push failed",
			"device_token", maskToken(n.DeviceToken),
		)
		if c.metrics != nil {
			c.metrics.RecordPushNotification(false)
		}
		return fmt.Errorf("APNs push: %w", err)
	}

	// Check response
	if !res.Sent() {
		c.log.Warn("APNs push rejected",
			"device_token", maskToken(n.DeviceToken),
			"reason", res.Reason,
		)
		if c.metrics != nil {
			c.metrics.RecordPushNotification(false)
		}
		return &APNsError{
			StatusCode: res.StatusCode,
			Reason:     res.Reason,
		}
	}

	c.log.Debug("APNs push sent",
		"device_token", maskToken(n.DeviceToken),
		"apns_id", res.ApnsID,
	)
	if c.metrics != nil {
		c.metrics.RecordPushNotification(true)
	}

	return nil
}

// SendBatch sends multiple notifications concurrently with rate limiting.
func (c *APNsClient) SendBatch(ctx context.Context, notifications []*Notification, concurrency int) []error {
	if concurrency <= 0 {
		concurrency = 10
	}

	errors := make([]error, len(notifications))
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for i, n := range notifications {
		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, notif *Notification) {
			defer wg.Done()
			defer func() { <-sem }()

			errors[idx] = c.Send(ctx, notif)
		}(i, n)
	}

	wg.Wait()
	return errors
}

// APNsError represents an error from APNs.
type APNsError struct {
	StatusCode int
	Reason     string
}

func (e *APNsError) Error() string {
	return fmt.Sprintf("APNs error %d: %s", e.StatusCode, e.Reason)
}

// IsInvalidToken returns true if the device token is invalid or unregistered.
func (e *APNsError) IsInvalidToken() bool {
	return e.Reason == apns2.ReasonBadDeviceToken ||
		e.Reason == apns2.ReasonUnregistered ||
		e.Reason == apns2.ReasonDeviceTokenNotForTopic
}

// maskToken masks a device token for logging (show first and last 8 chars).
func maskToken(token string) string {
	if len(token) <= 16 {
		return "***"
	}
	return token[:8] + "..." + token[len(token)-8:]
}

// ── Convenience functions ───────────────────────────────────────────────────

// SendCoachMessage sends a daily coach message notification.
func (c *APNsClient) SendCoachMessage(ctx context.Context, deviceToken, message string) error {
	n := DefaultNotification(deviceToken, "Your Daily Coach", message)
	n.Category = "COACH_MESSAGE"
	n.ThreadID = "coach"
	return c.Send(ctx, n)
}

// SendMealReminder sends a meal logging reminder.
func (c *APNsClient) SendMealReminder(ctx context.Context, deviceToken, mealType string) error {
	titles := map[string]string{
		"breakfast": "Good morning!",
		"lunch":     "Lunchtime!",
		"dinner":    "Dinner time!",
		"snack":     "Snack time!",
	}

	title := titles[mealType]
	if title == "" {
		title = "Meal reminder"
	}

	n := DefaultNotification(deviceToken, title, "Don't forget to log your meal!")
	n.Category = "MEAL_REMINDER"
	n.Data = map[string]interface{}{"meal_type": mealType}
	return c.Send(ctx, n)
}

// SendWeeklyMealPlanReady notifies user that their weekly meal plan is ready.
func (c *APNsClient) SendWeeklyMealPlanReady(ctx context.Context, deviceToken, weekLabel string) error {
	n := DefaultNotification(
		deviceToken,
		"New Meal Plan Ready!",
		fmt.Sprintf("Your meal plan for %s is ready to view.", weekLabel),
	)
	n.Category = "MEAL_PLAN"
	n.Badge = intPtr(1)
	return c.Send(ctx, n)
}

// SendProgressMilestone notifies user of a progress milestone.
func (c *APNsClient) SendProgressMilestone(ctx context.Context, deviceToken, milestone string) error {
	n := DefaultNotification(deviceToken, "Milestone Achieved!", milestone)
	n.Category = "MILESTONE"
	n.Sound = "celebration.caf"
	return c.Send(ctx, n)
}

// ClearBadge clears the app badge.
func (c *APNsClient) ClearBadge(ctx context.Context, deviceToken string) error {
	n := &Notification{
		DeviceToken: deviceToken,
		Badge:       intPtr(0),
	}
	return c.Send(ctx, n)
}

func intPtr(i int) *int {
	return &i
}

// ── Mock client for testing ─────────────────────────────────────────────────

// MockAPNsClient is a mock APNs client for testing.
type MockAPNsClient struct {
	Notifications []*Notification
	ShouldFail    bool
	mu            sync.Mutex
}

// NewMockAPNsClient creates a new mock APNs client.
func NewMockAPNsClient() *MockAPNsClient {
	return &MockAPNsClient{
		Notifications: make([]*Notification, 0),
	}
}

// Send records the notification for testing.
func (m *MockAPNsClient) Send(ctx context.Context, n *Notification) error {
	if m.ShouldFail {
		return &APNsError{StatusCode: 500, Reason: "mock failure"}
	}

	m.mu.Lock()
	m.Notifications = append(m.Notifications, n)
	m.mu.Unlock()

	return nil
}

// LastNotification returns the last sent notification.
func (m *MockAPNsClient) LastNotification() *Notification {
	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.Notifications) == 0 {
		return nil
	}
	return m.Notifications[len(m.Notifications)-1]
}

// Clear clears all recorded notifications.
func (m *MockAPNsClient) Clear() {
	m.mu.Lock()
	m.Notifications = make([]*Notification, 0)
	m.mu.Unlock()
}
