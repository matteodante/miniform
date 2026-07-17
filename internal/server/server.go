package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"

	"github.com/matteodante/miniform/internal/config"
)

var (
	buildCommit           = "dev"
	developmentAssetStamp = time.Now().UTC().Format("20060102150405")
)

func TemplateFuncs() map[string]any {
	return map[string]any{
		"truncateJSON": compactPreview,
		"timeRFC3339": func(value any) string {
			if timestamp, ok := asUTC(value); ok {
				return timestamp.Format(time.RFC3339Nano)
			}
			return ""
		},
		"formatUTC": func(value any, layout string) string {
			if timestamp, ok := asUTC(value); ok {
				return timestamp.Format(layout)
			}
			return ""
		},
		"assetVersion": assetVersion,
	}
}

func ErrorHandler(logger *slog.Logger, cfg *config.Config) fiber.ErrorHandler {
	return func(ctx *fiber.Ctx, failure error) error {
		status, message := publicError(failure, cfg.IsDevelopment())
		logger.Error("HTTP request failed",
			slog.Int("status", status), slog.String("method", ctx.Method()),
			slog.String("path", ctx.Path()), slog.Any("error", failure),
		)
		if ctx.Accepts(fiber.MIMEApplicationJSON) == fiber.MIMEApplicationJSON {
			return ctx.Status(status).JSON(fiber.Map{"error": http.StatusText(status), "message": message})
		}
		if status == fiber.StatusInternalServerError {
			return ctx.Status(status).Render("layouts/base", fiber.Map{
				"Title": "500 - Internal Server Error", "ContentView": "errors/500/content",
				"DevMode": cfg.IsDevelopment(), "ErrorMessage": message, "HideHeaderActions": true,
			}, "")
		}
		return ctx.Status(status).SendString(fmt.Sprintf("Error: %d - %s", status, message))
	}
}

func publicError(err error, development bool) (int, string) {
	var fiberError *fiber.Error
	if errors.As(err, &fiberError) {
		return fiberError.Code, fiberError.Message
	}
	if development {
		return fiber.StatusInternalServerError, err.Error()
	}
	return fiber.StatusInternalServerError, fiber.ErrInternalServerError.Message
}

func asUTC(value any) (time.Time, bool) {
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

func assetVersion() string {
	if buildCommit == "dev" {
		return developmentAssetStamp
	}
	if len(buildCommit) > 8 {
		return buildCommit[:8]
	}
	return buildCommit
}

func compactPreview(raw string) string {
	var compact bytes.Buffer
	if json.Compact(&compact, []byte(raw)) == nil {
		raw = compact.String()
	}
	if utf8.RuneCountInString(raw) <= 80 {
		return raw
	}
	return string([]rune(raw)[:80]) + "..."
}
