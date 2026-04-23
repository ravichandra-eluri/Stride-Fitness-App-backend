package handlers

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"time"

	"stride/backend/internal/auth"
	"stride/backend/internal/db"
	apperrors "stride/backend/internal/errors"
	"stride/backend/internal/logger"
	"stride/backend/internal/metrics"
	"stride/backend/internal/middleware"
	"stride/backend/internal/validator"

	ai "stride/backend"

	"github.com/golang-jwt/jwt/v5"
)

// Deps holds all handler dependencies (injected from main).
type Deps struct {
	DB            *db.DB
	AIClient      *ai.Client
	AppleVerifier *auth.AppleAuthVerifier
	JWTSecret     []byte
	JWTAccessTTL  time.Duration
	JWTRefreshTTL time.Duration
	Log           *logger.Logger
	Metrics       *metrics.Metrics
}

// ── Response helpers ─────────────────────────────────────────────────────────

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func decode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return apperrors.NewBadRequestError("Invalid JSON body")
	}
	return nil
}

func (d Deps) issueTokens(userID string) (access, refresh string, err error) {
	access, err = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(d.JWTAccessTTL).Unix(),
		"iat": time.Now().Unix(),
		"typ": "access",
	}).SignedString(d.JWTSecret)
	if err != nil {
		return
	}
	refresh, err = jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": userID,
		"exp": time.Now().Add(d.JWTRefreshTTL).Unix(),
		"iat": time.Now().Unix(),
		"typ": "refresh",
	}).SignedString(d.JWTSecret)
	return
}

// ── Health ────────────────────────────────────────────────────────────────────

func Health(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]string{"status": "ok"})
}

// ── Auth ─────────────────────────────────────────────────────────────────────

// POST /api/v1/auth/apple
func AppleSignIn(d Deps) http.HandlerFunc {
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		var body validator.AppleSignInRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		// Verify Apple identity token
		claims, err := d.AppleVerifier.VerifyIdentityToken(r.Context(), body.IdentityToken)
		if err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		appleUserID := claims.Subject
		email := body.Email
		if email == "" && claims.Email != "" {
			email = claims.Email
		}

		// Get or create user
		user, err := d.DB.GetUserByAppleID(r.Context(), appleUserID)
		if err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		isNew := user == nil
		if isNew {
			user, err = d.DB.CreateUser(r.Context(), email, appleUserID)
			if err != nil {
				apperrors.WriteErrorFromErr(w, err)
				return
			}
			d.Log.Info("new user created",
				"user_id", user.ID,
				"email", email,
			)
		}

		// Touch user activity
		d.DB.TouchUser(r.Context(), user.ID)

		access, refresh, err := d.issueTokens(user.ID)
		if err != nil {
			apperrors.WriteError(w, apperrors.NewInternalError(err))
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

// POST /api/v1/auth/refresh
func RefreshToken(d Deps) http.HandlerFunc {
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		var body validator.RefreshTokenRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		token, err := jwt.Parse(body.RefreshToken, func(t *jwt.Token) (interface{}, error) {
			return d.JWTSecret, nil
		})
		if err != nil || !token.Valid {
			apperrors.WriteError(w, apperrors.NewUnauthorizedError("Invalid refresh token"))
			return
		}

		claims, ok := token.Claims.(jwt.MapClaims)
		if !ok {
			apperrors.WriteError(w, apperrors.NewUnauthorizedError("Invalid token claims"))
			return
		}

		if claims["typ"] != "refresh" {
			apperrors.WriteError(w, apperrors.NewUnauthorizedError("Not a refresh token"))
			return
		}

		userID, _ := claims["sub"].(string)
		if userID == "" {
			apperrors.WriteError(w, apperrors.NewUnauthorizedError("Missing user ID in token"))
			return
		}

		// Verify user still exists
		_, err = d.DB.GetUserByID(r.Context(), userID)
		if err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		access, refresh, err := d.issueTokens(userID)
		if err != nil {
			apperrors.WriteError(w, apperrors.NewInternalError(err))
			return
		}

		respond(w, 200, map[string]string{
			"access_token":  access,
			"refresh_token": refresh,
		})
	}
}

// ── Onboarding ────────────────────────────────────────────────────────────────

// POST /api/v1/onboarding/complete
func OnboardingComplete(d Deps) http.HandlerFunc {
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body validator.OnboardingRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
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
		plan, err := d.AIClient.GenerateOnboardingPlan(r.Context(), profile)
		if err != nil {
			d.Log.WithContext(r.Context()).Error("onboarding plan generation failed",
				"user_id", userID,
				"error", err.Error(),
			)
			apperrors.WriteError(w, apperrors.NewAIServiceError(err))
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
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		d.Log.Info("onboarding completed",
			"user_id", userID,
			"calorie_target", plan.CalorieTarget,
		)

		respond(w, 200, map[string]any{
			"calorie_target": plan.CalorieTarget,
			"protein_target": plan.ProteinTargetG,
			"carbs_target":   plan.CarbsTargetG,
			"fat_target":     plan.FatTargetG,
			"weekly_loss_kg": plan.WeeklyLossKg,
			"goal_date":      plan.GoalDate,
			"coach_message":  plan.CoachMessage,
			"plan_summary":   plan.PlanSummary,
		})
	}
}

// ── Profile ───────────────────────────────────────────────────────────────────

func GetProfile(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}
		if p == nil {
			apperrors.WriteError(w, apperrors.NewNotFoundError("profile"))
			return
		}
		respond(w, 200, p)
	}
}

