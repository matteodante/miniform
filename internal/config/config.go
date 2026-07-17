package config

import (
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	cartridgeconfig "github.com/karloscodes/cartridge/config"
)

const appName = "miniform"

type Config struct {
	*cartridgeconfig.Config
	MaxInputFields int
	Webhook        WebhookConfig
}

type WebhookConfig struct {
	SignatureHeader string
	RetryLimit      int
	BackoffSchedule string
}

func Load() (*Config, error) {
	environment := envOr("MINIFORM_ENV", cartridgeconfig.Development)
	if environment != cartridgeconfig.Development && environment != cartridgeconfig.Production && environment != cartridgeconfig.Test {
		return nil, fmt.Errorf("invalid MINIFORM_ENV value %q", environment)
	}

	dataDirectory := envOr("MINIFORM_DATA_DIR", "storage")
	databaseFilename := envOr("MINIFORM_DATABASE_FILENAME", appName+".db")
	databasePath := strings.TrimSpace(os.Getenv("MINIFORM_DATABASE_PATH"))
	if databasePath == "" {
		databasePath = environmentDatabasePath(dataDirectory, databaseFilename, environment)
	}

	sessionSecret := strings.TrimSpace(os.Getenv("MINIFORM_SESSION_SECRET"))
	if sessionSecret == "" {
		// Matcha's current install contract injects PRIVATE_KEY.
		sessionSecret = strings.TrimSpace(os.Getenv("PRIVATE_KEY"))
	}
	if sessionSecret == "" && environment == cartridgeconfig.Production {
		return nil, fmt.Errorf("MINIFORM_SESSION_SECRET is required in production")
	}
	if sessionSecret == "" {
		sessionSecret = rand.Text()
	}

	logLevel := "info"
	if environment == cartridgeconfig.Production {
		logLevel = "error"
	}
	cfg := &Config{
		Config: &cartridgeconfig.Config{
			AppName:          appName,
			Environment:      environment,
			Port:             envOr("MINIFORM_PORT", "8080"),
			LogLevel:         envOr("MINIFORM_LOG_LEVEL", logLevel),
			DataDirectory:    dataDirectory,
			DatabaseFilename: databaseFilename,
			DatabasePath:     databasePath,
			LogsDirectory:    envOr("MINIFORM_LOGS_DIR", filepath.Join(dataDirectory, "logs")),
			LogsMaxSizeMB:    20,
			LogsMaxBackups:   10,
			LogsMaxAgeDays:   30,
			SessionSecret:    sessionSecret,
			SessionTimeout:   positiveEnvInt("MINIFORM_SESSION_TIMEOUT_SECONDS", 604800),
		},
		MaxInputFields: positiveEnvInt("MINIFORM_MAX_INPUT_FIELDS", 200),
		Webhook: WebhookConfig{
			SignatureHeader: envOr("MINIFORM_WEBHOOK_SIGNATURE_HEADER", "X-Miniform-Signature"),
			RetryLimit:      positiveEnvInt("MINIFORM_WEBHOOK_RETRY_LIMIT", 3),
			BackoffSchedule: envOr("MINIFORM_WEBHOOK_BACKOFF_SCHEDULE", "1,5,15,60"),
		},
	}
	return cfg, nil
}

func (cfg *Config) EnsureDirectories() error {
	for _, directory := range []string{cfg.DataDirectory, cfg.LogsDirectory} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create directory %q: %w", directory, err)
		}
	}
	return nil
}

func environmentDatabasePath(directory, filename, environment string) string {
	extension := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, extension)
	if extension == "" {
		extension = ".db"
	}
	filename = fmt.Sprintf("%s.%s%s", base, environment, extension)
	if filepath.IsAbs(filename) {
		return filename
	}
	return filepath.Join(directory, filename)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func positiveEnvInt(key string, fallback int) int {
	value, err := strconv.Atoi(strings.TrimSpace(os.Getenv(key)))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

func (cfg *Config) WebhookBackoff() []int {
	var schedule []int
	for _, item := range strings.Split(cfg.Webhook.BackoffSchedule, ",") {
		seconds, err := strconv.Atoi(strings.TrimSpace(item))
		if err == nil && seconds > 0 {
			schedule = append(schedule, seconds)
		}
	}
	if len(schedule) == 0 {
		return []int{1, 5, 15, 60}
	}
	return schedule
}
