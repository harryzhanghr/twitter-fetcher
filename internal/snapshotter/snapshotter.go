package snapshotter

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

// Snapshotter periodically checks for due engagement snapshots and captures metrics.
type Snapshotter struct {
	cfg     *config.Config
	queries *db.Queries
	client  *twitter.Client
	delays  []config.SnapshotDelay

	rlMu    sync.Mutex
	rlUntil time.Time
}

// New creates a new Snapshotter.
func New(cfg *config.Config, queries *db.Queries, client *twitter.Client, delays []config.SnapshotDelay) *Snapshotter {
	return &Snapshotter{
		cfg:     cfg,
		queries: queries,
		client:  client,
		delays:  delays,
	}
}

// Delays returns the parsed snapshot delays.
func (s *Snapshotter) Delays() []config.SnapshotDelay {
	return s.delays
}

// Run checks for due snapshots on every tick until ctx is cancelled.
func (s *Snapshotter) Run(ctx context.Context) {
	s.runCycle(ctx)

	ticker := time.NewTicker(time.Duration(s.cfg.SnapshotCheckInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runCycle(ctx)
		}
	}
}

func (s *Snapshotter) runCycle(ctx context.Context) {
	s.waitForRateLimit(ctx)

	due, err := s.queries.GetDueSnapshots(ctx, s.cfg.SnapshotBatchSize)
	if err != nil {
		log.Error().Err(err).Msg("snapshotter: failed to get due snapshots")
		return
	}
	if len(due) == 0 {
		log.Debug().Msg("snapshotter: no due snapshots")
		return
	}

	sourceIDs := uniqueSourceIDs(due)
	log.Info().Int("due", len(due)).Int("unique_sources", len(sourceIDs)).Msg("snapshotter: processing due snapshots")

	metricsMap, err := s.fetchMetrics(ctx, sourceIDs)
	if err != nil {
		log.Error().Err(err).Msg("snapshotter: failed to fetch metrics")
		return
	}

	var captured []db.CapturedSnapshot
	var missingIDs []int64
	for _, d := range due {
		m, ok := metricsMap[d.SourceTweetID]
		if !ok {
			missingIDs = append(missingIDs, d.ID)
			log.Warn().Str("tweet_id", d.TweetID).Str("source_tweet_id", d.SourceTweetID).Str("label", d.Label).Msg("snapshotter: source tweet not found, removing pending snapshot")
			continue
		}
		captured = append(captured, db.CapturedSnapshot{
			PendingID: d.ID,
			TweetID:   d.TweetID,
			Label:     d.Label,
			Views:     m.ImpressionCount,
			Likes:     m.LikeCount,
			Reposts:   m.RetweetCount,
			Quotes:    m.QuoteCount,
			Replies:   m.ReplyCount,
			Bookmarks: m.BookmarkCount,
		})
	}

	if err := s.queries.CompleteDueSnapshots(ctx, captured); err != nil {
		log.Error().Err(err).Msg("snapshotter: failed to complete snapshots")
		return
	}

	if len(missingIDs) > 0 {
		if err := s.queries.DeletePendingSnapshots(ctx, missingIDs); err != nil {
			log.Error().Err(err).Msg("snapshotter: failed to delete missing pending snapshots")
		}
	}

	log.Info().Int("captured", len(captured)).Int("missing", len(missingIDs)).Msg("snapshotter: cycle complete")
}

// fetchMetrics batch-fetches public metrics for the given tweet IDs in chunks of 100.
func (s *Snapshotter) fetchMetrics(ctx context.Context, tweetIDs []string) (map[string]twitter.PublicMetrics, error) {
	result := make(map[string]twitter.PublicMetrics, len(tweetIDs))

	for i := 0; i < len(tweetIDs); i += 100 {
		end := i + 100
		if end > len(tweetIDs) {
			end = len(tweetIDs)
		}
		chunk := tweetIDs[i:end]

		resp, err := s.client.GetTweets(ctx, chunk)
		if err != nil {
			var rl twitter.RateLimitError
			if errors.As(err, &rl) {
				log.Warn().Time("resets_at", rl.ResetAt).Msg("snapshotter: rate limited — waiting for reset")
				s.markRateLimit(rl.ResetAt)
				s.waitForRateLimit(ctx)

				resp, err = s.client.GetTweets(ctx, chunk)
				if err != nil {
					return nil, fmt.Errorf("retry chunk after rate limit: %w", err)
				}
			} else {
				return nil, fmt.Errorf("fetch tweets chunk: %w", err)
			}
		}

		for _, tweet := range resp.Data {
			result[tweet.ID] = tweet.PublicMetrics
		}
	}

	return result, nil
}

func (s *Snapshotter) waitForRateLimit(ctx context.Context) {
	s.rlMu.Lock()
	until := s.rlUntil
	s.rlMu.Unlock()

	if wait := time.Until(until); wait > 0 {
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
	}
}

func (s *Snapshotter) markRateLimit(resetAt time.Time) {
	until := resetAt.Add(5 * time.Second)
	s.rlMu.Lock()
	if until.After(s.rlUntil) {
		s.rlUntil = until
	}
	s.rlMu.Unlock()
}

func uniqueSourceIDs(due []db.DueSnapshot) []string {
	seen := make(map[string]struct{}, len(due))
	ids := make([]string, 0, len(due))
	for _, d := range due {
		if _, ok := seen[d.SourceTweetID]; !ok {
			seen[d.SourceTweetID] = struct{}{}
			ids = append(ids, d.SourceTweetID)
		}
	}
	return ids
}
