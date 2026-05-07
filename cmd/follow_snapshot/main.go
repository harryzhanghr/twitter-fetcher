package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog/log"

	"github.com/harryz/twitter-fetcher/internal/config"
	"github.com/harryz/twitter-fetcher/internal/db"
	"github.com/harryz/twitter-fetcher/internal/twitter"
)

const fetcherLabel = "com.harryz.twitter-fetcher"
const fetcherPlist = "/Users/harry/Library/LaunchAgents/com.harryz.twitter-fetcher.plist"

type staticTokenProvider struct {
	token string
}

func (p staticTokenProvider) GetToken(context.Context) (string, error) {
	if p.token == "" {
		return "", fmt.Errorf("empty bearer token")
	}
	return p.token, nil
}

type options struct {
	auth          string
	enabledOnly   bool
	limitAccounts int
	manageFetcher bool
	maxResults    int
	sleep         time.Duration
	topN          int
}

type account struct {
	UserID   string
	Username string
}

type snapshotStats struct {
	processed int
	edges     int
}

func main() {
	var opts options
	flag.StringVar(&opts.auth, "auth", "auto", "auth mode: auto, bearer, or refresh")
	flag.BoolVar(&opts.enabledOnly, "enabled-only", false, "only snapshot enabled twitter_accounts rows")
	flag.IntVar(&opts.limitAccounts, "limit-accounts", 0, "optional account limit for testing")
	flag.BoolVar(&opts.manageFetcher, "manage-fetcher", false, "stop launchd tweet fetcher while using shared refresh token, then restart it")
	flag.IntVar(&opts.maxResults, "max-results", 1000, "max following users per API page")
	flag.DurationVar(&opts.sleep, "sleep", 3200*time.Millisecond, "delay between following API requests")
	flag.IntVar(&opts.topN, "top", 50, "number of commonly-followed accounts to print")
	flag.Parse()

	if err := run(context.Background(), opts); err != nil {
		log.Fatal().Err(err).Msg("following snapshot failed")
	}
}

func run(ctx context.Context, opts options) error {
	if err := loadDotEnv(".env"); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	authMode, err := selectAuthMode(opts)
	if err != nil {
		return err
	}

	restartFetcher := false
	if authMode == "refresh" {
		running, err := fetcherRunning()
		if err != nil {
			return err
		}
		if running && !opts.manageFetcher {
			return fmt.Errorf("tweet fetcher is running; refusing to consume the shared refresh token without --manage-fetcher")
		}
		if running {
			log.Info().Str("label", fetcherLabel).Msg("stopping tweet fetcher before shared refresh-token use")
			if err := stopFetcher(); err != nil {
				return err
			}
			restartFetcher = true
			defer func() {
				log.Info().Str("label", fetcherLabel).Msg("restarting tweet fetcher")
				if err := startFetcher(); err != nil {
					log.Error().Err(err).Msg("failed to restart tweet fetcher")
				}
			}()
		}
	}

	tokenProvider, err := buildTokenProvider(cfg, authMode)
	if err != nil {
		return err
	}

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect db: %w", err)
	}
	defer pool.Close()

	if err := ensureSchema(ctx, pool); err != nil {
		return err
	}

	accounts, err := loadAccounts(ctx, pool, opts.enabledOnly, opts.limitAccounts)
	if err != nil {
		return err
	}
	if len(accounts) == 0 {
		return fmt.Errorf("no accounts to snapshot")
	}

	snapshotID, err := createSnapshot(ctx, pool, len(accounts))
	if err != nil {
		return err
	}
	log.Info().
		Int64("snapshot_id", snapshotID).
		Int("accounts", len(accounts)).
		Str("auth", authMode).
		Bool("fetcher_will_restart", restartFetcher).
		Msg("created following snapshot")

	client := twitter.NewClient(tokenProvider)
	stats, runErr := captureFollowing(ctx, pool, client, snapshotID, accounts, opts)
	if runErr != nil {
		if err := finishSnapshot(ctx, pool, snapshotID, "failed", stats, runErr.Error()); err != nil {
			log.Error().Err(err).Msg("failed to mark snapshot failed")
		}
		return runErr
	}
	if err := finishSnapshot(ctx, pool, snapshotID, "completed", stats, ""); err != nil {
		return err
	}

	return printTop(ctx, pool, snapshotID, opts.topN)
}

