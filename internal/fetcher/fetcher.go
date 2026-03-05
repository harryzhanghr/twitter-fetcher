package fetcher

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/harryz/twitter-fetcher/internal/config"
	"github.com/harryz/twitter-fetcher/internal/db"
	"github.com/harryz/twitter-fetcher/internal/twitter"
)

const maxConcurrent = 10 // max simultaneous API requests

// Fetcher orchestrates polling and storing tweets for all enabled accounts.
type Fetcher struct {
	cfg     *config.Config
	queries *db.Queries
	client  *twitter.Client
	delays  []config.SnapshotDelay

	sema    chan struct{} // concurrency limiter
	rlMu    sync.Mutex
	rlUntil time.Time // don't make new requests until this time (shared rate-limit state)
}

// New creates a new Fetcher.
func New(cfg *config.Config, queries *db.Queries, client *twitter.Client, delays []config.SnapshotDelay) *Fetcher {
	return &Fetcher{
		cfg:     cfg,
		queries: queries,
		client:  client,
		delays:  delays,
		sema:    make(chan struct{}, maxConcurrent),
	}
}

// Run executes a poll cycle immediately on startup, then on every tick.
func (f *Fetcher) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(f.cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	f.runCycle(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.runCycle(ctx)
		}
	}
}

func (f *Fetcher) runCycle(ctx context.Context) {
	accounts, err := f.queries.GetEnabledAccounts(ctx)
	if err != nil {
		log.Error().Err(err).Msg("failed to load accounts")
		return
	}
	if len(accounts) == 0 {
		log.Debug().Msg("no enabled accounts")
		return
	}

	log.Info().Int("accounts", len(accounts)).Msg("starting cycle")

	var wg sync.WaitGroup
	for _, acc := range accounts {
		wg.Add(1)
		go func(a db.Account) {
			defer wg.Done()

			// Acquire semaphore slot — limits to maxConcurrent simultaneous requests.
			select {
			case f.sema <- struct{}{}:
			case <-ctx.Done():
				return
			}
			defer func() { <-f.sema }()

			// Wait if a rate limit is active (set by another goroutine).
			f.waitForRateLimit(ctx)

			if err := f.processAccount(ctx, a); err != nil {
				log.Error().Err(err).Str("username", a.Username).Msg("error processing account")
			}
		}(acc)
	}
	wg.Wait()
	log.Info().Msg("cycle finished")
}

