package validator

import (
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"

	apperrors "stride/backend/internal/errors"
)

// Validator wraps the go-playground validator with custom rules.
type Validator struct {
	v *validator.Validate
}

var (
	instance *Validator
	once     sync.Once
)

// Get returns a singleton Validator instance.
func Get() *Validator {
	once.Do(func() {
		v := validator.New()

		// Use JSON tag names in error messages
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			name := strings.SplitN(fld.Tag.Get("json"), ",", 2)[0]
			if name == "-" {
				return fld.Name
			}
			return name
		})

		// Register custom validations
		v.RegisterValidation("activity_level", validateActivityLevel)
		v.RegisterValidation("gender", validateGender)
		v.RegisterValidation("diet_pref", validateDietPref)
		v.RegisterValidation("meal_type", validateMealType)
		v.RegisterValidation("log_method", validateLogMethod)
		v.RegisterValidation("swap_filter", validateSwapFilter)
		v.RegisterValidation("subscription_plan", validateSubscriptionPlan)

		instance = &Validator{v: v}
	})
	return instance
}

// Struct validates a struct and returns an AppError if validation fails.
func (vld *Validator) Struct(s any) error {
	err := vld.v.Struct(s)
	if err == nil {
		return nil
	}

	// Get the first validation error
	validationErrors := err.(validator.ValidationErrors)
	if len(validationErrors) == 0 {
		return nil
	}

	firstErr := validationErrors[0]
	field := firstErr.Field()
	reason := formatValidationReason(firstErr)

	return apperrors.NewValidationErrorWithField(field, reason)
}

// StructMulti validates a struct and returns all validation errors.
func (vld *Validator) StructMulti(s any) []*apperrors.AppError {
	err := vld.v.Struct(s)
	if err == nil {
		return nil
	}

	validationErrors := err.(validator.ValidationErrors)
	appErrors := make([]*apperrors.AppError, 0, len(validationErrors))
	for _, e := range validationErrors {
		appErrors = append(appErrors, apperrors.NewValidationErrorWithField(
			e.Field(),
			formatValidationReason(e),
		))
	}
	return appErrors
}

// Var validates a single variable against a validation tag.
func (vld *Validator) Var(field any, tag string) error {
	return vld.v.Var(field, tag)
}

// ── Custom validators ────────────────────────────────────────────────────────

func validateActivityLevel(fl validator.FieldLevel) bool {
	validLevels := map[string]bool{
		"sedentary": true,
		"light":     true,
		"moderate":  true,
		"active":    true,
		"very_active": true,
	}
	return validLevels[fl.Field().String()]
}

func validateGender(fl validator.FieldLevel) bool {
	validGenders := map[string]bool{
		"male":   true,
		"female": true,
		"other":  true,
	}
	return validGenders[fl.Field().String()]
}

func validateDietPref(fl validator.FieldLevel) bool {
	validPrefs := map[string]bool{
		"vegetarian":  true,
		"vegan":       true,
		"gluten_free": true,
		"dairy_free":  true,
		"low_carb":    true,
		"keto":        true,
		"paleo":       true,
		"mediterranean": true,
		"halal":       true,
		"kosher":      true,
	}
	return validPrefs[fl.Field().String()]
}

func validateMealType(fl validator.FieldLevel) bool {
	validTypes := map[string]bool{
		"breakfast": true,
		"lunch":     true,
		"dinner":    true,
		"snack":     true,
	}
	return validTypes[fl.Field().String()]
}

func validateLogMethod(fl validator.FieldLevel) bool {
	validMethods := map[string]bool{
		"manual":  true,
		"barcode": true,
		"ai":      true,
		"meal_plan": true,
	}
	return validMethods[fl.Field().String()]
}

func validateSwapFilter(fl validator.FieldLevel) bool {
	validFilters := map[string]bool{
		"":            true, // empty is valid (no filter)
		"vegetarian":  true,
		"vegan":       true,
		"gluten_free": true,
		"dairy_free":  true,
		"low_carb":    true,
	}
	return validFilters[fl.Field().String()]
}

func validateSubscriptionPlan(fl validator.FieldLevel) bool {
	validPlans := map[string]bool{
		"monthly": true,
		"annual":  true,
		"free":    true,
	}
	return validPlans[fl.Field().String()]
}

// ── Helper functions ─────────────────────────────────────────────────────────

