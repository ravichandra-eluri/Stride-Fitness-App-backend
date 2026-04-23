package cron

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"stride/backend/internal/ai"
	"stride/backend/internal/db"
	"stride/backend/internal/logger"
	"stride/backend/internal/metrics"
	"stride/backend/internal/push"

	"github.com/robfig/cron/v3"
)

// Deps contains dependencies for cron jobs.
type Deps struct {
	DB          *db.DB
	AIClient    *ai.ResilientClient
	PushService *push.APNsService
	Logger      *logger.Logger
	Metrics     *metrics.Collector
}

// Scheduler manages background cron jobs.
type Scheduler struct {
	deps    Deps
	cron    *cron.Cron
	running bool
	mu      sync.Mutex
}

// New creates a new cron scheduler.
func New(deps Deps) *Scheduler {
	c := cron.New(cron.WithLocation(time.UTC))
	return &Scheduler{
		deps: deps,
		cron: c,
	}
}

// Start begins the cron scheduler.
func (s *Scheduler) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return
	}

	// Weekly meal plan generation — every Monday at 6:00 AM UTC
	// Cron expression: "0 6 * * 1" = minute 0, hour 6, any day, any month, Monday
	_, err := s.cron.AddFunc("0 6 * * 1", func() {
		s.generateWeeklyMealPlans()
	})
	if err != nil {
		s.deps.Logger.Error("failed to add weekly meal plan job", "error", err)
	}

	// Daily coach messages — every day at 7:00 AM UTC
	// Cron expression: "0 7 * * *" = minute 0, hour 7, any day, any month, any weekday
	_, err = s.cron.AddFunc("0 7 * * *", func() {
		s.generateDailyCoachMessages()
	})
	if err != nil {
		s.deps.Logger.Error("failed to add daily coach job", "error", err)
	}

	// Cleanup expired cache entries — every hour
	_, err = s.cron.AddFunc("0 * * * *", func() {
		s.cleanupExpiredData()
	})
	if err != nil {
		s.deps.Logger.Error("failed to add cleanup job", "error", err)
	}

	// Health metrics collection — every 5 minutes
	_, err = s.cron.AddFunc("*/5 * * * *", func() {
		s.collectHealthMetrics()
	})
	if err != nil {
		s.deps.Logger.Error("failed to add health metrics job", "error", err)
	}

	s.cron.Start()
	s.running = true
	s.deps.Logger.Info("cron scheduler started",
		"jobs", len(s.cron.Entries()),
	)
}

// Stop gracefully stops the cron scheduler.
func (s *Scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running {
		return
	}

	ctx := s.cron.Stop()
	<-ctx.Done()
	s.running = false
	s.deps.Logger.Info("cron scheduler stopped")
}

// ────────────────────────────────────────────────────────────────────────────
// Job: Generate Weekly Meal Plans
// ────────────────────────────────────────────────────────────────────────────
// Runs every Monday at 6am UTC. Generates a fresh 7-day meal plan for every
// active subscriber. Uses a worker pool to control concurrency.

