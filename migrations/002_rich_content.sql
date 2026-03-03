ALTER TABLE fetched_tweets
    ADD COLUMN author_display_name TEXT,
    ADD COLUMN quoted_tweet_url    TEXT,
    ADD COLUMN embedded_urls       TEXT[],
    ADD COLUMN image_urls          TEXT[],
    ADD COLUMN video_urls          TEXT[];
