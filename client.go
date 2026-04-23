package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const (
	anthropicAPI   = "https://api.anthropic.com/v1/messages"
	anthropicModel = "claude-sonnet-4-6"
	apiVersion     = "2023-06-01"
)

// Client wraps the Anthropic Claude API.
type Client struct {
	apiKey     string
	httpClient *http.Client
}

// NewClient creates a new Claude API client.
func NewClient(apiKey string) *Client {
	return &Client{
		apiKey: apiKey,
		httpClient: &http.Client{Timeout: 60 * time.Second},
	}
}

// ── Claude API plumbing ───────────────────────────────────────────────────────

type claudeRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type claudeResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
}

func (c *Client) ask(ctx context.Context, prompt string, maxTokens int) (string, error) {
	body, _ := json.Marshal(claudeRequest{
		Model:     anthropicModel,
		MaxTokens: maxTokens,
		Messages:  []message{{Role: "user", Content: prompt}},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPI, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		return "", fmt.Errorf("claude api error %d: %v", resp.StatusCode, errBody)
	}

	var result claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty response from claude")
	}
	return result.Content[0].Text, nil
}

// extractJSON pulls the first JSON object or array out of a text response.
func extractJSON(text string) string {
	start := strings.IndexAny(text, "{[")
	if start == -1 {
		return text
	}
	end := strings.LastIndexAny(text, "}]")
	if end == -1 || end < start {
		return text
	}
	return text[start : end+1]
}

// ── GenerateOnboardingPlan ────────────────────────────────────────────────────

func (c *Client) GenerateOnboardingPlan(ctx context.Context, p UserProfile) (*OnboardingPlan, error) {
	prompt := fmt.Sprintf(`You are an expert nutrition and fitness coach. Given this user profile, create a personalized plan.

User: %s, %d years old, %s
Height: %d cm, Current weight: %.1f kg, Goal weight: %.1f kg
Timeline: %d months, Activity level: %s, Daily workout minutes: %d
Diet preferences: %s

Respond with ONLY valid JSON matching this structure (no markdown, no explanation):
{
  "calorie_target": <integer daily calories>,
  "protein_target_g": <float daily protein grams>,
  "carbs_target_g": <float daily carbs grams>,
  "fat_target_g": <float daily fat grams>,
  "weekly_loss_kg": <float expected weekly weight loss>,
  "goal_date": <"YYYY-MM-DD" estimated goal date>,
  "coach_message": <short motivational message>,
  "plan_summary": <2-3 sentence plan overview>
}`,
		p.Name, p.Age, p.Gender,
		p.HeightCm, p.CurrentWeightKg, p.GoalWeightKg,
		p.TimelineMonths, p.ActivityLevel, p.DailyMinutes,
		strings.Join(p.DietPrefs, ", "),
	)

	text, err := c.ask(ctx, prompt, 512)
	if err != nil {
		return nil, err
	}

	var plan OnboardingPlan
	if err := json.Unmarshal([]byte(extractJSON(text)), &plan); err != nil {
		return nil, fmt.Errorf("parse onboarding plan: %w", err)
	}
	return &plan, nil
}

// ── GenerateWeeklyMealPlan ────────────────────────────────────────────────────

