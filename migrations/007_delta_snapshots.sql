-- Wipe old engagement data (incompatible labels, no baseline).
DELETE FROM engagement_snapshots;
DELETE FROM pending_snapshots;

-- Upgrade to unique index for idempotent baseline inserts.
DROP INDEX IF EXISTS idx_engagement_snapshots_tweet;
CREATE UNIQUE INDEX idx_engagement_snapshots_tweet_label
    ON engagement_snapshots (tweet_id, label);
