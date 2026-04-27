package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/lib/pq"
)

// DB wraps sql.DB and exposes typed query methods.
type DB struct {
	*sql.DB
}

// New opens a Postgres connection pool.
func New(databaseURL string) (*DB, error) {
	conn, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}

	conn.SetMaxOpenConns(25)
	conn.SetMaxIdleConns(10)
	conn.SetConnMaxLifetime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := conn.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return &DB{conn}, nil
}

// ── User queries ─────────────────────────────────────────────────────────────

func (db *DB) GetUserByAppleID(ctx context.Context, appleUserID string) (*User, error) {
	q := `SELECT id, email, apple_user_id, created_at, last_active_at
		  FROM users WHERE apple_user_id = $1`
	u := &User{}
	err := db.QueryRowContext(ctx, q, appleUserID).Scan(
		&u.ID, &u.Email, &u.AppleUserID, &u.CreatedAt, &u.LastActiveAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return u, err
}

func (db *DB) CreateUser(ctx context.Context, email, appleUserID string) (*User, error) {
	q := `INSERT INTO users (email, apple_user_id)
		  VALUES ($1, $2)
		  RETURNING id, email, apple_user_id, created_at, last_active_at`
	u := &User{}
	err := db.QueryRowContext(ctx, q, email, appleUserID).Scan(
		&u.ID, &u.Email, &u.AppleUserID, &u.CreatedAt, &u.LastActiveAt,
	)
	return u, err
}

func (db *DB) TouchUser(ctx context.Context, userID string) error {
	_, err := db.ExecContext(ctx,
		`UPDATE users SET last_active_at = NOW() WHERE id = $1`, userID)
	return err
}

// ── Profile queries ──────────────────────────────────────────────────────────

func (db *DB) GetProfile(ctx context.Context, userID string) (*Profile, error) {
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
		&p.ActivityLevel, &p.DailyMinutes, pq.Array(&p.DietPrefs), &p.PrimaryGoal,
		&p.CalorieTarget, &p.ProteinTargetG, &p.CarbsTargetG, &p.FatTargetG,
		&p.GoalDate, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return p, err
}

func (db *DB) UpsertProfile(ctx context.Context, p *Profile) error {
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
		p.ActivityLevel, p.DailyMinutes, pq.Array(p.DietPrefs), p.PrimaryGoal,
		p.CalorieTarget, p.ProteinTargetG, p.CarbsTargetG, p.FatTargetG, p.GoalDate,
	)
	return err
}

// ── Meal plan queries ────────────────────────────────────────────────────────

