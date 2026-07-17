package http

import (
	"errors"
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/accounts"
)

func AdminSettingsPage(ctx *cartridge.Context) error {
	return renderSettings(ctx, "", "")
}

func AdminSettingsUpdatePassword(ctx *cartridge.Context) error {
	current := ctx.FormValue("current_password")
	password := ctx.FormValue("new_password")
	confirmation := ctx.FormValue("confirm_password")
	if current == "" || password == "" || confirmation == "" {
		return renderSettings(ctx, "All password fields are required", "")
	}
	if password != confirmation {
		return renderSettings(ctx, "New passwords do not match", "")
	}
	user, err := currentUser(ctx)
	if err != nil {
		return err
	}
	if err := accounts.ChangePassword(ctx.Logger, ctx.DB(), user.Email, current, password); err != nil {
		switch {
		case errors.Is(err, accounts.ErrWeakPassword):
			return renderSettings(ctx, "Password must be at least 8 characters long", "")
		case errors.Is(err, accounts.ErrPasswordMismatch):
			return renderSettings(ctx, "Current password is incorrect", "")
		default:
			ctx.Logger.Error("change admin password", slog.Any("error", err))
			return fiber.ErrInternalServerError
		}
	}
	return renderSettings(ctx, "", "Password updated successfully")
}

func AdminSettingsUpdateEmail(ctx *cartridge.Context) error {
	email := ctx.FormValue("new_email")
	password := ctx.FormValue("current_password_email")
	if email == "" || password == "" {
		return renderSettings(ctx, "Email and current password are required", "")
	}
	user, err := currentUser(ctx)
	if err != nil {
		return err
	}
	if err := accounts.ChangeEmail(ctx.Logger, ctx.DB(), user.Email, email, password); err != nil {
		switch {
		case errors.Is(err, accounts.ErrInvalidEmail):
			return renderSettings(ctx, "Please enter a valid email address", "")
		case errors.Is(err, accounts.ErrPasswordMismatch):
			return renderSettings(ctx, "Current password is incorrect", "")
		case errors.Is(err, accounts.ErrDuplicateEmail):
			return renderSettings(ctx, "That email is already in use", "")
		default:
			ctx.Logger.Error("change admin email", slog.Any("error", err))
			return fiber.ErrInternalServerError
		}
	}
	return renderSettings(ctx, "", "Email updated successfully")
}

func AdminSettingsUpdateTurnstile(ctx *cartridge.Context) error {
	return ctx.Redirect("/admin/settings/captcha")
}

func currentUser(ctx *cartridge.Context) (*accounts.User, error) {
	if ctx.Session == nil {
		return nil, fiber.ErrUnauthorized
	}
	userID, ok := ctx.Session.GetUserID(ctx.Ctx)
	if !ok {
		return nil, fiber.ErrUnauthorized
	}
	user, err := accounts.FindByID(ctx.DB(), userID)
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	return user, nil
}

func renderSettings(ctx *cartridge.Context, failure, success string) error {
	user, err := currentUser(ctx)
	if err != nil {
		return err
	}
	return renderPage(ctx, "Workspace", "admin/settings/content", fiber.Map{
		"User": user, "Error": failure, "Success": success,
	})
}
