CREATE TABLE following_snapshots (
    id                   BIGSERIAL PRIMARY KEY,
    started_at           TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at         TIMESTAMPTZ,
    status               TEXT NOT NULL DEFAULT 'running',
    source_account_count INTEGER NOT NULL DEFAULT 0,
    fetched_account_count INTEGER NOT NULL DEFAULT 0,
    edge_count           INTEGER NOT NULL DEFAULT 0,
    error                TEXT
);

CREATE TABLE following_edges (
    snapshot_id               BIGINT NOT NULL REFERENCES following_snapshots(id) ON DELETE CASCADE,
    source_user_id            TEXT NOT NULL REFERENCES twitter_accounts(user_id),
    followed_user_id          TEXT NOT NULL,
    followed_username         TEXT NOT NULL,
    followed_name             TEXT NOT NULL,
    followed_description      TEXT,
    followed_verified         BOOLEAN NOT NULL DEFAULT FALSE,
    followed_verified_type    TEXT,
    followed_followers_count  INTEGER,
    followed_following_count  INTEGER,
    followed_tweet_count      INTEGER,
    followed_listed_count     INTEGER,
    captured_at               TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (snapshot_id, source_user_id, followed_user_id)
);

CREATE INDEX idx_following_edges_snapshot_followed
    ON following_edges (snapshot_id, followed_user_id);

CREATE INDEX idx_following_edges_snapshot_source
    ON following_edges (snapshot_id, source_user_id);
