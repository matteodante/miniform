package http

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

type siteKeyEntry struct {
	HostPattern string `json:"host_pattern"`
	SiteKey     string `json:"site_key"`
}

type captchaSummary struct {
	integrations.CaptchaProfile
	SiteKeyCount int
}

func CaptchaProfileList(ctx *cartridge.Context) error {
	profiles, err := integrations.ListCaptchaProfiles(ctx.DB())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	summaries := make([]captchaSummary, len(profiles))
	for i := range profiles {
		summaries[i] = captchaSummary{CaptchaProfile: profiles[i], SiteKeyCount: len(decodeSiteKeys(profiles[i].SiteKeysJSON))}
	}
	return renderPage(ctx, "Safeguards", "admin/captcha/index/content", fiber.Map{"Profiles": summaries})
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
		"Profile": profile, "SiteKeys": decodeSiteKeys(profile.SiteKeysJSON), "UsageCount": usage,
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
	params := captchaParams(ctx)
	profile, err := integrations.UpdateCaptchaProfile(ctx.Logger, ctx.DB(), id, params)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		var validation *integrations.ValidationError
		if errors.As(err, &validation) {
			if _, loadErr := integrations.GetCaptchaProfileByID(ctx.DB(), id); errors.Is(loadErr, gorm.ErrRecordNotFound) {
				return fiber.ErrNotFound
			} else if loadErr != nil {
				return fiber.ErrInternalServerError
			}
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
		Name: ctx.FormValue("name"), Provider: ctx.FormValue("provider"), SecretKey: ctx.FormValue("secret_key"),
		SiteKeysJSON: ctx.FormValue("site_keys_json"), PolicyJSON: ctx.FormValue("policy_json"),
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
		ID: id, Name: params.Name, Provider: params.Provider, SecretKey: params.SecretKey,
		SiteKeysJSON: params.SiteKeysJSON, PolicyJSON: params.PolicyJSON,
	}
}

func decodeSiteKeys(raw string) []siteKeyEntry {
	var entries []siteKeyEntry
	_ = json.Unmarshal([]byte(raw), &entries)
	return entries
}
