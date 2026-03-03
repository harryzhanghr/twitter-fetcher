-- pending_snapshots: queue of snapshots to be taken.
-- Each tweet gets rows inserted on creation (one per configured delay).
CREATE TABLE pending_snapshots (
    id         BIGSERIAL PRIMARY KEY,
    tweet_id   TEXT NOT NULL REFERENCES fetched_tweets(tweet_id) ON DELETE CASCADE,
    label      TEXT NOT NULL,        -- "15m", "1h", "12h"
    due_at     TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE UNIQUE INDEX idx_pending_snapshots_unique ON pending_snapshots (tweet_id, label);
CREATE INDEX idx_pending_snapshots_due_at ON pending_snapshots (due_at);

-- engagement_snapshots: stores the captured metrics at each checkpoint.
CREATE TABLE engagement_snapshots (
    id          BIGSERIAL PRIMARY KEY,
    tweet_id    TEXT NOT NULL REFERENCES fetched_tweets(tweet_id) ON DELETE CASCADE,
    label       TEXT NOT NULL,        -- "15m", "1h", "12h"
    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    views       INTEGER NOT NULL DEFAULT 0,
    likes       INTEGER NOT NULL DEFAULT 0,
    reposts     INTEGER NOT NULL DEFAULT 0,
    quotes      INTEGER NOT NULL DEFAULT 0,
    replies     INTEGER NOT NULL DEFAULT 0,
    bookmarks   INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX idx_engagement_snapshots_tweet ON engagement_snapshots (tweet_id, label);
