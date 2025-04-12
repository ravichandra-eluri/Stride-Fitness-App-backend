package cron

import (
	"context"
	"encoding/json"
	"log"
	"sync"
	"time"

	ai "stride/backend"
	"stride/backend/internal/db"
	"stride/backend/internal/handlers"
)

// Scheduler runs background jobs on a timer.
type Scheduler struct {
	deps    handlers.Deps
	tickers []*time.Ticker
	done    chan struct{}
}

func New(deps handlers.Deps) *Scheduler {
	return &Scheduler{deps: deps, done: make(chan struct{})}
}

func (s *Scheduler) Start() {
	// Weekly meal plan — every Monday at 6:00 AM UTC
	go s.runAt(time.Monday, 6, 0, s.generateWeeklyMealPlans)

	// Daily coach message — every day at 7:00 AM UTC
	go s.runDaily(7, 0, s.generateDailyCoachMessages)

	log.Println("cron: scheduler started")
}

func (s *Scheduler) Stop() {
	close(s.done)
}

// runAt runs a job on a specific weekday at a specific hour:minute UTC.
func (s *Scheduler) runAt(weekday time.Weekday, hour, min int, job func()) {
	for {
		now := time.Now().UTC()
		next := nextWeekday(now, weekday, hour, min)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-timer.C:
			job()
		case <-s.done:
			timer.Stop()
			return
		}
	}
}

// runDaily runs a job every day at a specific hour:minute UTC.
func (s *Scheduler) runDaily(hour, min int, job func()) {
	for {
		now := time.Now().UTC()
		next := nextDaily(now, hour, min)
		timer := time.NewTimer(time.Until(next))
		select {
		case <-timer.C:
			job()
		case <-s.done:
			timer.Stop()
			return
		}
	}
}

// ── Job: Generate weekly meal plans ─────────────────────────────────────────
// Runs every Monday 6am. Generates a fresh 7-day plan for every active user.
// Uses a worker pool (10 concurrent) to avoid hammering Claude API.

func (s *Scheduler) generateWeeklyMealPlans() {
	ctx := context.Background()
	log.Println("cron: generating weekly meal plans...")

	userIDs, err := s.deps.DB.GetAllActiveUserIDs(ctx)
	if err != nil {
		log.Printf("cron: get users failed: %v", err)
		return
	}

	client := ai.NewClient(s.deps.ClaudeKey)
	weekLabel := currentWeekLabel()

	// Worker pool — 10 concurrent AI calls max
	sem := make(chan struct{}, 10)
	var wg sync.WaitGroup

	for _, userID := range userIDs {
		wg.Add(1)
		sem <- struct{}{}

		go func(uid string) {
			defer wg.Done()
			defer func() { <-sem }()

			profile, err := s.deps.DB.GetProfile(ctx, uid)
			if err != nil || profile == nil {
				return
			}

			aiProfile := dbProfileToAI(profile)
			plan, err := client.GenerateWeeklyMealPlan(ctx, aiProfile, weekLabel)
			if err != nil {
				log.Printf("cron: meal plan failed for %s: %v", uid, err)
				return
			}

			daysJSON, _ := json.Marshal(plan.Days)
			mealPlan := &db.MealPlan{
				UserID:           uid,
				WeekLabel:        plan.Week,
				WeekStartDate:    weekStart(),
				DaysJSON:         daysJSON,
				AvgDailyCalories: plan.AvgDailyCalories,
			}
			if err := s.deps.DB.SaveMealPlan(ctx, mealPlan); err != nil {
				log.Printf("cron: save plan failed for %s: %v", uid, err)
			}
		}(userID)
	}

	wg.Wait()
	log.Printf("cron: generated meal plans for %d users", len(userIDs))
}

// ── Job: Generate daily coach messages ──────────────────────────────────────
// Runs every day at 7am. Generates a personalized message + sends push notification.

