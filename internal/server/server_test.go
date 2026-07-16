package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

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
