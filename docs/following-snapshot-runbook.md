# One-Time Following Snapshot Runbook

This repo has a one-off command for capturing `twitter_accounts -> followed accounts`
edges and computing the accounts most commonly followed by the monitored set.

## What It Stores

The command writes two tables:

- `following_snapshots`: one row per run, with status, source account count,
  completed account count, edge count, and any failure text.
- `following_edges`: one row per edge in a snapshot:
  `source_user_id -> followed_user_id`, plus cached followed-account metadata
  such as username, display name, bio, verified type, and public metrics.

The schema lives in `migrations/008_following_snapshots.sql`. The command also
runs `CREATE TABLE IF NOT EXISTS` guards on startup, so an operator can run it
even if the migration has not yet been applied manually.

## Token Safety

Twitter/X rotates OAuth2 refresh tokens on every refresh. Never manually call the
token endpoint with the shared `.refresh_token` while the normal tweet fetcher is
running.

Safe modes:

1. Prefer an independent app/user bearer token:

   ```bash
   X_BEARER_TOKEN=... ./follow_snapshot --auth bearer --top 50
   ```

   This does not consume the repo's refresh token and does not affect the normal
   `twitter-fetcher` launchd service.

2. If using the shared refresh token, let the command manage the live fetcher:

   ```bash
   ./follow_snapshot --manage-fetcher --top 50
   ```

   In this mode the command:

   - refuses to run if `twitter-fetcher` is active and `--manage-fetcher` is not
     present;
   - stops `com.harryz.twitter-fetcher` before reading `.refresh_token`;
   - refreshes through the normal Go `OAuth2TokenProvider`, which synchronously
     writes the rotated refresh token;
   - restarts the launchd service after success or failure, so the tweet fetcher
     reads the latest token from disk.

The command intentionally does not use ad hoc `curl` for token refresh.

## Build

```bash
go build -o follow_snapshot ./cmd/follow_snapshot
```

The binary is local-only and ignored by git.

## Run A Canary

Before a full run, test one source account:

```bash
./follow_snapshot --manage-fetcher --limit-accounts 1 --top 10
```

Expected behavior:

- It logs `stopping tweet fetcher before shared refresh-token use`.
- It creates a `following_snapshots` row.
- It prints a small top list.
- It logs `restarting tweet fetcher`.

If the command exits before creating a snapshot with:

```text
tweet fetcher is running; refusing to consume the shared refresh token without --manage-fetcher
```

that is the safety guard working.

## Run The Full Snapshot

All known rows in `twitter_accounts`:

```bash
./follow_snapshot --manage-fetcher --top 50
```

Only enabled rows:

```bash
./follow_snapshot --manage-fetcher --enabled-only --top 50
```

Useful flags:

- `--auth auto`: default; uses `X_BEARER_TOKEN`/`X_APP_BEARER_TOKEN` if present,
  otherwise uses the shared refresh token.
- `--auth bearer`: require independent bearer token.
- `--auth refresh`: require shared refresh-token flow.
- `--manage-fetcher`: required when using shared refresh token while the normal
  fetcher is running.
- `--limit-accounts N`: canary or partial run.
- `--max-results 1000`: following page size.
- `--sleep 3.2s`: delay between paginated following requests.
- `--top 50`: number of commonly-followed rows to print at the end.

The X following endpoint is paginated and rate-limited. A full run over ~1k
accounts can take a long time. The command honors HTTP 429 reset headers and
waits before continuing.

The command strips embedded NUL bytes from Twitter text fields before inserting
rows, because PostgreSQL rejects `0x00` in text columns.

## Monitor A Running Snapshot

From another shell, check the running command:

```bash
pgrep -fl follow_snapshot
pgrep -fl twitter-fetcher
```

During `--manage-fetcher` runs, `twitter-fetcher` should be absent until the
snapshot exits.

Read progress from the database:

```bash
cd frontend
set -a; source ../.env; set +a
node --input-type=module -e '
import postgres from "postgres";
const sql = postgres(process.env.DATABASE_URL, { max: 1 });
const rows = await sql`
  SELECT id, status, started_at, completed_at,
         source_account_count, fetched_account_count, edge_count, error
  FROM following_snapshots
  ORDER BY id DESC
  LIMIT 10
`;
console.table(rows);
await sql.end({ timeout: 5 });
'
```

For the current/latest snapshot edge count:

```bash
cd frontend
set -a; source ../.env; set +a
node --input-type=module -e '
import postgres from "postgres";
const sql = postgres(process.env.DATABASE_URL, { max: 1 });
const [snap] = await sql`
  SELECT id FROM following_snapshots ORDER BY id DESC LIMIT 1
`;
const rows = await sql`
  SELECT COUNT(*)::int AS edges,
         COUNT(DISTINCT source_user_id)::int AS source_accounts,
         COUNT(DISTINCT followed_user_id)::int AS followed_accounts
  FROM following_edges
  WHERE snapshot_id = ${snap.id}
`;
console.table(rows);
await sql.end({ timeout: 5 });
'
```

## Read Top Commonly Followed Accounts

Latest completed snapshot:

```bash
cd frontend
set -a; source ../.env; set +a
node --input-type=module -e '
import postgres from "postgres";
const sql = postgres(process.env.DATABASE_URL, { max: 1 });
const [snap] = await sql`
  SELECT id
  FROM following_snapshots
  WHERE status = ${"completed"}
  ORDER BY id DESC
  LIMIT 1
`;
const rows = await sql`
  SELECT followed_user_id,
         MIN(followed_username) AS username,
         MIN(followed_name) AS name,
         COUNT(DISTINCT source_user_id)::int AS followed_by_accounts,
         MAX(followed_followers_count)::int AS followers_count,
         MAX(followed_verified_type) AS verified_type
  FROM following_edges
  WHERE snapshot_id = ${snap.id}
  GROUP BY followed_user_id
  ORDER BY followed_by_accounts DESC, followers_count DESC NULLS LAST
  LIMIT 100
`;
console.table(rows);
await sql.end({ timeout: 5 });
'
```

Do not use `failed` snapshots for final ranking; they can contain partial edges
from the account that failed. Use `status = 'completed'` for official results.

Specific snapshot id:

```bash
cd frontend
set -a; source ../.env; set +a
export SNAPSHOT_ID=2
node --input-type=module -e '
import postgres from "postgres";
const snapshotId = Number(process.env.SNAPSHOT_ID);
const sql = postgres(process.env.DATABASE_URL, { max: 1 });
const rows = await sql`
  SELECT followed_user_id,
         MIN(followed_username) AS username,
         MIN(followed_name) AS name,
         COUNT(DISTINCT source_user_id)::int AS followed_by_accounts,
         MAX(followed_followers_count)::int AS followers_count,
         MAX(followed_verified_type) AS verified_type
  FROM following_edges
  WHERE snapshot_id = ${snapshotId}
  GROUP BY followed_user_id
  ORDER BY followed_by_accounts DESC, followers_count DESC NULLS LAST
  LIMIT 100
`;
console.table(rows);
await sql.end({ timeout: 5 });
'
```

## After A Run

Confirm the normal tweet fetcher is back:

```bash
pgrep -fl twitter-fetcher
launchctl print gui/$(id -u)/com.harryz.twitter-fetcher | sed -n '1,40p'
```

If it is not running:

```bash
launchctl bootstrap gui/$(id -u) ~/Library/LaunchAgents/com.harryz.twitter-fetcher.plist
launchctl enable gui/$(id -u)/com.harryz.twitter-fetcher
launchctl kickstart -k gui/$(id -u)/com.harryz.twitter-fetcher
```
