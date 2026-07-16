// Package server provides miniform-specific server configuration.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/matteodante/miniform/internal/config"
)

// Build info set at compile time via ldflags
var buildCommit = "dev"

// TemplateFuncs returns miniform-specific template functions.
func TemplateFuncs() map[string]any {
	return map[string]any{
		"truncateJSON": truncateJSON,
		"timeRFC3339":  timeRFC3339,
		"formatUTC":    formatUTC,
		"assetVersion": func() string {
			if buildCommit == "dev" {
				return time.Now().UTC().Format("20060102150405")
			}
			if len(buildCommit) > 8 {
				return buildCommit[:8]
			}
			return buildCommit
		},
	}
}

func timeRFC3339(value any) string {
	timestamp, ok := utcTime(value)
	if !ok {
		return ""
	}
	return timestamp.Format(time.RFC3339Nano)
}

func formatUTC(value any, layout string) string {
	timestamp, ok := utcTime(value)
	if !ok {
		return ""
	}
	return timestamp.Format(layout)
}

func utcTime(value any) (time.Time, bool) {
	switch timestamp := value.(type) {
	case time.Time:
		return timestamp.UTC(), true
	case *time.Time:
		if timestamp != nil {
			return timestamp.UTC(), true
		}
	}
	return time.Time{}, false
}

// ErrorHandler returns miniform-specific error handler.
func ErrorHandler(log *slog.Logger, cfg *config.Config) fiber.ErrorHandler {
	return func(c *fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		publicMessage := fiber.ErrInternalServerError.Message
		var fiberError *fiber.Error
		if errors.As(err, &fiberError) {
			code = fiberError.Code
			publicMessage = fiberError.Message
		} else if cfg.IsDevelopment() {
			publicMessage = err.Error()
		}

		log.Error("request failed",
			slog.Any("error", err),
			slog.String("path", c.Path()),
			slog.String("method", c.Method()),
			slog.Int("status", code),
		)

		// JSON error response for API requests
		if c.Accepts(fiber.MIMEApplicationJSON) == fiber.MIMEApplicationJSON {
			return c.Status(code).JSON(fiber.Map{
				"error":   http.StatusText(code),
				"message": publicMessage,
			})
		}

		// HTML error page for browser requests
		if code == fiber.StatusInternalServerError {
			return c.Status(code).Render("layouts/base", fiber.Map{
				"Title":             "500 - Internal Server Error",
				"ContentView":       "errors/500/content",
				"DevMode":           cfg.IsDevelopment(),
				"ErrorMessage":      publicMessage,
				"HideHeaderActions": true,
			}, "")
		}

		return c.Status(code).SendString(fmt.Sprintf("Error: %d - %s", code, publicMessage))
	}
}

func truncateJSON(raw string) string {
	if raw == "" {
		return ""
	}
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err == nil {
		if canonical, err := json.Marshal(payload); err == nil {
			raw = string(canonical)
		}
	}
	const limit = 80
	if len(raw) <= limit {
		return raw
	}
	return raw[:limit] + "..."
}
