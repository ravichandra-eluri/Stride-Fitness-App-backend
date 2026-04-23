package handlers

import (
	"encoding/json"
	"log"
	"math"
	"net/http"
	"time"

	"stride/backend/internal/db"
	"stride/backend/internal/middleware"
	ai "stride/backend"

	"github.com/golang-jwt/jwt/v5"
)

// Deps holds all handler dependencies (injected from main).
type Deps struct {
	DB          *db.DB
	ClaudeKey   string
	JWTSecret   []byte
	APNsKeyID   string
	APNsTeamID  string
	APNsKeyPath string
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func respondErr(w http.ResponseWriter, code int, msg string) {
	respond(w, code, map[string]string{"error": msg})
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

func (d Deps) aiClient() *ai.Client {
	return ai.NewClient(d.ClaudeKey)
}

func (d Deps) issueTokens(userID string) (access, refresh string, err error) {
	access, err = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(24 * time.Hour).Unix(),
		"typ": "access",
	}).SignedString(d.JWTSecret)
	if err != nil {
		return
	}
	refresh, err = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(30 * 24 * time.Hour).Unix(),
		"typ": "refresh",
	}).SignedString(d.JWTSecret)
	return
}

// ── Health ────────────────────────────────────────────────────────────────────

func Health(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]string{"status": "ok"})
}

// ── Auth ─────────────────────────────────────────────────────────────────────

// POST /api/auth/apple
// iOS sends the Apple identity token after Sign in with Apple.
// We verify it, create or fetch the user, and return our own JWT.
func AppleSignIn(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			IdentityToken string `json:"identity_token"` // from Apple SDK
			Email         string `json:"email"`           // only on first sign-in
			FullName      string `json:"full_name"`
		}
		if err := decode(r, &body); err != nil {
			respondErr(w, 400, "invalid body")
			return
		}

		// Verify Apple identity token and extract the Apple user ID (sub claim).
		// In production: validate JWT signature against Apple's public keys
		// at https://appleid.apple.com/auth/keys
		// For brevity here we parse without full Apple verification.
		token, _, err := jwt.NewParser().ParseUnverified(body.IdentityToken, jwt.MapClaims{})
		if err != nil {
			respondErr(w, 401, "invalid apple token")
			return
		}
		claims, _ := token.Claims.(jwt.MapClaims)
		appleUserID, _ := claims["sub"].(string)
		if appleUserID == "" {
			respondErr(w, 401, "missing sub in apple token")
			return
		}

		// Get or create user
		user, err := d.DB.GetUserByAppleID(r.Context(), appleUserID)
		if err != nil {
			respondErr(w, 500, "db error")
			return
		}
		isNew := user == nil
		if isNew {
			user, err = d.DB.CreateUser(r.Context(), body.Email, appleUserID)
			if err != nil {
				respondErr(w, 500, "create user failed")
				return
			}
		}

		access, refresh, err := d.issueTokens(user.ID)
		if err != nil {
			respondErr(w, 500, "token error")
			return
		}

		respond(w, 200, map[string]any{
			"access_token":  access,
			"refresh_token": refresh,
			"user_id":       user.ID,
			"is_new_user":   isNew,
		})
	}
}

// POST /api/auth/refresh
func RefreshToken(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		decode(r, &body)

		token, err := jwt.Parse(body.RefreshToken, func(t *jwt.Token) (interface{}, error) {
			return d.JWTSecret, nil
		})
		if err != nil || !token.Valid {
			respondErr(w, 401, "invalid refresh token")
			return
		}
		claims, _ := token.Claims.(jwt.MapClaims)
		if claims["typ"] != "refresh" {
			respondErr(w, 401, "not a refresh token")
			return
		}
		userID, _ := claims["sub"].(string)
		access, refresh, err := d.issueTokens(userID)
		if err != nil {
			respondErr(w, 500, "token error")
			return
		}
		respond(w, 200, map[string]string{
			"access_token":  access,
			"refresh_token": refresh,
		})
	}
}

// ── Onboarding ────────────────────────────────────────────────────────────────

