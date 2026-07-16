package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/matteodante/miniform/internal/config"
)

func TestErrorHandler(t *testing.T) {
	t.Run("hides unexpected errors in production JSON", func(t *testing.T) {
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{Environment: cartridgeconfig.Production}}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(logger, cfg)})
		app.Get("/failure", func(*fiber.Ctx) error {
			return errors.New("database password leaked")
		})

		req := httptest.NewRequest("GET", "/failure", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "Internal Server Error", body["message"])
		assert.NotContains(t, body["message"], "database password")
	})

	t.Run("preserves Fiber status and public message", func(t *testing.T) {
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{Environment: cartridgeconfig.Production}}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(logger, cfg)})
		app.Get("/missing", func(*fiber.Ctx) error {
			return fiber.ErrNotFound
		})

		req := httptest.NewRequest("GET", "/missing", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, 404, resp.StatusCode)
		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "Not Found", body["message"])
	})
}

func TestTemplateFuncs(t *testing.T) {
	funcs := TemplateFuncs()
	timestamp := time.Date(2026, time.July, 16, 16, 30, 0, 123456789, time.FixedZone("CEST", 2*60*60))

	t.Run("serializes timestamps as RFC3339 UTC", func(t *testing.T) {
		format := funcs["timeRFC3339"].(func(any) string)

		assert.Equal(t, "2026-07-16T14:30:00.123456789Z", format(timestamp))
		assert.Equal(t, "2026-07-16T14:30:00.123456789Z", format(&timestamp))
	})

	t.Run("formats UTC fallbacks", func(t *testing.T) {
		format := funcs["formatUTC"].(func(any, string) string)

		assert.Equal(t, "16 Jul 2026 14:30 UTC", format(timestamp, "02 Jan 2006 15:04 UTC"))
		assert.Empty(t, format((*time.Time)(nil), time.RFC3339))
	})
}
