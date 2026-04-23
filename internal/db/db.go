package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/lib/pq"

	apperrors "stride/backend/internal/errors"
	"stride/backend/internal/logger"
	"stride/backend/internal/metrics"
)

// Config holds database configuration.
type Config struct {
	URL            string
	MaxOpenConns   int
	MaxIdleConns   int
	ConnMaxLife    time.Duration
}

// DB wraps sql.DB and exposes typed query methods.
type DB struct {
	*sql.DB
	log     *logger.Logger
	metrics *metrics.Metrics
}

// New opens a Postgres connection pool.
func New(cfg Config) (*DB, error) {
	conn, err := sql.Open("postgres", cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	if cfg.MaxOpenConns > 0 {
		conn.SetMaxOpenConns(cfg.MaxOpenConns)
	} else {
		conn.SetMaxOpenConns(25)
	}

	if cfg.MaxIdleConns > 0 {
		conn.SetMaxIdleConns(cfg.MaxIdleConns)
	} else {
		conn.SetMaxIdleConns(10)
	}

	if cfg.ConnMaxLife > 0 {
		conn.SetConnMaxLifetime(cfg.ConnMaxLife)
	} else {
		conn.SetConnMaxLifetime(5 * time.Minute)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &DB{DB: conn}, nil
}

// WithLogger sets the logger for database operations.
func (db *DB) WithLogger(log *logger.Logger) *DB {
	db.log = log
	return db
}

// WithMetrics sets the metrics collector for database operations.
func (db *DB) WithMetrics(m *metrics.Metrics) *DB {
	db.metrics = m
	return db
}

// recordQuery records query timing and errors to metrics.
func (db *DB) recordQuery(operation, table string, start time.Time, err error) {
	duration := time.Since(start)
	if db.metrics != nil {
		db.metrics.RecordDBQuery(operation, table, duration, err)
	}
	if db.log != nil && err != nil {
		db.log.DBQuery(operation, table, float64(duration.Milliseconds()), err)
	}
}

// ── User queries ─────────────────────────────────────────────────────────────

func (db *DB) GetUserByAppleID(ctx context.Context, appleUserID string) (*User, error) {
	start := time.Now()
	q := `SELECT id, email, apple_user_id, created_at, last_active_at
		  FROM users WHERE apple_user_id = $1`
	u := &User{}
	err := db.QueryRowContext(ctx, q, appleUserID).Scan(
		&u.ID, &u.Email, &u.AppleUserID, &u.CreatedAt, &u.LastActiveAt,
	)
	db.recordQuery("select", "users", start, err)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return u, nil
}

func (db *DB) GetUserByID(ctx context.Context, userID string) (*User, error) {
	start := time.Now()
	q := `SELECT id, email, apple_user_id, created_at, last_active_at
		  FROM users WHERE id = $1`
	u := &User{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&u.ID, &u.Email, &u.AppleUserID, &u.CreatedAt, &u.LastActiveAt,
	)
	db.recordQuery("select", "users", start, err)
	if err == sql.ErrNoRows {
		return nil, apperrors.NewNotFoundError("user")
	}
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return u, nil
}

func (db *DB) CreateUser(ctx context.Context, email, appleUserID string) (*User, error) {
	start := time.Now()
	q := `INSERT INTO users (email, apple_user_id)
		  VALUES ($1, $2)
		  RETURNING id, email, apple_user_id, created_at, last_active_at`
	u := &User{}
	err := db.QueryRowContext(ctx, q, email, appleUserID).Scan(
		&u.ID, &u.Email, &u.AppleUserID, &u.CreatedAt, &u.LastActiveAt,
	)
	db.recordQuery("insert", "users", start, err)
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return u, nil
}

func (db *DB) TouchUser(ctx context.Context, userID string) error {
	start := time.Now()
	_, err := db.ExecContext(ctx,
		`UPDATE users SET last_active_at = NOW() WHERE id = $1`, userID)
	db.recordQuery("update", "users", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

// ── Profile queries ──────────────────────────────────────────────────────────

func (db *DB) GetProfile(ctx context.Context, userID string) (*Profile, error) {
	start := time.Now()
	q := `SELECT id, user_id, name, age, gender, height_cm,
			     current_weight_kg, goal_weight_kg, timeline_months,
			     activity_level, daily_minutes, diet_prefs, primary_goal,
			     calorie_target, protein_target_g, carbs_target_g, fat_target_g,
			     goal_date, created_at, updated_at
		  FROM user_profiles WHERE user_id = $1`
	p := &Profile{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&p.ID, &p.UserID, &p.Name, &p.Age, &p.Gender, &p.HeightCm,
		&p.CurrentWeightKg, &p.GoalWeightKg, &p.TimelineMonths,
		&p.ActivityLevel, &p.DailyMinutes, &p.DietPrefs, &p.PrimaryGoal,
		&p.CalorieTarget, &p.ProteinTargetG, &p.CarbsTargetG, &p.FatTargetG,
		&p.GoalDate, &p.CreatedAt, &p.UpdatedAt,
	)
	db.recordQuery("select", "user_profiles", start, err)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return p, nil
}

func (db *DB) UpsertProfile(ctx context.Context, p *Profile) error {
	start := time.Now()
	q := `INSERT INTO user_profiles
			(user_id, name, age, gender, height_cm, current_weight_kg, goal_weight_kg,
			 timeline_months, activity_level, daily_minutes, diet_prefs, primary_goal,
			 calorie_target, protein_target_g, carbs_target_g, fat_target_g, goal_date)
		  VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		  ON CONFLICT (user_id) DO UPDATE SET
			name = EXCLUDED.name,
			current_weight_kg = EXCLUDED.current_weight_kg,
			goal_weight_kg = EXCLUDED.goal_weight_kg,
			timeline_months = EXCLUDED.timeline_months,
			activity_level = EXCLUDED.activity_level,
			daily_minutes = EXCLUDED.daily_minutes,
			diet_prefs = EXCLUDED.diet_prefs,
			calorie_target = EXCLUDED.calorie_target,
			protein_target_g = EXCLUDED.protein_target_g,
			carbs_target_g = EXCLUDED.carbs_target_g,
			fat_target_g = EXCLUDED.fat_target_g,
			goal_date = EXCLUDED.goal_date,
			updated_at = NOW()`
	_, err := db.ExecContext(ctx, q,
		p.UserID, p.Name, p.Age, p.Gender, p.HeightCm,
		p.CurrentWeightKg, p.GoalWeightKg, p.TimelineMonths,
		p.ActivityLevel, p.DailyMinutes, p.DietPrefs, p.PrimaryGoal,
		p.CalorieTarget, p.ProteinTargetG, p.CarbsTargetG, p.FatTargetG, p.GoalDate,
	)
	db.recordQuery("upsert", "user_profiles", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

// ── Meal plan queries ────────────────────────────────────────────────────────

func (db *DB) GetActiveMealPlan(ctx context.Context, userID string) (*MealPlan, error) {
	start := time.Now()
	q := `SELECT id, user_id, week_label, week_start_date, days, avg_daily_calories, generated_at
		  FROM meal_plans
		  WHERE user_id = $1 AND is_active = true
		  ORDER BY week_start_date DESC LIMIT 1`
	m := &MealPlan{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&m.ID, &m.UserID, &m.WeekLabel, &m.WeekStartDate,
		&m.DaysJSON, &m.AvgDailyCalories, &m.GeneratedAt,
	)
	db.recordQuery("select", "meal_plans", start, err)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return m, nil
}

func (db *DB) SaveMealPlan(ctx context.Context, m *MealPlan) error {
	start := time.Now()
	// Deactivate any existing plan for this week first
	_, err := db.ExecContext(ctx,
		`UPDATE meal_plans SET is_active = false
		 WHERE user_id = $1 AND week_start_date = $2`,
		m.UserID, m.WeekStartDate,
	)
	if err != nil {
		db.recordQuery("update", "meal_plans", start, err)
		return apperrors.NewDatabaseError(err)
	}

	q := `INSERT INTO meal_plans (user_id, week_label, week_start_date, days, avg_daily_calories)
		  VALUES ($1, $2, $3, $4, $5)
		  RETURNING id, generated_at`
	err = db.QueryRowContext(ctx, q,
		m.UserID, m.WeekLabel, m.WeekStartDate, m.DaysJSON, m.AvgDailyCalories,
	).Scan(&m.ID, &m.GeneratedAt)
	db.recordQuery("insert", "meal_plans", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

func (db *DB) SaveMealSwap(ctx context.Context, s *MealSwap) error {
	start := time.Now()
	q := `INSERT INTO meal_swaps
			(user_id, meal_plan_id, day, meal_type, original_meal, swapped_to_meal, filter_used)
		  VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := db.ExecContext(ctx, q,
		s.UserID, s.MealPlanID, s.Day, s.MealType,
		s.OriginalMealJSON, s.SwappedToMealJSON, s.FilterUsed,
	)
	db.recordQuery("insert", "meal_swaps", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

// ── Daily log queries ────────────────────────────────────────────────────────

func (db *DB) GetTodayLog(ctx context.Context, userID string) (*DailyLog, error) {
	start := time.Now()
	q := `SELECT id, user_id, log_date, calories_eaten, protein_g, carbs_g, fat_g,
			     weight_kg, on_plan, streak_day, notes
		  FROM daily_logs
		  WHERE user_id = $1 AND log_date = CURRENT_DATE`
	l := &DailyLog{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&l.ID, &l.UserID, &l.LogDate, &l.CaloriesEaten,
		&l.ProteinG, &l.CarbsG, &l.FatG,
		&l.WeightKg, &l.OnPlan, &l.StreakDay, &l.Notes,
	)
	db.recordQuery("select", "daily_logs", start, err)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return l, nil
}

func (db *DB) UpsertDailyLog(ctx context.Context, l *DailyLog) error {
	start := time.Now()
	q := `INSERT INTO daily_logs (user_id, log_date, calories_eaten, protein_g, carbs_g, fat_g, on_plan, streak_day)
		  VALUES ($1, CURRENT_DATE, $2, $3, $4, $5, $6, $7)
		  ON CONFLICT (user_id, log_date) DO UPDATE SET
			calories_eaten = EXCLUDED.calories_eaten,
			protein_g      = EXCLUDED.protein_g,
			carbs_g        = EXCLUDED.carbs_g,
			fat_g          = EXCLUDED.fat_g,
			on_plan        = EXCLUDED.on_plan,
			updated_at     = NOW()
		  RETURNING id`
	err := db.QueryRowContext(ctx, q,
		l.UserID, l.CaloriesEaten, l.ProteinG, l.CarbsG, l.FatG,
		l.OnPlan, l.StreakDay,
	).Scan(&l.ID)
	db.recordQuery("upsert", "daily_logs", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

func (db *DB) AddFoodEntry(ctx context.Context, e *FoodEntry) error {
	start := time.Now()
	q := `INSERT INTO food_entries
			(user_id, daily_log_id, log_date, meal_type, food_name, calories,
			 protein_g, carbs_g, fat_g, serving_size, log_method, barcode)
		  VALUES ($1,$2,CURRENT_DATE,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		  RETURNING id`
	err := db.QueryRowContext(ctx, q,
		e.UserID, e.DailyLogID, e.MealType, e.FoodName, e.Calories,
		e.ProteinG, e.CarbsG, e.FatG, e.ServingSize, e.LogMethod, e.Barcode,
	).Scan(&e.ID)
	db.recordQuery("insert", "food_entries", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

func (db *DB) GetTodayFoodEntries(ctx context.Context, userID string) ([]*FoodEntry, error) {
	start := time.Now()
	q := `SELECT id, meal_type, food_name, calories, protein_g, carbs_g, fat_g,
			     serving_size, log_method, logged_at
		  FROM food_entries
		  WHERE user_id = $1 AND log_date = CURRENT_DATE
		  ORDER BY logged_at ASC`
	rows, err := db.QueryContext(ctx, q, userID)
	if err != nil {
		db.recordQuery("select", "food_entries", start, err)
		return nil, apperrors.NewDatabaseError(err)
	}
	defer rows.Close()

	var entries []*FoodEntry
	for rows.Next() {
		e := &FoodEntry{}
		if err := rows.Scan(
			&e.ID, &e.MealType, &e.FoodName, &e.Calories,
			&e.ProteinG, &e.CarbsG, &e.FatG,
			&e.ServingSize, &e.LogMethod, &e.LoggedAt,
		); err != nil {
			db.recordQuery("select", "food_entries", start, err)
			return nil, apperrors.NewDatabaseError(err)
		}
		entries = append(entries, e)
	}
	db.recordQuery("select", "food_entries", start, rows.Err())
	return entries, rows.Err()
}

// ── Weight queries ───────────────────────────────────────────────────────────

func (db *DB) LogWeight(ctx context.Context, userID string, weightKg float64, note string) error {
	start := time.Now()
	_, err := db.ExecContext(ctx,
		`INSERT INTO weight_logs (user_id, weight_kg, note) VALUES ($1, $2, $3)`,
		userID, weightKg, note,
	)
	db.recordQuery("insert", "weight_logs", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

func (db *DB) GetWeightHistory(ctx context.Context, userID string, days int) ([]*WeightEntry, error) {
	start := time.Now()
	q := `SELECT weight_kg, logged_at FROM weight_logs
		  WHERE user_id = $1 AND logged_at >= NOW() - ($2 || ' days')::INTERVAL
		  ORDER BY logged_at ASC`
	rows, err := db.QueryContext(ctx, q, userID, days)
	if err != nil {
		db.recordQuery("select", "weight_logs", start, err)
		return nil, apperrors.NewDatabaseError(err)
	}
	defer rows.Close()

	var entries []*WeightEntry
	for rows.Next() {
		e := &WeightEntry{}
		if err := rows.Scan(&e.WeightKg, &e.LoggedAt); err != nil {
			db.recordQuery("select", "weight_logs", start, err)
			return nil, apperrors.NewDatabaseError(err)
		}
		entries = append(entries, e)
	}
	db.recordQuery("select", "weight_logs", start, rows.Err())
	return entries, rows.Err()
}

// ── Coach message queries ────────────────────────────────────────────────────

func (db *DB) GetTodayCoachMessage(ctx context.Context, userID string) (*CoachMessage, error) {
	start := time.Now()
	q := `SELECT id, message, tip, priority_meal, tone, read_at
		  FROM coach_messages
		  WHERE user_id = $1 AND message_date = CURRENT_DATE`
	m := &CoachMessage{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&m.ID, &m.Message, &m.Tip, &m.PriorityMeal, &m.Tone, &m.ReadAt,
	)
	db.recordQuery("select", "coach_messages", start, err)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	// Mark as read
	db.ExecContext(ctx,
		`UPDATE coach_messages SET read_at = NOW()
		 WHERE user_id = $1 AND message_date = CURRENT_DATE AND read_at IS NULL`,
		userID,
	)
	return m, nil
}

func (db *DB) SaveCoachMessage(ctx context.Context, m *CoachMessage) error {
	start := time.Now()
	q := `INSERT INTO coach_messages (user_id, message_date, message, tip, priority_meal, tone)
		  VALUES ($1, CURRENT_DATE, $2, $3, $4, $5)
		  ON CONFLICT (user_id, message_date) DO UPDATE SET
			message = EXCLUDED.message, tip = EXCLUDED.tip,
			priority_meal = EXCLUDED.priority_meal, tone = EXCLUDED.tone
		  RETURNING id`
	err := db.QueryRowContext(ctx, q,
		m.UserID, m.Message, m.Tip, m.PriorityMeal, m.Tone,
	).Scan(&m.ID)
	db.recordQuery("upsert", "coach_messages", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

func (db *DB) GetYesterdayStats(ctx context.Context, userID string) (*YesterdayStats, error) {
	start := time.Now()
	q := `SELECT
			COALESCE(dl.calories_eaten, 0),
			COALESCE(dl.streak_day, 0),
			COALESCE(
				(SELECT SUM(start_weight - current_weight_kg)
				 FROM user_profiles WHERE user_id = $1), 0
			)
		  FROM user_profiles up
		  LEFT JOIN daily_logs dl
			ON dl.user_id = $1 AND dl.log_date = CURRENT_DATE - INTERVAL '1 day'
		  WHERE up.user_id = $1`
	s := &YesterdayStats{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&s.CaloriesEaten, &s.CurrentStreakDays, &s.TotalLostKg,
	)
	db.recordQuery("select", "daily_logs", start, err)
	if err != nil && err != sql.ErrNoRows {
		return nil, apperrors.NewDatabaseError(err)
	}
	return s, nil
}

// ── Progress queries ─────────────────────────────────────────────────────────

func (db *DB) GetWeeklySummary(ctx context.Context, userID string) (*WeeklySummary, error) {
	start := time.Now()
	q := `SELECT
			COALESCE(ROUND(AVG(calories_eaten))::int, 0),
			COALESCE(ROUND(AVG(protein_g)::numeric, 1), 0),
			COUNT(*) FILTER (WHERE on_plan),
			COUNT(*),
			COALESCE(MAX(streak_day), 0)
		  FROM daily_logs
		  WHERE user_id = $1
		    AND log_date >= DATE_TRUNC('week', CURRENT_DATE)`
	s := &WeeklySummary{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&s.AvgCalories, &s.AvgProteinG,
		&s.DaysOnPlan, &s.DaysLogged, &s.BestStreak,
	)
	db.recordQuery("select", "daily_logs", start, err)
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return s, nil
}

// ── Subscription queries ─────────────────────────────────────────────────────

func (db *DB) GetSubscription(ctx context.Context, userID string) (*Subscription, error) {
	start := time.Now()
	q := `SELECT id, plan, status, trial_ends_at, current_period_ends_at
		  FROM subscriptions WHERE user_id = $1`
	s := &Subscription{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&s.ID, &s.Plan, &s.Status, &s.TrialEndsAt, &s.CurrentPeriodEndsAt,
	)
	db.recordQuery("select", "subscriptions", start, err)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, apperrors.NewDatabaseError(err)
	}
	return s, nil
}

func (db *DB) UpsertSubscription(ctx context.Context, s *Subscription) error {
	start := time.Now()
	q := `INSERT INTO subscriptions
			(user_id, plan, status, apple_original_tx_id, trial_ends_at, current_period_ends_at)
		  VALUES ($1, $2, $3, $4, $5, $6)
		  ON CONFLICT (user_id) DO UPDATE SET
			plan = EXCLUDED.plan,
			status = EXCLUDED.status,
			current_period_ends_at = EXCLUDED.current_period_ends_at,
			updated_at = NOW()`
	_, err := db.ExecContext(ctx, q,
		s.UserID, s.Plan, s.Status, s.AppleOriginalTxID,
		s.TrialEndsAt, s.CurrentPeriodEndsAt,
	)
	db.recordQuery("upsert", "subscriptions", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

// IsSubscribed checks if a user has an active subscription.
func (db *DB) IsSubscribed(ctx context.Context, userID string) (bool, error) {
	start := time.Now()
	q := `SELECT EXISTS(
		SELECT 1 FROM subscriptions 
		WHERE user_id = $1 
		AND status IN ('active', 'free_trial', 'grace_period')
		AND (current_period_ends_at IS NULL OR current_period_ends_at > NOW())
	)`
	var exists bool
	err := db.QueryRowContext(ctx, q, userID).Scan(&exists)
	db.recordQuery("select", "subscriptions", start, err)
	if err != nil {
		return false, apperrors.NewDatabaseError(err)
	}
	return exists, nil
}

// ── Device token queries ─────────────────────────────────────────────────────

func (db *DB) UpsertDeviceToken(ctx context.Context, userID, token, deviceName string) error {
	start := time.Now()
	q := `INSERT INTO device_tokens (user_id, token, device_name)
		  VALUES ($1, $2, $3)
		  ON CONFLICT (token) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			last_used_at = NOW()`
	_, err := db.ExecContext(ctx, q, userID, token, deviceName)
	db.recordQuery("upsert", "device_tokens", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

func (db *DB) GetDeviceTokens(ctx context.Context, userID string) ([]string, error) {
	start := time.Now()
	rows, err := db.QueryContext(ctx,
		`SELECT token FROM device_tokens WHERE user_id = $1`, userID)
	if err != nil {
		db.recordQuery("select", "device_tokens", start, err)
		return nil, apperrors.NewDatabaseError(err)
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tokens = append(tokens, t)
	}
	db.recordQuery("select", "device_tokens", start, rows.Err())
	return tokens, rows.Err()
}

func (db *DB) DeleteDeviceToken(ctx context.Context, token string) error {
	start := time.Now()
	_, err := db.ExecContext(ctx, `DELETE FROM device_tokens WHERE token = $1`, token)
	db.recordQuery("delete", "device_tokens", start, err)
	if err != nil {
		return apperrors.NewDatabaseError(err)
	}
	return nil
}

func (db *DB) GetAllActiveUserIDs(ctx context.Context) ([]string, error) {
	start := time.Now()
	rows, err := db.QueryContext(ctx,
		`SELECT u.id FROM users u
		 JOIN subscriptions s ON s.user_id = u.id
		 WHERE u.is_active = true AND s.status IN ('active', 'free_trial', 'grace_period')`)
	if err != nil {
		db.recordQuery("select", "users", start, err)
		return nil, apperrors.NewDatabaseError(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	db.recordQuery("select", "users", start, rows.Err())
	return ids, rows.Err()
}

// ── Pagination helpers ──────────────────────────────────────────────────────

// PaginationParams holds pagination parameters.
type PaginationParams struct {
	Cursor string
	Limit  int
}

// PaginationResult holds pagination result metadata.
type PaginationResult struct {
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore    bool   `json:"has_more"`
}

// DefaultLimit is the default pagination limit.
const DefaultLimit = 20

// MaxLimit is the maximum pagination limit.
const MaxLimit = 100

// NormalizeLimit ensures the limit is within bounds.
func NormalizeLimit(limit int) int {
	if limit <= 0 {
		return DefaultLimit
	}
	if limit > MaxLimit {
		return MaxLimit
	}
	return limit
}

// GetFoodEntriesPaginated returns food entries with pagination.
func (db *DB) GetFoodEntriesPaginated(ctx context.Context, userID string, params PaginationParams) ([]*FoodEntry, PaginationResult, error) {
	start := time.Now()
	limit := NormalizeLimit(params.Limit)
	
	q := `SELECT id, meal_type, food_name, calories, protein_g, carbs_g, fat_g,
			     serving_size, log_method, logged_at
		  FROM food_entries
		  WHERE user_id = $1
		  ORDER BY logged_at DESC, id DESC
		  LIMIT $2`
	
	rows, err := db.QueryContext(ctx, q, userID, limit+1)
	if err != nil {
		db.recordQuery("select", "food_entries", start, err)
		return nil, PaginationResult{}, apperrors.NewDatabaseError(err)
	}
	defer rows.Close()

	var entries []*FoodEntry
	for rows.Next() {
		e := &FoodEntry{}
		if err := rows.Scan(
			&e.ID, &e.MealType, &e.FoodName, &e.Calories,
			&e.ProteinG, &e.CarbsG, &e.FatG,
			&e.ServingSize, &e.LogMethod, &e.LoggedAt,
		); err != nil {
			db.recordQuery("select", "food_entries", start, err)
			return nil, PaginationResult{}, apperrors.NewDatabaseError(err)
		}
		entries = append(entries, e)
	}
	db.recordQuery("select", "food_entries", start, rows.Err())

	result := PaginationResult{}
	if len(entries) > limit {
		result.HasMore = true
		result.NextCursor = entries[limit-1].ID
		entries = entries[:limit]
	}

	return entries, result, nil
}
