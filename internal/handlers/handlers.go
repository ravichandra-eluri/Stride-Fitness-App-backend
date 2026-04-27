package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/http"
	"time"

	"stride/backend/internal/db"
	"stride/backend/internal/middleware"
	ai "stride/backend"

	"github.com/go-chi/chi/v5"
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

// ── Privacy policy ────────────────────────────────────────────────────────────

func PrivacyPolicy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(privacyPolicyHTML))
}

const privacyPolicyHTML = `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Stride Fitness — Privacy Policy</title>
<style>
  body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
         max-width: 720px; margin: 48px auto; padding: 0 24px;
         color: #1a1a1a; line-height: 1.7; }
  h1   { font-size: 2rem; margin-bottom: 4px; }
  h2   { font-size: 1.15rem; margin-top: 36px; }
  p, li { font-size: 0.97rem; color: #333; }
  a    { color: #22c55e; }
  .date { color: #888; font-size: 0.875rem; margin-bottom: 36px; }
</style>
</head>
<body>
<h1>Stride Fitness — Privacy Policy</h1>
<p class="date">Last updated: April 23, 2026</p>

<p>Stride Fitness ("we", "our", or "us") is committed to protecting your privacy.
This policy explains what data we collect, why we collect it, and how you can
control it.</p>

<h2>1. Information We Collect</h2>
<ul>
  <li><strong>Account information</strong> — your Apple ID (anonymous user identifier
      provided by Apple's Sign in with Apple system). We never receive your Apple ID
      password.</li>
  <li><strong>Profile data</strong> — name, age, gender, height, weight, fitness goals,
      and dietary preferences that you enter during onboarding.</li>
  <li><strong>Food logs</strong> — meals and calories you log manually within the app.</li>
  <li><strong>Weight logs</strong> — weight entries you record over time.</li>
  <li><strong>Usage data</strong> — streak counts and daily log summaries used to
      generate your personalised coach messages.</li>
</ul>

<h2>2. How We Use Your Information</h2>
<ul>
  <li>To generate personalised meal plans and calorie targets via the Anthropic Claude AI API.</li>
  <li>To show you your daily progress, streaks, and historical trends.</li>
  <li>To deliver daily coach messages tailored to your recent activity.</li>
  <li>We do <strong>not</strong> sell, rent, or share your personal data with third parties
      for advertising or marketing purposes.</li>
</ul>

<h2>3. Third-Party Services</h2>
<ul>
  <li><strong>Anthropic Claude API</strong> — your profile and recent log data are sent
      to Anthropic's API to generate meal plans and coach messages. Anthropic's privacy
      policy is available at <a href="https://www.anthropic.com/privacy">anthropic.com/privacy</a>.</li>
  <li><strong>Apple Sign in with Apple</strong> — authentication is handled entirely by
      Apple. We only receive the anonymous user identifier Apple provides.</li>
  <li><strong>Google Cloud Run / Cloud SQL</strong> — our backend and database run on
      Google Cloud infrastructure in the United States.</li>
</ul>

<h2>4. Data Retention</h2>
<p>We retain your data for as long as your account is active. You can permanently
delete your account and all associated data at any time from the app's Profile screen
(Profile → Delete account). Deletion is immediate and irreversible.</p>

<h2>5. Children's Privacy</h2>
<p>Stride Fitness is not directed at children under the age of 13. We do not knowingly
collect personal information from children under 13.</p>

<h2>6. Changes to This Policy</h2>
<p>We may update this policy from time to time. We will notify you of material changes
by updating the "Last updated" date above. Continued use of the app after changes
constitutes acceptance of the revised policy.</p>

<h2>7. Contact</h2>
<p>If you have questions about this privacy policy, please contact us at
<a href="mailto:chandra.sk59@gmail.com">chandra.sk59@gmail.com</a>.</p>
</body>
</html>`

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

		// Compute goal date from the user's local date + timeline, so it's always
		// accurate regardless of what the AI returned.
		localDate := middleware.LocalDateFromCtx(r.Context())
		goalDate := plan.GoalDate
		baseDate := time.Now()
		if localDate != "" {
			if t, err := time.Parse("2006-01-02", localDate); err == nil {
				baseDate = t
			}
		}
		goalDate = baseDate.AddDate(0, body.TimelineMonths, 0).Format("2006-01-02")

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
			GoalDate:        goalDate,
		}
		if err := d.DB.UpsertProfile(r.Context(), dbProfile); err != nil {
			log.Printf("[onboarding] UpsertProfile userID=%s: %v", userID, err)
			respondErr(w, 500, "save profile failed")
			return
		}

		respond(w, 200, map[string]any{
			"calorie_target":  plan.CalorieTarget,
			"protein_target":  int(math.Round(plan.ProteinTargetG)),
			"carbs_target":    int(math.Round(plan.CarbsTargetG)),
			"fat_target":      int(math.Round(plan.FatTargetG)),
			"weekly_loss_kg":  plan.WeeklyLossKg,
			"goal_date":       goalDate,
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
		localDate := middleware.LocalDateFromCtx(r.Context())

		// Ensure today's daily_log exists
		log, _ := d.DB.GetTodayLog(r.Context(), userID, localDate)
		if log == nil {
			log = &db.DailyLog{UserID: userID}
			d.DB.UpsertDailyLog(r.Context(), log, localDate)
			log, _ = d.DB.GetTodayLog(r.Context(), userID, localDate)
		}
		entry.DailyLogID = log.ID

		if err := d.DB.AddFoodEntry(r.Context(), &entry, localDate); err != nil {
			respondErr(w, 500, "log failed")
			return
		}

		// Recompute daily totals
		entries, _ := d.DB.GetTodayFoodEntries(r.Context(), userID, localDate)
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
		d.DB.UpsertDailyLog(r.Context(), log, localDate)

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
		localDate := middleware.LocalDateFromCtx(r.Context())
		log, _ := d.DB.GetTodayLog(r.Context(), userID, localDate)
		entries, _ := d.DB.GetTodayFoodEntries(r.Context(), userID, localDate)
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

		// Return cached message if one already exists for today.
		msg, err := d.DB.GetTodayCoachMessage(r.Context(), userID)
		if err != nil {
			respondErr(w, 500, "db error")
			return
		}
		if msg != nil {
			respond(w, 200, msg)
			return
		}

		// No message yet — generate one on-demand.
		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil || p == nil {
			// No profile yet: return a friendly default without hitting Claude.
			respond(w, 200, map[string]any{
				"id":             "default",
				"message":        "Welcome to Stride! Complete your profile to get a personalised plan and daily coaching.",
				"tip":            "Start by logging your first meal — even a small step builds momentum.",
				"priority_meal":  "breakfast",
				"tone":           "encouraging",
			})
			return
		}

		dbYesterday, _ := d.DB.GetYesterdayStats(r.Context(), userID)
		ys := ai.YesterdayStats{CalorieTarget: p.CalorieTarget}
		if dbYesterday != nil {
			ys.CaloriesEaten      = dbYesterday.CaloriesEaten
			ys.CurrentStreakDays  = dbYesterday.CurrentStreakDays
			ys.TotalLostKg        = dbYesterday.TotalLostKg
		}

		aiMsg, err := d.aiClient().GenerateDailyCoach(r.Context(), dbProfileToAI(p), ys)
		if err != nil {
			log.Printf("[coach] GenerateDailyCoach userID=%s: %v", userID, err)
			// Return a safe fallback so the UI never shows an error state.
			respond(w, 200, map[string]any{
				"id":             "fallback",
				"message":        "Every day is a fresh start. Stay consistent and the results will follow!",
				"tip":            "Drink a glass of water before each meal to help manage portion sizes.",
				"priority_meal":  "breakfast",
				"tone":           "encouraging",
			})
			return
		}

		dbMsg := &db.CoachMessage{
			UserID:       userID,
			Message:      aiMsg.Message,
			Tip:          aiMsg.Tip,
			PriorityMeal: aiMsg.PriorityMeal,
			Tone:         aiMsg.Tone,
		}
		if saveErr := d.DB.SaveCoachMessage(r.Context(), dbMsg); saveErr != nil {
			log.Printf("[coach] SaveCoachMessage userID=%s: %v", userID, saveErr)
		}

		respond(w, 200, aiMsg)
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

// ── Food entry deletion ───────────────────────────────────────────────────────

// DELETE /api/log/food/{id}
func DeleteFoodEntry(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		entryID := chi.URLParam(r, "id")
		if entryID == "" {
			respondErr(w, 400, "missing entry id")
			return
		}
		if err := d.DB.DeleteFoodEntry(r.Context(), userID, entryID); err != nil {
			log.Printf("[food] DeleteFoodEntry userID=%s entryID=%s: %v", userID, entryID, err)
			respondErr(w, 500, "delete failed")
			return
		}
		// Recompute daily log totals after deletion — mirrors what LogFood does on add.
		localDate := middleware.LocalDateFromCtx(r.Context())
		if dailyLog, _ := d.DB.GetTodayLog(r.Context(), userID, localDate); dailyLog != nil {
			entries, _ := d.DB.GetTodayFoodEntries(r.Context(), userID, localDate)
			var totalCal int
			var totalP, totalC, totalF float64
			for _, e := range entries {
				totalCal += e.Calories
				totalP += e.ProteinG
				totalC += e.CarbsG
				totalF += e.FatG
			}
			dailyLog.CaloriesEaten = totalCal
			dailyLog.ProteinG = totalP
			dailyLog.CarbsG = totalC
			dailyLog.FatG = totalF
			d.DB.UpsertDailyLog(r.Context(), dailyLog, localDate)
		}
		respond(w, 200, map[string]string{"status": "deleted"})
	}
}

// ── Account deletion ──────────────────────────────────────────────────────────

// DELETE /api/account
// Required by Apple for apps using Sign in with Apple.
func DeleteAccount(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		if err := d.DB.DeleteUser(r.Context(), userID); err != nil {
			log.Printf("[account] DeleteUser userID=%s: %v", userID, err)
			respondErr(w, 500, "delete account failed")
			return
		}
		respond(w, 200, map[string]string{"status": "deleted"})
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

// ── Food lookup ───────────────────────────────────────────────────────────────

// GET /api/food/barcode/{barcode}
// Fetches nutrition data from Open Food Facts.
func FoodBarcodeLookup(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		barcode := chi.URLParam(r, "barcode")
		if barcode == "" {
			respondErr(w, 400, "barcode required")
			return
		}

		client := &http.Client{Timeout: 10 * time.Second}
		url := fmt.Sprintf("https://world.openfoodfacts.org/api/v0/product/%s.json", barcode)
		resp, err := client.Get(url)
		if err != nil {
			log.Printf("[barcode] lookup %s: %v", barcode, err)
			respondErr(w, 502, "barcode lookup failed")
			return
		}
		defer resp.Body.Close()

		var offResp struct {
			Status  int `json:"status"`
			Product struct {
				ProductName string `json:"product_name"`
				Nutriments  struct {
					EnergyKcal100g float64 `json:"energy-kcal_100g"`
					Proteins100g   float64 `json:"proteins_100g"`
					Carbs100g      float64 `json:"carbohydrates_100g"`
					Fat100g        float64 `json:"fat_100g"`
				} `json:"nutriments"`
				ServingSize     string  `json:"serving_size"`
				ServingQuantity float64 `json:"serving_quantity"`
			} `json:"product"`
		}

		if err := json.NewDecoder(resp.Body).Decode(&offResp); err != nil || offResp.Status == 0 || offResp.Product.ProductName == "" {
			respondErr(w, 404, "product not found")
			return
		}

		p := offResp.Product
		qty := p.ServingQuantity
		if qty <= 0 {
			qty = 100
		}
		ratio := qty / 100.0

		serving := p.ServingSize
		if serving == "" {
			serving = fmt.Sprintf("%.0fg", qty)
		}

		respond(w, 200, ai.FoodNutrition{
			Name:        p.ProductName,
			Calories:    int(p.Nutriments.EnergyKcal100g * ratio),
			ProteinG:    p.Nutriments.Proteins100g * ratio,
			CarbsG:      p.Nutriments.Carbs100g * ratio,
			FatG:        p.Nutriments.Fat100g * ratio,
			ServingSize: serving,
		})
	}
}

// POST /api/food/analyze-photo
// Sends a base64 image to Claude vision and returns estimated nutrition.
func FoodAnalyzePhoto(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ImageBase64 string `json:"image_base64"`
		}
		if err := decode(r, &body); err != nil || body.ImageBase64 == "" {
			respondErr(w, 400, "image_base64 required")
			return
		}

		nutrition, err := d.aiClient().AnalyzeFoodPhoto(r.Context(), body.ImageBase64)
		if err != nil {
			log.Printf("[food] analyze photo: %v", err)
			respondErr(w, 502, "photo analysis failed")
			return
		}

		respond(w, 200, nutrition)
	}
}
