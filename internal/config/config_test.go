package config

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfig(t *testing.T) {
	t.Run("uses safe local defaults", func(t *testing.T) {
		cleanEnvironment(t)
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "development", cfg.Environment)
		assert.Equal(t, "8080", cfg.Port)
		assert.Equal(t, "info", cfg.LogLevel)
		assert.Equal(t, "storage/miniform.development.db", cfg.DatabasePath)
		assert.Equal(t, 200, cfg.MaxInputFields)
		assert.Equal(t, "X-Miniform-Signature", cfg.Webhook.SignatureHeader)
		assert.NotEmpty(t, cfg.SessionSecret)
	})

	t.Run("applies every Miniform environment override", func(t *testing.T) {
		cleanEnvironment(t)
		values := map[string]string{
			"MINIFORM_ENV": "production", "MINIFORM_SESSION_SECRET": "production-secret",
			"MINIFORM_PORT": "3000", "MINIFORM_DATA_DIR": "data", "MINIFORM_LOG_LEVEL": "debug",
			"MINIFORM_DATABASE_FILENAME": "custom.sqlite", "MINIFORM_LOGS_DIR": "logs",
			"MINIFORM_SESSION_TIMEOUT_SECONDS": "321", "MINIFORM_MAX_INPUT_FIELDS": "123",
			"MINIFORM_WEBHOOK_SIGNATURE_HEADER": "X-Signature", "MINIFORM_WEBHOOK_RETRY_LIMIT": "7",
			"MINIFORM_WEBHOOK_BACKOFF_SCHEDULE": "2,4,8", "MINIFORM_ANON_SALT": "pepper",
		}
		for key, value := range values {
			t.Setenv(key, value)
		}
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "3000", cfg.Port)
		assert.Equal(t, "data/custom.production.sqlite", cfg.DatabasePath)
		assert.Equal(t, "logs", cfg.LogsDirectory)
		assert.Equal(t, 321, cfg.SessionTimeout)
		assert.Equal(t, 123, cfg.MaxInputFields)
		assert.Equal(t, WebhookConfig{"X-Signature", 7, "2,4,8"}, cfg.Webhook)
		assert.Equal(t, "pepper", cfg.AnonSalt)
		assert.Equal(t, 10, cfg.GetMaxOpenConns())
	})

	t.Run("preserves an explicit error log level", func(t *testing.T) {
		cleanEnvironment(t)
		t.Setenv("MINIFORM_ENV", "test")
		t.Setenv("MINIFORM_LOG_LEVEL", "error")

		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "error", cfg.LogLevel)
	})

	t.Run("loads dotenv without leaking values into process environment", func(t *testing.T) {
		cleanEnvironment(t)
		require.NoError(t, os.WriteFile(".env", []byte(strings.Join([]string{
			"MINIFORM_ENV=test", "MINIFORM_DATA_DIR=dotenv-data",
			"MINIFORM_DATABASE_PATH=explicit/database.db", "MINIFORM_MAX_INPUT_FIELDS=77",
		}, "\n")), 0o600))
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "test", cfg.Environment)
		assert.Equal(t, "explicit/database.db", cfg.DatabasePath)
		assert.Equal(t, 77, cfg.MaxInputFields)
		_, leaked := os.LookupEnv("MINIFORM_MAX_INPUT_FIELDS")
		assert.False(t, leaked)
	})

	t.Run("real environment wins over dotenv", func(t *testing.T) {
		cleanEnvironment(t)
		require.NoError(t, os.WriteFile(".env", []byte("MINIFORM_ENV=test\nMINIFORM_PORT=9000\n"), 0o600))
		t.Setenv("MINIFORM_PORT", "4000")
		cfg, err := Load()
		require.NoError(t, err)
		assert.Equal(t, "4000", cfg.Port)
	})

	t.Run("rejects invalid environment", func(t *testing.T) {
		cleanEnvironment(t)
		t.Setenv("MINIFORM_ENV", "staging")
		_, err := Load()
		assert.Error(t, err)
	})

	t.Run("parses only positive retry delays", func(t *testing.T) {
		cfg := &Config{Webhook: WebhookConfig{BackoffSchedule: " 2, nope, -1, 8 "}}
		assert.Equal(t, []int{2, 8}, cfg.WebhookBackoff())
		cfg.Webhook.BackoffSchedule = "invalid"
		assert.Equal(t, []int{1, 5, 15, 60}, cfg.WebhookBackoff())
	})

	t.Run("reset rebuilds singleton", func(t *testing.T) {
		cleanEnvironment(t)
		first, err := Get()
		require.NoError(t, err)
		Reset()
		second, err := Get()
		require.NoError(t, err)
		assert.NotSame(t, first, second)
	})
}

func cleanEnvironment(t *testing.T) {
	t.Helper()
	original := os.Environ()
	os.Clearenv()
	t.Chdir(t.TempDir())
	Reset()
	t.Cleanup(func() {
		os.Clearenv()
		for _, entry := range original {
			key, value, _ := strings.Cut(entry, "=")
			_ = os.Setenv(key, value)
		}
		Reset()
	})
}