func UpdateProfile(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}
		if p == nil {
			apperrors.WriteError(w, apperrors.NewNotFoundError("profile"))
			return
		}

		// Partial update — decode into existing profile
		if err := decode(r, p); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}
		p.UserID = userID

		if err := d.DB.UpsertProfile(r.Context(), p); err != nil {
			apperrors.WriteErrorFromErr(w, err)
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
			apperrors.WriteErrorFromErr(w, err)
			return
		}
		if plan == nil {
			apperrors.WriteError(w, apperrors.NewNotFoundError("meal plan"))
			return
		}
		// Return raw JSONB — already the right shape for iOS
		w.Header().Set("Content-Type", "application/json")
		w.Write(plan.DaysJSON)
	}
}

func RegenerateMealPlan(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())
		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}
		if p == nil {
			apperrors.WriteError(w, apperrors.NewNotFoundError("profile"))
			return
		}

		aiProfile := dbProfileToAI(p)
		weekLabel := currentWeekLabel()

		plan, err := d.AIClient.GenerateWeeklyMealPlan(r.Context(), aiProfile, weekLabel)
		if err != nil {
			d.Log.WithContext(r.Context()).Error("meal plan generation failed",
				"user_id", userID,
				"error", err.Error(),
			)
			apperrors.WriteError(w, apperrors.NewAIServiceError(err))
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
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if d.Metrics != nil {
			d.Metrics.MealPlansGenerated.Inc()
		}

		d.Log.Info("meal plan generated",
			"user_id", userID,
			"week", weekLabel,
		)

		respond(w, 200, plan)
	}
}

func SwapMeal(d Deps) http.HandlerFunc {
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body validator.SwapMealRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		p, err := d.DB.GetProfile(r.Context(), userID)
		if err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}
		if p == nil {
			apperrors.WriteError(w, apperrors.NewNotFoundError("profile"))
			return
		}

		// Convert DTO to AI types
		meal := ai.Meal{
			MealType:    body.Meal.MealType,
			Name:        body.Meal.Name,
			Description: body.Meal.Description,
			Calories:    body.Meal.Calories,
			ProteinG:    body.Meal.ProteinG,
			CarbsG:      body.Meal.CarbsG,
			FatG:        body.Meal.FatG,
			Ingredients: body.Meal.Ingredients,
		}

		swaps, err := d.AIClient.SwapMeal(r.Context(), dbProfileToAI(p), meal, ai.SwapFilter(body.Filter))
		if err != nil {
			d.Log.WithContext(r.Context()).Error("meal swap failed",
				"user_id", userID,
				"error", err.Error(),
			)
			apperrors.WriteError(w, apperrors.NewAIServiceError(err))
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
			FilterUsed:       body.Filter,
		})

		respond(w, 200, swaps)
	}
}

