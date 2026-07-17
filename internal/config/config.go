package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/spf13/viper"
)

const appName = "miniform"

type Config struct {
	*cartridgeconfig.Config
	AnonSalt       string        `mapstructure:"anonsalt"`
	MaxInputFields int           `mapstructure:"maxinputfields"`
	Webhook        WebhookConfig `mapstructure:"webhook"`
}

type WebhookConfig struct {
	SignatureHeader string `mapstructure:"signatureheader"`
	RetryLimit      int    `mapstructure:"retrylimit"`
	BackoffSchedule string `mapstructure:"backoffschedule"`
}

var (
	configOnce sync.Once
	instance   *Config
	loadError  error
)

func Get() (*Config, error) {
	configOnce.Do(func() { instance, loadError = Load() })
	return instance, loadError
}

func Load() (*Config, error) {
	restore, err := importDotEnv()
	if err != nil {
		return nil, err
	}
	defer restore()
	if os.Getenv("MINIFORM_ENV") == "" {
		_ = os.Setenv("MINIFORM_ENV", cartridgeconfig.Development)
		defer func() { _ = os.Unsetenv("MINIFORM_ENV") }()
	}

	base, err := cartridgeconfig.Load(appName)
	if err != nil {
		return nil, err
	}
	applyBaseEnvironment(base)
	cfg := &Config{
		Config:         base,
		AnonSalt:       strings.TrimSpace(os.Getenv("MINIFORM_ANON_SALT")),
		MaxInputFields: positiveEnvInt("MINIFORM_MAX_INPUT_FIELDS", 200),
		Webhook: WebhookConfig{
			SignatureHeader: envOr("MINIFORM_WEBHOOK_SIGNATURE_HEADER", "X-Miniform-Signature"),
			RetryLimit:      positiveEnvInt("MINIFORM_WEBHOOK_RETRY_LIMIT", 3),
			BackoffSchedule: envOr("MINIFORM_WEBHOOK_BACKOFF_SCHEDULE", "1,5,15,60"),
		},
	}
	return cfg, nil
}

func importDotEnv() (func(), error) {
	reader := viper.New()
	reader.SetConfigName(".env")
	reader.SetConfigType("env")
	reader.AddConfigPath(".")
	if err := reader.ReadInConfig(); err != nil {
		var missing viper.ConfigFileNotFoundError
		if errors.As(err, &missing) {
			return func() {}, nil
		}
		return nil, fmt.Errorf("read .env: %w", err)
	}

	var imported []string
	for _, key := range reader.AllKeys() {
		environmentKey := strings.ToUpper(key)
		if !strings.HasPrefix(environmentKey, "MINIFORM_") {
			continue
		}
		if _, exists := os.LookupEnv(environmentKey); exists {
			continue
		}
		if err := os.Setenv(environmentKey, fmt.Sprint(reader.Get(key))); err != nil {
			for _, added := range imported {
				_ = os.Unsetenv(added)
			}
			return nil, fmt.Errorf("load %s from .env: %w", environmentKey, err)
		}
		imported = append(imported, environmentKey)
	}
	return func() {
		for _, key := range imported {
			_ = os.Unsetenv(key)
		}
	}, nil
}

func applyBaseEnvironment(base *cartridgeconfig.Config) {
	if value := strings.TrimSpace(os.Getenv("MINIFORM_DATABASE_FILENAME")); value != "" {
		base.DatabaseFilename = value
	}
	if value := strings.TrimSpace(os.Getenv("MINIFORM_LOGS_DIR")); value != "" {
		base.LogsDirectory = value
	}
	base.SessionTimeout = positiveEnvInt("MINIFORM_SESSION_TIMEOUT_SECONDS", base.SessionTimeout)
	if path := strings.TrimSpace(os.Getenv("MINIFORM_DATABASE_PATH")); path != "" {
		base.DatabasePath = path
	} else {
		base.DatabasePath = databasePath(base.DataDirectory, base.DatabaseFilename, base.Environment)
	}
	for _, directory := range []string{base.DataDirectory, base.LogsDirectory} {
		if directory != "" {
			_ = os.MkdirAll(directory, 0o700)
		}
	}
}

func databasePath(directory, filename, environment string) string {
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

func Reset() {
	configOnce = sync.Once{}
	instance = nil
	loadError = nil
}