func (c *Client) GenerateWeeklyMealPlan(ctx context.Context, p UserProfile, weekLabel string) (*WeeklyMealPlan, error) {
	prompt := fmt.Sprintf(`You are a nutrition expert. Create a 7-day meal plan for week: %s.

User: %s, %d years old, %s
Calorie target: %d kcal/day, Diet preferences: %s
Goals: %s → %s kg

Respond with ONLY valid JSON (no markdown):
{
  "week": "%s",
  "avg_daily_calories": <integer>,
  "days": [
    {
      "day": "Monday",
      "total_calories": <integer>,
      "meals": [
        {
          "meal_type": "breakfast",
          "name": "<name>",
          "description": "<brief description>",
          "calories": <integer>,
          "protein_g": <float>,
          "carbs_g": <float>,
          "fat_g": <float>,
          "ingredients": ["<ingredient>"]
        }
      ]
    }
  ]
}
Include breakfast, lunch, dinner, and one snack per day.`,
		weekLabel,
		p.Name, p.Age, p.Gender,
		p.CalorieTarget,
		strings.Join(p.DietPrefs, ", "),
		fmt.Sprintf("%.1f", p.CurrentWeightKg), fmt.Sprintf("%.1f", p.GoalWeightKg),
		weekLabel,
	)

	// 7 days × 4 meals with descriptions + ingredients easily exceeds 4096
	// tokens. Claude Sonnet 4.6 supports up to 64k output tokens; give this
	// call plenty of headroom so the JSON never ends mid-object.
	text, err := c.ask(ctx, prompt, 16000)
	if err != nil {
		return nil, err
	}

	var plan WeeklyMealPlan
	trimmed := extractJSON(text)
	if err := json.Unmarshal([]byte(trimmed), &plan); err != nil {
		// Log a short preview so future failures are debuggable without
		// dumping the whole Claude response into the log stream.
		preview := trimmed
		if len(preview) > 400 {
			preview = preview[:200] + "…" + preview[len(preview)-200:]
		}
		return nil, fmt.Errorf("parse weekly meal plan (len=%d): %w; preview=%q", len(trimmed), err, preview)
	}
	return &plan, nil
}

// ── SwapMeal ─────────────────────────────────────────────────────────────────

func (c *Client) SwapMeal(ctx context.Context, p UserProfile, meal Meal, filter SwapFilter) ([]Meal, error) {
	filterStr := string(filter)
	if filterStr == "" {
		filterStr = "none"
	}

	prompt := fmt.Sprintf(`You are a nutrition expert. Suggest 3 alternative meals to replace the one below.

User calorie target: %d kcal/day, Diet filter: %s
Meal to replace: %s (%s) — %d kcal, %.0fg protein, %.0fg carbs, %.0fg fat

Respond with ONLY valid JSON array (no markdown):
[
  {
    "meal_type": "%s",
    "name": "<name>",
    "description": "<brief description>",
    "calories": <integer>,
    "protein_g": <float>,
    "carbs_g": <float>,
    "fat_g": <float>,
    "ingredients": ["<ingredient>"]
  }
]
Return exactly 3 alternatives with similar calories (±100 kcal).`,
		p.CalorieTarget, filterStr,
		meal.Name, meal.MealType, meal.Calories, meal.ProteinG, meal.CarbsG, meal.FatG,
		meal.MealType,
	)

	text, err := c.ask(ctx, prompt, 1024)
	if err != nil {
		return nil, err
	}

	var meals []Meal
	if err := json.Unmarshal([]byte(extractJSON(text)), &meals); err != nil {
		return nil, fmt.Errorf("parse meal swaps: %w", err)
	}
	return meals, nil
}

// ── GenerateDailyCoach ────────────────────────────────────────────────────────

func (c *Client) GenerateDailyCoach(ctx context.Context, p UserProfile, yesterday YesterdayStats) (*CoachMessage, error) {
	adherence := "on track"
	if yesterday.CalorieTarget > 0 {
		ratio := float64(yesterday.CaloriesEaten) / float64(yesterday.CalorieTarget)
		switch {
		case ratio > 1.1:
			adherence = "over target"
		case ratio < 0.8:
			adherence = "under target"
		}
	}

	prompt := fmt.Sprintf(`You are a supportive fitness coach. Write a short daily motivational message.

User: %s, goal: %.1f → %.1f kg
Yesterday: %d kcal eaten (target %d, %s), streak: %d days, total lost: %.1f kg

Respond with ONLY valid JSON (no markdown):
{
  "message": "<2-3 sentence motivational message personalised to their progress>",
  "tip": "<one practical nutrition or fitness tip for today>",
  "priority_meal": "<breakfast|lunch|dinner|snack — the meal to focus on today>",
  "tone": "<encouraging|celebratory|gentle|motivating>"
}`,
		p.Name, p.CurrentWeightKg, p.GoalWeightKg,
		yesterday.CaloriesEaten, yesterday.CalorieTarget, adherence,
		yesterday.CurrentStreakDays, yesterday.TotalLostKg,
	)

	text, err := c.ask(ctx, prompt, 512)
	if err != nil {
		return nil, err
	}

	var msg CoachMessage
	if err := json.Unmarshal([]byte(extractJSON(text)), &msg); err != nil {
		return nil, fmt.Errorf("parse coach message: %w", err)
	}
	return &msg, nil
}
