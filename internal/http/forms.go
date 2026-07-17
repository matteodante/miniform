package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
	formembed "github.com/matteodante/miniform/internal/forms/embed"
	"github.com/matteodante/miniform/internal/integrations"
)

func AdminFormsIndex(ctx *cartridge.Context) error {
	list, err := forms.List(ctx.DB())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Endpoints", "admin/forms/index/content", fiber.Map{
		"Forms": list, "CreateRoute": "/admin/forms/new",
	})
}

func AdminFormsNew(ctx *cartridge.Context) error {
	templateID := strings.TrimSpace(ctx.Query("template"))
	if templateID == "" {
		return renderPage(ctx, "Choose a starting point", "admin/forms/templates/content", fiber.Map{
			"Templates": forms.GetFormTemplates(),
		})
	}
	template := forms.GetTemplateByID(templateID)
	if template == nil {
		return ctx.Redirect("/admin/forms/new")
	}
	return renderFormEditor(ctx, nil, template, "")
}

func AdminFormsCreate(ctx *cartridge.Context) error {
	template := forms.GetTemplateByID(strings.TrimSpace(ctx.FormValue("template_id")))
	params := createFormParams(ctx)
	created, err := forms.Create(ctx.Logger, ctx.DB(), params)
	if err != nil {
		var validation *forms.ValidationError
		if errors.As(err, &validation) {
			return renderFormEditor(ctx, createFormDraft(params), template, validation.Message)
		}
		ctx.Logger.Error("create form", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}

	if template != nil {
		html := template.RenderHTML(formAction(created.Slug, created.Token))
		if html != "" {
			if _, err := forms.SetGeneratedHTML(ctx.Logger, ctx.DB(), created.ID, html); err != nil {
				ctx.Logger.Error("store generated form HTML", slog.Uint64("form_id", uint64(created.ID)), slog.Any("error", err))
			}
		}
	}
	return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", created.ID))
}

func AdminFormShow(ctx *cartridge.Context) error {
	form, err := requestedForm(ctx)
	if err != nil {
		return err
	}

	submissions, err := forms.GetSubmissions(ctx.DB(), form.ID, 25)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	webhooks, err := forms.GetWebhookEvents(ctx.DB(), form.ID, 20)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	emails, err := forms.GetEmailEvents(ctx.DB(), form.ID, 20)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	embed := formembed.Build(form, formembed.Options{ShowToken: true})
	if embed.Warning != "" {
		ctx.Logger.Warn("normalize generated form HTML", slog.Uint64("form_id", uint64(form.ID)), slog.String("warning", embed.Warning))
	}
	return renderPage(ctx, form.Name, "admin/forms/show/content", fiber.Map{
		"Form": form, "Submissions": submissions,
		"Endpoint": fmt.Sprintf("/forms/%s/submit", form.Slug), "Token": form.Token,
		"WebhookEvents": webhooks, "EmailEvents": emails,
		"EmailRecipient": deliveryRecipient(form.EmailDelivery),
		"FormCode":       embed.HTML, "HasGeneratedHTML": strings.TrimSpace(form.GeneratedHTML) != "",
	})
}

func AdminFormsEdit(ctx *cartridge.Context) error {
	form, err := requestedForm(ctx)
	if err != nil {
		return err
	}
	return renderFormEditor(ctx, form, nil, "")
}

func AdminFormsUpdate(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	params := updateFormParams(ctx, id)
	updated, err := forms.Update(ctx.Logger, ctx.DB(), params)
	if err == nil {
		return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", updated.ID))
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	var validation *forms.ValidationError
	if errors.As(err, &validation) {
		form, loadErr := forms.GetByID(ctx.DB(), id)
		if loadErr != nil {
			return fiber.ErrInternalServerError
		}
		return renderFormEditor(ctx, updateFormDraft(form, params), nil, validation.Message)
	}
	ctx.Logger.Error("update form", slog.Uint64("form_id", uint64(id)), slog.Any("error", err))
	return fiber.ErrInternalServerError
}

func createFormParams(ctx *cartridge.Context) forms.CreateParams {
	return forms.CreateParams{
		Name: ctx.FormValue("name"), Slug: ctx.FormValue("slug"),
		AllowedOrigins: ctx.FormValue("allowed_origins"), UseSDK: checkbox(ctx, "use_sdk"),
		GeneratedHTML:    ctx.FormValue("generated_html"),
		MailerProfileID:  optionalID(ctx.FormValue("mailer_profile_id")),
		CaptchaProfileID: optionalID(ctx.FormValue("captcha_profile_id")),
		EmailRecipient:   ctx.FormValue("email_recipient"), EmailEnabled: checkbox(ctx, "email_enabled"),
		WebhookEnabled: checkbox(ctx, "webhook_enabled"), WebhookURL: ctx.FormValue("webhook_url"),
		WebhookSecret: ctx.FormValue("webhook_secret"), WebhookHeadersJSON: ctx.FormValue("webhook_headers"),
	}
}

func updateFormParams(ctx *cartridge.Context, id uint) forms.UpdateParams {
	return forms.UpdateParams{
		ID: id, Name: ctx.FormValue("name"), AllowedOrigins: ctx.FormValue("allowed_origins"),
		UseSDK:           checkbox(ctx, "use_sdk"),
		MailerProfileID:  optionalID(ctx.FormValue("mailer_profile_id")),
		CaptchaProfileID: optionalID(ctx.FormValue("captcha_profile_id")),
		EmailRecipient:   ctx.FormValue("email_recipient"), EmailEnabled: checkbox(ctx, "email_enabled"),
		WebhookEnabled: checkbox(ctx, "webhook_enabled"), WebhookURL: ctx.FormValue("webhook_url"),
		WebhookSecret: ctx.FormValue("webhook_secret"), WebhookHeadersJSON: ctx.FormValue("webhook_headers"),
	}
}

func renderFormEditor(ctx *cartridge.Context, form *forms.Form, template *forms.FormTemplate, message string) error {
	mailers, err := integrations.ListMailerProfiles(ctx.DB())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	captchas, err := integrations.ListCaptchaProfiles(ctx.DB())
	if err != nil {
		return fiber.ErrInternalServerError
	}
	isEdit := form != nil && form.ID != 0
	data := fiber.Map{
		"Error": message, "Form": form, "IsEdit": isEdit,
		"MailerProfiles": mailers, "CaptchaProfiles": captchas,
		"DefaultSlug": "new-form", "ContentView": "admin/forms/new/content",
		"SelectedMailerProfileID": uint(0), "SelectedCaptchaProfileID": uint(0),
		"Title": "Create endpoint",
	}

	var email *forms.EmailDelivery
	var webhook *forms.WebhookDelivery
	if form != nil {
		if isEdit {
			data["Title"] = "Edit endpoint"
		}
		data["DefaultSlug"] = form.Slug
		email, webhook = form.EmailDelivery, form.WebhookDelivery
		data["PreviewHTML"] = form.GeneratedHTML
		if form.CaptchaProfileID != nil {
			data["SelectedCaptchaProfileID"] = *form.CaptchaProfileID
		}
	}
	if template != nil && !isEdit {
		data["Template"] = template
		data["TemplateID"] = template.ID
		if form == nil {
			emailCopy, webhookCopy := template.EmailDelivery, template.WebhookDelivery
			email, webhook = &emailCopy, &webhookCopy
			data["FormName"] = template.Name
			if template.Slug != "" {
				data["DefaultSlug"] = template.Slug
			}
		}
		data["PreviewHTML"] = template.RenderHTML(exampleAction(template.Slug))
	}
	data["EmailDelivery"] = email
	data["WebhookDelivery"] = webhook
	data["EmailRecipient"] = deliveryRecipient(email)
	if email != nil && email.MailerProfileID != nil {
		data["SelectedMailerProfileID"] = *email.MailerProfileID
	}
	if preview, _ := data["PreviewHTML"].(string); strings.TrimSpace(preview) == "" {
		if blank := forms.GetTemplateByID("blank"); blank != nil {
			action := exampleAction("")
			if form != nil {
				action = formAction(form.Slug, form.Token)
			}
			data["PreviewHTML"] = blank.RenderHTML(action)
		}
	}
	return ctx.Render("layouts/base", data, "")
}

func createFormDraft(params forms.CreateParams) *forms.Form {
	form := &forms.Form{
		Name: params.Name, Slug: params.Slug, AllowedOrigins: params.AllowedOrigins,
		UseSDK: params.UseSDK, GeneratedHTML: params.GeneratedHTML,
		CaptchaProfileID: params.CaptchaProfileID,
	}
	setDraftDeliveries(
		form,
		params.EmailEnabled, params.MailerProfileID, params.EmailRecipient,
		params.WebhookEnabled, params.WebhookURL, params.WebhookSecret, params.WebhookHeadersJSON,
	)
	return form
}

func updateFormDraft(form *forms.Form, params forms.UpdateParams) *forms.Form {
	form.Name = params.Name
	form.AllowedOrigins = params.AllowedOrigins
	form.UseSDK = params.UseSDK
	form.CaptchaProfileID = params.CaptchaProfileID
	setDraftDeliveries(
		form,
		params.EmailEnabled, params.MailerProfileID, params.EmailRecipient,
		params.WebhookEnabled, params.WebhookURL, params.WebhookSecret, params.WebhookHeadersJSON,
	)
	return form
}

func setDraftDeliveries(form *forms.Form, emailEnabled bool, mailerID *uint, recipient string, webhookEnabled bool, webhookURL, webhookSecret, webhookHeaders string) {
	overrides, err := json.Marshal(map[string]string{"to": recipient})
	if err != nil {
		overrides = nil
	}
	form.EmailDelivery = &forms.EmailDelivery{
		Enabled: emailEnabled, MailerProfileID: mailerID, OverridesJSON: string(overrides),
	}
	form.WebhookDelivery = &forms.WebhookDelivery{
		Enabled: webhookEnabled, URL: webhookURL, Secret: webhookSecret, HeadersJSON: webhookHeaders,
	}
}

func deliveryRecipient(delivery *forms.EmailDelivery) string {
	if delivery == nil {
		return ""
	}
	var overrides struct {
		To string `json:"to"`
	}
	_ = json.Unmarshal([]byte(delivery.OverridesJSON), &overrides)
	return overrides.To
}

func requestedForm(ctx *cartridge.Context) (*forms.Form, error) {
	id, err := requestedID(ctx)
	if err != nil {
		return nil, err
	}
	form, err := forms.GetByID(ctx.DB(), id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fiber.ErrNotFound
	}
	if err != nil {
		return nil, fiber.ErrInternalServerError
	}
	return form, nil
}

func requestedID(ctx *cartridge.Context) (uint, error) {
	id, err := strconv.ParseUint(ctx.Params("id"), 10, 32)
	if err != nil || id == 0 {
		return 0, fiber.ErrNotFound
	}
	return uint(id), nil
}

func optionalID(value string) *uint {
	id, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil || id == 0 {
		return nil
	}
	parsed := uint(id)
	return &parsed
}

func checkbox(ctx *cartridge.Context, name string) bool {
	value := ctx.FormValue(name)
	return value == "on" || value == "true" || value == "1"
}

func renderPage(ctx *cartridge.Context, title, content string, values fiber.Map) error {
	if values == nil {
		values = fiber.Map{}
	}
	values["Title"] = title
	values["ContentView"] = content
	return ctx.Render("layouts/base", values, "")
}

func exampleAction(slug string) string {
	if strings.TrimSpace(slug) == "" {
		slug = "your-form"
	}
	return fmt.Sprintf("/forms/%s/submit?token=YOUR_FORM_TOKEN", slug)
}

func formAction(slug, token string) string {
	if strings.TrimSpace(slug) == "" {
		slug = "your-form"
	}
	if strings.TrimSpace(token) == "" {
		token = "YOUR_FORM_TOKEN"
	}
	return fmt.Sprintf("/forms/%s/submit?token=%s", slug, token)
}
