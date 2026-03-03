package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TweetType represents the classification of a stored tweet.
type TweetType string

const (
	TweetTypeTweet      TweetType = "tweet"
	TweetTypeQuoteTweet TweetType = "quote_tweet"
	TweetTypeRetweet    TweetType = "retweet"
)

// Account represents a row from the twitter_accounts table.
type Account struct {
	UserID      string
	Username    string
	LastTweetID string
}

// FetchedTweet is the data to store for a single tweet.
type FetchedTweet struct {
	TweetID                   string
	AccountUserID             string
	TweetType                 TweetType
	TweetURL                  string
	Text                      string
	AuthorUsername            string
	AuthorDisplayName         string
	OriginalAuthorUsername     string
	OriginalAuthorDisplayName  string
	QuotedTweetURL             string
	EmbeddedURLs              []string
	ImageURLs                 []string
	VideoURLs                 []string
	CreatedAt                 time.Time
	Views                     int
	Likes                     int
	Reposts                   int
	Quotes                    int
	Replies                   int
	Bookmarks                 int
}

// Queries wraps a pgx pool with typed query methods.
type Queries struct {
	pool *pgxpool.Pool
}

// New creates a new Queries instance.
func New(pool *pgxpool.Pool) *Queries {
	return &Queries{pool: pool}
}

// GetEnabledAccounts returns all accounts with enabled=true.
func (q *Queries) GetEnabledAccounts(ctx context.Context) ([]Account, error) {
	rows, err := q.pool.Query(ctx,
		`SELECT user_id, username, COALESCE(last_tweet_id, '') FROM twitter_accounts WHERE enabled = TRUE`)
	if err != nil {
		return nil, fmt.Errorf("query accounts: %w", err)
	}
	defer rows.Close()

	var accounts []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.UserID, &a.Username, &a.LastTweetID); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

// UpdateLastTweetID advances the cursor for an account after a successful poll.
func (q *Queries) UpdateLastTweetID(ctx context.Context, userID, lastTweetID string) error {
	_, err := q.pool.Exec(ctx,
		`UPDATE twitter_accounts
		    SET last_tweet_id = $1, last_polled_at = NOW(), updated_at = NOW()
		  WHERE user_id = $2`,
		lastTweetID, userID)
	if err != nil {
		return fmt.Errorf("update last tweet id for %s: %w", userID, err)
	}
	return nil
}

// BatchUpsertTweets inserts tweets, ignoring conflicts on tweet_id.
func (q *Queries) BatchUpsertTweets(ctx context.Context, tweets []FetchedTweet) error {
	if len(tweets) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, t := range tweets {
		batch.Queue(
			`INSERT INTO fetched_tweets
			    (tweet_id, account_user_id, tweet_type, tweet_url, text,
			     author_username, author_display_name, original_author_username,
			     original_author_display_name, quoted_tweet_url,
			     embedded_urls, image_urls, video_urls, created_at,
			     views, likes, reposts, quotes, replies, bookmarks)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
			 ON CONFLICT (tweet_id) DO NOTHING`,
			t.TweetID,
			t.AccountUserID,
			string(t.TweetType),
			t.TweetURL,
			t.Text,
			t.AuthorUsername,
			nullableString(t.AuthorDisplayName),
			nullableString(t.OriginalAuthorUsername),
			nullableString(t.OriginalAuthorDisplayName),
			nullableString(t.QuotedTweetURL),
			nullableStringSlice(t.EmbeddedURLs),
			nullableStringSlice(t.ImageURLs),
			nullableStringSlice(t.VideoURLs),
			t.CreatedAt,
			t.Views,
			t.Likes,
			t.Reposts,
			t.Quotes,
			t.Replies,
			t.Bookmarks,
		)
	}

	results := q.pool.SendBatch(ctx, batch)
	defer results.Close()

	for range tweets {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("batch upsert tweet: %w", err)
		}
	}
	return nil
}

// PendingSnapshot represents a snapshot to schedule.
type PendingSnapshot struct {
	TweetID       string
	SourceTweetID string // the tweet ID to fetch metrics for (original tweet for retweets)
	Label         string
	DueAt         time.Time
}

// DueSnapshot represents a pending snapshot that is ready to be captured.
type DueSnapshot struct {
	ID            int64
	TweetID       string
	SourceTweetID string
	Label         string
}

