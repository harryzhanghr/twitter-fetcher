package config

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	XClientID            string `envconfig:"X_CLIENT_ID" required:"true"`
	DatabaseURL          string `envconfig:"DATABASE_URL" required:"true"`
	PollIntervalSeconds  int    `envconfig:"POLL_INTERVAL_SECONDS" default:"300"`
	MaxResultsPerFetch   int    `envconfig:"MAX_RESULTS_PER_FETCH" default:"100"`
	InitialLookbackMinutes int    `envconfig:"INITIAL_LOOKBACK_MINUTES" default:"5"`
	LogLevel               string   `envconfig:"LOG_LEVEL" default:"info"`
	LogJSON                bool     `envconfig:"LOG_JSON" default:"true"`
	SnapshotDelays         []string `envconfig:"SNAPSHOT_DELAYS" default:"15m,30m,45m,60m"`
	SnapshotCheckInterval  int      `envconfig:"SNAPSHOT_CHECK_INTERVAL_SECONDS" default:"60"`
	SnapshotBatchSize      int      `envconfig:"SNAPSHOT_BATCH_SIZE" default:"100"`
}

// SnapshotDelay maps a human-readable label to a parsed duration.
type SnapshotDelay struct {
	Label    string
	Duration time.Duration
}

// ParseSnapshotDelays converts string delay values to typed SnapshotDelay entries.
func ParseSnapshotDelays(delays []string) ([]SnapshotDelay, error) {
	result := make([]SnapshotDelay, 0, len(delays))
	for _, s := range delays {
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("parse snapshot delay %q: %w", s, err)
		}
		result = append(result, SnapshotDelay{Label: s, Duration: d})
	}
	return result, nil
}

func Load() (*Config, error) {
	var cfg Config
	if err := envconfig.Process("", &cfg); err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	return &cfg, nil
}

// LoadKeychain retrieves a secret from macOS Keychain for the current user.
// It shells out to `security find-generic-password` to avoid CGo dependencies.
func LoadKeychain(service string) (string, error) {
	user := os.Getenv("USER")
	if user == "" {
		return "", fmt.Errorf("$USER env var not set")
	}
	cmd := exec.Command("security", "find-generic-password", "-w", "-a", user, "-s", service)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("keychain lookup for %q: %w", service, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// WriteKeychain stores or updates a secret in macOS Keychain for the current user.
func WriteKeychain(service, secret string) error {
	user := os.Getenv("USER")
	if user == "" {
		return fmt.Errorf("$USER env var not set")
	}
	cmd := exec.Command("security", "add-generic-password", "-U", "-a", user, "-s", service, "-w", secret)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("keychain write for %q: %w: %s", service, err, strings.TrimSpace(string(out)))
	}
	return nil
}
