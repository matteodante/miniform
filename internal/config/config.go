package config

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/karloscodes/cartridge/config"
	"github.com/spf13/viper"
)

const appName = "miniform"

// Config extends cartridge config with miniform-specific settings.
type Config struct {
	*config.Config

	// Privacy configuration.
	AnonSalt string `mapstructure:"anonsalt"`

	// Form limits.
	MaxInputFields int `mapstructure:"maxinputfields"`

	// Webhook configuration.
	Webhook WebhookConfig `mapstructure:"webhook"`
}

// WebhookConfig configures outbound webhook delivery.
type WebhookConfig struct {
	SignatureHeader string `mapstructure:"signatureheader"`
	RetryLimit      int    `mapstructure:"retrylimit"`
	BackoffSchedule string `mapstructure:"backoffschedule"`
}

var (
	cfgOnce sync.Once
	cfgInst *Config
)

// Get returns the singleton configuration instance.
func Get() *Config {
	cfgOnce.Do(func() {
		loaded, err := Load()
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		cfgInst = loaded
	})
	return cfgInst
}

// Load resolves one configuration instance and returns validation errors to callers.
func Load() (*Config, error) {
	loadDotEnv()

	// Default MINIFORM_ENV to development; production is opt-in.
	// Cartridge defaults to production, which marks the session cookie
	// Secure and breaks login on plain-HTTP self-hosted deploys.
	if os.Getenv("MINIFORM_ENV") == "" {
		if err := os.Setenv("MINIFORM_ENV", config.Development); err != nil {
			return nil, fmt.Errorf("set default environment: %w", err)
		}
	}

	base, err := config.Load(appName)
	if err != nil {
		return nil, err
	}
	applyBaseOverrides(base)

	v := viper.New()
	v.SetConfigName(".env")
	v.SetConfigType("env")
	v.AddConfigPath(".")
	_ = v.ReadInConfig()
	v.SetDefault("maxinputfields", 200)
	v.SetDefault("webhook.signatureheader", "X-Miniform-Signature")
	v.SetDefault("webhook.retrylimit", 3)
	v.SetDefault("webhook.backoffschedule", "1,5,15,60")
	_ = v.BindEnv("anonsalt", "MINIFORM_ANON_SALT")
	_ = v.BindEnv("maxinputfields", "MINIFORM_MAX_INPUT_FIELDS")
	_ = v.BindEnv("webhook.signatureheader", "MINIFORM_WEBHOOK_SIGNATURE_HEADER")
	_ = v.BindEnv("webhook.retrylimit", "MINIFORM_WEBHOOK_RETRY_LIMIT")
	_ = v.BindEnv("webhook.backoffschedule", "MINIFORM_WEBHOOK_BACKOFF_SCHEDULE")

	loaded := &Config{Config: base}
	if err := v.Unmarshal(loaded); err != nil {
		return nil, fmt.Errorf("unmarshal miniform configuration: %w", err)
	}
	return loaded, nil
}

var dotenvKeys = []string{
	"MINIFORM_ENV",
	"MINIFORM_PORT",
	"MINIFORM_SESSION_SECRET",
	"MINIFORM_ANON_SALT",
	"MINIFORM_LOG_LEVEL",
	"MINIFORM_DATA_DIR",
	"MINIFORM_DATABASE_FILENAME",
	"MINIFORM_DATABASE_PATH",
	"MINIFORM_LOGS_DIR",
	"MINIFORM_SESSION_TIMEOUT_SECONDS",
	"MINIFORM_DEBUG",
	"MINIFORM_MAX_INPUT_FIELDS",
	"MINIFORM_WEBHOOK_SIGNATURE_HEADER",
	"MINIFORM_WEBHOOK_RETRY_LIMIT",
	"MINIFORM_WEBHOOK_BACKOFF_SCHEDULE",
}

func loadDotEnv() {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		return
	}

	for _, key := range dotenvKeys {
		if _, exists := os.LookupEnv(key); exists || !v.IsSet(key) {
			continue
		}
		if err := os.Setenv(key, fmt.Sprint(v.Get(key))); err != nil {
			log.Printf("config: failed to load %s from .env: %v", key, err)
		}
	}
}

func applyBaseOverrides(base *config.Config) {
	if filename := strings.TrimSpace(os.Getenv("MINIFORM_DATABASE_FILENAME")); filename != "" {
		base.DatabaseFilename = filename
	}
	if logsDir := strings.TrimSpace(os.Getenv("MINIFORM_LOGS_DIR")); logsDir != "" {
		base.LogsDirectory = logsDir
	}
	if timeout := strings.TrimSpace(os.Getenv("MINIFORM_SESSION_TIMEOUT_SECONDS")); timeout != "" {
		if value, err := strconv.Atoi(timeout); err == nil && value > 0 {
			base.SessionTimeout = value
		}
	}

	if explicitPath := strings.TrimSpace(os.Getenv("MINIFORM_DATABASE_PATH")); explicitPath != "" {
		base.DatabasePath = explicitPath
		return
	}
	ext := filepath.Ext(base.DatabaseFilename)
	name := strings.TrimSuffix(base.DatabaseFilename, ext)
	if ext == "" {
		ext = ".db"
	}
	filename := fmt.Sprintf("%s.%s%s", name, base.Environment, ext)
	if filepath.IsAbs(filename) {
		base.DatabasePath = filename
		return
	}
	base.DatabasePath = filepath.Join(base.DataDirectory, filename)
}

// WebhookBackoff returns the parsed retry schedule for webhook delivery.
func (c *Config) WebhookBackoff() []int {
	if c.Webhook.BackoffSchedule == "" {
		return []int{1, 5, 15, 60}
	}
	parts := strings.Split(c.Webhook.BackoffSchedule, ",")
	backoff := make([]int, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if val, err := strconv.Atoi(part); err == nil {
			backoff = append(backoff, val)
		}
	}
	if len(backoff) == 0 {
		return []int{1, 5, 15, 60}
	}
	return backoff
}

// Reset clears the cached configuration; intended for tests.
func Reset() {
	cfgOnce = sync.Once{}
	cfgInst = nil
}
