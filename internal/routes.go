package internal

import (
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/limiter"
	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/config"
	handlers "github.com/matteodante/miniform/internal/http"
	miniformserver "github.com/matteodante/miniform/internal/server"
)

type endpoint struct {
	post    bool
	path    string
	handler cartridge.HandlerFunc
}

func MountRoutes(server *cartridge.Server, cfg *config.Config) {
	mountUtilityRoutes(server, cfg)
	mountPublicSubmission(server, cfg)
	mountLogin(server, cfg)
	mountAdmin(server, cfg)
}

func mountUtilityRoutes(server *cartridge.Server, cfg *config.Config) {
	health := func(ctx *cartridge.Context) error {
		db, err := ctx.DBManager.Connect()
		if err != nil {
			ctx.Logger.Warn("health check database unavailable", "error", err)
			return ctx.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable"})
		}
		var ready int
		if err := db.WithContext(ctx.UserContext()).Raw("SELECT 1").Scan(&ready).Error; err != nil || ready != 1 {
			ctx.Logger.Warn("health check database not ready", "error", err)
			return ctx.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"status": "unavailable"})
		}
		return ctx.JSON(fiber.Map{"status": "ok"})
	}
	server.Get("/_health", health)
	server.Head("/_health", health)
	server.Get("/", func(ctx *cartridge.Context) error { return ctx.Redirect("/admin/submissions") })
	if cfg.IsDevelopment() || cfg.IsTest() {
		server.Get("/_demo", handlers.DemoContactForm)
	}
}

func mountPublicSubmission(server *cartridge.Server, cfg *config.Config) {
	public := &cartridge.RouteConfig{
		EnableSecFetchSite: cartridge.Bool(false),
		EnableCORS:         true,
		CORSConfig: &cors.Config{
			AllowOrigins: "*", AllowMethods: "POST,OPTIONS",
			AllowHeaders: "Content-Type",
		},
		CustomMiddleware: []fiber.Handler{limiter.New(limiter.Config{
			Max: 30, Expiration: time.Minute,
			Storage:      newRateLimitStorage(),
			KeyGenerator: func(ctx *fiber.Ctx) string { return miniformserver.ClientIP(ctx, cfg.IsMatchaManaged()) },
			LimitReached: func(ctx *fiber.Ctx) error {
				return ctx.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
					"ok": false, "error": "rate limit exceeded",
				})
			},
			Next: func(*fiber.Ctx) bool { return cfg.IsDevelopment() || cfg.IsTest() },
		})},
	}
	server.Post("/forms/:slug/submit", func(ctx *cartridge.Context) error {
		return handlers.PublicFormSubmission(ctx, cfg)
	}, public)
	server.Options("/forms/:slug/submit", func(ctx *cartridge.Context) error {
		return ctx.SendStatus(fiber.StatusNoContent)
	}, public)
}

func mountLogin(server *cartridge.Server, cfg *config.Config) {
	server.Get("/admin/login", handlers.AdminLoginPage)
	loginLimiter := limiter.New(limiter.Config{
		Max: 5, Expiration: time.Minute,
		Storage:      newRateLimitStorage(),
		KeyGenerator: func(ctx *fiber.Ctx) string { return miniformserver.ClientIP(ctx, cfg.IsMatchaManaged()) },
		LimitReached: func(ctx *fiber.Ctx) error {
			return ctx.Status(fiber.StatusTooManyRequests).Render("layouts/base", fiber.Map{
				"Title": "Sign in", "Error": "Too many login attempts. Please try again in a minute.",
				"HideHeaderActions": true, "ContentView": "admin/login/content",
			}, "")
		},
		Next: func(*fiber.Ctx) bool { return cfg.IsDevelopment() || cfg.IsTest() },
	})
	server.Post("/admin/login", handlers.AdminLoginSubmit, &cartridge.RouteConfig{
		EnableSecFetchSite: cartridge.Bool(false),
		CustomMiddleware:   []fiber.Handler{loginLimiter},
	})
}

