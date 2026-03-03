package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/kelseyhightower/envconfig"
)

type Config struct {
	XClientID              string `envconfig:"X_CLIENT_ID" required:"true"`
	DatabaseURL            string `envconfig:"DATABASE_URL" required:"true"`
	PollIntervalSeconds    int    `envconfig:"POLL_INTERVAL_SECONDS" default:"300"`
	MaxResultsPerFetch     int    `envconfig:"MAX_RESULTS_PER_FETCH" default:"100"`
	InitialLookbackMinutes int    `envconfig:"INITIAL_LOOKBACK_MINUTES" default:"5"`
	LogLevel               string   `envconfig:"LOG_LEVEL" default:"info"`
	LogJSON                bool     `envconfig:"LOG_JSON" default:"true"`
	SnapshotDelays         []string `envconfig:"SNAPSHOT_DELAYS" default:"15m,30m,45m,60m"`
	SnapshotCheckInterval  int      `envconfig:"SNAPSHOT_CHECK_INTERVAL_SECONDS" default:"60"`
	SnapshotBatchSize      int      `envconfig:"SNAPSHOT_BATCH_SIZE" default:"100"`

	// Token storage: "file" (default, portable) or "keychain" (macOS only).
	TokenStore       string `envconfig:"TOKEN_STORE" default:"file"`
	KeychainService  string `envconfig:"KEYCHAIN_SERVICE" default:"twitter-fetcher.refresh_token"`
	RefreshTokenFile string `envconfig:"REFRESH_TOKEN_FILE" default:".refresh_token"`
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

// LoadRefreshToken reads the refresh token based on TOKEN_STORE config.
// For "file" mode: reads from RefreshTokenFile, falling back to X_REFRESH_TOKEN env var.
// For "keychain" mode: reads from macOS Keychain using KeychainService.
func LoadRefreshToken(cfg *Config) (string, error) {
	switch cfg.TokenStore {
	case "keychain":
		return LoadKeychain(cfg.KeychainService)
	case "file":
		data, err := os.ReadFile(cfg.RefreshTokenFile)
		if err == nil {
			token := strings.TrimSpace(string(data))
			if token != "" {
				return token, nil
			}
		}
		// Fall back to env var for initial bootstrap.
		if token := os.Getenv("X_REFRESH_TOKEN"); token != "" {
			return token, nil
		}
		return "", fmt.Errorf("no refresh token found: set X_REFRESH_TOKEN env var or run scripts/oauth_setup.py")
	default:
		return "", fmt.Errorf("unknown TOKEN_STORE %q: use \"file\" or \"keychain\"", cfg.TokenStore)
	}
}

// WriteRefreshToken persists a rotated refresh token based on TOKEN_STORE config.
// For "file" mode: writes atomically (temp file + rename) with 0600 permissions.
// For "keychain" mode: updates macOS Keychain.
func WriteRefreshToken(cfg *Config, token string) error {
	switch cfg.TokenStore {
	case "keychain":
		return WriteKeychain(cfg.KeychainService, token)
	case "file":
		dir := filepath.Dir(cfg.RefreshTokenFile)
		tmp, err := os.CreateTemp(dir, ".refresh_token_tmp_*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
		tmpName := tmp.Name()
		if _, err := tmp.WriteString(token + "\n"); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return fmt.Errorf("write temp file: %w", err)
		}
		if err := tmp.Chmod(0600); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return fmt.Errorf("chmod temp file: %w", err)
		}
		if err := tmp.Close(); err != nil {
			os.Remove(tmpName)
			return fmt.Errorf("close temp file: %w", err)
		}
		if err := os.Rename(tmpName, cfg.RefreshTokenFile); err != nil {
			os.Remove(tmpName)
			return fmt.Errorf("rename temp file: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unknown TOKEN_STORE %q", cfg.TokenStore)
	}
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
