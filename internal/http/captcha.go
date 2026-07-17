package http

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

func CaptchaProfileList(ctx *cartridge.Context) error {
	profiles, err := integrations.ListCaptchaProfiles(ctx.DB())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Safeguards", "admin/captcha/index/content", fiber.Map{"Profiles": profiles})
}

func CaptchaProfileNew(ctx *cartridge.Context) error {
	return renderPage(ctx, "New safeguard", "admin/captcha/new/content", nil)
}

func CaptchaProfileCreate(ctx *cartridge.Context) error {
	params := captchaParams(ctx)
	_, err := integrations.CreateCaptchaProfile(ctx.Logger, ctx.DB(), params)
	if err != nil {
		var validation *integrations.ValidationError
		if errors.As(err, &validation) {
			return renderCaptchaEditor(ctx, captchaProfileDraft(0, params), integrationMessage(err))
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect("/admin/settings/captcha")
}

func CaptchaProfileShow(ctx *cartridge.Context) error {
	profile, err := requestedCaptcha(ctx)
	if err != nil {
		return err
	}
	usage, err := forms.CaptchaProfileUsage(ctx.DB(), profile.ID)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Safeguard: "+profile.Name, "admin/captcha/show/content", fiber.Map{
		"Profile": profile, "UsageCount": usage,
	})
}

func CaptchaProfileEdit(ctx *cartridge.Context) error {
	profile, err := requestedCaptcha(ctx)
	if err != nil {
		return err
	}
	return renderCaptchaEditor(ctx, profile, "")
}

func CaptchaProfileUpdate(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	current, err := integrations.GetCaptchaProfileByID(ctx.DB(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	params := captchaParams(ctx)
	if strings.TrimSpace(params.SecretKey) == "" {
		params.SecretKey = current.SecretKey
	}
	profile, err := integrations.UpdateCaptchaProfile(ctx.Logger, ctx.DB(), id, params)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		var validation *integrations.ValidationError
		if errors.As(err, &validation) {
			return renderCaptchaEditor(ctx, captchaProfileDraft(id, params), integrationMessage(err))
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect(fmt.Sprintf("/admin/settings/captcha/%d", profile.ID))
}

func CaptchaProfileDelete(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	usage, err := forms.CaptchaProfileUsage(ctx.DB(), id)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if usage != 0 {
		return ctx.Status(fiber.StatusBadRequest).SendString("Cannot delete profile: it is being used by forms")
	}
	if err := integrations.DeleteCaptchaProfile(ctx.Logger, ctx.DB(), id); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}
	return ctx.Redirect("/admin/settings/captcha")
}

func captchaParams(ctx *cartridge.Context) integrations.CaptchaProfileParams {
	return integrations.CaptchaProfileParams{
		Name: ctx.FormValue("name"), SiteKey: ctx.FormValue("site_key"), SecretKey: ctx.FormValue("secret_key"),
	}
}

func requestedCaptcha(ctx *cartridge.Context) (*integrations.CaptchaProfile, error) {
	id, err := requestedID(ctx)
	if err != nil {
		return nil, err
	}
	profile, err := integrations.GetCaptchaProfileByID(ctx.DB(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fiber.ErrNotFound
	}
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	return profile, nil
}

func renderCaptchaEditor(ctx *cartridge.Context, profile *integrations.CaptchaProfile, message string) error {
	title := "New safeguard"
	isEdit := profile != nil && profile.ID != 0
	if isEdit {
		title = "Edit safeguard"
	}
	return renderPage(ctx, title, "admin/captcha/new/content", fiber.Map{
		"Profile": profile, "Error": message, "IsEdit": isEdit,
	})
}

func captchaProfileDraft(id uint, params integrations.CaptchaProfileParams) *integrations.CaptchaProfile {
	return &integrations.CaptchaProfile{
		ID: id, Name: params.Name, SiteKey: params.SiteKey,
	}
}
