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
	ID              string
	UserID          string
	Name            string
	Age             int
	Gender          string
	HeightCm        int
	CurrentWeightKg float64
	GoalWeightKg    float64
	TimelineMonths  int
	ActivityLevel   string
	DailyMinutes    int
	DietPrefs       []string
	PrimaryGoal     string
	CalorieTarget   int
	ProteinTargetG  int
	CarbsTargetG    int
	FatTargetG      int
	GoalDate        string
	CreatedAt       time.Time
	UpdatedAt       time.Time
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
