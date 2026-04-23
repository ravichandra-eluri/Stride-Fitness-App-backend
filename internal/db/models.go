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
	ID               string
	UserID           string
	WeekLabel        string
	WeekStartDate    time.Time
	DaysJSON         []byte // JSONB — marshalled WeeklyMealPlanResponse.Days
	AvgDailyCalories int
	GeneratedAt      time.Time
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
	ID            string
	UserID        string
	LogDate       time.Time
	CaloriesEaten int
	ProteinG      float64
	CarbsG        float64
	FatG          float64
	WeightKg      sql.NullFloat64
	OnPlan        bool
	StreakDay     int
	Notes         sql.NullString
}

type FoodEntry struct {
	ID          string
	UserID      string
	DailyLogID  string
	MealType    string
	FoodName    string
	Calories    int
	ProteinG    float64
	CarbsG      float64
	FatG        float64
	ServingSize string
	LogMethod   string
	Barcode     string
	LoggedAt    time.Time
}

type WeightEntry struct {
	WeightKg float64
	LoggedAt time.Time
}

type CoachMessage struct {
	ID           string
	UserID       string
	Message      string
	Tip          string
	PriorityMeal string
	Tone         string
	ReadAt       sql.NullTime
}

type YesterdayStats struct {
	CaloriesEaten     int
	CalorieTarget     int
	CurrentStreakDays int
	TotalLostKg       float64
}

type WeeklySummary struct {
	AvgCalories  int
	AvgProteinG  float64
	DaysOnPlan   int
	DaysLogged   int
	BestStreak   int
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
