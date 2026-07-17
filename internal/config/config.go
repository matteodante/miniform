package config

import (
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"golang.org/x/net/http/httpguts"
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
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("load .env: %w", err)
	}

	environment := envOr("MINIFORM_ENV", cartridgeconfig.Development)
	if environment != cartridgeconfig.Development && environment != cartridgeconfig.Production && environment != cartridgeconfig.Test {
		return nil, fmt.Errorf("invalid MINIFORM_ENV value %q", environment)
	}

	dataDirectory := envOr("MINIFORM_DATA_DIR", "storage")
	databaseFilename := envOr("MINIFORM_DATABASE_FILENAME", appName+".db")
	databasePath := strings.TrimSpace(os.Getenv("MINIFORM_DATABASE_PATH"))
	if databasePath == "" {
		if databaseFilename == "." || databaseFilename == ".." ||
			filepath.IsAbs(databaseFilename) || filepath.Base(databaseFilename) != databaseFilename {
			return nil, fmt.Errorf("MINIFORM_DATABASE_FILENAME must be a filename; use MINIFORM_DATABASE_PATH for a path")
		}
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
	logLevel, err := normalizeLogLevel(envOr("MINIFORM_LOG_LEVEL", logLevel))
	if err != nil {
		return nil, fmt.Errorf("invalid MINIFORM_LOG_LEVEL: %w", err)
	}
	port := envOr("MINIFORM_PORT", "8080")
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("invalid MINIFORM_PORT value %q", port)
	}
	webhookSignatureHeader := envOr("MINIFORM_WEBHOOK_SIGNATURE_HEADER", "X-Miniform-Signature")
	if !httpguts.ValidHeaderFieldName(webhookSignatureHeader) {
		return nil, fmt.Errorf("invalid MINIFORM_WEBHOOK_SIGNATURE_HEADER value %q", webhookSignatureHeader)
	}
	sessionTimeout, err := positiveEnvInt("MINIFORM_SESSION_TIMEOUT_SECONDS", 604800)
	if err != nil {
		return nil, err
	}
	maxInputFields, err := positiveEnvInt("MINIFORM_MAX_INPUT_FIELDS", 200)
	if err != nil {
		return nil, err
	}
	webhookRetryLimit, err := positiveEnvInt("MINIFORM_WEBHOOK_RETRY_LIMIT", 3)
	if err != nil {
		return nil, err
	}
	webhookBackoffSchedule := envOr("MINIFORM_WEBHOOK_BACKOFF_SCHEDULE", "1,5,15,60")
	if _, err := parsePositiveInts(webhookBackoffSchedule); err != nil {
		return nil, fmt.Errorf("invalid MINIFORM_WEBHOOK_BACKOFF_SCHEDULE value %q: %w", webhookBackoffSchedule, err)
	}
	cfg := &Config{
		Config: &cartridgeconfig.Config{
			AppName:          appName,
			Environment:      environment,
			Port:             port,
			LogLevel:         logLevel,
			DataDirectory:    dataDirectory,
			DatabaseFilename: databaseFilename,
			DatabasePath:     databasePath,
			LogsDirectory:    envOr("MINIFORM_LOGS_DIR", filepath.Join(dataDirectory, "logs")),
			LogsMaxSizeMB:    20,
			LogsMaxBackups:   10,
			LogsMaxAgeDays:   30,
			SessionSecret:    sessionSecret,
			SessionTimeout:   sessionTimeout,
		},
		MaxInputFields: maxInputFields,
		Webhook: WebhookConfig{
			SignatureHeader: webhookSignatureHeader,
			RetryLimit:      webhookRetryLimit,
			BackoffSchedule: webhookBackoffSchedule,
		},
	}
	return cfg, nil
}

func (cfg *Config) EnsureDirectories() error {
	for _, directory := range []string{cfg.DataDirectory, cfg.LogsDirectory, filepath.Dir(cfg.DatabasePath)} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create directory %q: %w", directory, err)
		}
	}
	return nil
}

func (cfg *Config) IsMatchaManaged() bool {
	return cfg.IsProduction() && strings.TrimSpace(os.Getenv("MATCHA_MANAGER_VERSION")) != ""
}

func environmentDatabasePath(directory, filename, environment string) string {
	extension := filepath.Ext(filename)
	base := strings.TrimSuffix(filename, extension)
	if extension == "" {
		extension = ".db"
	}
	filename = fmt.Sprintf("%s.%s%s", base, environment, extension)
	return filepath.Join(directory, filename)
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func positiveEnvInt(key string, fallback int) (int, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("invalid %s value %q: must be a positive integer", key, raw)
	}
	return value, nil
}

func normalizeLogLevel(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return "debug", nil
	case "info":
		return "info", nil
	case "warn", "warning":
		return "warn", nil
	case "error":
		return "error", nil
	default:
		return "", fmt.Errorf("unsupported value %q", value)
	}
}

func (cfg *Config) WebhookBackoff() []int {
	schedule, err := parsePositiveInts(cfg.Webhook.BackoffSchedule)
	if err != nil {
		return []int{1, 5, 15, 60}
	}
	return schedule
}

func parsePositiveInts(value string) ([]int, error) {
	items := strings.Split(value, ",")
	schedule := make([]int, 0, len(items))
	for _, item := range items {
		seconds, err := strconv.Atoi(strings.TrimSpace(item))
		if err != nil || seconds <= 0 {
			return nil, fmt.Errorf("all delays must be positive integers")
		}
		schedule = append(schedule, seconds)
	}
	return schedule, nil
}