func mountAdmin(server *cartridge.Server, cfg *config.Config) {
	requireSession := handlers.RequireSession(server.Session())
	requirePasswordChanged := handlers.RequirePasswordChanged(server.Session(), server.GetDBManager())
	authenticated := &cartridge.RouteConfig{CustomMiddleware: []fiber.Handler{requireSession, requirePasswordChanged}}
	passwordSetup := &cartridge.RouteConfig{CustomMiddleware: []fiber.Handler{requireSession}}

	server.Post("/admin/logout", handlers.AdminLogout, passwordSetup)
	server.Get("/admin/settings", handlers.AdminSettingsPage, passwordSetup)
	server.Post("/admin/settings/password", handlers.AdminSettingsUpdatePassword, passwordSetup)
	registerEndpoints(server, authenticated, []endpoint{
		{false, "/admin", redirectTo("/admin/submissions")},
		{false, "/admin/forms", handlers.AdminFormsIndex},
		{false, "/admin/forms/new", handlers.AdminFormsNew},
		{true, "/admin/forms", handlers.AdminFormsCreate},
		{false, "/admin/forms/:id", handlers.AdminFormShow},
		{false, "/admin/forms/:id/edit", handlers.AdminFormsEdit},
		{true, "/admin/forms/:id", handlers.AdminFormsUpdate},
		{false, "/admin/forms/:id/emails/new", handlers.AdminFormEmailNew},
		{true, "/admin/forms/:id/emails", handlers.AdminFormEmailCreate},
		{false, "/admin/forms/:id/emails/:delivery_id/edit", handlers.AdminFormEmailEdit},
		{true, "/admin/forms/:id/emails/:delivery_id", handlers.AdminFormEmailUpdate},
		{true, "/admin/forms/:id/emails/:delivery_id/delete", handlers.AdminFormEmailDelete},
		{false, "/admin/submissions", handlers.SubmissionList},
		{false, "/admin/submissions/:id", handlers.AdminSubmissionShow},
		{false, "/admin/submissions/:id/files/:file_id", func(ctx *cartridge.Context) error {
			return handlers.AdminSubmissionFileDownload(ctx, cfg)
		}},
		{true, "/admin/settings/email", handlers.AdminSettingsUpdateEmail},
		{false, "/admin/settings/mailers", handlers.MailerProfileList},
		{false, "/admin/settings/mailers/new", handlers.MailerProfileNew},
		{true, "/admin/settings/mailers", handlers.MailerProfileCreate},
		{false, "/admin/settings/mailers/:id", handlers.MailerProfileShow},
		{false, "/admin/settings/mailers/:id/edit", handlers.MailerProfileEdit},
		{true, "/admin/settings/mailers/:id", handlers.MailerProfileUpdate},
		{true, "/admin/settings/mailers/:id/delete", handlers.MailerProfileDelete},
		{false, "/admin/settings/captcha", handlers.CaptchaProfileList},
		{false, "/admin/settings/captcha/new", handlers.CaptchaProfileNew},
		{true, "/admin/settings/captcha", handlers.CaptchaProfileCreate},
		{false, "/admin/settings/captcha/:id", handlers.CaptchaProfileShow},
		{false, "/admin/settings/captcha/:id/edit", handlers.CaptchaProfileEdit},
		{true, "/admin/settings/captcha/:id", handlers.CaptchaProfileUpdate},
		{true, "/admin/settings/captcha/:id/delete", handlers.CaptchaProfileDelete},
	})
}

func registerEndpoints(server *cartridge.Server, cfg *cartridge.RouteConfig, endpoints []endpoint) {
	for _, route := range endpoints {
		if route.post {
			server.Post(route.path, route.handler, cfg)
		} else {
			server.Get(route.path, route.handler, cfg)
		}
	}
}

func redirectTo(path string) cartridge.HandlerFunc {
	return func(ctx *cartridge.Context) error { return ctx.Redirect(path) }
}
