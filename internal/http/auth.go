package http

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/accounts"
)

func AdminLoginPage(ctx *cartridge.Context) error {
	return renderLogin(ctx, "")
}

func AdminLoginSubmit(ctx *cartridge.Context) error {
	result, err := accounts.Authenticate(ctx.Logger, ctx.DB(), ctx.FormValue("email"), ctx.FormValue("password"))
	if err != nil {
		if errors.Is(err, accounts.ErrInvalidCredentials) || errors.Is(err, accounts.ErrMissingFields) {
			return renderLogin(ctx, "Invalid credentials")
		}
		ctx.Logger.Error("authenticate admin", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	if ctx.Session == nil {
		return fiber.ErrInternalServerError
	}
	if err := ctx.Session.SetSession(ctx.Ctx, result.User.ID); err != nil {
		ctx.Logger.Error("start admin session", slog.Uint64("user_id", uint64(result.User.ID)), slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect("/admin/submissions")
}

func AdminLogout(ctx *cartridge.Context) error {
	if ctx.Session != nil {
		ctx.Session.ClearSession(ctx.Ctx)
	}
	return ctx.Redirect("/admin/login")
}

func renderLogin(ctx *cartridge.Context, message string) error {
	return renderPage(ctx, "Sign in", "admin/login/content", fiber.Map{
		"Error": message, "HideHeaderActions": true,
		"ShowFirstLoginHelp": accounts.IsFirstLoginPending(ctx.DB()),
	})
}
