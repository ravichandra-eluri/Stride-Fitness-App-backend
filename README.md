# Stride — Backend

Go backend for the Stride iOS fitness app. Runs on GCP Cloud Run with Postgres on Cloud SQL.

## Stack

- **Go 1.22** with chi router
- **Postgres 15** via GCP Cloud SQL
- **Claude Sonnet 4.6** (Anthropic) — meal plans, coach messages, food photo analysis
- **Apple Sign-In + JWT** for auth
- **APNs** for push notifications
- **Docker + GCP Artifact Registry** for deployment

## Project layout

```
server/main.go        # Entrypoint, router, env config
internal/
  handlers/           # One function per endpoint
  db/                 # All DB queries and row models
  middleware/         # JWT auth, CORS
  cron/               # Background jobs
client.go             # Anthropic API wrapper (text + vision)
types.go              # Shared types used across AI and handlers
schema.sql            # DB schema — run once to bootstrap
```

## Environment variables

| Variable | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | Postgres connection string |
| `CLAUDE_API_KEY` | yes | Anthropic API key |
| `JWT_SECRET` | yes | HS256 signing secret |
| `APNS_KEY_ID` | no | Apple push key ID |
| `APNS_TEAM_ID` | no | Apple developer team ID |
| `APNS_KEY_PATH` | no | Path to .p8 key file |
| `PORT` | no | Defaults to 8080 |

## Running locally

```bash
go mod tidy

export DATABASE_URL="postgres://user:pass@localhost/stride"
export CLAUDE_API_KEY="sk-ant-..."
export JWT_SECRET="any-local-secret"

go run ./server
```

## Deploying

Make sure Docker is running, then:

```bash
./deploy.sh
```

Builds a linux/amd64 image, pushes to GCR, and deploys to Cloud Run with the right secrets and Cloud SQL instance attached.

## API reference

All endpoints except `/api/auth/*`, `/health`, and `/privacy` require `Authorization: Bearer <token>`.

| Method | Path | Description |
|---|---|---|
| POST | `/api/auth/apple` | Sign in with Apple |
| POST | `/api/auth/refresh` | Refresh JWT |
| POST | `/api/onboarding/complete` | Submit profile → AI-generated plan |
| GET | `/api/profile` | Get user profile |
| PATCH | `/api/profile` | Update profile |
| GET | `/api/meals/plan` | Current week's meal plan |
| POST | `/api/meals/regenerate` | Ask Claude for a new meal plan |
| POST | `/api/meals/swap` | Swap one meal with AI alternatives |
| POST | `/api/log/food` | Log a food entry |
| GET | `/api/log/today` | Today's log and totals |
| DELETE | `/api/log/food/{id}` | Remove a food entry |
| POST | `/api/log/weight` | Log a weight entry |
| GET | `/api/progress/weekly` | Weekly summary |
| GET | `/api/progress/weights` | Weight history |
| GET | `/api/coach/today` | Today's coach message (generated on demand if missing) |
| GET | `/api/food/barcode/{barcode}` | Nutrition lookup via Open Food Facts |
| POST | `/api/food/analyze-photo` | Calorie estimate from food photo (Claude vision) |
| DELETE | `/api/account` | Delete account and all user data |
| GET | `/privacy` | Privacy policy page (public) |

## Background jobs

Two cron jobs run in-process:

- **Monday 6am UTC** — generate weekly meal plans for active users
- **Daily 7am UTC** — generate coach messages and send push notifications

## A few things worth knowing

Claude calls for onboarding and meal plan generation can take 30–90s. Cloud Run timeout is set to 180s; the iOS client gives it 160s on the resource side. Min instances is set to 1 so there are no cold starts. Food photo analysis sends a base64 JPEG from iOS to Claude's vision API — the client resizes before upload to keep the payload reasonable.
