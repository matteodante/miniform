package http

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
)

func AdminSettingsPage(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	return renderSettings(ctx, db, "", "")
}

func AdminSettingsUpdatePassword(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	current := ctx.FormValue("current_password")
	password := ctx.FormValue("new_password")
	confirmation := ctx.FormValue("confirm_password")
	if current == "" || password == "" || confirmation == "" {
		return renderSettings(ctx, db, "All password fields are required", "")
	}
	if password != confirmation {
		return renderSettings(ctx, db, "New passwords do not match", "")
	}
	user, err := currentUser(ctx, db)
	if err != nil {
		return err
	}
	if err := accounts.ChangePassword(ctx.Logger, db, user.Email, current, password); err != nil {
		switch {
		case errors.Is(err, accounts.ErrWeakPassword):
			return renderSettings(ctx, db, "Password must be at least 8 characters long", "")
		case errors.Is(err, accounts.ErrPasswordMismatch):
			return renderSettings(ctx, db, "Current password is incorrect", "")
		case errors.Is(err, accounts.ErrPasswordUnchanged):
			return renderSettings(ctx, db, "New password must be different from the current password", "")
		default:
			ctx.Logger.Error("change admin password", slog.Any("error", err))
			return fiber.ErrInternalServerError
		}
	}
	if ctx.Session != nil {
		ctx.Session.ClearSession(ctx.Ctx)
	}
	return ctx.Redirect("/admin/login")
}

func AdminSettingsUpdateEmail(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	email := ctx.FormValue("new_email")
	password := ctx.FormValue("current_password_email")
	if email == "" || password == "" {
		return renderSettings(ctx, db, "Email and current password are required", "")
	}
	user, err := currentUser(ctx, db)
	if err != nil {
		return err
	}
	if err := accounts.ChangeEmail(ctx.Logger, db, user.Email, email, password); err != nil {
		switch {
		case errors.Is(err, accounts.ErrInvalidEmail):
			return renderSettings(ctx, db, "Please enter a valid email address", "")
		case errors.Is(err, accounts.ErrPasswordMismatch):
			return renderSettings(ctx, db, "Current password is incorrect", "")
		case errors.Is(err, accounts.ErrDuplicateEmail):
			return renderSettings(ctx, db, "That email is already in use", "")
		default:
			ctx.Logger.Error("change admin email", slog.Any("error", err))
			return fiber.ErrInternalServerError
		}
	}
	if ctx.Session != nil {
		ctx.Session.ClearSession(ctx.Ctx)
	}
	return ctx.Redirect("/admin/login")
}

func currentUser(ctx *cartridge.Context, db *gorm.DB) (*accounts.User, error) {
	if ctx.Session == nil {
		return nil, fiber.ErrUnauthorized
	}
	userID, ok := ctx.Session.GetUserID(ctx.Ctx)
	if !ok {
		return nil, fiber.ErrUnauthorized
	}
	user, err := accounts.FindByID(db, userID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	return user, nil
}

func renderSettings(ctx *cartridge.Context, db *gorm.DB, failure, success string) error {
	user, err := currentUser(ctx, db)
	if err != nil {
		return err
	}
	return renderPage(ctx, "Workspace", "admin/settings/content", fiber.Map{
		"User": user, "PasswordChangeRequired": user.PasswordChangeRequired,
		"Error": failure, "Success": success,
	})
}
