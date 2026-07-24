package server

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gofiber/fiber/v2"

	"github.com/matteodante/miniform/internal/config"
)

var (
	buildCommit           = "dev"
	developmentAssetStamp = time.Now().UTC().Format("20060102150405")
)

const cspNonceLocal = "miniform.csp_nonce"

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

func ClientIP(ctx *fiber.Ctx, mode config.ProxyMode) string {
	return clientIP(
		ctx.Context().RemoteIP().String(),
		ctx.Get(fiber.HeaderXForwardedFor),
		ctx.Get("X-Real-IP"),
		mode,
	)
}

func clientIP(remoteIP, forwardedFor, realIP string, mode config.ProxyMode) string {
	if mode == config.ProxyRailway {
		if address := net.ParseIP(strings.TrimSpace(realIP)); address != nil {
			return address.String()
		}
		return remoteIP
	}
	if mode != config.ProxyMatcha {
		return remoteIP
	}
	addresses := strings.Split(forwardedFor, ",")
	if len(addresses) == 0 {
		return remoteIP
	}
	address := net.ParseIP(strings.TrimSpace(addresses[len(addresses)-1]))
	if address == nil {
		return remoteIP
	}
	return address.String()
}

func SecurityHeaders(cfg *config.Config) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		nonceBytes := make([]byte, 18)
		if _, err := rand.Read(nonceBytes); err != nil {
			return fmt.Errorf("generate content security policy nonce: %w", err)
		}
		nonce := base64.RawStdEncoding.EncodeToString(nonceBytes)
		ctx.Locals(cspNonceLocal, nonce)
		impeccableLiveSource := ""
		if cfg.IsDevelopment() {
			impeccableLiveSource = " http://localhost:8400"
		}
		ctx.Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"base-uri 'self'",
			"object-src 'none'",
			"frame-ancestors 'none'",
			"form-action 'self'",
			"script-src 'self' 'nonce-" + nonce + "' https://challenges.cloudflare.com" + impeccableLiveSource,
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"font-src 'self'",
			"connect-src 'self' https://challenges.cloudflare.com" + impeccableLiveSource,
			"frame-src 'self' https://challenges.cloudflare.com",
			"worker-src 'none'",
		}, "; "))
		ctx.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		ctx.Set("Referrer-Policy", "same-origin")
		ctx.Set("X-Content-Type-Options", "nosniff")
		ctx.Set("X-Frame-Options", "DENY")
		if cfg.IsProduction() {
			ctx.Set("Strict-Transport-Security", "max-age=31536000")
		}
		if strings.HasPrefix(ctx.Path(), "/admin") {
			ctx.Set(fiber.HeaderCacheControl, "no-store")
			ctx.Set(fiber.HeaderPragma, "no-cache")
			ctx.Set(fiber.HeaderExpires, "0")
		}
		return ctx.Next()
	}
}

func TemplateSecurity(ctx *fiber.Ctx, values fiber.Map) fiber.Map {
	if values == nil {
		values = fiber.Map{}
	}
	values["CSPNonce"], _ = ctx.Locals(cspNonceLocal).(string)
	return values
}

func ErrorHandler(logger *slog.Logger, cfg *config.Config) fiber.ErrorHandler {
	return func(ctx *fiber.Ctx, failure error) error {
		status, message := publicError(failure, cfg.IsDevelopment())
		logger.Error("HTTP request failed",
			slog.Int("status", status), slog.String("method", ctx.Method()),
			slog.String("path", ctx.Path()), slog.Any("error", failure),
		)
		ctx.Vary(fiber.HeaderAccept)
		if ctx.Method() == fiber.MethodPost && fiber.RoutePatternMatch(ctx.Path(), "/forms/:slug/submit") {
			return ctx.Status(status).JSON(fiber.Map{"ok": false, "error": message})
		}
		if ctx.Accepts(fiber.MIMETextHTML, fiber.MIMEApplicationJSON) == fiber.MIMEApplicationJSON {
			return ctx.Status(status).JSON(fiber.Map{"error": http.StatusText(status), "message": message})
		}
		if status == fiber.StatusInternalServerError {
			return ctx.Status(status).Render("layouts/base", TemplateSecurity(ctx, fiber.Map{
				"Title": "500 - Internal Server Error", "ContentView": "errors/500/content",
				"DevMode": cfg.IsDevelopment(), "ErrorMessage": message, "HideHeaderActions": true,
			}), "")
		}
		if status >= fiber.StatusBadRequest && status < fiber.StatusInternalServerError {
			const recoveryURL = "/admin/submissions"
			if ctx.Get("HX-Request") == "true" {
				ctx.Set("HX-Redirect", recoveryURL)
			}
			return ctx.Status(status).Render("layouts/base", TemplateSecurity(ctx, fiber.Map{
				"Title":       fmt.Sprintf("%d - %s", status, http.StatusText(status)),
				"ContentView": "errors/4xx/content", "Status": status,
				"ErrorMessage": message, "RecoveryURL": recoveryURL, "HideHeaderActions": true,
			}), "")
		}
		ctx.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
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