// POST /api/onboarding/complete
// Receives the full onboarding form, calls Claude, saves everything.
func OnboardingComplete(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body struct {
			Name           string   `json:"name"`
			Age            int      `json:"age"`
			Gender         string   `json:"gender"`
			HeightCm       int      `json:"height_cm"`
			CurrentWeight  float64  `json:"current_weight_kg"`
			GoalWeight     float64  `json:"goal_weight_kg"`
			TimelineMonths int      `json:"timeline_months"`
			ActivityLevel  string   `json:"activity_level"`
			DailyMinutes   int      `json:"daily_minutes"`
			DietPrefs      []string `json:"diet_prefs"`
			PrimaryGoal    string   `json:"primary_goal"`
		}
		if err := decode(r, &body); err != nil {
			respondErr(w, 400, "invalid body")
			return
		}

		profile := ai.UserProfile{
			Name:            body.Name,
			Age:             body.Age,
			Gender:          body.Gender,
			HeightCm:        body.HeightCm,
			CurrentWeightKg: body.CurrentWeight,
			GoalWeightKg:    body.GoalWeight,
			TimelineMonths:  body.TimelineMonths,
			ActivityLevel:   body.ActivityLevel,
			DailyMinutes:    body.DailyMinutes,
			DietPrefs:       body.DietPrefs,
		}

		// Call Claude — generate plan
		plan, err := d.aiClient().GenerateOnboardingPlan(r.Context(), profile)
		if err != nil {
			log.Printf("[onboarding] GenerateOnboardingPlan: %v", err)
			respondErr(w, 502, "ai error")
			return
		}

		// Save profile
		dbProfile := &db.Profile{
			UserID:          userID,
			Name:            body.Name,
			Age:             body.Age,
			Gender:          body.Gender,
			HeightCm:        body.HeightCm,
			CurrentWeightKg: body.CurrentWeight,
			GoalWeightKg:    body.GoalWeight,
			TimelineMonths:  body.TimelineMonths,
			ActivityLevel:   body.ActivityLevel,
			DailyMinutes:    body.DailyMinutes,
			DietPrefs:       body.DietPrefs,
			PrimaryGoal:     body.PrimaryGoal,
			CalorieTarget:   plan.CalorieTarget,
			ProteinTargetG:  int(math.Round(plan.ProteinTargetG)),
			CarbsTargetG:    int(math.Round(plan.CarbsTargetG)),
			FatTargetG:      int(math.Round(plan.FatTargetG)),
			GoalDate:        plan.GoalDate,
		}
		if err := d.DB.UpsertProfile(r.Context(), dbProfile); err != nil {
			log.Printf("[onboarding] UpsertProfile userID=%s: %v", userID, err)
			respondErr(w, 500, "save profile failed")
			return
		}

		respond(w, 200, map[string]any{
			"calorie_target":  plan.CalorieTarget,
			"protein_target":  plan.ProteinTargetG,
			"carbs_target":    plan.CarbsTargetG,
			"fat_target":      plan.FatTargetG,
			"weekly_loss_kg":  plan.WeeklyLossKg,
			"goal_date":       plan.GoalDate,
			"coach_message":   plan.CoachMessage,
			"plan_summary":    plan.PlanSummary,
		})
	}
}

// ── Profile ───────────────────────────────────────────────────────────────────

func GetProfile(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil {
			log.Printf("[profile] GetProfile userID=%s: %v", userID, err)
			respondErr(w, 500, "db error")
			return
		}
		if p == nil {
			respondErr(w, 404, "profile not found")
			return
		}
		respond(w, 200, p)
	}
}

func UpdateProfile(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil || p == nil {
			respondErr(w, 404, "profile not found")
			return
		}
		decode(r, p) // partial update — only override provided fields
		p.UserID = userID
		if err := d.DB.UpsertProfile(r.Context(), p); err != nil {
			respondErr(w, 500, "update failed")
			return
		}
		respond(w, 200, p)
	}
}

// ── Meal plans ────────────────────────────────────────────────────────────────