func selectAuthMode(opts options) (string, error) {
	bearer := strings.TrimSpace(os.Getenv("X_BEARER_TOKEN"))
	if bearer == "" {
		bearer = strings.TrimSpace(os.Getenv("X_APP_BEARER_TOKEN"))
	}

	switch opts.auth {
	case "auto":
		if bearer != "" {
			return "bearer", nil
		}
		return "refresh", nil
	case "bearer":
		if bearer == "" {
			return "", fmt.Errorf("auth=bearer requires X_BEARER_TOKEN or X_APP_BEARER_TOKEN")
		}
		return "bearer", nil
	case "refresh":
		return "refresh", nil
	default:
		return "", fmt.Errorf("unknown auth mode %q", opts.auth)
	}
}

func buildTokenProvider(cfg *config.Config, authMode string) (twitter.TokenProvider, error) {
	switch authMode {
	case "bearer":
		bearer := strings.TrimSpace(os.Getenv("X_BEARER_TOKEN"))
		if bearer == "" {
			bearer = strings.TrimSpace(os.Getenv("X_APP_BEARER_TOKEN"))
		}
		return staticTokenProvider{token: bearer}, nil
	case "refresh":
		return buildRefreshTokenProvider(cfg)
	default:
		return nil, fmt.Errorf("unknown auth mode %q", authMode)
	}
}

func loadDotEnv(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		os.Setenv(key, strings.Trim(strings.TrimSpace(val), `"'`))
	}
	return nil
}

func buildRefreshTokenProvider(cfg *config.Config) (twitter.TokenProvider, error) {
	refreshToken, err := config.LoadRefreshToken(cfg)
	if err != nil {
		return nil, fmt.Errorf("load refresh token: %w", err)
	}
	provider := twitter.NewOAuth2TokenProvider(cfg.XClientID, refreshToken, func(newToken string) error {
		if err := config.WriteRefreshToken(cfg, newToken); err != nil {
			return err
		}
		log.Info().Str("store", cfg.TokenStore).Msg("refresh token rotated")
		return nil
	})
	return provider, nil
}