// CapturedSnapshot holds metrics for one completed snapshot.
type CapturedSnapshot struct {
	PendingID int64
	TweetID   string
	Label     string
	Views     int
	Likes     int
	Reposts   int
	Quotes    int
	Replies   int
	Bookmarks int
}

// QueueSnapshots bulk-inserts pending snapshot rows.
func (q *Queries) QueueSnapshots(ctx context.Context, snapshots []PendingSnapshot) error {
	if len(snapshots) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, s := range snapshots {
		batch.Queue(
			`INSERT INTO pending_snapshots (tweet_id, source_tweet_id, label, due_at)
			 VALUES ($1, $2, $3, $4)
			 ON CONFLICT (tweet_id, label) DO NOTHING`,
			s.TweetID, s.SourceTweetID, s.Label, s.DueAt,
		)
	}

	results := q.pool.SendBatch(ctx, batch)
	defer results.Close()

	for range snapshots {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("queue snapshot: %w", err)
		}
	}
	return nil
}

// GetDueSnapshots returns pending snapshots where due_at <= NOW(), up to limit.
func (q *Queries) GetDueSnapshots(ctx context.Context, limit int) ([]DueSnapshot, error) {
	rows, err := q.pool.Query(ctx,
		`SELECT id, tweet_id, source_tweet_id, label
		   FROM pending_snapshots
		  WHERE due_at <= NOW()
		  ORDER BY due_at
		  LIMIT $1`,
		limit)
	if err != nil {
		return nil, fmt.Errorf("query due snapshots: %w", err)
	}
	defer rows.Close()

	var snapshots []DueSnapshot
	for rows.Next() {
		var s DueSnapshot
		if err := rows.Scan(&s.ID, &s.TweetID, &s.SourceTweetID, &s.Label); err != nil {
			return nil, fmt.Errorf("scan due snapshot: %w", err)
		}
		snapshots = append(snapshots, s)
	}
	return snapshots, rows.Err()
}

// CompleteDueSnapshots inserts engagement_snapshots rows and deletes the
// corresponding pending_snapshots rows in a single batch.
func (q *Queries) CompleteDueSnapshots(ctx context.Context, captured []CapturedSnapshot) error {
	if len(captured) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, c := range captured {
		batch.Queue(
			`INSERT INTO engagement_snapshots
			    (tweet_id, label, views, likes, reposts, quotes, replies, bookmarks)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			c.TweetID, c.Label, c.Views, c.Likes, c.Reposts, c.Quotes, c.Replies, c.Bookmarks,
		)
		batch.Queue(
			`DELETE FROM pending_snapshots WHERE id = $1`,
			c.PendingID,
		)
	}

	results := q.pool.SendBatch(ctx, batch)
	defer results.Close()

	for i := 0; i < len(captured)*2; i++ {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("complete snapshot batch op %d: %w", i, err)
		}
	}
	return nil
}

// InsertBaselineSnapshots inserts "0m" engagement_snapshots rows for newly
// indexed tweets. Uses ON CONFLICT DO NOTHING for idempotency.
func (q *Queries) InsertBaselineSnapshots(ctx context.Context, baselines []CapturedSnapshot) error {
	if len(baselines) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	for _, b := range baselines {
		batch.Queue(
			`INSERT INTO engagement_snapshots
			    (tweet_id, label, views, likes, reposts, quotes, replies, bookmarks)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			 ON CONFLICT (tweet_id, label) DO NOTHING`,
			b.TweetID, b.Label, b.Views, b.Likes, b.Reposts, b.Quotes, b.Replies, b.Bookmarks,
		)
	}

	results := q.pool.SendBatch(ctx, batch)
	defer results.Close()

	for range baselines {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("insert baseline snapshot: %w", err)
		}
	}
	return nil
}

// DeletePendingSnapshots removes pending_snapshots rows by their IDs.
func (q *Queries) DeletePendingSnapshots(ctx context.Context, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	batch := &pgx.Batch{}
	for _, id := range ids {
		batch.Queue(`DELETE FROM pending_snapshots WHERE id = $1`, id)
	}
	results := q.pool.SendBatch(ctx, batch)
	defer results.Close()
	for range ids {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("delete pending snapshot: %w", err)
		}
	}
	return nil
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func nullableStringSlice(s []string) interface{} {
	if len(s) == 0 {
		return nil
	}
	return s
}
