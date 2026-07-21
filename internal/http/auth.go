package http

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
)

func RequireSession(session *cartridge.SessionManager) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		if session.IsAuthenticated(ctx) {
			return ctx.Next()
		}
		return browserRedirect(ctx, "/admin/login")
	}
}

func RequirePasswordChanged(session *cartridge.SessionManager, database cartridge.DBManager) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		userID, ok := session.GetUserID(ctx)
		if !ok {
			return browserRedirect(ctx, "/admin/login")
		}
		if database == nil {
			return errors.New("password-change database manager is unavailable")
		}
		db, err := database.Connect()
		if err != nil {
			return fmt.Errorf("connect password-change database: %w", err)
		}
		if db == nil {
			return errors.New("connect password-change database: connection is unavailable")
		}
		user, err := accounts.FindByID(db.WithContext(ctx.UserContext()), userID)
		if errors.Is(err, accounts.ErrUserNotFound) {
			session.ClearSession(ctx)
			return browserRedirect(ctx, "/admin/login")
		}
		if err != nil {
			return fiber.ErrInternalServerError
		}
		if user.PasswordChangeRequired {
			return browserRedirect(ctx, "/admin/settings")
		}
		return ctx.Next()
	}
}

func browserRedirect(ctx *fiber.Ctx, location string) error {
	if ctx.Get("HX-Request") == "true" {
		ctx.Set("HX-Redirect", location)
		return ctx.SendStatus(fiber.StatusOK)
	}
	return ctx.Redirect(location)
}

func AdminLoginPage(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	return renderLogin(ctx, db, "")
}

func AdminLoginSubmit(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	user, err := accounts.Authenticate(ctx.Logger, db, ctx.FormValue("email"), ctx.FormValue("password"))
	if err != nil {
		if errors.Is(err, accounts.ErrInvalidCredentials) || errors.Is(err, accounts.ErrMissingFields) {
			return renderLogin(ctx, db, "Invalid credentials")
		}
		ctx.Logger.Error("authenticate admin", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	if ctx.Session == nil {
		return fiber.ErrInternalServerError
	}
	if err := ctx.Session.SetSession(ctx.Ctx, user.ID); err != nil {
		ctx.Logger.Error("start admin session", slog.Uint64("user_id", uint64(user.ID)), slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	if user.PasswordChangeRequired {
		return ctx.Redirect("/admin/settings")
	}
	return ctx.Redirect("/admin/submissions")
}

func AdminLogout(ctx *cartridge.Context) error {
	if ctx.Session != nil {
		ctx.Session.ClearSession(ctx.Ctx)
	}
	return ctx.Redirect("/admin/login")
}

func renderLogin(ctx *cartridge.Context, db *gorm.DB, message string) error {
	return renderPage(ctx, "Sign in", "admin/login/content", fiber.Map{
		"Error": message, "HideHeaderActions": true,
		"ShowTemporaryPasswordHelp": accounts.RequiresPasswordChange(db),
	})
}