func formatValidationReason(e validator.FieldError) string {
	switch e.Tag() {
	case "required":
		return "is required"
	case "min":
		return fmt.Sprintf("must be at least %s", e.Param())
	case "max":
		return fmt.Sprintf("must be at most %s", e.Param())
	case "gte":
		return fmt.Sprintf("must be greater than or equal to %s", e.Param())
	case "lte":
		return fmt.Sprintf("must be less than or equal to %s", e.Param())
	case "gt":
		return fmt.Sprintf("must be greater than %s", e.Param())
	case "lt":
		return fmt.Sprintf("must be less than %s", e.Param())
	case "email":
		return "must be a valid email address"
	case "uuid":
		return "must be a valid UUID"
	case "oneof":
		return fmt.Sprintf("must be one of: %s", e.Param())
	case "activity_level":
		return "must be one of: sedentary, light, moderate, active, very_active"
	case "gender":
		return "must be one of: male, female, other"
	case "diet_pref":
		return "must be a valid diet preference"
	case "meal_type":
		return "must be one of: breakfast, lunch, dinner, snack"
	case "log_method":
		return "must be one of: manual, barcode, ai, meal_plan"
	case "swap_filter":
		return "must be a valid swap filter"
	case "subscription_plan":
		return "must be one of: monthly, annual, free"
	default:
		return fmt.Sprintf("failed validation '%s'", e.Tag())
	}
}

// ── Request DTOs with validation tags ────────────────────────────────────────

// OnboardingRequest is the validated request for onboarding completion.
type OnboardingRequest struct {
	Name           string   `json:"name" validate:"required,min=1,max=100"`
	Age            int      `json:"age" validate:"required,gte=13,lte=120"`
	Gender         string   `json:"gender" validate:"required,gender"`
	HeightCm       int      `json:"height_cm" validate:"required,gte=50,lte=300"`
	CurrentWeight  float64  `json:"current_weight_kg" validate:"required,gte=20,lte=500"`
	GoalWeight     float64  `json:"goal_weight_kg" validate:"required,gte=20,lte=500"`
	TimelineMonths int      `json:"timeline_months" validate:"required,gte=1,lte=60"`
	ActivityLevel  string   `json:"activity_level" validate:"required,activity_level"`
	DailyMinutes   int      `json:"daily_minutes" validate:"gte=0,lte=300"`
	DietPrefs      []string `json:"diet_prefs" validate:"dive,diet_pref"`
	PrimaryGoal    string   `json:"primary_goal" validate:"required,max=50"`
}

// FoodEntryRequest is the validated request for logging food.
type FoodEntryRequest struct {
	MealType    string  `json:"meal_type" validate:"required,meal_type"`
	FoodName    string  `json:"food_name" validate:"required,min=1,max=200"`
	Calories    int     `json:"calories" validate:"required,gte=0,lte=10000"`
	ProteinG    float64 `json:"protein_g" validate:"gte=0,lte=500"`
	CarbsG      float64 `json:"carbs_g" validate:"gte=0,lte=1000"`
	FatG        float64 `json:"fat_g" validate:"gte=0,lte=500"`
	ServingSize string  `json:"serving_size" validate:"max=100"`
	LogMethod   string  `json:"log_method" validate:"log_method"`
	Barcode     string  `json:"barcode" validate:"max=50"`
}

// LogWeightRequest is the validated request for logging weight.
type LogWeightRequest struct {
	WeightKg float64 `json:"weight_kg" validate:"required,gte=20,lte=500"`
	Note     string  `json:"note" validate:"max=500"`
}

// SwapMealRequest is the validated request for swapping a meal.
type SwapMealRequest struct {
	MealPlanID string `json:"meal_plan_id" validate:"required,uuid"`
	Day        string `json:"day" validate:"required,oneof=Monday Tuesday Wednesday Thursday Friday Saturday Sunday"`
	Meal       MealDTO `json:"meal" validate:"required"`
	Filter     string `json:"filter" validate:"swap_filter"`
}

// MealDTO is a validated meal structure.
type MealDTO struct {
	MealType    string   `json:"meal_type" validate:"required,meal_type"`
	Name        string   `json:"name" validate:"required,min=1,max=200"`
	Description string   `json:"description" validate:"max=500"`
	Calories    int      `json:"calories" validate:"required,gte=0,lte=5000"`
	ProteinG    float64  `json:"protein_g" validate:"gte=0,lte=200"`
	CarbsG      float64  `json:"carbs_g" validate:"gte=0,lte=500"`
	FatG        float64  `json:"fat_g" validate:"gte=0,lte=200"`
	Ingredients []string `json:"ingredients" validate:"dive,min=1,max=100"`
}

// VerifySubscriptionRequest is the validated request for subscription verification.
type VerifySubscriptionRequest struct {
	TransactionID string `json:"transaction_id" validate:"required,min=1,max=100"`
	Plan          string `json:"plan" validate:"required,subscription_plan"`
}

// RegisterDeviceRequest is the validated request for device registration.
type RegisterDeviceRequest struct {
	Token      string `json:"token" validate:"required,min=10,max=200"`
	DeviceName string `json:"device_name" validate:"max=100"`
}

// RefreshTokenRequest is the validated request for token refresh.
type RefreshTokenRequest struct {
	RefreshToken string `json:"refresh_token" validate:"required"`
}

// AppleSignInRequest is the validated request for Apple Sign In.
type AppleSignInRequest struct {
	IdentityToken string `json:"identity_token" validate:"required"`
	Email         string `json:"email" validate:"omitempty,email"`
	FullName      string `json:"full_name" validate:"max=200"`
}
