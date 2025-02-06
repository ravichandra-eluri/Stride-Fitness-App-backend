package backend

// UserProfile holds all user data needed for AI prompts.
type UserProfile struct {
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
	CalorieTarget   int
}

// SwapFilter describes dietary constraints for meal swaps.
type SwapFilter string

const (
	SwapFilterNone       SwapFilter = ""
	SwapFilterVegetarian SwapFilter = "vegetarian"
	SwapFilterVegan      SwapFilter = "vegan"
	SwapFilterGlutenFree SwapFilter = "gluten_free"
	SwapFilterDairyFree  SwapFilter = "dairy_free"
	SwapFilterLowCarb    SwapFilter = "low_carb"
)

// Meal represents a single meal within a day's plan.
type Meal struct {
	MealType    string       `json:"meal_type"`    // breakfast | lunch | dinner | snack
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Calories    int          `json:"calories"`
	ProteinG    float64      `json:"protein_g"`
	CarbsG      float64      `json:"carbs_g"`
	FatG        float64      `json:"fat_g"`
	Ingredients []string     `json:"ingredients,omitempty"`
}

// Day represents a single day within a weekly meal plan.
type Day struct {
	DayName  string  `json:"day"`
	Meals    []Meal  `json:"meals"`
	TotalCal int     `json:"total_calories"`
}

// OnboardingPlan is the result of GenerateOnboardingPlan.
type OnboardingPlan struct {
	CalorieTarget  int     `json:"calorie_target"`
	ProteinTargetG int     `json:"protein_target_g"`
	CarbsTargetG   int     `json:"carbs_target_g"`
	FatTargetG     int     `json:"fat_target_g"`
	WeeklyLossKg   float64 `json:"weekly_loss_kg"`
	GoalDate       string  `json:"goal_date"` // YYYY-MM-DD
	CoachMessage   string  `json:"coach_message"`
	PlanSummary    string  `json:"plan_summary"`
}

// WeeklyMealPlan is the result of GenerateWeeklyMealPlan.
type WeeklyMealPlan struct {
	Week             string  `json:"week"`
	Days             []Day   `json:"days"`
	AvgDailyCalories int     `json:"avg_daily_calories"`
}

// CoachMessage is the result of GenerateDailyCoach.
type CoachMessage struct {
	Message      string `json:"message"`
	Tip          string `json:"tip"`
	PriorityMeal string `json:"priority_meal"`
	Tone         string `json:"tone"`
}

// YesterdayStats holds the previous day's metrics used for coach messages.
type YesterdayStats struct {
	CaloriesEaten     int
	CalorieTarget     int
	CurrentStreakDays int
	TotalLostKg       float64
}
