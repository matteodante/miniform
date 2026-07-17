package http

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

func MailerProfileList(ctx *cartridge.Context) error {
	profiles, err := integrations.ListMailerProfiles(ctx.DB())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Email routes", "admin/mailers/index", fiber.Map{"Profiles": profiles})
}

func MailerProfileNew(ctx *cartridge.Context) error {
	return renderPage(ctx, "New email route", "admin/mailers/new/content", nil)
}

func MailerProfileCreate(ctx *cartridge.Context) error {
	params := mailerParams(ctx)
	_, err := integrations.CreateMailerProfile(ctx.Logger, ctx.DB(), params)
	if err != nil {
		var validation *integrations.ValidationError
		if errors.As(err, &validation) {
			return renderMailerEditor(ctx, mailerProfileDraft(0, params), integrationMessage(err))
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect("/admin/settings/mailers")
}

func MailerProfileShow(ctx *cartridge.Context) error {
	profile, err := requestedMailer(ctx)
	if err != nil {
		return err
	}
	usage, err := forms.MailerProfileUsage(ctx.DB(), profile.ID)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Email route: "+profile.Name, "admin/mailers/show/content", fiber.Map{
		"Profile": profile, "UsageCount": usage,
	})
}

func MailerProfileEdit(ctx *cartridge.Context) error {
	profile, err := requestedMailer(ctx)
	if err != nil {
		return err
	}
	return renderMailerEditor(ctx, profile, "")
}

func MailerProfileUpdate(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	params := mailerParams(ctx)
	profile, err := integrations.UpdateMailerProfile(ctx.Logger, ctx.DB(), id, params)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		var validation *integrations.ValidationError
		if errors.As(err, &validation) {
			if _, loadErr := integrations.GetMailerProfileByID(ctx.DB(), id); errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return fiber.ErrNotFound
			} else if loadErr != nil {
				return fiber.ErrInternalServerError
			}
			return renderMailerEditor(ctx, mailerProfileDraft(id, params), integrationMessage(err))
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect(fmt.Sprintf("/admin/settings/mailers/%d", profile.ID))
}

func MailerProfileDelete(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	usage, err := forms.MailerProfileUsage(ctx.DB(), id)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if usage != 0 {
		return ctx.Status(fiber.StatusBadRequest).SendString("Cannot delete profile: it is being used by forms")
	}
	if err := integrations.DeleteMailerProfile(ctx.Logger, ctx.DB(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect("/admin/settings/mailers")
}

func mailerParams(ctx *cartridge.Context) integrations.MailerProfileParams {
	rawPort := strings.TrimSpace(ctx.FormValue("smtp_port"))
	port := 587
	if rawPort != "" {
		parsed, err := strconv.Atoi(rawPort)
		if err != nil || parsed <= 0 {
			port = -1
		} else {
			port = parsed
		}
	}
	return integrations.MailerProfileParams{
		Name: ctx.FormValue("name"), Provider: ctx.FormValue("provider"),
		APIKey: ctx.FormValue("api_key"), Domain: ctx.FormValue("domain"),
		DefaultFromName: ctx.FormValue("default_from_name"), DefaultFromEmail: ctx.FormValue("default_from_email"),
		DefaultsJSON: ctx.FormValue("defaults_json"), SMTPHost: ctx.FormValue("smtp_host"), SMTPPort: port,
		SMTPUsername: ctx.FormValue("smtp_username"), SMTPPassword: ctx.FormValue("smtp_password"),
		SMTPEncryption: ctx.FormValue("smtp_encryption"),
	}
}

func requestedMailer(ctx *cartridge.Context) (*integrations.MailerProfile, error) {
	id, err := requestedID(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := integrations.GetMailerProfileByID(ctx.DB(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fiber.ErrNotFound
	}
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	return profile, nil
}

func renderMailerEditor(ctx *cartridge.Context, profile *integrations.MailerProfile, message string) error {
	title := "New email route"
	isEdit := profile != nil && profile.ID != 0
	if isEdit {
		title = "Edit email route"
	}
	return renderPage(ctx, title, "admin/mailers/new/content", fiber.Map{
		"Profile": profile, "Error": message, "IsEdit": isEdit,
	})
}

func mailerProfileDraft(id uint, params integrations.MailerProfileParams) *integrations.MailerProfile {
	return &integrations.MailerProfile{
		ID: id, Name: params.Name, Provider: params.Provider,
		APIKey: params.APIKey, Domain: params.Domain,
		DefaultFromName: params.DefaultFromName, DefaultFromEmail: params.DefaultFromEmail,
		DefaultsJSON: params.DefaultsJSON, SMTPHost: params.SMTPHost, SMTPPort: params.SMTPPort,
		SMTPUsername: params.SMTPUsername, SMTPPassword: params.SMTPPassword,
		SMTPEncryption: params.SMTPEncryption,
	}
}

func integrationMessage(err error) string {
	var validation *integrations.ValidationError
	if errors.As(err, &validation) {
		return validation.Message
	}
	return err.Error()
}