func (db *DB) GetActiveMealPlan(ctx context.Context, userID string) (*MealPlan, error) {
	q := `SELECT id, user_id, week_label, week_start_date, days, avg_daily_calories, generated_at
		  FROM meal_plans
		  WHERE user_id = $1 AND is_active = true
		  ORDER BY week_start_date DESC LIMIT 1`
	m := &MealPlan{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&m.ID, &m.UserID, &m.WeekLabel, &m.WeekStartDate,
		&m.DaysJSON, &m.AvgDailyCalories, &m.GeneratedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return m, err
}

func (db *DB) SaveMealPlan(ctx context.Context, m *MealPlan) error {
	// Deactivate any existing plan for this week first
	_, err := db.ExecContext(ctx,
		`UPDATE meal_plans SET is_active = false
		 WHERE user_id = $1 AND week_start_date = $2`,
		m.UserID, m.WeekStartDate,
	)
	if err != nil {
		return err
	}

	q := `INSERT INTO meal_plans (user_id, week_label, week_start_date, days, avg_daily_calories)
		  VALUES ($1, $2, $3, $4, $5)
		  RETURNING id, generated_at`
	return db.QueryRowContext(ctx, q,
		m.UserID, m.WeekLabel, m.WeekStartDate, m.DaysJSON, m.AvgDailyCalories,
	).Scan(&m.ID, &m.GeneratedAt)
}

func (db *DB) SaveMealSwap(ctx context.Context, s *MealSwap) error {
	q := `INSERT INTO meal_swaps
			(user_id, meal_plan_id, day, meal_type, original_meal, swapped_to_meal, filter_used)
		  VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := db.ExecContext(ctx, q,
		s.UserID, s.MealPlanID, s.Day, s.MealType,
		s.OriginalMealJSON, s.SwappedToMealJSON, s.FilterUsed,
	)
	return err
}

// ── Daily log queries ────────────────────────────────────────────────────────

func (db *DB) GetTodayLog(ctx context.Context, userID string, localDate string) (*DailyLog, error) {
	dateExpr := "CURRENT_DATE"
	var args []any
	args = append(args, userID)
	if localDate != "" {
		dateExpr = "$2"
		args = append(args, localDate)
	}
	q := `SELECT id, user_id, log_date, calories_eaten, protein_g, carbs_g, fat_g,
			     weight_kg, on_plan, streak_day, notes
		  FROM daily_logs
		  WHERE user_id = $1 AND log_date = ` + dateExpr
	l := &DailyLog{}
	err := db.QueryRowContext(ctx, q, args...).Scan(
		&l.ID, &l.UserID, &l.LogDate, &l.CaloriesEaten,
		&l.ProteinG, &l.CarbsG, &l.FatG,
		&l.WeightKg, &l.OnPlan, &l.StreakDay, &l.Notes,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

func (db *DB) UpsertDailyLog(ctx context.Context, l *DailyLog, localDate string) error {
	dateExpr := "CURRENT_DATE"
	args := []any{l.UserID, l.CaloriesEaten, l.ProteinG, l.CarbsG, l.FatG, l.OnPlan, l.StreakDay}
	if localDate != "" {
		dateExpr = "$8"
		args = append(args, localDate)
	}
	q := `INSERT INTO daily_logs (user_id, log_date, calories_eaten, protein_g, carbs_g, fat_g, on_plan, streak_day)
		  VALUES ($1, ` + dateExpr + `, $2, $3, $4, $5, $6, $7)
		  ON CONFLICT (user_id, log_date) DO UPDATE SET
			calories_eaten = EXCLUDED.calories_eaten,
			protein_g      = EXCLUDED.protein_g,
			carbs_g        = EXCLUDED.carbs_g,
			fat_g          = EXCLUDED.fat_g,
			on_plan        = EXCLUDED.on_plan,
			updated_at     = NOW()
		  RETURNING id`
	return db.QueryRowContext(ctx, q, args...).Scan(&l.ID)
}

func (db *DB) AddFoodEntry(ctx context.Context, e *FoodEntry, localDate string) error {
	dateExpr := "CURRENT_DATE"
	args := []any{e.UserID, e.DailyLogID, e.MealType, e.FoodName, e.Calories, e.ProteinG, e.CarbsG, e.FatG, e.ServingSize, e.LogMethod, e.Barcode}
	if localDate != "" {
		dateExpr = "$12"
		args = append(args, localDate)
	}
	q := `INSERT INTO food_entries
			(user_id, daily_log_id, log_date, meal_type, food_name, calories,
			 protein_g, carbs_g, fat_g, serving_size, log_method, barcode)
		  VALUES ($1,$2,` + dateExpr + `,$3,$4,$5,$6,$7,$8,$9,$10,$11)
		  RETURNING id`
	return db.QueryRowContext(ctx, q, args...).Scan(&e.ID)
}

func (db *DB) GetTodayFoodEntries(ctx context.Context, userID string, localDate string) ([]*FoodEntry, error) {
	dateExpr := "CURRENT_DATE"
	args := []any{userID}
	if localDate != "" {
		dateExpr = "$2"
		args = append(args, localDate)
	}
	q := `SELECT id, meal_type, food_name, calories, protein_g, carbs_g, fat_g,
			     serving_size, log_method, logged_at
		  FROM food_entries
		  WHERE user_id = $1 AND log_date = ` + dateExpr + `
		  ORDER BY logged_at ASC`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
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
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ── Weight queries ───────────────────────────────────────────────────────────

func (db *DB) LogWeight(ctx context.Context, userID string, weightKg float64, note string) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO weight_logs (user_id, weight_kg, note) VALUES ($1, $2, $3)`,
		userID, weightKg, note,
	)
	return err
}

