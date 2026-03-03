package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/harryz/twitter-fetcher/internal/config"
	"github.com/harryz/twitter-fetcher/internal/db"
	"github.com/harryz/twitter-fetcher/internal/fetcher"
	"github.com/harryz/twitter-fetcher/internal/snapshotter"
	"github.com/harryz/twitter-fetcher/internal/twitter"
)

func main() {
	// 1. Load config from env.
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load config")
	}

	// Configure logger before anything else.
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	zerolog.SetGlobalLevel(level)
	if !cfg.LogJSON {
		log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})
	}

	// 2. Load refresh token (from file or macOS Keychain, based on TOKEN_STORE).
	refreshToken, err := config.LoadRefreshToken(cfg)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load refresh token")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// 3. Connect to database.
	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer pool.Close()
	log.Info().Msg("database connected")

	// 4. Build OAuth2 token provider with token rotation callback.
	tokenProvider := twitter.NewOAuth2TokenProvider(cfg.XClientID, refreshToken, func(newToken string) {
		if err := config.WriteRefreshToken(cfg, newToken); err != nil {
			log.Error().Err(err).Msg("failed to rotate refresh token")
		} else {
			log.Info().Str("store", cfg.TokenStore).Msg("refresh token rotated")
		}
	})

	// 5. Build Twitter client.
	twitterClient := twitter.NewClient(tokenProvider)

	// 6. Build and start fetcher + snapshotter.
	queries := db.New(pool)

	delays, err := config.ParseSnapshotDelays(cfg.SnapshotDelays)
	if err != nil {
		log.Fatal().Err(err).Msg("invalid snapshot delays")
	}

	f := fetcher.New(cfg, queries, twitterClient, delays)
	go f.Run(ctx)
	log.Info().
		Int("poll_interval_seconds", cfg.PollIntervalSeconds).
		Msg("fetcher started")

	snap := snapshotter.New(cfg, queries, twitterClient, delays)
	go snap.Run(ctx)
	log.Info().
		Int("check_interval_seconds", cfg.SnapshotCheckInterval).
		Int("delays", len(delays)).
		Msg("snapshotter started")

	// 7. Block until SIGINT or SIGTERM, then drain gracefully.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info().Msg("shutting down")
	cancel()
	time.Sleep(2 * time.Second)
	log.Info().Msg("goodbye")
}
