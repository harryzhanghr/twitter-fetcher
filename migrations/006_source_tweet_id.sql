-- Add source_tweet_id to pending_snapshots so we fetch the original tweet's
-- metrics for retweets instead of the retweet wrapper.
ALTER TABLE pending_snapshots ADD COLUMN source_tweet_id TEXT;

-- Backfill: set source_tweet_id = tweet_id for all existing rows.
UPDATE pending_snapshots SET source_tweet_id = tweet_id WHERE source_tweet_id IS NULL;

-- Make it NOT NULL going forward.
ALTER TABLE pending_snapshots ALTER COLUMN source_tweet_id SET NOT NULL;
