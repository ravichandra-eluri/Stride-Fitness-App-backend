package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"stride/backend/internal/db"
	"stride/backend/internal/handlers"
	"stride/backend/internal/middleware"
	"stride/backend/internal/cron"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

func main() {
	// ── Config from env ───────────────────────────────────────────────────
	port        := getEnv("PORT", "8080")
	databaseURL := mustEnv("DATABASE_URL")   // postgres://user:pass@host/dbname
	claudeKey   := mustEnv("CLAUDE_API_KEY")
	jwtSecret   := mustEnv("JWT_SECRET")
	apnsKeyID   := getEnv("APNS_KEY_ID", "")
	apnsTeamID  := getEnv("APNS_TEAM_ID", "")
	apnsKeyPath := getEnv("APNS_KEY_PATH", "")

	// ── Database ──────────────────────────────────────────────────────────
	database, err := db.New(databaseURL)
	if err != nil {
		log.Fatalf("connect db: %v", err)
	}
	defer database.Close()

	// ── Handler dependencies ──────────────────────────────────────────────
	deps := handlers.Deps{
		DB:         database,
		ClaudeKey:  claudeKey,
		JWTSecret:  []byte(jwtSecret),
		APNsKeyID:  apnsKeyID,
		APNsTeamID: apnsTeamID,
		APNsKeyPath: apnsKeyPath,
	}

	// ── Router ────────────────────────────────────────────────────────────
	r := chi.NewRouter()

	r.Use(chimiddleware.RequestID)
	r.Use(chimiddleware.RealIP)
	r.Use(chimiddleware.Logger)
	r.Use(chimiddleware.Recoverer)
	// Default per-request timeout. Routes that call Claude (onboarding plan,
	// meal plan) can easily run 30–90s; individual routes can override below.
	r.Use(chimiddleware.Timeout(150 * time.Second))
	r.Use(middleware.CORS)

	// Public routes — no auth required
	r.Group(func(r chi.Router) {
		r.Post("/api/auth/apple",    handlers.AppleSignIn(deps))
		r.Post("/api/auth/refresh",  handlers.RefreshToken(deps))
		r.Get("/health",             handlers.Health)
	})

	// Protected routes — JWT required
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(deps.JWTSecret))

		// Onboarding
		r.Post("/api/onboarding/complete", handlers.OnboardingComplete(deps))

		// Profile
		r.Get("/api/profile",    handlers.GetProfile(deps))
		r.Patch("/api/profile",  handlers.UpdateProfile(deps))

		// Meal plans
		r.Get("/api/meals/plan",         handlers.GetMealPlan(deps))
		r.Post("/api/meals/regenerate",  handlers.RegenerateMealPlan(deps))
		r.Post("/api/meals/swap",        handlers.SwapMeal(deps))

		// Food logging
		r.Post("/api/log/food",         handlers.LogFood(deps))
		r.Get("/api/log/today",         handlers.GetTodayLog(deps))
		r.Post("/api/log/weight",       handlers.LogWeight(deps))

		// Progress
		r.Get("/api/progress/weekly",   handlers.WeeklyProgress(deps))
		r.Get("/api/progress/weights",  handlers.WeightHistory(deps))

		// Coach
		r.Get("/api/coach/today",       handlers.TodayCoachMessage(deps))

		// Subscriptions (StoreKit 2)
		r.Post("/api/subscription/verify",  handlers.VerifySubscription(deps))

		// Device tokens (push notifications)
		r.Post("/api/device/register",      handlers.RegisterDevice(deps))
	})

	// Apple server-to-server subscription webhooks (no user auth, but signed by Apple)
	r.Post("/webhooks/apple/subscriptions", handlers.AppleSubscriptionWebhook(deps))

	// ── Cron jobs ─────────────────────────────────────────────────────────
	scheduler := cron.New(deps)
	scheduler.Start()
	defer scheduler.Stop()

	// ── HTTP server ───────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 60 * time.Second, // longer for AI calls
		IdleTimeout:  120 * time.Second,
	}

	go func() {
		log.Printf("server listening on :%s", port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("done")
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s is not set", key)
	}
	return v
}