func (s *Scheduler) generateWeeklyMealPlans() {
	ctx := context.Background()
	startTime := time.Now()
	log := s.deps.Logger.With("job", "weekly_meal_plans")

	log.Info("starting weekly meal plan generation")

	// Get all active subscribed users
	userIDs, err := s.deps.DB.GetActiveSubscribedUserIDs(ctx)
	if err != nil {
		log.Error("failed to get active users", "error", err)
		s.deps.Metrics.CronJobFailed("weekly_meal_plans")
		return
	}

	if len(userIDs) == 0 {
		log.Info("no active users found, skipping")
		return
	}

	weekLabel := currentWeekLabel()
	weekStart := weekStartDate()

	// Worker pool — limit concurrent AI calls
	const maxWorkers = 10
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	var successCount, failCount int64
	var mu sync.Mutex

	for _, userID := range userIDs {
		wg.Add(1)
		sem <- struct{}{} // acquire

		go func(uid string) {
			defer wg.Done()
			defer func() { <-sem }() // release

			jobCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()

			if err := s.generateMealPlanForUser(jobCtx, uid, weekLabel, weekStart); err != nil {
				log.Warn("meal plan generation failed",
					"user_id", uid,
					"error", err,
				)
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			mu.Lock()
			successCount++
			mu.Unlock()
		}(userID)
	}

	wg.Wait()

	duration := time.Since(startTime)
	log.Info("weekly meal plan generation complete",
		"total_users", len(userIDs),
		"success", successCount,
		"failed", failCount,
		"duration", duration,
	)

	s.deps.Metrics.CronJobCompleted("weekly_meal_plans", duration)
	s.deps.Metrics.CronJobProcessed("weekly_meal_plans", float64(successCount))
}

func (s *Scheduler) generateMealPlanForUser(ctx context.Context, userID, weekLabel string, weekStart time.Time) error {
	// Get user profile
	profile, err := s.deps.DB.GetProfile(ctx, userID)
	if err != nil || profile == nil {
		return err
	}

	// Check if they already have a plan for this week
	existing, _ := s.deps.DB.GetMealPlanByWeek(ctx, userID, weekLabel)
	if existing != nil {
		return nil // already has plan
	}

	// Generate meal plan via AI
	aiProfile := dbProfileToAI(profile)
	plan, err := s.deps.AIClient.GenerateWeeklyMealPlan(ctx, aiProfile, weekLabel)
	if err != nil {
		return err
	}

	// Save to database
	daysJSON, err := json.Marshal(plan.Days)
	if err != nil {
		return err
	}

	mealPlan := &db.MealPlan{
		UserID:           userID,
		WeekLabel:        plan.Week,
		WeekStartDate:    weekStart,
		DaysJSON:         daysJSON,
		AvgDailyCalories: plan.AvgDailyCalories,
	}

	return s.deps.DB.SaveMealPlan(ctx, mealPlan)
}

// ────────────────────────────────────────────────────────────────────────────
// Job: Generate Daily Coach Messages
// ────────────────────────────────────────────────────────────────────────────
// Runs every day at 7am UTC. Generates a personalized motivational message
// and sends a push notification to each active user.

func (s *Scheduler) generateDailyCoachMessages() {
	ctx := context.Background()
	startTime := time.Now()
	log := s.deps.Logger.With("job", "daily_coach_messages")

	log.Info("starting daily coach message generation")

	userIDs, err := s.deps.DB.GetActiveSubscribedUserIDs(ctx)
	if err != nil {
		log.Error("failed to get active users", "error", err)
		s.deps.Metrics.CronJobFailed("daily_coach_messages")
		return
	}

	if len(userIDs) == 0 {
		log.Info("no active users found, skipping")
		return
	}

	// Higher concurrency for coach messages (lighter AI calls)
	const maxWorkers = 20
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	var successCount, failCount, pushSent int64
	var mu sync.Mutex

	for _, userID := range userIDs {
		wg.Add(1)
		sem <- struct{}{}

		go func(uid string) {
			defer wg.Done()
			defer func() { <-sem }()

			jobCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()

			sent, err := s.generateCoachMessageForUser(jobCtx, uid)
			if err != nil {
				log.Warn("coach message generation failed",
					"user_id", uid,
					"error", err,
				)
				mu.Lock()
				failCount++
				mu.Unlock()
				return
			}

			mu.Lock()
			successCount++
			if sent {
				pushSent++
			}
			mu.Unlock()
		}(userID)
	}

	wg.Wait()

	duration := time.Since(startTime)
	log.Info("daily coach message generation complete",
		"total_users", len(userIDs),
		"success", successCount,
		"failed", failCount,
		"push_sent", pushSent,
		"duration", duration,
	)

	s.deps.Metrics.CronJobCompleted("daily_coach_messages", duration)
	s.deps.Metrics.CronJobProcessed("daily_coach_messages", float64(successCount))
}

func (s *Scheduler) generateCoachMessageForUser(ctx context.Context, userID string) (pushSent bool, err error) {
	// Get user profile
	profile, err := s.deps.DB.GetProfile(ctx, userID)
	if err != nil || profile == nil {
		return false, err
	}

	// Get yesterday's stats
	yesterday, err := s.deps.DB.GetYesterdayStats(ctx, userID)
	if err != nil {
		// Use empty stats if none available
		yesterday = &db.YesterdayStats{}
	}
	yesterday.CalorieTarget = profile.CalorieTarget

	// Generate message via AI
	aiProfile := dbProfileToAI(profile)
	aiYesterday := ai.YesterdayStats{
		CaloriesEaten:     yesterday.CaloriesEaten,
		CalorieTarget:     yesterday.CalorieTarget,
		CurrentStreakDays: yesterday.CurrentStreakDays,
		TotalLostKg:       yesterday.TotalLostKg,
	}

	msg, err := s.deps.AIClient.GenerateDailyCoach(ctx, aiProfile, aiYesterday)
	if err != nil {
		return false, err
	}

	// Save to database
	dbMsg := &db.CoachMessage{
		UserID:       userID,
		Message:      msg.Message,
		Tip:          msg.Tip,
		PriorityMeal: msg.PriorityMeal,
		Tone:         msg.Tone,
	}
	if err := s.deps.DB.SaveCoachMessage(ctx, dbMsg); err != nil {
		return false, err
	}

	// Send push notifications
	if s.deps.PushService != nil {
		tokens, _ := s.deps.DB.GetDeviceTokens(ctx, userID)
		if len(tokens) > 0 {
			notification := &push.Notification{
				Title:    "Your Daily Coach",
				Body:     truncateMessage(msg.Message, 100),
				Badge:    1,
				Sound:    "default",
				Category: "COACH_MESSAGE",
				Data: map[string]interface{}{
					"type":    "coach_message",
					"user_id": userID,
				},
			}

			for _, token := range tokens {
				if err := s.deps.PushService.Send(ctx, token, notification); err != nil {
					s.deps.Logger.Warn("push notification failed",
						"user_id", userID,
						"error", err,
					)
				} else {
					pushSent = true
				}
			}
		}
	}

	return pushSent, nil
}

// ────────────────────────────────────────────────────────────────────────────
// Job: Cleanup Expired Data
// ────────────────────────────────────────────────────────────────────────────

func (s *Scheduler) cleanupExpiredData() {
	ctx := context.Background()
	log := s.deps.Logger.With("job", "cleanup")

	// Clean up old device tokens (inactive for 90 days)
	deleted, err := s.deps.DB.DeleteExpiredDeviceTokens(ctx, 90*24*time.Hour)
	if err != nil {
		log.Error("failed to cleanup device tokens", "error", err)
	} else if deleted > 0 {
		log.Info("cleaned up expired device tokens", "count", deleted)
	}

	// Clean up old coach messages (older than 30 days)
	deleted, err = s.deps.DB.DeleteOldCoachMessages(ctx, 30*24*time.Hour)
	if err != nil {
		log.Error("failed to cleanup coach messages", "error", err)
	} else if deleted > 0 {
		log.Info("cleaned up old coach messages", "count", deleted)
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Job: Collect Health Metrics
// ────────────────────────────────────────────────────────────────────────────

func (s *Scheduler) collectHealthMetrics() {
	ctx := context.Background()

	// Get database stats
	stats := s.deps.DB.Stats()
	s.deps.Metrics.SetDBConnections(float64(stats.OpenConnections))
	s.deps.Metrics.SetDBIdleConnections(float64(stats.Idle))
	s.deps.Metrics.SetDBInUseConnections(float64(stats.InUse))

	// Get active user count
	count, err := s.deps.DB.GetActiveUserCount(ctx)
	if err == nil {
		s.deps.Metrics.SetActiveUsers(float64(count))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Helpers
// ────────────────────────────────────────────────────────────────────────────

func dbProfileToAI(p *db.Profile) ai.UserProfile {
	return ai.UserProfile{
		Name:            p.Name,
		Age:             p.Age,
		Gender:          p.Gender,
		HeightCm:        p.HeightCm,
		CurrentWeightKg: p.CurrentWeightKg,
		GoalWeightKg:    p.GoalWeightKg,
		TimelineMonths:  p.TimelineMonths,
		ActivityLevel:   p.ActivityLevel,
		DailyMinutes:    p.DailyMinutes,
		DietPrefs:       p.DietPrefs,
		CalorieTarget:   p.CalorieTarget,
	}
}

func currentWeekLabel() string {
	now := time.Now().UTC()
	// Find Monday of current week
	offset := int(time.Monday - now.Weekday())
	if offset > 0 {
		offset -= 7
	}
	start := now.AddDate(0, 0, offset)
	end := start.AddDate(0, 0, 6)
	return start.Format("Jan 2") + " – " + end.Format("Jan 2")
}

func weekStartDate() time.Time {
	now := time.Now().UTC()
	offset := int(time.Monday - now.Weekday())
	if offset > 0 {
		offset -= 7
	}
	start := now.AddDate(0, 0, offset)
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC)
}

func truncateMessage(msg string, maxLen int) string {
	if len(msg) <= maxLen {
		return msg
	}
	return msg[:maxLen-3] + "..."
}
