package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/matteodante/miniform/internal/config"
)

func TestLogger(t *testing.T) {
	t.Run("keeps the Miniform log level authoritative", func(t *testing.T) {
		t.Setenv("LOG_LEVEL", "debug")
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{
			Environment:   cartridgeconfig.Production,
			LogLevel:      "error",
			AppName:       "miniform",
			LogsDirectory: t.TempDir(),
		}}

		logger, closer := newLogger(cfg, io.Discard)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		assert.False(t, logger.Enabled(t.Context(), slog.LevelDebug))
		assert.True(t, logger.Enabled(t.Context(), slog.LevelError))
	})

	t.Run("closes the production log output idempotently", func(t *testing.T) {
		logsDirectory := t.TempDir()
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{
			Environment:   cartridgeconfig.Production,
			LogLevel:      "info",
			AppName:       "miniform",
			LogsDirectory: logsDirectory,
			LogsMaxSizeMB: 1,
		}}
		logger, closer := newLogger(cfg, io.Discard)
		require.NotNil(t, closer)

		logger.Info("before close")
		logPath := filepath.Join(logsDirectory, "miniform.log")
		content, err := os.ReadFile(logPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), `"msg":"before close"`)

		require.NoError(t, closer.Close())
		require.NoError(t, closer.Close())
		before, err := os.Stat(logPath)
		require.NoError(t, err)

		logger.Info("after close")
		after, err := os.Stat(logPath)
		require.NoError(t, err)
		assert.Equal(t, before.Size(), after.Size())
	})
}
