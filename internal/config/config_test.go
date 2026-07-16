package config

import (
	"os"
	"testing"
)

func TestConfig(t *testing.T) {
	t.Run("Get returns config with defaults", func(t *testing.T) {
		defer Reset()
		os.Clearenv()
		os.Setenv("MINIFORM_ENV", "development")

		cfg := Get()
		if cfg == nil {
			t.Fatal("Get() returned nil")
		}

		if cfg.Environment != "development" {
			t.Errorf("Expected Environment=development, got %s", cfg.Environment)
		}
		if cfg.Port != "8080" {
			t.Errorf("Expected Port=8080, got %s", cfg.Port)
		}
		if cfg.LogLevel != "info" {
			t.Errorf("Expected LogLevel=info (development default), got %s", cfg.LogLevel)
		}
		if cfg.DataDirectory != "storage" {
			t.Errorf("Expected DataDirectory=storage, got %s", cfg.DataDirectory)
		}
	})

	t.Run("defaults Environment to development when MINIFORM_ENV is unset", func(t *testing.T) {
		defer Reset()
		os.Clearenv()
		// Intentionally do not set MINIFORM_ENV.

		cfg := Get()
		if cfg.Environment != "development" {
			t.Errorf("Expected default Environment=development, got %s", cfg.Environment)
		}
	})

	t.Run("Reset creates new config instance", func(t *testing.T) {
		defer Reset()
		os.Clearenv()
		os.Setenv("MINIFORM_ENV", "development")

		cfg1 := Get()
		if cfg1 == nil {
			t.Fatal("First Get() returned nil")
		}

		Reset()
		os.Setenv("MINIFORM_ENV", "development")

		cfg2 := Get()
		if cfg2 == nil {
			t.Fatal("Second Get() after Reset() returned nil")
		}

		if cfg1 == cfg2 {
			t.Error("Expected different config instances after Reset()")
		}
	})

	t.Run("loads every documented dotenv override", func(t *testing.T) {
		defer Reset()
		os.Clearenv()
		t.Chdir(t.TempDir())

		dotenv := []byte("MINIFORM_ENV=test\n" +
			"MINIFORM_DATA_DIR=custom-data\n" +
			"MINIFORM_DATABASE_FILENAME=custom.db\n" +
			"MINIFORM_LOGS_DIR=custom-logs\n" +
			"MINIFORM_SESSION_TIMEOUT_SECONDS=321\n" +
			"MINIFORM_MAX_INPUT_FIELDS=123\n" +
			"MINIFORM_WEBHOOK_SIGNATURE_HEADER=X-Custom-Signature\n" +
			"MINIFORM_WEBHOOK_RETRY_LIMIT=7\n" +
			"MINIFORM_WEBHOOK_BACKOFF_SCHEDULE=2,4,8\n")
		if err := os.WriteFile(".env", dotenv, 0o600); err != nil {
			t.Fatalf("write .env: %v", err)
		}

		cfg := Get()
		if cfg.DatabasePath != "custom-data/custom.test.db" {
			t.Errorf("DatabasePath = %q, want custom-data/custom.test.db", cfg.DatabasePath)
		}
		if cfg.LogsDirectory != "custom-logs" {
			t.Errorf("LogsDirectory = %q, want custom-logs", cfg.LogsDirectory)
		}
		if cfg.SessionTimeout != 321 {
			t.Errorf("SessionTimeout = %d, want 321", cfg.SessionTimeout)
		}
		if cfg.MaxInputFields != 123 {
			t.Errorf("MaxInputFields = %d, want 123", cfg.MaxInputFields)
		}
		if cfg.Webhook.SignatureHeader != "X-Custom-Signature" || cfg.Webhook.RetryLimit != 7 || cfg.Webhook.BackoffSchedule != "2,4,8" {
			t.Errorf("Webhook config = %#v, want dotenv overrides", cfg.Webhook)
		}
	})

	t.Run("Load returns validation errors to CLI callers", func(t *testing.T) {
		defer Reset()
		os.Clearenv()
		t.Chdir(t.TempDir())
		os.Setenv("MINIFORM_ENV", "invalid")

		if _, err := Load(); err == nil {
			t.Fatal("Load() error = nil, want invalid environment error")
		}
	})
}

func TestEnvironmentVariableOverrides(t *testing.T) {
	tests := []struct {
		name     string
		envVar   string
		envValue string
		setup    func()
		check    func(*Config) error
	}{
		{
			name:     "MINIFORM_ENV",
			envVar:   "MINIFORM_ENV",
			envValue: "development",
			setup:    func() {},
			check: func(c *Config) error {
				if c.Environment != "development" {
					t.Errorf("Expected Environment=development, got %s", c.Environment)
				}
				return nil
			},
		},
		{
			name:     "MINIFORM_PORT",
			envVar:   "MINIFORM_PORT",
			envValue: "3000",
			setup: func() {
				os.Setenv("MINIFORM_ENV", "development")
			},
			check: func(c *Config) error {
				if c.Port != "3000" {
					t.Errorf("Expected Port=3000, got %s", c.Port)
				}
				return nil
			},
		},
		{
			name:     "MINIFORM_LOG_LEVEL",
			envVar:   "MINIFORM_LOG_LEVEL",
			envValue: "debug",
			setup: func() {
				os.Setenv("MINIFORM_ENV", "development")
			},
			check: func(c *Config) error {
				if c.LogLevel != "debug" {
					t.Errorf("Expected LogLevel=debug, got %s", c.LogLevel)
				}
				return nil
			},
		},
		{
			name:     "MINIFORM_DATA_DIR",
			envVar:   "MINIFORM_DATA_DIR",
			envValue: "/custom/path",
			setup: func() {
				os.Setenv("MINIFORM_ENV", "development")
			},
			check: func(c *Config) error {
				if c.DataDirectory != "/custom/path" {
					t.Errorf("Expected DataDirectory=/custom/path, got %s", c.DataDirectory)
				}
				return nil
			},
		},
		{
			name:     "MINIFORM_SESSION_SECRET",
			envVar:   "MINIFORM_SESSION_SECRET",
			envValue: "custom-secret-123",
			setup: func() {
				os.Setenv("MINIFORM_ENV", "production")
			},
			check: func(c *Config) error {
				if c.SessionSecret != "custom-secret-123" {
					t.Errorf("Expected SessionSecret=custom-secret-123, got %s", c.SessionSecret)
				}
				return nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset()
			os.Clearenv()
			tt.setup()
			os.Setenv(tt.envVar, tt.envValue)

			cfg := Get()
			tt.check(cfg)
		})
	}
}

