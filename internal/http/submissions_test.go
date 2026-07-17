package http

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadStream(t *testing.T) {
	request := func(t *testing.T) *fiber.App {
		t.Helper()
		path := filepath.Join(t.TempDir(), "report.txt")
		require.NoError(t, os.WriteFile(path, []byte("0123456789"), 0o600))
		modified := time.Date(2026, time.July, 17, 8, 0, 0, 0, time.UTC)
		require.NoError(t, os.Chtimes(path, modified, modified))

		app := fiber.New()
		app.Get("/download", func(fiberCtx *fiber.Ctx) error {
			source, err := os.Open(path)
			if err != nil {
				return err
			}
			info, err := source.Stat()
			if err != nil {
				_ = source.Close()
				return err
			}
			ctx := &cartridge.Context{Ctx: fiberCtx, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			return sendDownload(ctx, source, info)
		})
		return app
	}

	t.Run("streams the complete file with metadata", func(t *testing.T) {
		app := request(t)
		response, err := app.Test(httptest.NewRequestWithContext(t.Context(), "GET", "/download", nil), -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, response.StatusCode)
		assert.Equal(t, "bytes", response.Header.Get(fiber.HeaderAcceptRanges))
		assert.NotEmpty(t, response.Header.Get(fiber.HeaderLastModified))
		assert.Equal(t, "0123456789", string(body))
	})

	t.Run("serves a single byte range", func(t *testing.T) {
		app := request(t)
		req := httptest.NewRequestWithContext(t.Context(), "GET", "/download", nil)
		req.Header.Set(fiber.HeaderRange, "bytes=2-5")
		response, err := app.Test(req, -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusPartialContent, response.StatusCode)
		assert.Equal(t, "bytes 2-5/10", response.Header.Get(fiber.HeaderContentRange))
		assert.Equal(t, "2345", string(body))
	})

	t.Run("rejects an unsatisfiable range", func(t *testing.T) {
		app := request(t)
		req := httptest.NewRequestWithContext(t.Context(), "GET", "/download", nil)
		req.Header.Set(fiber.HeaderRange, "bytes=20-30")
		response, err := app.Test(req, -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		assert.Equal(t, fiber.StatusRequestedRangeNotSatisfiable, response.StatusCode)
		assert.Equal(t, "bytes */10", response.Header.Get(fiber.HeaderContentRange))
	})

	t.Run("caps a suffix range at the file size", func(t *testing.T) {
		app := request(t)
		req := httptest.NewRequestWithContext(t.Context(), "GET", "/download", nil)
		req.Header.Set(fiber.HeaderRange, "bytes=-20")
		response, err := app.Test(req, -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusPartialContent, response.StatusCode)
		assert.Equal(t, "bytes 0-9/10", response.Header.Get(fiber.HeaderContentRange))
		assert.Equal(t, "0123456789", string(body))
	})

	t.Run("rejects a malformed range", func(t *testing.T) {
		app := request(t)
		req := httptest.NewRequestWithContext(t.Context(), "GET", "/download", nil)
		req.Header.Set(fiber.HeaderRange, "bytes=x-2")
		response, err := app.Test(req, -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		assert.Equal(t, fiber.StatusRequestedRangeNotSatisfiable, response.StatusCode)
	})

	t.Run("returns not modified for a fresh cached copy", func(t *testing.T) {
		app := request(t)
		req := httptest.NewRequestWithContext(t.Context(), "GET", "/download", nil)
		req.Header.Set(fiber.HeaderIfModifiedSince, "Fri, 17 Jul 2026 08:00:00 GMT")
		response, err := app.Test(req, -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotModified, response.StatusCode)
		assert.Empty(t, body)
	})
}
