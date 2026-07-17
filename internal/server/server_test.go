package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appconfig "github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/web"
)

func TestErrorHandler(t *testing.T) {
	t.Run("hides unexpected errors in production JSON", func(t *testing.T) {
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{Environment: cartridgeconfig.Production}}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		app := fiber.New(fiber.Config{ErrorHandler: ErrorHandler(logger, cfg)})
		app.Get("/failure", func(*fiber.Ctx) error {
			return errors.New("database password leaked")
		})

		req := httptest.NewRequestWithContext(t.Context(), "GET", "/failure", nil)
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

		req := httptest.NewRequestWithContext(t.Context(), "GET", "/missing", nil)
		req.Header.Set("Accept", "application/json")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, 404, resp.StatusCode)
		var body map[string]string
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
		assert.Equal(t, "Not Found", body["message"])
	})

	t.Run("prefers HTML for a browser navigation accept header", func(t *testing.T) {
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{Environment: cartridgeconfig.Production}}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		app := errorTestApp(t, logger, cfg)
		app.Get("/missing", func(*fiber.Ctx) error { return fiber.ErrNotFound })

		req := httptest.NewRequestWithContext(t.Context(), "GET", "/missing", nil)
		req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, 404, resp.StatusCode)
		assert.Equal(t, fiber.MIMETextHTMLCharsetUTF8, resp.Header.Get(fiber.HeaderContentType))
		assert.Equal(t, fiber.HeaderAccept, resp.Header.Get(fiber.HeaderVary))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "<!DOCTYPE html>")
		assert.Contains(t, string(body), ">404</div>")
		assert.Contains(t, string(body), ">Not Found</h1>")
		assert.Contains(t, string(body), `href="/admin/submissions"`)
		assert.NotContains(t, string(body), "Error: 404")
	})

	t.Run("redirects an HTMX 404 to a working page", func(t *testing.T) {
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{Environment: cartridgeconfig.Production}}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		app := errorTestApp(t, logger, cfg)
		app.Get("/missing", func(*fiber.Ctx) error { return fiber.ErrNotFound })

		req := httptest.NewRequestWithContext(t.Context(), "GET", "/missing", nil)
		req.Header.Set("Accept", "text/html")
		req.Header.Set("HX-Request", "true")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, 404, resp.StatusCode)
		assert.Equal(t, "/admin/submissions", resp.Header.Get("HX-Redirect"))
	})

	t.Run("renders an HTMX 500 page for the client lifecycle to swap", func(t *testing.T) {
		cfg := &appconfig.Config{Config: &cartridgeconfig.Config{Environment: cartridgeconfig.Production}}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		app := errorTestApp(t, logger, cfg)
		app.Get("/failure", func(*fiber.Ctx) error { return errors.New("database unavailable") })

		req := httptest.NewRequestWithContext(t.Context(), "GET", "/failure", nil)
		req.Header.Set("Accept", "text/html")
		req.Header.Set("HX-Request", "true")
		resp, err := app.Test(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, 500, resp.StatusCode)
		assert.Empty(t, resp.Header.Get("HX-Redirect"))
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.Contains(t, string(body), "The request could not be completed")
	})
}

func errorTestApp(t *testing.T, logger *slog.Logger, cfg *appconfig.Config) *fiber.App {
	t.Helper()
	engine := html.NewFileSystem(http.FS(web.Templates), ".html")
	engine.AddFunc("render", func(name string, data any) (template.HTML, error) {
		if !engine.Loaded {
			if err := engine.Load(); err != nil {
				return "", err
			}
		}
		view := engine.Templates.Lookup(name)
		if view == nil {
			return "", fmt.Errorf("template %q not found", name)
		}
		var output bytes.Buffer
		if err := view.Execute(&output, data); err != nil {
			return "", err
		}
		return template.HTML(output.String()), nil // #nosec G203 -- already rendered by html/template.
	})
	for name, function := range TemplateFuncs() {
		engine.AddFunc(name, function)
	}
	return fiber.New(fiber.Config{Views: engine, ErrorHandler: ErrorHandler(logger, cfg)})
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

func TestClientIP(t *testing.T) {
	t.Run("uses only the address appended by the trusted Matcha proxy", func(t *testing.T) {
		assert.Equal(t, "203.0.113.7", clientIP("172.18.0.2", "198.51.100.10, 203.0.113.7", true))
	})

	t.Run("ignores forwarding headers outside Matcha", func(t *testing.T) {
		assert.Equal(t, "172.18.0.2", clientIP("172.18.0.2", "198.51.100.10", false))
	})

	t.Run("falls back to the socket address when the appended value is invalid", func(t *testing.T) {
		assert.Equal(t, "172.18.0.2", clientIP("172.18.0.2", "198.51.100.10, invalid", true))
	})
}
