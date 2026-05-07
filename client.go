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

// Vision (multimodal) types — used for photo food analysis.
type visionRequest struct {
	Model     string           `json:"model"`
	MaxTokens int              `json:"max_tokens"`
	Messages  []visionMessage  `json:"messages"`
}

type visionMessage struct {
	Role    string               `json:"role"`
	Content []visionContentBlock `json:"content"`
}

type visionContentBlock struct {
	Type   string              `json:"type"`
	Source *visionImageSource  `json:"source,omitempty"`
	Text   string              `json:"text,omitempty"`
}

type visionImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
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

// ── AnalyzeFoodPhoto ──────────────────────────────────────────────────────────

func (c *Client) AnalyzeFoodPhoto(ctx context.Context, imageBase64 string) (*FoodNutrition, error) {
	body, _ := json.Marshal(visionRequest{
		Model:     anthropicModel,
		MaxTokens: 256,
		Messages: []visionMessage{{
			Role: "user",
			Content: []visionContentBlock{
				{
					Type: "image",
					Source: &visionImageSource{
						Type:      "base64",
						MediaType: "image/jpeg",
						Data:      imageBase64,
					},
				},
				{
					Type: "text",
					Text: `Estimate the nutritional content of this food. Respond with ONLY valid JSON (no markdown):
{"name":"<food name>","calories":<integer>,"protein_g":<float>,"carbs_g":<float>,"fat_g":<float>,"serving_size":"<e.g. 1 plate or 200g>"}`,
				},
			},
		}},
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPI, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", apiVersion)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody map[string]any
		json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("claude api error %d: %v", resp.StatusCode, errBody)
	}

	var result claudeResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if len(result.Content) == 0 {
		return nil, fmt.Errorf("empty response from claude")
	}

	var nutrition FoodNutrition
	if err := json.Unmarshal([]byte(extractJSON(result.Content[0].Text)), &nutrition); err != nil {
		return nil, fmt.Errorf("parse food nutrition: %w", err)
	}
	return &nutrition, nil
}

// ── EstimateFoodByName ───────────────────────────────────────────────────────
//
// Cheap text-only Claude call that estimates per-serving nutrition for an
// arbitrary food name. Used as a fallback when Open Food Facts returns
// nothing — OFF is a packaged-goods database and doesn't cover home-cooked
// or ethnic dishes ("chicken biryani", "khichdi", "pho", etc).

func (c *Client) EstimateFoodByName(ctx context.Context, name string) (*FoodNutrition, error) {
	prompt := fmt.Sprintf(`Estimate the nutritional content of a typical single serving of "%s".

Rules:
- Pick a realistic, commonly-eaten serving size (e.g. "1 plate", "1 cup", "1 medium", "100g"). Avoid 100g unless that's actually how this food is served.
- Use mainstream nutritional reference values; assume a typical home-cooked or restaurant preparation.
- If the input is too vague to nutritionally estimate (e.g. just "food"), still produce a best-effort guess — don't refuse.

Respond with ONLY valid JSON (no markdown, no explanation):
{"name":"<canonical food name>","calories":<integer>,"protein_g":<float>,"carbs_g":<float>,"fat_g":<float>,"serving_size":"<e.g. 1 plate>"}`,
		name,
	)

	text, err := c.ask(ctx, prompt, 256)
	if err != nil {
		return nil, err
	}

	var nutrition FoodNutrition
	if err := json.Unmarshal([]byte(extractJSON(text)), &nutrition); err != nil {
		return nil, fmt.Errorf("parse estimated food nutrition: %w", err)
	}
	return &nutrition, nil
}

// ── GenerateOnboardingPlan ────────────────────────────────────────────────────

func (c *Client) GenerateOnboardingPlan(ctx context.Context, p UserProfile) (*OnboardingPlan, error) {
	prompt := fmt.Sprintf(`You are an expert nutrition and fitness coach. Given this user profile, create a personalized plan.

User: %s, %d years old, %s
Height: %d cm, Current weight: %.1f kg, Goal weight: %.1f kg
Timeline: %d months, Activity level: %s, Daily workout minutes: %d
Diet preferences: %s

%s

Respond with ONLY valid JSON matching this structure (no markdown, no explanation):
{
  "calorie_target": <integer daily calories>,
  "protein_target_g": <float daily protein grams>,
  "carbs_target_g": <float daily carbs grams>,
  "fat_target_g": <float daily fat grams>,
  "weekly_loss_kg": <float expected weekly weight loss>,
  "goal_date": <"YYYY-MM-DD" estimated goal date>,
  "coach_message": <short motivational message, warm and direct, addressed to the user>,
  "plan_summary": <2-3 sentences written directly to the user in a warm coach voice, e.g. "Your plan is..." or "We're going to..."  conversational, no jargon, no third-person>
}`,
		p.Name, p.Age, p.Gender,
		p.HeightCm, p.CurrentWeightKg, p.GoalWeightKg,
		p.TimelineMonths, p.ActivityLevel, p.DailyMinutes,
		strings.Join(p.DietPrefs, ", "),
		humanToneRules,
	)

	text, err := c.ask(ctx, prompt, 512)
	if err != nil {
		return nil, err
	}

	var plan OnboardingPlan
	if err := json.Unmarshal([]byte(extractJSON(text)), &plan); err != nil {
		return nil, fmt.Errorf("parse onboarding plan: %w", err)
	}
	plan.CoachMessage = sanitizeHumanText(plan.CoachMessage)
	plan.PlanSummary  = sanitizeHumanText(plan.PlanSummary)
	return &plan, nil
}

// Style rules appended to user-facing coach prompts. Em-dashes and AI-typical
// punctuation patterns are flagged by users as feeling "AI-written".
const humanToneRules = `Style rules (important):
- Do NOT use em-dashes (—), en-dashes (–), or " -- ". Use commas or short sentences instead.
- Avoid the AI-typical "X — Y" / "X, but Y, however Z" cadence.
- Write like a human friend, not a chatbot.`

// Belt-and-suspenders: even with the prompt rule, Claude sometimes slips an
// em-dash through. Strip them in post-processing.
func sanitizeHumanText(s string) string {
	r := strings.NewReplacer(
		" — ", ", ",
		"— ",  ", ",
		" —",  ",",
		"—",   ", ",
		" – ", ", ",
		"–",   ", ",
		" -- ", ", ",
		"--",  ", ",
	)
	out := r.Replace(s)
	// Collapse any accidental ", ," from chained replacements.
	for strings.Contains(out, ", ,") {
		out = strings.ReplaceAll(out, ", ,", ",")
	}
	return strings.TrimSpace(out)
}

// ── GenerateWeeklyMealPlan ────────────────────────────────────────────────────

func (c *Client) GenerateWeeklyMealPlan(ctx context.Context, p UserProfile, weekLabel string) (*WeeklyMealPlan, error) {
	prompt := fmt.Sprintf(`Nutrition expert. 7-day meal plan, week: %s.
User: %s, %d y/o %s, %d kcal/day target, diet: %s, goal: %s→%s kg.

Respond with ONLY valid JSON (no markdown, no explanation):
{"week":"%s","avg_daily_calories":<int>,"days":[{"day":"Monday","total_calories":<int>,"meals":[{"meal_type":"breakfast","name":"<name>","calories":<int>,"protein_g":<float>,"carbs_g":<float>,"fat_g":<float>}]}]}

Rules:
- 4 meals per day: breakfast, lunch, snack, dinner
- No description field. No ingredients field.
- Calories per day must be within 50 kcal of target.
- Output all 7 days: Monday through Sunday.`,
		weekLabel,
		p.Name, p.Age, p.Gender,
		p.CalorieTarget,
		strings.Join(p.DietPrefs, ", "),
		fmt.Sprintf("%.1f", p.CurrentWeightKg), fmt.Sprintf("%.1f", p.GoalWeightKg),
		weekLabel,
	)

	// Stripped-down schema (no descriptions/ingredients) keeps response under
	// ~3000 tokens, well within the 8000 limit and fast for Claude Sonnet.
	text, err := c.ask(ctx, prompt, 8000)
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

User: %s, goal: %.1f to %.1f kg
Yesterday: %d kcal eaten (target %d, %s), streak: %d days, total lost: %.1f kg

%s

Respond with ONLY valid JSON (no markdown):
{
  "message": "<2-3 sentence motivational message personalised to their progress>",
  "tip": "<one practical nutrition or fitness tip for today>",
  "priority_meal": "<breakfast|lunch|dinner|snack, the meal to focus on today>",
  "tone": "<encouraging|celebratory|gentle|motivating>"
}`,
		p.Name, p.CurrentWeightKg, p.GoalWeightKg,
		yesterday.CaloriesEaten, yesterday.CalorieTarget, adherence,
		yesterday.CurrentStreakDays, yesterday.TotalLostKg,
		humanToneRules,
	)

	text, err := c.ask(ctx, prompt, 512)
	if err != nil {
		return nil, err
	}

	var msg CoachMessage
	if err := json.Unmarshal([]byte(extractJSON(text)), &msg); err != nil {
		return nil, fmt.Errorf("parse coach message: %w", err)
	}
	msg.Message = sanitizeHumanText(msg.Message)
	msg.Tip     = sanitizeHumanText(msg.Tip)
	return &msg, nil
}

// ── GenerateGroceryList ──────────────────────────────────────────────────────
//
// Synthesises a categorised, deduplicated grocery list from a week's meal
// plan. We pass meal names + macros (the only data we have today) and let
// Claude infer plausible ingredients and shopping quantities for one week.

func (c *Client) GenerateGroceryList(ctx context.Context, plan *WeeklyMealPlan) (*GroceryList, error) {
	// Compact the meal plan into a short text block — names only, no macros.
	// Ingredients aren't stored on meals today, so Claude infers them from name.
	var sb strings.Builder
	for _, day := range plan.Days {
		sb.WriteString(day.DayName)
		sb.WriteString(": ")
		for i, m := range day.Meals {
			if i > 0 {
				sb.WriteString("; ")
			}
			sb.WriteString(m.MealType)
			sb.WriteString("=")
			sb.WriteString(m.Name)
		}
		sb.WriteString("\n")
	}

	prompt := fmt.Sprintf(`You are a meal-prep assistant. Build a deduplicated weekly grocery list from this 7-day meal plan.

Plan:
%s
Rules:
- Aggregate ingredients across all meals — one entry per ingredient with a sensible weekly quantity (e.g. "3", "500 g", "1 bunch", "200 ml").
- Group items into these categories in this order: Produce, Proteins, Grains & Bakery, Dairy & Eggs, Pantry & Spices, Other. Skip empty categories.
- Be realistic: assume 1 person eating these meals for the week.
- Don't include water, salt, or pepper.

Respond with ONLY valid JSON (no markdown, no explanation):
{"categories":[{"name":"<category>","items":[{"name":"<item>","quantity":"<qty>"}]}]}`,
		sb.String(),
	)

	text, err := c.ask(ctx, prompt, 2048)
	if err != nil {
		return nil, err
	}

	var list GroceryList
	if err := json.Unmarshal([]byte(extractJSON(text)), &list); err != nil {
		return nil, fmt.Errorf("parse grocery list: %w", err)
	}
	return &list, nil
}
