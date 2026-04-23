-- Stride Fitness App — PostgreSQL 15 schema
-- Run once against a fresh database: psql $DATABASE_URL -f schema.sql

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- ── Users ─────────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS users (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           TEXT NOT NULL DEFAULT '',
    apple_user_id   TEXT NOT NULL UNIQUE,
    is_active       BOOLEAN NOT NULL DEFAULT true,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_active_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── User profiles ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS user_profiles (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    name                TEXT NOT NULL DEFAULT '',
    age                 INT NOT NULL DEFAULT 0,
    gender              TEXT NOT NULL DEFAULT '',
    height_cm           INT NOT NULL DEFAULT 0,
    current_weight_kg   DOUBLE PRECISION NOT NULL DEFAULT 0,
    goal_weight_kg      DOUBLE PRECISION NOT NULL DEFAULT 0,
    timeline_months     INT NOT NULL DEFAULT 0,
    activity_level      TEXT NOT NULL DEFAULT '',
    daily_minutes       INT NOT NULL DEFAULT 0,
    diet_prefs          TEXT[] NOT NULL DEFAULT '{}',
    primary_goal        TEXT NOT NULL DEFAULT '',
    calorie_target      INT NOT NULL DEFAULT 0,
    protein_target_g    INT NOT NULL DEFAULT 0,
    carbs_target_g      INT NOT NULL DEFAULT 0,
    fat_target_g        INT NOT NULL DEFAULT 0,
    goal_date           TEXT NOT NULL DEFAULT '',
    created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Meal plans ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS meal_plans (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    week_label          TEXT NOT NULL DEFAULT '',
    week_start_date     DATE NOT NULL,
    days                JSONB NOT NULL DEFAULT '{}',
    avg_daily_calories  INT NOT NULL DEFAULT 0,
    is_active           BOOLEAN NOT NULL DEFAULT true,
    generated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS meal_plans_user_active
    ON meal_plans(user_id, is_active, week_start_date DESC);

-- ── Meal swaps ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS meal_swaps (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    meal_plan_id        UUID NOT NULL REFERENCES meal_plans(id) ON DELETE CASCADE,
    day                 TEXT NOT NULL,
    meal_type           TEXT NOT NULL,
    original_meal       JSONB NOT NULL DEFAULT '{}',
    swapped_to_meal     JSONB NOT NULL DEFAULT '{}',
    filter_used         TEXT NOT NULL DEFAULT '',
    swapped_at          TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Daily logs ────────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS daily_logs (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    log_date        DATE NOT NULL,
    calories_eaten  INT NOT NULL DEFAULT 0,
    protein_g       DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs_g         DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat_g           DOUBLE PRECISION NOT NULL DEFAULT 0,
    weight_kg       DOUBLE PRECISION,
    on_plan         BOOLEAN NOT NULL DEFAULT false,
    streak_day      INT NOT NULL DEFAULT 0,
    notes           TEXT,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(user_id, log_date)
);

-- ── Food entries ──────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS food_entries (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    daily_log_id    UUID NOT NULL REFERENCES daily_logs(id) ON DELETE CASCADE,
    log_date        DATE NOT NULL,
    meal_type       TEXT NOT NULL DEFAULT '',
    food_name       TEXT NOT NULL DEFAULT '',
    calories        INT NOT NULL DEFAULT 0,
    protein_g       DOUBLE PRECISION NOT NULL DEFAULT 0,
    carbs_g         DOUBLE PRECISION NOT NULL DEFAULT 0,
    fat_g           DOUBLE PRECISION NOT NULL DEFAULT 0,
    serving_size    TEXT NOT NULL DEFAULT '',
    log_method      TEXT NOT NULL DEFAULT 'manual',
    barcode         TEXT NOT NULL DEFAULT '',
    logged_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS food_entries_user_date
    ON food_entries(user_id, log_date);

-- ── Weight logs ───────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS weight_logs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    weight_kg   DOUBLE PRECISION NOT NULL,
    note        TEXT NOT NULL DEFAULT '',
    logged_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS weight_logs_user_date
    ON weight_logs(user_id, logged_at DESC);

-- ── Coach messages ────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS coach_messages (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_date    DATE NOT NULL,
    message         TEXT NOT NULL DEFAULT '',
    tip             TEXT NOT NULL DEFAULT '',
    priority_meal   TEXT NOT NULL DEFAULT '',
    tone            TEXT NOT NULL DEFAULT '',
    read_at         TIMESTAMPTZ,
    UNIQUE(user_id, message_date)
);

-- ── Subscriptions ─────────────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS subscriptions (
    id                      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id                 UUID NOT NULL UNIQUE REFERENCES users(id) ON DELETE CASCADE,
    plan                    TEXT NOT NULL DEFAULT 'free',
    status                  TEXT NOT NULL DEFAULT 'free_trial',
    apple_original_tx_id    TEXT NOT NULL DEFAULT '',
    trial_ends_at           TIMESTAMPTZ,
    current_period_ends_at  TIMESTAMPTZ,
    cancelled_at            TIMESTAMPTZ,
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ── Device tokens (APNs) ──────────────────────────────────────────────────────

CREATE TABLE IF NOT EXISTS device_tokens (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    token       TEXT NOT NULL UNIQUE,
    device_name TEXT NOT NULL DEFAULT '',
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