func TestSessionSecret(t *testing.T) {
	t.Run("production requires secret", func(t *testing.T) {
		Reset()
		os.Clearenv()
		os.Setenv("MINIFORM_ENV", "production")
		os.Setenv("MINIFORM_SESSION_SECRET", "required-secret")

		cfg := Get()
		if cfg.SessionSecret != "required-secret" {
			t.Errorf("Expected SessionSecret=required-secret in production, got %s", cfg.SessionSecret)
		}
	})

	t.Run("development uses fixed dev secret", func(t *testing.T) {
		Reset()
		os.Clearenv()
		os.Setenv("MINIFORM_ENV", "development")

		cfg := Get()
		if cfg.SessionSecret == "" {
			t.Error("Expected SessionSecret to be auto-generated in development")
		}
		if cfg.SessionSecret != "dev-secret-do-not-use-in-production-f8e3a9c2d1b7e6a4" {
			t.Errorf("Expected fixed dev secret, got %s", cfg.SessionSecret)
		}
	})

	t.Run("test uses fixed dev secret", func(t *testing.T) {
		Reset()
		os.Clearenv()
		os.Setenv("MINIFORM_ENV", "test")

		cfg := Get()
		if cfg.SessionSecret == "" {
			t.Error("Expected SessionSecret to be auto-generated in test")
		}
		if cfg.SessionSecret != "dev-secret-do-not-use-in-production-f8e3a9c2d1b7e6a4" {
			t.Errorf("Expected fixed dev secret, got %s", cfg.SessionSecret)
		}
	})
}

func TestIsEnvironment(t *testing.T) {
	tests := []struct {
		env      string
		wantProd bool
		wantDev  bool
		wantTest bool
	}{
		{"production", true, false, false},
		{"development", false, true, false},
		{"test", false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			Reset()
			os.Clearenv()
			os.Setenv("MINIFORM_ENV", tt.env)
			os.Setenv("MINIFORM_SESSION_SECRET", "test-secret")

			cfg := Get()
			if cfg.IsProduction() != tt.wantProd {
				t.Errorf("IsProduction() = %v, want %v", cfg.IsProduction(), tt.wantProd)
			}
			if cfg.IsDevelopment() != tt.wantDev {
				t.Errorf("IsDevelopment() = %v, want %v", cfg.IsDevelopment(), tt.wantDev)
			}
			if cfg.IsTest() != tt.wantTest {
				t.Errorf("IsTest() = %v, want %v", cfg.IsTest(), tt.wantTest)
			}
		})
	}
}

func TestDatabasePath(t *testing.T) {
	tests := []struct {
		name     string
		env      string
		wantPath string
	}{
		{"development environment", "development", "storage/miniform.development.db"},
		{"production environment", "production", "storage/miniform.production.db"},
		{"test environment", "test", "storage/miniform.test.db"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			Reset()
			os.Clearenv()
			os.Setenv("MINIFORM_ENV", tt.env)
			os.Setenv("MINIFORM_SESSION_SECRET", "test-secret")

			cfg := Get()
			if cfg.DatabasePath != tt.wantPath {
				t.Errorf("DatabasePath = %v, want %v", cfg.DatabasePath, tt.wantPath)
			}
		})
	}
}

func TestConnectionPooling(t *testing.T) {
	tests := []struct {
		env         string
		wantMaxOpen int
		wantMaxIdle int
	}{
		{"production", 10, 5},
		{"development", 1, 1},
		{"test", 1, 1},
	}

	for _, tt := range tests {
		t.Run(tt.env, func(t *testing.T) {
			Reset()
			os.Clearenv()
			os.Setenv("MINIFORM_ENV", tt.env)
			os.Setenv("MINIFORM_SESSION_SECRET", "test-secret")

			cfg := Get()
			if cfg.GetMaxOpenConns() != tt.wantMaxOpen {
				t.Errorf("GetMaxOpenConns() = %v, want %v", cfg.GetMaxOpenConns(), tt.wantMaxOpen)
			}
			if cfg.GetMaxIdleConns() != tt.wantMaxIdle {
				t.Errorf("GetMaxIdleConns() = %v, want %v", cfg.GetMaxIdleConns(), tt.wantMaxIdle)
			}
		})
	}
}

func TestWebhookBackoff(t *testing.T) {
	t.Run("returns expected backoff schedule", func(t *testing.T) {
		Reset()
		os.Clearenv()
		os.Setenv("MINIFORM_ENV", "development")

		cfg := Get()
		backoff := cfg.WebhookBackoff()

		expected := []int{1, 5, 15, 60}
		if len(backoff) != len(expected) {
			t.Errorf("WebhookBackoff() length = %v, want %v", len(backoff), len(expected))
		}

		for i, v := range expected {
			if backoff[i] != v {
				t.Errorf("WebhookBackoff()[%d] = %v, want %v", i, backoff[i], v)
			}
		}
	})
}
