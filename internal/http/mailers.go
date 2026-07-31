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
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	profiles, err := integrations.ListMailerProfiles(db)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Email routes", "admin/mailers/index", fiber.Map{"Profiles": profiles})
}

func MailerProfileNew(ctx *cartridge.Context) error {
	return renderPage(ctx, "New email route", "admin/mailers/new/content", nil)
}

func MailerProfileCreate(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	params := mailerParams(ctx)
	_, err = integrations.CreateMailerProfile(ctx.Logger, db, params)
	if err != nil {
		var validation *integrations.ValidationError
		if errors.As(err, &validation) {
			return renderMailerEditor(ctx, mailerProfileDraft(0, params), integrationMessage(err), mailerEditorSecrets{})
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect("/admin/settings/mailers")
}

func MailerProfileShow(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	profile, err := requestedMailer(ctx, db)
	if err != nil {
		return err
	}
	usage, err := forms.MailerProfileUsage(db, profile.ID)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderMailerProfile(ctx, profile, usage, "")
}

func renderMailerProfile(ctx *cartridge.Context, profile *integrations.MailerProfile, usage int64, message string) error {
	return renderPage(ctx, "Email route: "+profile.Name, "admin/mailers/show/content", fiber.Map{
		"Profile": profile, "UsageCount": usage, "Error": message,
	})
}

func MailerProfileEdit(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	profile, err := requestedMailer(ctx, db)
	if err != nil {
		return err
	}
	return renderMailerEditor(ctx, profile, "", mailerEditorSecrets{
		hasStoredSMTPPassword: profile.SMTPPassword != "",
	})
}

func MailerProfileUpdate(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	current, err := integrations.GetMailerProfileByID(db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	params := mailerParams(ctx)
	secrets := mailerEditorSecrets{
		hasStoredSMTPPassword: current.SMTPPassword != "",
		clearSMTPPassword:     checkbox(ctx, "clear_smtp_password"),
	}
	if secrets.clearSMTPPassword {
		params.SMTPPassword = ""
	} else if params.SMTPPassword == "" {
		params.SMTPPassword = current.SMTPPassword
	}
	profile, err := integrations.UpdateMailerProfile(ctx.Logger, db, id, params)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		var validation *integrations.ValidationError
		if errors.As(err, &validation) {
			return renderMailerEditor(ctx, mailerProfileDraft(id, params), integrationMessage(err), secrets)
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
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	usage, err := forms.MailerProfileUsage(db, id)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if usage != 0 {
		return renderMailerDeleteConflict(ctx, db, id)
	}
	if err := integrations.DeleteMailerProfile(ctx.Logger, db, id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		if errors.Is(err, integrations.ErrProfileInUse) {
			return renderMailerDeleteConflict(ctx, db, id)
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect("/admin/settings/mailers")
}

func renderMailerDeleteConflict(ctx *cartridge.Context, db *gorm.DB, id uint) error {
	profile, err := integrations.GetMailerProfileByID(db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	usage, err := forms.MailerProfileUsage(db, id)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	ctx.Status(fiber.StatusBadRequest)
	return renderMailerProfile(ctx, profile, usage, "Cannot delete profile: it is being used by forms")
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
		Name:            ctx.FormValue("name"),
		DefaultFromName: ctx.FormValue("default_from_name"), DefaultFromEmail: ctx.FormValue("default_from_email"),
		SMTPHost: ctx.FormValue("smtp_host"), SMTPPort: port,
		SMTPUsername: ctx.FormValue("smtp_username"), SMTPPassword: ctx.FormValue("smtp_password"),
		SMTPEncryption: ctx.FormValue("smtp_encryption"),
	}
}

func requestedMailer(ctx *cartridge.Context, db *gorm.DB) (*integrations.MailerProfile, error) {
	id, err := requestedID(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := integrations.GetMailerProfileByID(db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fiber.ErrNotFound
	}
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	return profile, nil
}

type mailerEditorSecrets struct {
	hasStoredSMTPPassword bool
	clearSMTPPassword     bool
}

func renderMailerEditor(ctx *cartridge.Context, profile *integrations.MailerProfile, message string, secrets mailerEditorSecrets) error {
	title := "New email route"
	isEdit := profile != nil && profile.ID != 0
	if isEdit {
		title = "Edit email route"
	}
	if profile != nil {
		safeProfile := *profile
		safeProfile.SMTPPassword = ""
		profile = &safeProfile
	}
	return renderPage(ctx, title, "admin/mailers/new/content", fiber.Map{
		"Profile": profile, "Error": message, "IsEdit": isEdit,
		"HasStoredSMTPPassword": secrets.hasStoredSMTPPassword,
		"ClearSMTPPassword":     secrets.clearSMTPPassword,
	})
}

func mailerProfileDraft(id uint, params integrations.MailerProfileParams) *integrations.MailerProfile {
	return &integrations.MailerProfile{
		ID: id, Name: params.Name,
		DefaultFromName: params.DefaultFromName, DefaultFromEmail: params.DefaultFromEmail,
		SMTPHost: params.SMTPHost, SMTPPort: params.SMTPPort,
		SMTPUsername:   params.SMTPUsername,
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