func GetMealPlan(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		plan, err := d.DB.GetActiveMealPlan(r.Context(), userID)
		if err != nil {
			log.Printf("[mealplan] GetActiveMealPlan userID=%s: %v", userID, err)
			respondErr(w, 500, "db error")
			return
		}
		if plan == nil {
			respondErr(w, 404, "no meal plan found")
			return
		}
		// Wrap the stored Days JSONB in the envelope iOS expects.
		days := json.RawMessage(plan.DaysJSON)
		if len(days) == 0 {
			days = json.RawMessage("[]")
		}
		respond(w, 200, struct {
			Week             string          `json:"week"`
			Days             json.RawMessage `json:"days"`
			AvgDailyCalories int             `json:"avg_daily_calories"`
		}{
			Week:             plan.WeekLabel,
			Days:             days,
			AvgDailyCalories: plan.AvgDailyCalories,
		})
	}
}

func RegenerateMealPlan(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil || p == nil {
			respondErr(w, 404, "profile not found")
			return
		}

		aiProfile := dbProfileToAI(p)
		weekLabel := currentWeekLabel()

		plan, err := d.aiClient().GenerateWeeklyMealPlan(r.Context(), aiProfile, weekLabel)
		if err != nil {
			log.Printf("[mealplan] GenerateWeeklyMealPlan: %v", err)
			respondErr(w, 502, "ai error")
			return
		}

		daysJSON, _ := json.Marshal(plan.Days)
		mealPlan := &db.MealPlan{
			UserID:           userID,
			WeekLabel:        plan.Week,
			WeekStartDate:    weekStart(),
			DaysJSON:         daysJSON,
			AvgDailyCalories: plan.AvgDailyCalories,
		}
		if err := d.DB.SaveMealPlan(r.Context(), mealPlan); err != nil {
			respondErr(w, 500, "save plan failed")
			return
		}
		respond(w, 200, plan)
	}
}

func SwapMeal(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body struct {
			MealPlanID string         `json:"meal_plan_id"`
			Day        string         `json:"day"`
			Meal       ai.Meal        `json:"meal"`
			Filter     ai.SwapFilter  `json:"filter"`
		}
		if err := decode(r, &body); err != nil {
			respondErr(w, 400, "invalid body")
			return
		}

		p, _ := d.DB.GetProfile(r.Context(), userID)
		swaps, err := d.aiClient().SwapMeal(r.Context(), dbProfileToAI(p), body.Meal, body.Filter)
		if err != nil {
			log.Printf("[mealswap] SwapMeal: %v", err)
			respondErr(w, 502, "ai error")
			return
		}

		// Log the swap
		origJSON, _ := json.Marshal(body.Meal)
		d.DB.SaveMealSwap(r.Context(), &db.MealSwap{
			UserID:           userID,
			MealPlanID:       body.MealPlanID,
			Day:              body.Day,
			MealType:         body.Meal.MealType,
			OriginalMealJSON: origJSON,
			FilterUsed:       string(body.Filter),
		})

		respond(w, 200, swaps)
	}
}

// ── Food logging ──────────────────────────────────────────────────────────────

func LogFood(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var entry db.FoodEntry
		if err := decode(r, &entry); err != nil {
			respondErr(w, 400, "invalid body")
			return
		}
		entry.UserID = userID

		// Ensure today's daily_log exists
		log, _ := d.DB.GetTodayLog(r.Context(), userID)
		if log == nil {
			log = &db.DailyLog{UserID: userID}
			d.DB.UpsertDailyLog(r.Context(), log)
			log, _ = d.DB.GetTodayLog(r.Context(), userID)
		}
		entry.DailyLogID = log.ID

		if err := d.DB.AddFoodEntry(r.Context(), &entry); err != nil {
			respondErr(w, 500, "log failed")
			return
		}

		// Recompute daily totals
		entries, _ := d.DB.GetTodayFoodEntries(r.Context(), userID)
		var totalCal int
		var totalP, totalC, totalF float64
		for _, e := range entries {
			totalCal += e.Calories
			totalP += e.ProteinG
			totalC += e.CarbsG
			totalF += e.FatG
		}
		log.CaloriesEaten = totalCal
		log.ProteinG = totalP
		log.CarbsG = totalC
		log.FatG = totalF
		d.DB.UpsertDailyLog(r.Context(), log)

		respond(w, 201, map[string]any{
			"entry_id":       entry.ID,
			"total_calories": totalCal,
			"total_protein":  totalP,
			"total_carbs":    totalC,
			"total_fat":      totalF,
		})
	}
}