// waitForRateLimit blocks until the shared rate-limit window has passed.
func (f *Fetcher) waitForRateLimit(ctx context.Context) {
	f.rlMu.Lock()
	until := f.rlUntil
	f.rlMu.Unlock()

	if wait := time.Until(until); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

// markRateLimit records the reset time so all goroutines pause until the window reopens.
func (f *Fetcher) markRateLimit(resetAt time.Time) {
	// Add a 5-second buffer after the reset to avoid hitting a not-yet-refreshed window.
	until := resetAt.Add(5 * time.Second)
	f.rlMu.Lock()
	if until.After(f.rlUntil) {
		f.rlUntil = until
	}
	f.rlMu.Unlock()
}

func (f *Fetcher) processAccount(ctx context.Context, account db.Account) error {
	req := twitter.UserTweetsRequest{
		UserID:     account.UserID,
		MaxResults: f.cfg.MaxResultsPerFetch,
	}

	if account.LastTweetID != "" {
		req.SinceID = account.LastTweetID
	} else {
		since := time.Now().UTC().Add(-time.Duration(f.cfg.InitialLookbackMinutes) * time.Minute)
		req.StartTime = since.Format(time.RFC3339)
	}

	resp, err := f.client.GetUserTweets(ctx, req)
	if err != nil {
		var rl twitter.RateLimitError
		if errors.As(err, &rl) {
			log.Warn().
				Str("username", account.Username).
				Time("resets_at", rl.ResetAt).
				Str("wait_until", rl.ResetAt.Add(5*time.Second).Format("15:04:05")).
				Msg("rate limited — waiting for reset then retrying")

			f.markRateLimit(rl.ResetAt)

			// Wait until the window resets, then retry once.
			f.waitForRateLimit(ctx)

			resp, err = f.client.GetUserTweets(ctx, req)
			if err != nil {
				if errors.As(err, &rl) {
					log.Warn().Str("username", account.Username).Msg("rate limited again after reset; skipping")
					return nil
				}
				return fmt.Errorf("retry after rate limit for %s: %w", account.Username, err)
			}
		} else {
			return fmt.Errorf("fetch tweets for %s: %w", account.Username, err)
		}
	}

	if len(resp.Data) == 0 {
		log.Debug().Str("username", account.Username).Msg("no new tweets")
		return nil
	}

	tweetMap, userMap, mediaMap := BuildLookupMaps(resp.Includes)

	var toStore []db.FetchedTweet
	var newestID string
	sourceTweetIDs := make(map[string]string) // tweet_id -> source tweet ID (original for retweets)

	for _, tweet := range resp.Data {
		authorUser, _ := userMap[tweet.AuthorID]

		result, ok := ClassifyTweet(tweet, authorUser, tweetMap, userMap, mediaMap)
		if !ok {
			continue
		}

		createdAt, err := time.Parse(time.RFC3339, tweet.CreatedAt)
		if err != nil {
			createdAt = time.Now().UTC()
		}

		text := tweet.Text
		if result.FullText != "" {
			text = result.FullText
		}

		// For retweets, resolve the original tweet ID for engagement tracking.
		sourceID := tweet.ID
		for _, ref := range tweet.ReferencedTweets {
			if ref.Type == "retweeted" {
				sourceID = ref.ID
				break
			}
		}
		sourceTweetIDs[tweet.ID] = sourceID

		toStore = append(toStore, db.FetchedTweet{
			TweetID:                   tweet.ID,
			AccountUserID:             account.UserID,
			TweetType:                 result.TweetType,
			TweetURL:                  result.TweetURL,
			Text:                      text,
			AuthorUsername:            authorUser.Username,
			AuthorDisplayName:         authorUser.Name,
			OriginalAuthorUsername:    result.OriginalAuthorUsername,
			OriginalAuthorDisplayName: result.OriginalAuthorDisplayName,
			QuotedTweetURL:            result.QuotedTweetURL,
			EmbeddedURLs:              result.EmbeddedURLs,
			ImageURLs:                 result.ImageURLs,
			VideoURLs:                 result.VideoURLs,
			CreatedAt:                 createdAt,
			Views:                     tweet.PublicMetrics.ImpressionCount,
			Likes:                     tweet.PublicMetrics.LikeCount,
			Reposts:                   tweet.PublicMetrics.RetweetCount,
			Quotes:                    tweet.PublicMetrics.QuoteCount,
			Replies:                   tweet.PublicMetrics.ReplyCount,
			Bookmarks:                 tweet.PublicMetrics.BookmarkCount,
		})

		if newestID == "" || tweet.ID > newestID {
			newestID = tweet.ID
		}
	}

	if err := f.queries.BatchUpsertTweets(ctx, toStore); err != nil {
		return fmt.Errorf("upsert tweets for %s: %w", account.Username, err)
	}

	// Capture "0m" baseline snapshots for all newly indexed tweets.
	if len(toStore) > 0 {
		var baselines []db.CapturedSnapshot
		for _, t := range toStore {
			sourceID := sourceTweetIDs[t.TweetID]
			if sourceID == "" {
				sourceID = t.TweetID
			}

			var views, likes, reposts, quotes, replies, bookmarks int
			if sourceID == t.TweetID {
				// Original tweet or quote tweet: use the tweet's own metrics.
				views = t.Views
				likes = t.Likes
				reposts = t.Reposts
				quotes = t.Quotes
				replies = t.Replies
				bookmarks = t.Bookmarks
			} else if orig, ok := tweetMap[sourceID]; ok {
				// Retweet: use the original tweet's metrics from includes.
				views = orig.PublicMetrics.ImpressionCount
				likes = orig.PublicMetrics.LikeCount
				reposts = orig.PublicMetrics.RetweetCount
				quotes = orig.PublicMetrics.QuoteCount
				replies = orig.PublicMetrics.ReplyCount
				bookmarks = orig.PublicMetrics.BookmarkCount
			} else {
				// Fallback: original not in includes (deleted?), use wrapper metrics.
				views = t.Views
				likes = t.Likes
				reposts = t.Reposts
				quotes = t.Quotes
				replies = t.Replies
				bookmarks = t.Bookmarks
			}

			baselines = append(baselines, db.CapturedSnapshot{
				TweetID:   t.TweetID,
				Label:     "0m",
				Views:     views,
				Likes:     likes,
				Reposts:   reposts,
				Quotes:    quotes,
				Replies:   replies,
				Bookmarks: bookmarks,
			})
		}
		if err := f.queries.InsertBaselineSnapshots(ctx, baselines); err != nil {
			log.Error().Err(err).Str("username", account.Username).Msg("failed to insert baseline snapshots")
		} else {
			log.Debug().Int("baselines", len(baselines)).Str("username", account.Username).Msg("baseline snapshots inserted")
		}
	}

	// Queue engagement snapshots for newly inserted tweets.
	// Use index time (now) as the base for due_at so snapshots are always in the
	// future — avoids collapsed snapshots when a tweet is fetched late.
	if len(f.delays) > 0 && len(toStore) > 0 {
		now := time.Now().UTC()
		var pending []db.PendingSnapshot
		for _, t := range toStore {
			sourceID := sourceTweetIDs[t.TweetID]
			if sourceID == "" {
				sourceID = t.TweetID
			}
			for _, delay := range f.delays {
				pending = append(pending, db.PendingSnapshot{
					TweetID:       t.TweetID,
					SourceTweetID: sourceID,
					Label:         delay.Label,
					DueAt:         now.Add(delay.Duration),
				})
			}
		}
		if err := f.queries.QueueSnapshots(ctx, pending); err != nil {
			log.Error().Err(err).Str("username", account.Username).Msg("failed to queue engagement snapshots")
		} else {
			log.Debug().Int("queued", len(pending)).Str("username", account.Username).Msg("engagement snapshots queued")
		}
	}

	log.Info().
		Str("username", account.Username).
		Int("fetched", len(resp.Data)).
		Int("stored", len(toStore)).
		Msg("cycle complete")

	cursorID := resp.Meta.NewestID
	if cursorID == "" {
		cursorID = newestID
	}
	if cursorID != "" {
		if err := f.queries.UpdateLastTweetID(ctx, account.UserID, cursorID); err != nil {
			return fmt.Errorf("advance cursor for %s: %w", account.Username, err)
		}
	}

	return nil
}
