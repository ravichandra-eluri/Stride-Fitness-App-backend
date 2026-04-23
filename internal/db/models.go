package db

import (
	"database/sql"
	"time"
)

type User struct {
	ID           string
	Email        string
	AppleUserID  string
	CreatedAt    time.Time
	LastActiveAt time.Time
}

type Profile struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	Name            string    `json:"name"`
	Age             int       `json:"age"`
	Gender          string    `json:"gender"`
	HeightCm        int       `json:"height_cm"`
	CurrentWeightKg float64   `json:"current_weight_kg"`
	GoalWeightKg    float64   `json:"goal_weight_kg"`
	TimelineMonths  int       `json:"timeline_months"`
	ActivityLevel   string    `json:"activity_level"`
	DailyMinutes    int       `json:"daily_minutes"`
	DietPrefs       []string  `json:"diet_prefs"`
	PrimaryGoal     string    `json:"primary_goal"`
	CalorieTarget   int       `json:"calorie_target"`
	ProteinTargetG  int       `json:"protein_target_g"`
	CarbsTargetG    int       `json:"carbs_target_g"`
	FatTargetG      int       `json:"fat_target_g"`
	GoalDate        string    `json:"goal_date"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type MealPlan struct {
	ID               string    `json:"id"`
	UserID           string    `json:"user_id"`
	WeekLabel        string    `json:"week"`
	WeekStartDate    time.Time `json:"week_start_date"`
	DaysJSON         []byte    `json:"-"` // marshalled separately — see GetMealPlan handler
	AvgDailyCalories int       `json:"avg_daily_calories"`
	GeneratedAt      time.Time `json:"generated_at"`
}

type MealSwap struct {
	ID                string
	UserID            string
	MealPlanID        string
	Day               string
	MealType          string
	OriginalMealJSON  []byte
	SwappedToMealJSON []byte
	FilterUsed        string
}

type DailyLog struct {
	ID            string          `json:"id"`
	UserID        string          `json:"user_id"`
	LogDate       time.Time       `json:"log_date"`
	CaloriesEaten int             `json:"calories_eaten"`
	ProteinG      float64         `json:"protein_g"`
	CarbsG        float64         `json:"carbs_g"`
	FatG          float64         `json:"fat_g"`
	WeightKg      sql.NullFloat64 `json:"weight_kg,omitempty"`
	OnPlan        bool            `json:"on_plan"`
	StreakDay     int             `json:"streak_day"`
	Notes         sql.NullString  `json:"notes,omitempty"`
}

type FoodEntry struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	DailyLogID  string    `json:"daily_log_id"`
	MealType    string    `json:"meal_type"`
	FoodName    string    `json:"food_name"`
	Calories    int       `json:"calories"`
	ProteinG    float64   `json:"protein_g"`
	CarbsG      float64   `json:"carbs_g"`
	FatG        float64   `json:"fat_g"`
	ServingSize string    `json:"serving_size"`
	LogMethod   string    `json:"log_method"`
	Barcode     string    `json:"barcode,omitempty"`
	LoggedAt    time.Time `json:"logged_at"`
}

type WeightEntry struct {
	WeightKg float64   `json:"weight_kg"`
	LoggedAt time.Time `json:"logged_at"`
}

type CoachMessage struct {
	ID           string       `json:"id"`
	UserID       string       `json:"user_id"`
	Message      string       `json:"message"`
	Tip          string       `json:"tip"`
	PriorityMeal string       `json:"priority_meal,omitempty"`
	Tone         string       `json:"tone"`
	ReadAt       sql.NullTime `json:"read_at,omitempty"`
}

type YesterdayStats struct {
	CaloriesEaten     int     `json:"calories_eaten"`
	CalorieTarget     int     `json:"calorie_target"`
	CurrentStreakDays int     `json:"current_streak_days"`
	TotalLostKg       float64 `json:"total_lost_kg"`
}

type WeeklySummary struct {
	AvgCalories int     `json:"avg_calories"`
	AvgProteinG float64 `json:"avg_protein_g"`
	DaysOnPlan  int     `json:"days_on_plan"`
	DaysLogged  int     `json:"days_logged"`
	BestStreak  int     `json:"best_streak"`
}

type Subscription struct {
	ID                  string
	UserID              string
	Plan                string
	Status              string
	AppleOriginalTxID   string
	TrialEndsAt         sql.NullTime
	CurrentPeriodEndsAt sql.NullTime
	CancelledAt         sql.NullTime
}
