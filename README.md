# twitter-fetcher

A self-hosted Twitter/X feed aggregator. Polls the Twitter API v2 for tweets from configured accounts, stores them in PostgreSQL with engagement snapshots over time, and serves them through a Next.js frontend.

## Architecture

```
┌──────────────┐      ┌─────────────┐      ┌────────────┐
│  Twitter API │◄────►│  Go Backend │◄────►│ PostgreSQL │
│    (v2)      │      │             │      │            │
└──────────────┘      │  - fetcher  │      └─────┬──────┘
                      │  - snapshot │            │
                      └─────────────┘            │
                                           ┌─────▼──────┐
                                           │  Next.js   │
                                           │  Frontend  │
                                           └────────────┘
```

- **Go backend** — two goroutines: `fetcher` (polls tweets on an interval) and `snapshotter` (captures engagement metrics at 15m/30m/45m/60m after each tweet)
- **Next.js 14 frontend** — server-rendered feed with infinite scroll, reads directly from PostgreSQL
- **OAuth2 PKCE** — public client flow (no client secret required); refresh token stored in a local file (default) or macOS Keychain

## Requirements

- Go 1.22+
- Node.js 18+
- PostgreSQL (or any hosted Postgres — Supabase, Neon, etc.)
- Python 3.10+ (for setup scripts only)
- A Twitter/X API app with OAuth 2.0 User Authentication enabled

## Setup

### 1. Create a Twitter API App

1. Go to the [Twitter Developer Portal](https://developer.twitter.com/en/portal/dashboard)
2. Create a project and app
3. Under **User authentication settings**, enable OAuth 2.0 with:
   - Type: **Public** client
   - Callback URL: `http://localhost:8080/callback`
   - Website URL: any valid URL
4. Copy the **Client ID**

### 2. Configure Environment

```bash
cp .env.example .env
```

Edit `.env` and set:
- `X_CLIENT_ID` — your Twitter app's Client ID
- `DATABASE_URL` — PostgreSQL connection string

### 3. Set Up the Database

Run all migrations in order:

```bash
for f in migrations/*.sql; do psql "$DATABASE_URL" -f "$f"; done
```

Or individually:

```bash
psql $DATABASE_URL -f migrations/001_init.sql
psql $DATABASE_URL -f migrations/002_rich_content.sql
psql $DATABASE_URL -f migrations/003_original_author_display_name.sql
psql $DATABASE_URL -f migrations/004_engagement_metrics.sql
psql $DATABASE_URL -f migrations/005_engagement_snapshots.sql
psql $DATABASE_URL -f migrations/006_source_tweet_id.sql
psql $DATABASE_URL -f migrations/007_delta_snapshots.sql
```

### 4. Authorize with Twitter

```bash
pip3 install -r requirements.txt
python3 scripts/oauth_setup.py
```

This opens your browser for the OAuth2 PKCE flow and stores the refresh token. By default, the token is saved to a `.refresh_token` file in the project root.

### 5. Add Twitter Accounts to Monitor

Create a CSV file with Twitter usernames in the second column (column index 1), with a header row:

```csv
id,username
,elonmusk
,naval
```

Then import:

```bash
python3 scripts/load_accounts.py accounts.csv
```

Use `--replace` to disable all existing accounts and only keep the ones in the CSV.

### 6. Run the Backend

```bash
go build -o twitter-fetcher ./cmd
./twitter-fetcher
```

The service polls for new tweets every 5 minutes (configurable) and captures engagement snapshots at 15m, 30m, 45m, and 60m after each tweet.

### 7. Run the Frontend

```bash
cd frontend
cp .env.example .env.local   # Edit DATABASE_URL
npm install
npm run dev
```

The frontend runs at `http://localhost:3001` by default.

## Token Storage

Twitter rotates the refresh token on every API call — the old one is immediately invalidated. The service handles this automatically, but the token must be persisted.

Two storage backends are supported, configured via `TOKEN_STORE`:

### File mode (default)

Stores the refresh token in `.refresh_token` (or a custom path via `REFRESH_TOKEN_FILE`). Works on any OS. To bootstrap on first run, either:
- Run `scripts/oauth_setup.py`, or
- Set `X_REFRESH_TOKEN` in your environment with an initial token

### Keychain mode (macOS only)

Set `TOKEN_STORE=keychain` in `.env`. Uses the macOS Keychain via the `security` CLI. Configure the service name with `KEYCHAIN_SERVICE` (default: `twitter-fetcher.refresh_token`).

## Configuration

All configuration is via environment variables (loaded from `.env`):

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `X_CLIENT_ID` | Yes | — | Twitter API OAuth2 Client ID |
| `DATABASE_URL` | Yes | — | PostgreSQL connection string |
| `TOKEN_STORE` | No | `file` | Token storage: `file` or `keychain` |
| `REFRESH_TOKEN_FILE` | No | `.refresh_token` | Path for file-based token storage |
| `X_REFRESH_TOKEN` | No | — | Initial refresh token (file mode bootstrap) |
| `KEYCHAIN_SERVICE` | No | `twitter-fetcher.refresh_token` | macOS Keychain service name |
| `POLL_INTERVAL_SECONDS` | No | `300` | Seconds between tweet polling cycles |
| `MAX_RESULTS_PER_FETCH` | No | `100` | Max tweets per API request |
| `INITIAL_LOOKBACK_MINUTES` | No | `5` | Lookback window on first poll for an account |
| `LOG_LEVEL` | No | `info` | Log level (debug, info, warn, error) |
| `LOG_JSON` | No | `true` | JSON structured logging |
| `SNAPSHOT_DELAYS` | No | `15m,30m,45m,60m` | Engagement snapshot intervals after each tweet |
| `SNAPSHOT_CHECK_INTERVAL_SECONDS` | No | `60` | How often to check for due snapshots |
| `SNAPSHOT_BATCH_SIZE` | No | `100` | Max snapshots per check cycle |

## Project Structure

```
cmd/main.go                  Entry point — loads config, starts fetcher + snapshotter
internal/
  config/config.go           Env var loading, token storage (file + keychain)
  twitter/                   API v2 client, OAuth2 token provider, response types
  fetcher/                   Tweet polling orchestrator + tweet classifier
  snapshotter/               Engagement metrics capture at configured intervals
  db/                        PostgreSQL connection pool + typed queries
frontend/                    Next.js 14 app
  app/page.tsx               Server component — initial tweet load (ISR)
  app/api/tweets/route.ts    Infinite scroll pagination API
  components/Feed.tsx        Client component — filtering, blacklisting, infinite scroll
  components/TweetCard.tsx   Tweet card with engagement snapshot table
migrations/                  SQL schema (001–007, run in order)
scripts/
  oauth_setup.py             One-time OAuth2 PKCE authorization
  load_accounts.py           Batch import Twitter accounts from CSV
```

## License

MIT
