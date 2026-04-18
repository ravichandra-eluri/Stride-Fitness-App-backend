# Stride — Backend

Golang backend for the Stride wellness app. Runs on GCP Cloud Run.

## Tech stack
- **Language:** Go 1.22
- **Router:** chi
- **Database:** Postgres 15 (GCP Cloud SQL)
- **AI:** Claude API (Anthropic)
- **Auth:** Apple Sign-In + JWT
- **Push:** APNs (iOS push notifications)
- **Deploy:** GCP Cloud Run + Docker

## Project structure

```
cmd/server/          # Main entrypoint
internal/
  db/                # Database layer — all queries + models
  handlers/          # HTTP handlers (one per endpoint)
  middleware/        # JWT auth, CORS
  cron/              # Background jobs (meal plans, coach messages)
```

## Environment variables

| Variable | Description |
|---|---|
| `DATABASE_URL` | Postgres connection string |
| `CLAUDE_API_KEY` | Anthropic API key |
| `JWT_SECRET` | Secret for signing JWTs |
| `APNS_KEY_ID` | Apple Push Notification key ID |
| `APNS_TEAM_ID` | Apple Developer Team ID |
| `APNS_KEY_PATH` | Path to .p8 APNs key file |
| `PORT` | Server port (default: 8080) |

## Running locally

```bash
go mod tidy
export DATABASE_URL="postgres://user:pass@localhost/stride"
export CLAUDE_API_KEY="sk-ant-..."
export JWT_SECRET="your-secret"
go run ./cmd/server
```

## Deploying to Cloud Run

```bash
chmod +x deploy.sh
./deploy.sh
```

## API endpoints

| Method | Path | Description |
|---|---|---|
| POST | `/api/auth/apple` | Apple Sign-In |
| POST | `/api/auth/refresh` | Refresh JWT |
| POST | `/api/onboarding/complete` | Generate AI plan |
| GET | `/api/profile` | Get user profile |
| PATCH | `/api/profile` | Update profile |
| GET | `/api/meals/plan` | Get weekly meal plan |
| POST | `/api/meals/regenerate` | Regenerate meal plan |
| POST | `/api/meals/swap` | Swap a meal |
| POST | `/api/log/food` | Log food entry |
| GET | `/api/log/today` | Get today's log |
| POST | `/api/log/weight` | Log weight |
| GET | `/api/progress/weekly` | Weekly summary |
| GET | `/api/progress/weights` | Weight history |
| GET | `/api/coach/today` | Today's coach message |
| POST | `/api/subscription/verify` | Verify StoreKit purchase |
| POST | `/api/device/register` | Register push token |

## Cron jobs

- **Monday 6am UTC** — Generate weekly meal plans for all active users
- **Daily 7am UTC** — Generate coach messages + send push notifications