// ── Food logging ──────────────────────────────────────────────────────────────

func LogFood(d Deps) http.HandlerFunc {
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body validator.FoodEntryRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		// Ensure today's daily_log exists
		log, _ := d.DB.GetTodayLog(r.Context(), userID)
		if log == nil {
			log = &db.DailyLog{UserID: userID}
			if err := d.DB.UpsertDailyLog(r.Context(), log); err != nil {
				apperrors.WriteErrorFromErr(w, err)
				return
			}
			log, _ = d.DB.GetTodayLog(r.Context(), userID)
		}

		entry := &db.FoodEntry{
			UserID:      userID,
			DailyLogID:  log.ID,
			MealType:    body.MealType,
			FoodName:    body.FoodName,
			Calories:    body.Calories,
			ProteinG:    body.ProteinG,
			CarbsG:      body.CarbsG,
			FatG:        body.FatG,
			ServingSize: body.ServingSize,
			LogMethod:   body.LogMethod,
			Barcode:     body.Barcode,
		}

		if err := d.DB.AddFoodEntry(r.Context(), entry); err != nil {
			apperrors.WriteErrorFromErr(w, err)
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

		if d.Metrics != nil {
			d.Metrics.FoodEntriesLogged.Inc()
		}

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
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body validator.LogWeightRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := d.DB.LogWeight(r.Context(), userID, body.WeightKg, body.Note); err != nil {
			apperrors.WriteErrorFromErr(w, err)
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
			apperrors.WriteErrorFromErr(w, err)
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
			apperrors.WriteErrorFromErr(w, err)
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
			apperrors.WriteErrorFromErr(w, err)
			return
		}
		if msg == nil {
			apperrors.WriteError(w, apperrors.NewNotFoundError("coach message"))
			return
		}
		respond(w, 200, msg)
	}
}

// ── Subscriptions ─────────────────────────────────────────────────────────────

func VerifySubscription(d Deps) http.HandlerFunc {
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body validator.VerifySubscriptionRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		// TODO: In production, verify transaction with Apple's App Store Server API
		// POST https://api.storekit.itunes.apple.com/inApps/v1/transactions/{transactionId}

		sub := &db.Subscription{
			UserID:            userID,
			Plan:              body.Plan,
			Status:            "active",
			AppleOriginalTxID: body.TransactionID,
		}
		if err := d.DB.UpsertSubscription(r.Context(), sub); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		d.Log.Info("subscription verified",
			"user_id", userID,
			"plan", body.Plan,
		)

		respond(w, 200, map[string]string{"status": "active"})
	}
}

// POST /webhooks/apple/subscriptions
func AppleSubscriptionWebhook(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// TODO: In production:
		// 1. Verify the signed payload from Apple
		// 2. Decode the signedPayload JWT
		// 3. Extract notificationType and data
		// 4. Update subscription status accordingly
		// notificationType: DID_RENEW | EXPIRED | REFUND | CANCEL | etc.
		w.WriteHeader(http.StatusOK)
	}
}

// ── Device tokens ─────────────────────────────────────────────────────────────

func RegisterDevice(d Deps) http.HandlerFunc {
	v := validator.Get()

	return func(w http.ResponseWriter, r *http.Request) {
		userID := middleware.UserIDFromCtx(r.Context())

		var body validator.RegisterDeviceRequest
		if err := decode(r, &body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := v.Struct(body); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}

		if err := d.DB.UpsertDeviceToken(r.Context(), userID, body.Token, body.DeviceName); err != nil {
			apperrors.WriteErrorFromErr(w, err)
			return
		}
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

// WithContext is a helper that adds user ID to context for logging.
func WithContext(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, middleware.UserIDKey, userID)
}
