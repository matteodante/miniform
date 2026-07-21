package internal

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRateLimitStorage(t *testing.T) {
	t.Run("stores copies and expires entries", func(t *testing.T) {
		storage := newRateLimitStorage()
		value := []byte("value")
		require.NoError(t, storage.Set("key", value, 10*time.Millisecond))
		value[0] = 'X'

		stored, err := storage.Get("key")
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), stored)
		stored[0] = 'Y'

		storedAgain, err := storage.Get("key")
		require.NoError(t, err)
		assert.Equal(t, []byte("value"), storedAgain)
		require.Eventually(t, func() bool {
			stored, getErr := storage.Get("key")
			return getErr == nil && stored == nil
		}, time.Second, time.Millisecond)
	})

	t.Run("implements idempotent lifecycle operations", func(t *testing.T) {
		storage := newRateLimitStorage()
		require.NoError(t, storage.Set("key", []byte("value"), 0))
		require.NoError(t, storage.Reset())
		stored, err := storage.Get("key")
		require.NoError(t, err)
		assert.Nil(t, stored)
		require.NoError(t, storage.Close())
		require.NoError(t, storage.Close())
	})

	t.Run("sweeps inactive expired entries during writes", func(t *testing.T) {
		storage := newRateLimitStorage()
		storage.entries["expired"] = rateLimitEntry{
			value:     []byte("old"),
			expiresAt: time.Now().Add(-time.Second),
		}

		require.NoError(t, storage.Set("current", []byte("new"), time.Minute))

		assert.NotContains(t, storage.entries, "expired")
		assert.Contains(t, storage.entries, "current")
	})

	t.Run("backs Fiber limiter state", func(t *testing.T) {
		app := fiber.New()
		app.Use(limiter.New(limiter.Config{
			Max:        1,
			Expiration: time.Minute,
			Storage:    newRateLimitStorage(),
		}))
		app.Get("/", func(ctx *fiber.Ctx) error { return ctx.SendStatus(fiber.StatusNoContent) })

		first, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), -1)
		require.NoError(t, err)
		require.NoError(t, first.Body.Close())
		assert.Equal(t, fiber.StatusNoContent, first.StatusCode)

		second, err := app.Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil), -1)
		require.NoError(t, err)
		require.NoError(t, second.Body.Close())
		assert.Equal(t, fiber.StatusTooManyRequests, second.StatusCode)
	})
}