func ensureSchema(ctx context.Context, pool *pgxpool.Pool) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS following_snapshots (
		    id BIGSERIAL PRIMARY KEY,
		    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		    completed_at TIMESTAMPTZ,
		    status TEXT NOT NULL DEFAULT 'running',
		    source_account_count INTEGER NOT NULL DEFAULT 0,
		    fetched_account_count INTEGER NOT NULL DEFAULT 0,
		    edge_count INTEGER NOT NULL DEFAULT 0,
		    error TEXT
		)`,
		`CREATE TABLE IF NOT EXISTS following_edges (
		    snapshot_id BIGINT NOT NULL REFERENCES following_snapshots(id) ON DELETE CASCADE,
		    source_user_id TEXT NOT NULL REFERENCES twitter_accounts(user_id),
		    followed_user_id TEXT NOT NULL,
		    followed_username TEXT NOT NULL,
		    followed_name TEXT NOT NULL,
		    followed_description TEXT,
		    followed_verified BOOLEAN NOT NULL DEFAULT FALSE,
		    followed_verified_type TEXT,
		    followed_followers_count INTEGER,
		    followed_following_count INTEGER,
		    followed_tweet_count INTEGER,
		    followed_listed_count INTEGER,
		    captured_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		    PRIMARY KEY (snapshot_id, source_user_id, followed_user_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_following_edges_snapshot_followed
		    ON following_edges (snapshot_id, followed_user_id)`,
		`CREATE INDEX IF NOT EXISTS idx_following_edges_snapshot_source
		    ON following_edges (snapshot_id, source_user_id)`,
	}
	for _, stmt := range statements {
		if _, err := pool.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("ensure schema: %w", err)
		}
	}
	return nil
}

func loadAccounts(ctx context.Context, pool *pgxpool.Pool, enabledOnly bool, limit int) ([]account, error) {
	where := ""
	if enabledOnly {
		where = "WHERE enabled = TRUE"
	}
	limitSQL := ""
	if limit > 0 {
		limitSQL = "LIMIT " + strconv.Itoa(limit)
	}

	query := fmt.Sprintf(`SELECT user_id, username FROM twitter_accounts %s ORDER BY username %s`, where, limitSQL)
	rows, err := pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("load accounts: %w", err)
	}
	defer rows.Close()

	var accounts []account
	for rows.Next() {
		var a account
		if err := rows.Scan(&a.UserID, &a.Username); err != nil {
			return nil, fmt.Errorf("scan account: %w", err)
		}
		accounts = append(accounts, a)
	}
	return accounts, rows.Err()
}

func createSnapshot(ctx context.Context, pool *pgxpool.Pool, sourceCount int) (int64, error) {
	var id int64
	err := pool.QueryRow(ctx,
		`INSERT INTO following_snapshots (source_account_count)
		 VALUES ($1)
		 RETURNING id`,
		sourceCount,
	).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("create snapshot: %w", err)
	}
	return id, nil
}

func captureFollowing(ctx context.Context, pool *pgxpool.Pool, client *twitter.Client, snapshotID int64, accounts []account, opts options) (snapshotStats, error) {
	var stats snapshotStats
	for i, a := range accounts {
		edges, err := captureAccountFollowing(ctx, pool, client, snapshotID, a, opts)
		if err != nil {
			return stats, err
		}
		stats.processed++
		stats.edges += edges
		log.Info().
			Int("account_index", i+1).
			Int("accounts", len(accounts)).
			Str("username", a.Username).
			Int("edges", edges).
			Int("total_edges", stats.edges).
			Msg("captured following")
	}
	return stats, nil
}

func captureAccountFollowing(ctx context.Context, pool *pgxpool.Pool, client *twitter.Client, snapshotID int64, a account, opts options) (int, error) {
	var total int
	var paginationToken string

	for {
		resp, err := client.GetUserFollowing(ctx, twitter.UserFollowingRequest{
			UserID:          a.UserID,
			PaginationToken: paginationToken,
			MaxResults:      opts.maxResults,
		})
		if err != nil {
			var rl twitter.RateLimitError
			if errors.As(err, &rl) {
				if rl.ResetAt.IsZero() {
					log.Warn().Msg("rate limited with no reset time; sleeping 15 minutes")
					if err := sleepContext(ctx, 15*time.Minute); err != nil {
						return total, err
					}
				} else {
					wait := time.Until(rl.ResetAt.Add(5 * time.Second))
					log.Warn().Time("reset_at", rl.ResetAt).Dur("wait", wait).Msg("rate limited; waiting")
					if err := sleepContext(ctx, wait); err != nil {
						return total, err
					}
				}
				continue
			}
			return total, fmt.Errorf("fetch following for @%s (%s): %w", a.Username, a.UserID, err)
		}

		if len(resp.Data) > 0 {
			if err := insertEdges(ctx, pool, snapshotID, a.UserID, resp.Data); err != nil {
				return total, err
			}
			total += len(resp.Data)
		}

		paginationToken = resp.Meta.NextToken
		if paginationToken == "" {
			return total, nil
		}
		if opts.sleep > 0 {
			if err := sleepContext(ctx, opts.sleep); err != nil {
				return total, err
			}
		}
	}
}

func insertEdges(ctx context.Context, pool *pgxpool.Pool, snapshotID int64, sourceUserID string, users []twitter.UserInfo) error {
	batch := &pgx.Batch{}
	for _, u := range users {
		batch.Queue(
			`INSERT INTO following_edges
			    (snapshot_id, source_user_id, followed_user_id, followed_username,
			     followed_name, followed_description, followed_verified,
			     followed_verified_type, followed_followers_count,
			     followed_following_count, followed_tweet_count, followed_listed_count)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			 ON CONFLICT (snapshot_id, source_user_id, followed_user_id) DO NOTHING`,
			snapshotID,
			sourceUserID,
			cleanString(u.ID),
			cleanString(u.Username),
			cleanString(u.Name),
			nullableString(cleanString(u.Description)),
			u.Verified,
			nullableString(cleanString(u.VerifiedType)),
			u.PublicMetrics.FollowersCount,
			u.PublicMetrics.FollowingCount,
			u.PublicMetrics.TweetCount,
			u.PublicMetrics.ListedCount,
		)
	}

	results := pool.SendBatch(ctx, batch)
	defer results.Close()
	for range users {
		if _, err := results.Exec(); err != nil {
			return fmt.Errorf("insert following edge: %w", err)
		}
	}
	return nil
}

func finishSnapshot(ctx context.Context, pool *pgxpool.Pool, snapshotID int64, status string, stats snapshotStats, errText string) error {
	_, err := pool.Exec(ctx,
		`UPDATE following_snapshots
		    SET completed_at = NOW(),
		        status = $2,
		        fetched_account_count = $3,
		        edge_count = $4,
		        error = NULLIF($5, '')
		  WHERE id = $1`,
		snapshotID,
		status,
		stats.processed,
		stats.edges,
		errText,
	)
	if err != nil {
		return fmt.Errorf("finish snapshot: %w", err)
	}
	return nil
}

func printTop(ctx context.Context, pool *pgxpool.Pool, snapshotID int64, limit int) error {
	rows, err := pool.Query(ctx,
		`SELECT followed_user_id,
		        MIN(followed_username) AS username,
		        MIN(followed_name) AS name,
		        COUNT(DISTINCT source_user_id)::int AS followed_by_accounts,
		        MAX(followed_followers_count)::int AS followers_count
		   FROM following_edges
		  WHERE snapshot_id = $1
		  GROUP BY followed_user_id
		  ORDER BY followed_by_accounts DESC, followers_count DESC NULLS LAST
		  LIMIT $2`,
		snapshotID,
		limit,
	)
	if err != nil {
		return fmt.Errorf("query top followed: %w", err)
	}
	defer rows.Close()

	fmt.Printf("\nTop commonly followed accounts for snapshot %d\n", snapshotID)
	fmt.Printf("%-5s %-22s %-34s %-12s %-12s %s\n", "rank", "username", "name", "common", "followers", "user_id")
	rank := 1
	for rows.Next() {
		var userID, username, name string
		var common int
		var followers *int
		if err := rows.Scan(&userID, &username, &name, &common, &followers); err != nil {
			return fmt.Errorf("scan top followed: %w", err)
		}
		followerText := ""
		if followers != nil {
			followerText = strconv.Itoa(*followers)
		}
		fmt.Printf("%-5d @%-21s %-34s %-12d %-12s %s\n", rank, username, truncate(name, 34), common, followerText, userID)
		rank++
	}
	return rows.Err()
}

func fetcherRunning() (bool, error) {
	cmd := exec.Command("pgrep", "-f", "/Users/harry/twitter-fetcher/twitter-fetcher")
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)) != "", nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("check fetcher process: %w", err)
}

func stopFetcher() error {
	target := launchctlTarget()
	if err := runCommand("launchctl", "disable", target); err != nil {
		return err
	}
	if err := runCommand("launchctl", "bootout", target); err != nil {
		log.Warn().Err(err).Msg("launchctl bootout returned an error")
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		running, err := fetcherRunning()
		if err != nil {
			return err
		}
		if !running {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return fmt.Errorf("tweet fetcher still running after launchctl bootout")
}

func startFetcher() error {
	target := launchctlTarget()
	if err := runCommand("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), fetcherPlist); err != nil {
		log.Warn().Err(err).Msg("launchctl bootstrap returned an error")
	}
	if err := runCommand("launchctl", "enable", target); err != nil {
		return err
	}
	if err := runCommand("launchctl", "kickstart", "-k", target); err != nil {
		return err
	}
	return nil
}

func launchctlTarget() string {
	return fmt.Sprintf("gui/%d/%s", os.Getuid(), fetcherLabel)
}

func runCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

func cleanString(s string) string {
	return strings.ReplaceAll(s, "\x00", "")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
