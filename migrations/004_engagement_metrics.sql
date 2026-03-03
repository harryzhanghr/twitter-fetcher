ALTER TABLE fetched_tweets
    ADD COLUMN views     INTEGER,
    ADD COLUMN likes     INTEGER,
    ADD COLUMN reposts   INTEGER,
    ADD COLUMN quotes    INTEGER,
    ADD COLUMN replies   INTEGER,
    ADD COLUMN bookmarks INTEGER;
