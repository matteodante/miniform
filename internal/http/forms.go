package http

import (
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
	miniformserver "github.com/matteodante/miniform/internal/server"
)

func AdminFormsIndex(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	list, err := forms.List(db)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	return renderPage(ctx, "Endpoints", "admin/forms/index/content", fiber.Map{
		"Forms": list,
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
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	return renderFormEditor(ctx, db, nil, template, "", formEditorSecrets{})
}

func AdminFormsCreate(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	template := forms.GetTemplateByID(strings.TrimSpace(ctx.FormValue("template_id")))
	params := createFormParams(ctx)
	created, err := forms.Create(ctx.Logger, db, params)
	if err != nil {
		var validation *forms.ValidationError
		if errors.As(err, &validation) {
			return renderFormEditor(ctx, db, createFormDraft(params), template, validation.Message, formEditorSecrets{})
		}
		ctx.Logger.Error("create form", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}

	return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", created.ID))
}

func AdminFormShow(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}

	submissions, err := forms.GetSubmissions(db, form.ID, 25)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	webhooks, err := forms.GetWebhookEvents(db, form.ID, 20)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	emails, err := forms.GetEmailEvents(db, form.ID, 20)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	embed := formembed.Build(form, formembed.Options{BaseURL: ctx.BaseURL(), ShowToken: true})
	if embed.Warning != "" {
		ctx.Logger.Warn("normalize generated form HTML", slog.Uint64("form_id", uint64(form.ID)), slog.String("warning", embed.Warning))
	}
	enabledEmailCount := 0
	for i := range form.EmailDeliveries {
		if form.EmailDeliveries[i].Enabled {
			enabledEmailCount++
		}
		form.EmailDeliveries[i] = *displayEmailDelivery(&form.EmailDeliveries[i])
	}
	return renderPage(ctx, form.Name, "admin/forms/show/content", fiber.Map{
		"Form": form, "Submissions": submissions,
		"Endpoint": fmt.Sprintf("/forms/%s/submit", form.Slug), "Token": form.Token,
		"WebhookEvents": webhooks, "EmailEvents": emails,
		"EnabledEmailCount": enabledEmailCount,
		"EmailRecipient":    deliveryRecipient(forms.PrimaryEmailDelivery(form)),
		"FormCode":          embed.HTML, "FormCodeWarning": embed.Warning,
	})
}

func AdminFormsEdit(ctx *cartridge.Context) error {
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := requestedForm(ctx, db)
	if err != nil {
		return err
	}
	return renderFormEditor(ctx, db, form, nil, "", formEditorSecrets{
		hasStoredWebhookSecret:  form.WebhookDelivery != nil && form.WebhookDelivery.Secret != "",
		hasStoredWebhookHeaders: form.WebhookDelivery != nil && form.WebhookDelivery.HeadersJSON != "",
	})
}

func AdminFormsUpdate(ctx *cartridge.Context) error {
	id, err := requestedID(ctx)
	if err != nil {
		return err
	}
	db, err := requestDB(ctx)
	if err != nil {
		return err
	}
	form, err := forms.GetByID(db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	if err != nil {
		return fiber.ErrInternalServerError
	}
	params := updateFormParams(ctx, id)
	secrets := formEditorSecrets{
		hasStoredWebhookSecret:  form.WebhookDelivery != nil && form.WebhookDelivery.Secret != "",
		hasStoredWebhookHeaders: form.WebhookDelivery != nil && form.WebhookDelivery.HeadersJSON != "",
		clearWebhookSecret:      checkbox(ctx, "clear_webhook_secret"),
		clearWebhookHeaders:     checkbox(ctx, "clear_webhook_headers"),
	}
	if form.WebhookDelivery != nil {
		if secrets.clearWebhookSecret {
			params.WebhookSecret = ""
		} else if params.WebhookSecret == "" {
			params.WebhookSecret = form.WebhookDelivery.Secret
		}
		if secrets.clearWebhookHeaders {
			params.WebhookHeadersJSON = ""
		} else if strings.TrimSpace(params.WebhookHeadersJSON) == "" {
			params.WebhookHeadersJSON = form.WebhookDelivery.HeadersJSON
		}
	}
	updated, err := forms.Update(ctx.Logger, db, params)
	if err == nil {
		return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", updated.ID))
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fiber.ErrNotFound
	}
	var validation *forms.ValidationError
	if errors.As(err, &validation) {
		return renderFormEditor(ctx, db, updateFormDraft(form, params), nil, validation.Message, secrets)
	}
	ctx.Logger.Error("update form", slog.Uint64("form_id", uint64(id)), slog.Any("error", err))
	return fiber.ErrInternalServerError
}

func createFormParams(ctx *cartridge.Context) forms.CreateParams {
	return forms.CreateParams{
		Name: ctx.FormValue("name"), Slug: ctx.FormValue("slug"),
		AllowedOrigins: ctx.FormValue("allowed_origins"),
		UploadsEnabled: checkbox(ctx, "uploads_enabled"),
		GeneratedHTML:  ctx.FormValue("generated_html"), TemplateID: ctx.FormValue("template_id"),
		MailerProfileID:  optionalID(ctx.FormValue("mailer_profile_id")),
		CaptchaProfileID: optionalID(ctx.FormValue("captcha_profile_id")),
		EmailName:        ctx.FormValue("email_name"), EmailRecipientType: ctx.FormValue("email_recipient_source"),
		EmailRecipient: ctx.FormValue("email_recipient"), EmailReplyToType: ctx.FormValue("email_reply_to_source"),
		EmailReplyTo: ctx.FormValue("email_reply_to"), EmailSubject: ctx.FormValue("email_subject"),
		EmailFormat: ctx.FormValue("email_format"), EmailText: ctx.FormValue("email_text"), EmailHTML: ctx.FormValue("email_html"),
		EmailEnabled:   checkbox(ctx, "email_enabled"),
		WebhookEnabled: checkbox(ctx, "webhook_enabled"), WebhookURL: ctx.FormValue("webhook_url"),
		WebhookSecret: ctx.FormValue("webhook_secret"), WebhookHeadersJSON: ctx.FormValue("webhook_headers"),
	}
}

func updateFormParams(ctx *cartridge.Context, id uint) forms.UpdateParams {
	return forms.UpdateParams{
		ID: id, Name: ctx.FormValue("name"), AllowedOrigins: ctx.FormValue("allowed_origins"),
		UploadsEnabled:   checkbox(ctx, "uploads_enabled"),
		MailerProfileID:  optionalID(ctx.FormValue("mailer_profile_id")),
		CaptchaProfileID: optionalID(ctx.FormValue("captcha_profile_id")),
		EmailName:        ctx.FormValue("email_name"), EmailRecipientType: ctx.FormValue("email_recipient_source"),
		EmailRecipient: ctx.FormValue("email_recipient"), EmailReplyToType: ctx.FormValue("email_reply_to_source"),
		EmailReplyTo: ctx.FormValue("email_reply_to"), EmailSubject: ctx.FormValue("email_subject"),
		EmailFormat: ctx.FormValue("email_format"), EmailText: ctx.FormValue("email_text"), EmailHTML: ctx.FormValue("email_html"),
		EmailEnabled:   checkbox(ctx, "email_enabled"),
		WebhookEnabled: checkbox(ctx, "webhook_enabled"), WebhookURL: ctx.FormValue("webhook_url"),
		WebhookSecret: ctx.FormValue("webhook_secret"), WebhookHeadersJSON: ctx.FormValue("webhook_headers"),
	}
}

type formEditorSecrets struct {
	hasStoredWebhookSecret  bool
	hasStoredWebhookHeaders bool
	clearWebhookSecret      bool
	clearWebhookHeaders     bool
}

func renderFormEditor(ctx *cartridge.Context, db *gorm.DB, form *forms.Form, template *forms.FormTemplate, message string, secrets formEditorSecrets) error {
	mailers, err := integrations.ListMailerProfiles(db)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	captchas, err := integrations.ListCaptchaProfiles(db)
	if err != nil {
		return fiber.ErrInternalServerError
	}
	if form != nil {
		safeForm := *form
		if form.WebhookDelivery != nil {
			safeWebhook := *form.WebhookDelivery
			safeWebhook.Secret = ""
			safeWebhook.HeadersJSON = ""
			safeForm.WebhookDelivery = &safeWebhook
		}
		form = &safeForm
	}
	isEdit := form != nil && form.ID != 0
	data := fiber.Map{
		"Error": message, "Form": form, "IsEdit": isEdit,
		"MailerProfiles": mailers, "CaptchaProfiles": captchas,
		"DefaultSlug": "new-form", "ContentView": "admin/forms/new/content",
		"SelectedMailerProfileID": uint(0), "SelectedCaptchaProfileID": uint(0),
		"Title":                   "Create endpoint",
		"HasStoredWebhookSecret":  secrets.hasStoredWebhookSecret,
		"HasStoredWebhookHeaders": secrets.hasStoredWebhookHeaders,
		"ClearWebhookSecret":      secrets.clearWebhookSecret,
		"ClearWebhookHeaders":     secrets.clearWebhookHeaders,
	}

	var email *forms.EmailDelivery
	var webhook *forms.WebhookDelivery
	if form != nil {
		if isEdit {
			data["Title"] = "Edit endpoint"
		}
		data["DefaultSlug"] = form.Slug
		email, webhook = displayEmailDelivery(forms.PrimaryEmailDelivery(form)), form.WebhookDelivery
		data["PreviewHTML"] = form.GeneratedHTML
		if form.CaptchaProfileID != nil {
			data["SelectedCaptchaProfileID"] = *form.CaptchaProfileID
		}
	}
	if template != nil && !isEdit {
		data["TemplateID"] = template.ID
		if form == nil {
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
	data["EmailFormat"] = deliveryFormat(email)
	data["EmailName"] = forms.DefaultEmailDeliveryName
	data["EmailRecipientSource"] = forms.EmailRecipientStatic
	data["EmailReplyToSource"] = forms.EmailReplyToNone
	data["EmailSubject"] = forms.DefaultEmailSubject
	data["EmailText"] = forms.DefaultEmailText
	data["EmailHTML"] = forms.DefaultEmailHTML
	if email != nil {
		data["EmailName"] = email.Name
		data["EmailRecipientSource"] = email.RecipientSource
		data["EmailReplyToSource"] = email.ReplyToSource
		data["EmailReplyTo"] = email.ReplyTo
		data["EmailSubject"] = forms.EffectiveEmailSubject(email)
		data["EmailText"] = forms.EffectiveEmailText(email)
		data["EmailHTML"] = forms.EffectiveEmailHTML(email)
	}
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
	return ctx.Render("layouts/base", miniformserver.TemplateSecurity(ctx.Ctx, data), "")
}

func createFormDraft(params forms.CreateParams) *forms.Form {
	form := &forms.Form{
		Name: params.Name, Slug: params.Slug, AllowedOrigins: params.AllowedOrigins,
		GeneratedHTML: params.GeneratedHTML, UploadsEnabled: params.UploadsEnabled,
		CaptchaProfileID: params.CaptchaProfileID,
	}
	setDraftDeliveries(
		form,
		params.EmailEnabled, params.MailerProfileID, params.EmailName,
		params.EmailRecipientType, params.EmailRecipient,
		params.EmailReplyToType, params.EmailReplyTo,
		params.EmailSubject, params.EmailFormat, params.EmailText, params.EmailHTML,
		params.WebhookEnabled, params.WebhookURL,
	)
	return form
}

func updateFormDraft(form *forms.Form, params forms.UpdateParams) *forms.Form {
	form.Name = params.Name
	form.AllowedOrigins = params.AllowedOrigins
	form.UploadsEnabled = params.UploadsEnabled
	form.CaptchaProfileID = params.CaptchaProfileID
	setDraftDeliveries(
		form,
		params.EmailEnabled, params.MailerProfileID, params.EmailName,
		params.EmailRecipientType, params.EmailRecipient,
		params.EmailReplyToType, params.EmailReplyTo,
		params.EmailSubject, params.EmailFormat, params.EmailText, params.EmailHTML,
		params.WebhookEnabled, params.WebhookURL,
	)
	return form
}

func setDraftDeliveries(
	form *forms.Form,
	emailEnabled bool,
	mailerID *uint,
	name, recipientSource, recipient, replyToSource, replyTo, subject, emailFormat, textBody, htmlBody string,
	webhookEnabled bool,
	webhookURL string,
) {
	form.EmailDeliveries = []forms.EmailDelivery{{
		Name: name, Enabled: emailEnabled, MailerProfileID: mailerID,
		RecipientSource: recipientSource, Recipient: recipient,
		ReplyToSource: replyToSource, ReplyTo: replyTo,
		SubjectTemplate: subject, Format: emailFormat,
		TextTemplate: textBody, HTMLTemplate: htmlBody,
	}}
	form.WebhookDelivery = &forms.WebhookDelivery{
		Enabled: webhookEnabled, URL: webhookURL,
	}
}

func deliveryRecipient(delivery *forms.EmailDelivery) string {
	if delivery == nil {
		return ""
	}
	return delivery.Recipient
}

func deliveryFormat(delivery *forms.EmailDelivery) string {
	if delivery == nil {
		return forms.EmailFormatText
	}
	format, err := forms.NormalizeEmailFormat(delivery.Format)
	if err != nil {
		return forms.EmailFormatText
	}
	return format
}

func requestedForm(ctx *cartridge.Context, db *gorm.DB) (*forms.Form, error) {
	id, err := requestedID(ctx)
	if err != nil {
		return nil, err
	}
	form, err := forms.GetByID(db, id)
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
	return ctx.Render("layouts/base", miniformserver.TemplateSecurity(ctx.Ctx, values), "")
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
