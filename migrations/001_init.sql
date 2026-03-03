CREATE TYPE tweet_type AS ENUM ('tweet', 'quote_tweet', 'retweet');

CREATE TABLE twitter_accounts (
    user_id        TEXT PRIMARY KEY,
    username       TEXT NOT NULL,
    enabled        BOOLEAN NOT NULL DEFAULT TRUE,
    last_tweet_id  TEXT,
    last_polled_at TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE fetched_tweets (
    tweet_id                 TEXT PRIMARY KEY,
    account_user_id          TEXT NOT NULL REFERENCES twitter_accounts(user_id),
    tweet_type               tweet_type NOT NULL,
    tweet_url                TEXT NOT NULL,
    text                     TEXT NOT NULL,
    author_username          TEXT NOT NULL,
    original_author_username TEXT,
    created_at               TIMESTAMPTZ NOT NULL,
    fetched_at               TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_fetched_tweets_account_created ON fetched_tweets (account_user_id, created_at DESC);
CREATE INDEX idx_fetched_tweets_type ON fetched_tweets (tweet_type);
