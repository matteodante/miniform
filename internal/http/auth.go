package http

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/config"
)

func RequireSession(session *cartridge.SessionManager, database cartridge.DBManager, cfg *config.Config) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		if !session.IsAuthenticated(ctx) {
			return browserRedirect(ctx, "/admin/login")
		}
		db, err := sessionDatabase(database)
		if err != nil {
			return err
		}
		token := ctx.Cookies(cfg.SessionCookieName())
		userID, ok := session.GetUserID(ctx)
		if !ok {
			return clearSessionAndRedirect(session, ctx)
		}
		user, err := accounts.FindByID(db.WithContext(ctx.UserContext()), userID)
		if errors.Is(err, accounts.ErrUserNotFound) {
			return clearSessionAndRedirect(session, ctx)
		}
		if err != nil {
			return fiber.ErrInternalServerError
		}
		active, err := accounts.IsSessionActive(db.WithContext(ctx.UserContext()), user.ID, token, time.Now())
		if err != nil {
			return fiber.ErrInternalServerError
		}
		if !active {
			return clearSessionAndRedirect(session, ctx)
		}
		return ctx.Next()
	}
}

func RequirePasswordChanged(session *cartridge.SessionManager, database cartridge.DBManager) fiber.Handler {
	return func(ctx *fiber.Ctx) error {
		userID, ok := session.GetUserID(ctx)
		if !ok {
			return browserRedirect(ctx, "/admin/login")
		}
		db, err := sessionDatabase(database)
		if err != nil {
			return err
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

func sessionDatabase(database cartridge.DBManager) (*gorm.DB, error) {
	if database == nil {
		return nil, errors.New("session database manager is unavailable")
	}
	db, err := database.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect session database: %w", err)
	}
	if db == nil {
		return nil, errors.New("connect session database: connection is unavailable")
	}
	return db, nil
}

func clearSessionAndRedirect(session *cartridge.SessionManager, ctx *fiber.Ctx) error {
	session.ClearSession(ctx)
	return browserRedirect(ctx, "/admin/login")
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

func AdminLoginSubmit(ctx *cartridge.Context, cfg *config.Config) error {
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
	cookie, err := http.ParseSetCookie(string(ctx.Response().Header.PeekCookie(cfg.SessionCookieName())))
	if err != nil || cookie.Value == "" {
		ctx.Session.ClearSession(ctx.Ctx)
		ctx.Logger.Error("read new admin session cookie", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	expiresAt, err := accounts.SessionExpiresAt(cookie.Value)
	if err != nil {
		ctx.Session.ClearSession(ctx.Ctx)
		ctx.Logger.Error("parse new admin session", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	if err := accounts.RegisterSession(ctx.Logger, db, user.ID, cookie.Value, expiresAt); err != nil {
		ctx.Session.ClearSession(ctx.Ctx)
		ctx.Logger.Error("register admin session", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}
	if user.PasswordChangeRequired {
		return ctx.Redirect("/admin/settings")
	}
	return ctx.Redirect("/admin/submissions")
}

func AdminLogout(ctx *cartridge.Context, cfg *config.Config) error {
	if ctx.Session != nil {
		db, err := requestDB(ctx)
		if err != nil {
			return err
		}
		token := ctx.Cookies(cfg.SessionCookieName())
		if err := accounts.RevokeSession(ctx.Logger, db, token); err != nil {
			ctx.Logger.Error("revoke admin session", slog.Any("error", err))
			return fiber.ErrInternalServerError
		}
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