func (db *DB) GetWeightHistory(ctx context.Context, userID string, days int) ([]*WeightEntry, error) {
	q := `SELECT weight_kg, logged_at FROM weight_logs
		  WHERE user_id = $1 AND logged_at >= NOW() - ($2 || ' days')::INTERVAL
		  ORDER BY logged_at ASC`
	rows, err := db.QueryContext(ctx, q, userID, days)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*WeightEntry
	for rows.Next() {
		e := &WeightEntry{}
		if err := rows.Scan(&e.WeightKg, &e.LoggedAt); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// ── Coach message queries ────────────────────────────────────────────────────

func (db *DB) GetTodayCoachMessage(ctx context.Context, userID string) (*CoachMessage, error) {
	q := `SELECT id, message, tip, priority_meal, tone, read_at
		  FROM coach_messages
		  WHERE user_id = $1 AND message_date = CURRENT_DATE`
	m := &CoachMessage{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&m.ID, &m.Message, &m.Tip, &m.PriorityMeal, &m.Tone, &m.ReadAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	// Mark as read
	db.ExecContext(ctx,
		`UPDATE coach_messages SET read_at = NOW()
		 WHERE user_id = $1 AND message_date = CURRENT_DATE AND read_at IS NULL`,
		userID,
	)
	return m, err
}

func (db *DB) SaveCoachMessage(ctx context.Context, m *CoachMessage) error {
	q := `INSERT INTO coach_messages (user_id, message_date, message, tip, priority_meal, tone)
		  VALUES ($1, CURRENT_DATE, $2, $3, $4, $5)
		  ON CONFLICT (user_id, message_date) DO UPDATE SET
			message = EXCLUDED.message, tip = EXCLUDED.tip,
			priority_meal = EXCLUDED.priority_meal, tone = EXCLUDED.tone
		  RETURNING id`
	return db.QueryRowContext(ctx, q,
		m.UserID, m.Message, m.Tip, m.PriorityMeal, m.Tone,
	).Scan(&m.ID)
}

func (db *DB) GetYesterdayStats(ctx context.Context, userID string) (*YesterdayStats, error) {
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
	return s, err
}

// ── Progress queries ─────────────────────────────────────────────────────────

func (db *DB) GetWeeklySummary(ctx context.Context, userID string) (*WeeklySummary, error) {
	q := `SELECT
			ROUND(AVG(calories_eaten))::int,
			ROUND(AVG(protein_g)::numeric, 1),
			COUNT(*) FILTER (WHERE on_plan),
			COUNT(*),
			MAX(streak_day)
		  FROM daily_logs
		  WHERE user_id = $1
		    AND log_date >= DATE_TRUNC('week', CURRENT_DATE)`
	s := &WeeklySummary{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&s.AvgCalories, &s.AvgProteinG,
		&s.DaysOnPlan, &s.DaysLogged, &s.BestStreak,
	)
	return s, err
}

// ── Subscription queries ─────────────────────────────────────────────────────

func (db *DB) GetSubscription(ctx context.Context, userID string) (*Subscription, error) {
	q := `SELECT id, plan, status, trial_ends_at, current_period_ends_at
		  FROM subscriptions WHERE user_id = $1`
	s := &Subscription{}
	err := db.QueryRowContext(ctx, q, userID).Scan(
		&s.ID, &s.Plan, &s.Status, &s.TrialEndsAt, &s.CurrentPeriodEndsAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return s, err
}

func (db *DB) UpsertSubscription(ctx context.Context, s *Subscription) error {
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
	return err
}

// ── Device token queries ─────────────────────────────────────────────────────

func (db *DB) UpsertDeviceToken(ctx context.Context, userID, token, deviceName string) error {
	q := `INSERT INTO device_tokens (user_id, token, device_name)
		  VALUES ($1, $2, $3)
		  ON CONFLICT (token) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			last_used_at = NOW()`
	_, err := db.ExecContext(ctx, q, userID, token, deviceName)
	return err
}

func (db *DB) GetDeviceTokens(ctx context.Context, userID string) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT token FROM device_tokens WHERE user_id = $1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tokens []string
	for rows.Next() {
		var t string
		rows.Scan(&t)
		tokens = append(tokens, t)
	}
	return tokens, rows.Err()
}

func (db *DB) DeleteFoodEntry(ctx context.Context, userID, entryID string) error {
	_, err := db.ExecContext(ctx,
		`DELETE FROM food_entries WHERE id = $1 AND user_id = $2`,
		entryID, userID,
	)
	return err
}

// DeleteUser hard-deletes all data for a user (cascades to child tables that
// have ON DELETE CASCADE, and explicitly removes the rest).
func (db *DB) DeleteUser(ctx context.Context, userID string) error {
	tables := []string{
		"food_entries", "daily_logs", "weight_logs",
		"coach_messages", "meal_swaps", "meal_plans",
		"device_tokens", "subscriptions", "user_profiles",
	}
	for _, tbl := range tables {
		if _, err := db.ExecContext(ctx,
			`DELETE FROM `+tbl+` WHERE user_id = $1`, userID,
		); err != nil {
			return fmt.Errorf("delete %s: %w", tbl, err)
		}
	}
	_, err := db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return err
}

func (db *DB) GetAllActiveUserIDs(ctx context.Context) ([]string, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT u.id FROM users u
		 JOIN subscriptions s ON s.user_id = u.id
		 WHERE u.is_active = true AND s.status IN ('active', 'free_trial', 'grace_period')`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
