package main

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"stride/backend/internal/ai"
	"stride/backend/internal/auth"
	"stride/backend/internal/cache"
	"stride/backend/internal/config"
	"stride/backend/internal/cron"
	"stride/backend/internal/db"
	"stride/backend/internal/handlers"
	"stride/backend/internal/health"
	"stride/backend/internal/logger"
	"stride/backend/internal/metrics"
	"stride/backend/internal/middleware"
	"stride/backend/internal/push"
	"stride/backend/internal/validator"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func main() {
	// ── Load Configuration ────────────────────────────────────────────────
	cfg, err := config.Load()
	if err != nil {
		// Can't use logger yet, fall back to stdlib
		panic("failed to load config: " + err.Error())
	}

	// ── Initialize Structured Logger ──────────────────────────────────────
	log := logger.New(logger.Options{
		Level:       cfg.LogLevel,
		Format:      cfg.LogFormat,
		ServiceName: "stride-backend",
		Version:     cfg.Version,
		Environment: cfg.Environment,
	})

	log.Info("starting stride backend",
		"version", cfg.Version,
		"environment", cfg.Environment,
		"port", cfg.Port,
	)

	// ── Initialize Metrics ────────────────────────────────────────────────
	metricsCollector := metrics.New()
	metricsCollector.Register()
	log.Info("prometheus metrics initialized")

	// ── Initialize Cache ──────────────────────────────────────────────────
	appCache := cache.New(cache.Options{
		DefaultTTL:      15 * time.Minute,
		CleanupInterval: 5 * time.Minute,
		MaxSize:         10000,
		OnEvicted: func(key string, value interface{}) {
			log.Debug("cache item evicted", "key", key)
		},
	})
	log.Info("in-memory cache initialized")

	// ── Initialize Database ───────────────────────────────────────────────
	database, err := db.New(db.Options{
		URL:             cfg.DatabaseURL,
		MaxOpenConns:    cfg.DBMaxOpenConns,
		MaxIdleConns:    cfg.DBMaxIdleConns,
		ConnMaxLifetime: cfg.DBConnMaxLifetime,
		ConnMaxIdleTime: cfg.DBConnMaxIdleTime,
	})
	if err != nil {
		log.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer database.Close()
	log.Info("database connection established")

	// ── Initialize Apple Auth Verifier ────────────────────────────────────
	appleVerifier := auth.NewAppleVerifier(auth.AppleVerifierOptions{
		BundleID:       cfg.AppleBundleID,
		TeamID:         cfg.APNsTeamID,
		CacheDuration:  24 * time.Hour,
		RequestTimeout: 10 * time.Second,
	})
	if err := appleVerifier.RefreshKeys(context.Background()); err != nil {
		log.Warn("failed to prefetch Apple public keys", "error", err)
	} else {
		log.Info("apple public keys fetched")
	}

	// ── Initialize AI Client with Retry Logic ─────────────────────────────
	aiClient := ai.NewResilientClient(ai.ResilientClientOptions{
		APIKey:            cfg.ClaudeKey,
		Model:             cfg.ClaudeModel,
		MaxRetries:        3,
		InitialBackoff:    500 * time.Millisecond,
		MaxBackoff:        30 * time.Second,
		BackoffMultiplier: 2.0,
		Timeout:           2 * time.Minute,
		CircuitBreaker: ai.CircuitBreakerOptions{
			MaxFailures:     5,
			ResetTimeout:    60 * time.Second,
			HalfOpenMaxReqs: 2,
		},
		Logger: log,
	})
	log.Info("AI client initialized with circuit breaker")

	// ── Initialize Push Notification Service ──────────────────────────────
	var pushService *push.APNsService
	if cfg.APNsKeyPath != "" && cfg.APNsKeyID != "" && cfg.APNsTeamID != "" {
		pushService, err = push.NewAPNsService(push.APNsOptions{
			KeyPath:     cfg.APNsKeyPath,
			KeyID:       cfg.APNsKeyID,
			TeamID:      cfg.APNsTeamID,
			BundleID:    cfg.AppleBundleID,
			Production:  cfg.Environment == "production",
			Concurrency: 10,
			Logger:      log,
		})
		if err != nil {
			log.Error("failed to initialize APNs service", "error", err)
		} else {
			log.Info("APNs push notification service initialized",
				"production", cfg.Environment == "production",
			)
		}
	} else {
		log.Warn("APNs not configured, push notifications disabled")
	}

	// ── Initialize Validator ──────────────────────────────────────────────
	valid := validator.New()
	log.Info("request validator initialized")

	// ── Initialize Health Checker ─────────────────────────────────────────
	healthChecker := health.New(health.Options{
		ServiceName: "stride-backend",
		Version:     cfg.Version,
		Environment: cfg.Environment,
	})
	healthChecker.AddCheck("database", health.NewDatabaseCheck(database.Pool()))
	healthChecker.AddCheck("cache", health.NewCacheCheck(appCache))
	if pushService != nil {
		healthChecker.AddCheck("apns", health.NewAPNsCheck(pushService))
	}
	log.Info("health checker initialized")

	// ── Initialize Rate Limiter ───────────────────────────────────────────
	rateLimiter := middleware.NewRateLimiter(middleware.RateLimiterOptions{
		RequestsPerSecond: cfg.RateLimitRPS,
		BurstSize:         cfg.RateLimitBurst,
		CleanupInterval:   5 * time.Minute,
		Logger:            log,
	})
	log.Info("rate limiter initialized",
		"rps", cfg.RateLimitRPS,
		"burst", cfg.RateLimitBurst,
	)

	// ── Handler Dependencies ──────────────────────────────────────────────
	deps := handlers.Deps{
		DB:            database,
		AIClient:      aiClient,
		Cache:         appCache,
		AppleVerifier: appleVerifier,
		PushService:   pushService,
		Validator:     valid,
		JWTSecret:     []byte(cfg.JWTSecret),
		Logger:        log,
		Metrics:       metricsCollector,
		Config:        cfg,
	}

	// ── Router Setup ──────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Global middleware stack (order matters!)
	r.Use(middleware.RequestID)                                   // Add request ID first
	r.Use(middleware.StructuredLogger(log))                       // Structured logging
	r.Use(middleware.Metrics(metricsCollector))                   // Prometheus metrics
	r.Use(middleware.Recovery(log))                               // Panic recovery
	r.Use(middleware.SecurityHeaders(cfg.Environment))            // Security headers
	r.Use(chimiddleware.RealIP)                                   // Get real client IP
	r.Use(middleware.Timeout(cfg.RequestTimeout))                 // Request timeout
	r.Use(middleware.CORS(middleware.CORSOptions{                 // CORS handling
		AllowedOrigins:   cfg.CORSAllowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           86400,
	}))

	// ── Health & Metrics Endpoints (no auth, no rate limit) ───────────────
	r.Group(func(r chi.Router) {
		r.Get("/health", healthChecker.LivenessHandler())
		r.Get("/ready", healthChecker.ReadinessHandler())
		r.Get("/metrics", promhttp.Handler().ServeHTTP)
	})

	// ── API v1 Routes ─────────────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {
		// Public routes with rate limiting
		r.Group(func(r chi.Router) {
			r.Use(rateLimiter.LimitByIP(50, 100)) // 50 req/s, burst 100

			r.Post("/auth/apple", handlers.AppleSignIn(deps))
			r.Post("/auth/refresh", handlers.RefreshToken(deps))
		})

		// Protected routes
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireAuth(deps.JWTSecret, log))
			r.Use(rateLimiter.LimitByUserID(20, 50)) // 20 req/s per user

			// Onboarding
			r.Post("/onboarding/complete", handlers.OnboardingComplete(deps))

			// Profile
			r.Get("/profile", handlers.GetProfile(deps))
			r.Patch("/profile", handlers.UpdateProfile(deps))

			// Meal plans (longer timeout for AI calls)
			r.Group(func(r chi.Router) {
				r.Use(middleware.Timeout(2 * time.Minute))
				r.Get("/meals/plan", handlers.GetMealPlan(deps))
				r.Post("/meals/regenerate", handlers.RegenerateMealPlan(deps))
				r.Post("/meals/swap", handlers.SwapMeal(deps))
			})

			// Food logging
			r.Post("/log/food", handlers.LogFood(deps))
			r.Get("/log/today", handlers.GetTodayLog(deps))
			r.Delete("/log/food/{entryID}", handlers.DeleteFoodEntry(deps))
			r.Post("/log/weight", handlers.LogWeight(deps))

			// Progress
			r.Get("/progress/weekly", handlers.WeeklyProgress(deps))
			r.Get("/progress/weights", handlers.WeightHistory(deps))
			r.Get("/progress/streak", handlers.GetStreak(deps))

			// Coach
			r.Get("/coach/today", handlers.TodayCoachMessage(deps))
			r.Get("/coach/history", handlers.CoachMessageHistory(deps))

			// Subscriptions
			r.Post("/subscription/verify", handlers.VerifySubscription(deps))
			r.Get("/subscription/status", handlers.SubscriptionStatus(deps))

			// Device management
			r.Post("/device/register", handlers.RegisterDevice(deps))
			r.Delete("/device/{token}", handlers.UnregisterDevice(deps))
		})
	})

	// ── Legacy API Routes (backwards compatibility) ───────────────────────
	r.Route("/api", func(r chi.Router) {
		// Redirect old endpoints to v1
		r.HandleFunc("/*", func(w http.ResponseWriter, r *http.Request) {
			// Rewrite /api/* to /api/v1/*
			newPath := "/api/v1" + r.URL.Path[4:]
			http.Redirect(w, r, newPath, http.StatusPermanentRedirect)
		})
	})

	// ── Webhooks (server-to-server, signature verified) ───────────────────
	r.Route("/webhooks", func(r chi.Router) {
		r.Use(rateLimiter.LimitByIP(10, 20)) // Lower rate for webhooks
		r.Post("/apple/subscriptions", handlers.AppleSubscriptionWebhook(deps))
	})

	// ── Cron Jobs ─────────────────────────────────────────────────────────
	scheduler := cron.New(cron.Deps{
		DB:          database,
		AIClient:    aiClient,
		PushService: pushService,
		Logger:      log,
		Metrics:     metricsCollector,
	})
	scheduler.Start()
	defer scheduler.Stop()
	log.Info("cron scheduler started")

	// ── HTTP Server ───────────────────────────────────────────────────────
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadTimeout:       cfg.ReadTimeout,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      cfg.WriteTimeout,
		IdleTimeout:       cfg.IdleTimeout,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	// Start server in goroutine
	serverErrors := make(chan error, 1)
	go func() {
		log.Info("HTTP server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErrors <- err
		}
	}()

	// ── Graceful Shutdown ─────────────────────────────────────────────────
	shutdown := make(chan os.Signal, 1)
	signal.Notify(shutdown, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-serverErrors:
		log.Error("server error", "error", err)
		os.Exit(1)

	case sig := <-shutdown:
		log.Info("shutdown signal received", "signal", sig.String())

		// Mark as unhealthy immediately
		healthChecker.SetUnhealthy("shutting down")

		// Give load balancers time to stop sending traffic
		log.Info("waiting for load balancers to drain")
		time.Sleep(5 * time.Second)

		// Shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()

		log.Info("shutting down HTTP server")
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("HTTP server shutdown error", "error", err)
			srv.Close()
		}

		// Stop scheduler
		log.Info("stopping cron scheduler")
		scheduler.Stop()

		// Close database connections
		log.Info("closing database connections")
		database.Close()

		// Flush any remaining metrics
		log.Info("flushing metrics")
		metricsCollector.Flush()

		log.Info("shutdown complete")
	}
}