func GetTodayLog(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		log, _ := d.DB.GetTodayLog(r.Context(), userID)
		entries, _ := d.DB.GetTodayFoodEntries(r.Context(), userID)
		respond(w, 200, map[string]any{
			"log":     log,
			"entries": entries,
		})
	}
}

func LogWeight(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		var body struct {
			WeightKg float64 `json:"weight_kg"`
			Note     string  `json:"note"`
		}
		decode(r, &body)
		if err := d.DB.LogWeight(r.Context(), userID, body.WeightKg, body.Note); err != nil {
			respondErr(w, 500, "log weight failed")
			return
		}
		respond(w, 201, map[string]string{"status": "logged"})
	}
}

// ── Progress ──────────────────────────────────────────────────────────────────

func WeeklyProgress(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		summary, err := d.DB.GetWeeklySummary(r.Context(), userID)
		if err != nil {
			respondErr(w, 500, "db error")
			return
		}
		respond(w, 200, summary)
	}
}

func WeightHistory(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		entries, err := d.DB.GetWeightHistory(r.Context(), userID, 90)
		if err != nil {
			respondErr(w, 500, "db error")
			return
		}
		respond(w, 200, entries)
	}
}

// ── Coach ─────────────────────────────────────────────────────────────────────

func TodayCoachMessage(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		msg, err := d.DB.GetTodayCoachMessage(r.Context(), userID)
		if err != nil {
			respondErr(w, 500, "db error")
			return
		}
		if msg == nil {
			respondErr(w, 404, "no message today yet")
			return
		}
		respond(w, 200, msg)
	}
}

// ── Subscriptions ─────────────────────────────────────────────────────────────

func VerifySubscription(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		var body struct {
			TransactionID string `json:"transaction_id"`
			Plan          string `json:"plan"` // monthly | annual
		}
		decode(r, &body)

		// In production: verify transaction with Apple's App Store Server API
		// POST https://api.storekit.itunes.apple.com/inApps/v1/transactions/{transactionId}
		sub := &db.Subscription{
			UserID:            userID,
			Plan:              body.Plan,
			Status:            "active",
			AppleOriginalTxID: body.TransactionID,
		}
		if err := d.DB.UpsertSubscription(r.Context(), sub); err != nil {
			respondErr(w, 500, "save subscription failed")
			return
		}
		respond(w, 200, map[string]string{"status": "active"})
	}
}

// POST /webhooks/apple/subscriptions
// Apple sends renewal/cancellation events here (server-to-server notifications v2).
func AppleSubscriptionWebhook(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// In production: verify the signed payload from Apple
		// Decode the signedPayload JWT, extract notificationType and data
		// Update subscription status accordingly
		// notificationType: DID_RENEW | EXPIRED | REFUND | CANCEL | etc.
		w.WriteHeader(http.StatusOK)
	}
}

// ── Device tokens ─────────────────────────────────────────────────────────────

func RegisterDevice(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		var body struct {
			Token      string `json:"token"`
			DeviceName string `json:"device_name"`
		}
		decode(r, &body)
		d.DB.UpsertDeviceToken(r.Context(), userID, body.Token, body.DeviceName)
		respond(w, 200, map[string]string{"status": "registered"})
	}
}

// ── Helpers ───────────────────────────────────────────────────────────────────

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
	now := time.Now()
	start := now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
	end := start.AddDate(0, 0, 6)
	return start.Format("Jan 2") + " – " + end.Format("Jan 2")
}

func weekStart() time.Time {
	now := time.Now()
	return now.AddDate(0, 0, -int(now.Weekday()-time.Monday))
}