func (s *Scheduler) generateDailyCoachMessages() {
	ctx := context.Background()
	log.Println("cron: generating daily coach messages...")

	userIDs, err := s.deps.DB.GetAllActiveUserIDs(ctx)
	if err != nil {
		log.Printf("cron: get users failed: %v", err)
		return
	}

	client := ai.NewClient(s.deps.ClaudeKey)

	sem := make(chan struct{}, 20) // coach messages are cheaper, allow more concurrency
	var wg sync.WaitGroup

	for _, userID := range userIDs {
		wg.Add(1)
		sem <- struct{}{}

		go func(uid string) {
			defer wg.Done()
			defer func() { <-sem }()

			profile, err := s.deps.DB.GetProfile(ctx, uid)
			if err != nil || profile == nil {
				return
			}

			yesterday, err := s.deps.DB.GetYesterdayStats(ctx, uid)
			if err != nil {
				return
			}
			yesterday.CalorieTarget = profile.CalorieTarget

			aiProfile := dbProfileToAI(profile)
			aiYesterday := ai.YesterdayStats{
				CaloriesEaten:     yesterday.CaloriesEaten,
				CalorieTarget:     yesterday.CalorieTarget,
				CurrentStreakDays: yesterday.CurrentStreakDays,
				TotalLostKg:       yesterday.TotalLostKg,
			}

			msg, err := client.GenerateDailyCoach(ctx, aiProfile, aiYesterday)
			if err != nil {
				log.Printf("cron: coach failed for %s: %v", uid, err)
				return
			}

			dbMsg := &db.CoachMessage{
				UserID:       uid,
				Message:      msg.Message,
				Tip:          msg.Tip,
				PriorityMeal: msg.PriorityMeal,
				Tone:         msg.Tone,
			}
			if err := s.deps.DB.SaveCoachMessage(ctx, dbMsg); err != nil {
				log.Printf("cron: save coach msg failed for %s: %v", uid, err)
				return
			}

			// Send iOS push notification
			tokens, _ := s.deps.DB.GetDeviceTokens(ctx, uid)
			for _, token := range tokens {
				if err := sendPush(s.deps, token, msg.Message); err != nil {
					log.Printf("cron: push failed for token %s: %v", token[:8], err)
				}
			}
		}(userID)
	}

	wg.Wait()
	log.Printf("cron: generated coach messages for %d users", len(userIDs))
}

// ── APNs push notification ───────────────────────────────────────────────────
// Sends an iOS push notification via Apple Push Notification service.
// Uses token-based auth (.p8 key file) — no certificate renewal needed.

func sendPush(d handlers.Deps, deviceToken, message string) error {
	// In production: use a proper APNs library like github.com/sideshow/apns2
	// The call looks like:
	//
	// client := apns2.NewTokenClient(token).Production()
	// notification := &apns2.Notification{
	//     DeviceToken: deviceToken,
	//     Topic:       "com.stride.app",
	//     Payload: payload.NewPayload().
	//         AlertTitle("Your daily coach").
	//         AlertBody(message).
	//         Badge(1),
	// }
	// res, err := client.Push(notification)
	//
	log.Printf("push: would send to %s: %s", deviceToken[:8]+"...", message[:40])
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

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
	start := now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
	end := start.AddDate(0, 0, 6)
	return start.Format("Jan 2") + " – " + end.Format("Jan 2")
}

func weekStart() time.Time {
	now := time.Now().UTC()
	return now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
}

func nextWeekday(from time.Time, weekday time.Weekday, hour, min int) time.Time {
	daysUntil := int(weekday) - int(from.Weekday())
	if daysUntil < 0 {
		daysUntil += 7
	}
	next := time.Date(from.Year(), from.Month(), from.Day()+daysUntil, hour, min, 0, 0, time.UTC)
	if !next.After(from) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

func nextDaily(from time.Time, hour, min int) time.Time {
	next := time.Date(from.Year(), from.Month(), from.Day(), hour, min, 0, 0, time.UTC)
	if !next.After(from) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
