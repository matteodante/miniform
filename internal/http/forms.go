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
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

// AdminFormsIndex renders the list of forms.
func AdminFormsIndex(ctx *cartridge.Context) error {
	db := ctx.DB()

	formsList, err := forms.List(db)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	// Load relations for display
	for i := range formsList {
		db.Preload("EmailDelivery").Preload("WebhookDelivery").First(&formsList[i], formsList[i].ID)
	}

	return ctx.Render("layouts/base", fiber.Map{
		"Title":       "Endpoints",
		"Forms":       formsList,
		"CreateRoute": "/admin/forms/new",
		"ContentView": "admin/forms/index/content",
	}, "")
}

// AdminFormsNew renders the new form view or template selector.
func AdminFormsNew(ctx *cartridge.Context) error {
	db := ctx.DB()

	// Check if a template is selected
	templateID := ctx.Query("template")
	if templateID == "" {
		// Show template selector
		templates := GetFormTemplates()
		return ctx.Render("layouts/base", fiber.Map{
			"Title":       "Choose a starting point",
			"Templates":   templates,
			"ContentView": "admin/forms/templates/content",
		}, "")
	}

	// Load template
	template := GetTemplateByID(templateID)
	if template == nil {
		return ctx.Redirect("/admin/forms/new")
	}

	// Load profiles for dropdowns
	mailerProfiles, _ := integrations.ListMailerProfiles(db)
	captchaProfiles, _ := integrations.ListCaptchaProfiles(db)

	// Pre-fill form with template data
	emailDelivery := template.EmailDelivery
	webhookDelivery := template.WebhookDelivery

	// Extract email recipient from overrides for display
	emailRecipient := ""
	if emailDelivery.OverridesJSON != "" {
		var overrides map[string]interface{}
		if err := json.Unmarshal([]byte(emailDelivery.OverridesJSON), &overrides); err == nil {
			if to, ok := overrides["to"].(string); ok {
				emailRecipient = to
			}
		}
	}

	previewHTML := template.RenderHTML(exampleFormAction(template.Slug))

	return ctx.Render("layouts/base", fiber.Map{
		"Title":                    "New endpoint",
		"DefaultSlug":              template.Slug,
		"FormName":                 template.Name,
		"EmailDelivery":            &emailDelivery,
		"WebhookDelivery":          &webhookDelivery,
		"EmailRecipient":           emailRecipient,
		"EmailEnabled":             emailDelivery.Enabled,
		"WebhookEnabled":           webhookDelivery.Enabled,
		"Template":                 template,
		"TemplateID":               template.ID,
		"PreviewHTML":              previewHTML,
		"MailerProfiles":           mailerProfiles,
		"CaptchaProfiles":          captchaProfiles,
		"SelectedMailerProfileID":  uint(0),
		"SelectedCaptchaProfileID": uint(0),
		"ContentView":              "admin/forms/new/content",
	}, "")
}

// AdminFormsCreate persists a new form configuration.
func AdminFormsCreate(ctx *cartridge.Context) error {
	db := ctx.DB()

	templateID := strings.TrimSpace(ctx.FormValue("template_id"))
	selectedTemplate := GetTemplateByID(templateID)

	// Parse mailer profile ID if provided
	var mailerProfileID *uint
	if mailerIDStr := ctx.FormValue("mailer_profile_id"); mailerIDStr != "" {
		if id, err := strconv.ParseUint(mailerIDStr, 10, 32); err == nil {
			uid := uint(id)
			mailerProfileID = &uid
		}
	}

	// Parse captcha profile ID if provided
	var captchaProfileID *uint
	if captchaIDStr := ctx.FormValue("captcha_profile_id"); captchaIDStr != "" {
		if id, err := strconv.ParseUint(captchaIDStr, 10, 32); err == nil {
			uid := uint(id)
			captchaProfileID = &uid
		}
	}

	// Use forms context for business logic
	params := forms.CreateParams{
		Name:               ctx.FormValue("name"),
		Slug:               ctx.FormValue("slug"),
		AllowedOrigins:     ctx.FormValue("allowed_origins"),
		UseSDK:             ctx.FormValue("use_sdk") == "on",
		GeneratedHTML:      ctx.FormValue("generated_html"),
		MailerProfileID:    mailerProfileID,
		CaptchaProfileID:   captchaProfileID,
		EmailRecipient:     ctx.FormValue("email_recipient"),
		EmailEnabled:       ctx.FormValue("email_enabled") == "on",
		WebhookEnabled:     ctx.FormValue("webhook_enabled") == "on",
		WebhookURL:         ctx.FormValue("webhook_url"),
		WebhookSecret:      ctx.FormValue("webhook_secret"),
		WebhookHeadersJSON: ctx.FormValue("webhook_headers"),
		TemplateID:         templateID,
	}

	form, err := forms.Create(ctx.Logger, db, params)
	if err != nil {
		// Handle validation errors
		if validationErr, ok := err.(*forms.ValidationError); ok {
			return renderFormError(ctx, validationErr.Message, nil, nil, nil, false, selectedTemplate)
		}
		ctx.Logger.Error("failed to create form", slog.Any("error", err))
		return fiber.ErrInternalServerError
	}

	// Update generated HTML if template was selected
	if selectedTemplate != nil {
		if html := selectedTemplate.RenderHTML(liveFormAction(form.Slug, form.Token)); strings.TrimSpace(html) != "" {
			form.GeneratedHTML = html
			if err := dbtxn.WithRetry(ctx.Logger, db, func(tx *gorm.DB) error {
				return tx.Model(form).Update("generated_html", html).Error
			}); err != nil {
				ctx.Logger.Error("failed to update generated HTML", slog.Any("error", err))
			}
		}
	}

	return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", form.ID))
}

// AdminFormShow displays a form summary and recent submissions.
func AdminFormShow(ctx *cartridge.Context) error {
	db := ctx.DB()
	logger := ctx.Logger

	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return fiber.ErrNotFound
	}

	form, err := forms.GetByID(db, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	// Ensure delivery records exist
	if err := forms.EnsureDeliveryRecords(logger, db, form); err != nil {
		// Log but don't fail - continue showing the form
		logger.Error("failed to ensure delivery records", slog.Any("error", err))
	}

	submissions, err := forms.GetSubmissions(db, form.ID, 25)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	webhookEvents, err := forms.GetWebhookEvents(db, form.ID, 20)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	emailEvents, err := forms.GetEmailEvents(db, form.ID, 20)
	if err != nil {
		return fiber.ErrInternalServerError
	}

	// Extract email recipient from overrides for display
	emailRecipient := ""
	if form.EmailDelivery != nil && form.EmailDelivery.OverridesJSON != "" {
		var overrides map[string]interface{}
		if err := json.Unmarshal([]byte(form.EmailDelivery.OverridesJSON), &overrides); err == nil {
			if to, ok := overrides["to"].(string); ok {
				emailRecipient = to
			}
		}
	}

	endpoint := fmt.Sprintf("/forms/%s/submit", form.Slug)
	hasGeneratedHTML := strings.TrimSpace(form.GeneratedHTML) != ""
	embedResult := formembed.Build(form, formembed.Options{ShowToken: true})
	if embedResult.Warning != "" && logger != nil {
		logger.Warn("failed to normalize generated form HTML", slog.String("warning", embedResult.Warning), slog.Uint64("form_id", uint64(form.ID)))
	}

	return ctx.Render("layouts/base", fiber.Map{
		"Title":            form.Name,
		"Form":             form,
		"Submissions":      submissions,
		"Endpoint":         endpoint,
		"Token":            form.Token,
		"WebhookEvents":    webhookEvents,
		"EmailEvents":      emailEvents,
		"EmailRecipient":   emailRecipient,
		"FormCode":         embedResult.HTML,
		"HasGeneratedHTML": hasGeneratedHTML,
		"ContentView":      "admin/forms/show/content",
	}, "")
}

// AdminFormsEdit renders the edit form.
func AdminFormsEdit(ctx *cartridge.Context) error {
	db := ctx.DB()

	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return fiber.ErrNotFound
	}

	form, err := forms.GetByID(db, uint(id))
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	// Initialize deliveries if they don't exist
	logger := ctx.Logger
	if err := forms.EnsureDeliveryRecords(logger, db, form); err != nil {
		logger.Error("failed to ensure delivery records", slog.Any("error", err))
	}

	// Load profiles for dropdowns
	mailerProfiles, _ := integrations.ListMailerProfiles(db)
	captchaProfiles, _ := integrations.ListCaptchaProfiles(db)

	// Extract email recipient from overrides for display
	emailRecipient := ""
	if form.EmailDelivery != nil && form.EmailDelivery.OverridesJSON != "" {
		var overrides map[string]interface{}
		if err := json.Unmarshal([]byte(form.EmailDelivery.OverridesJSON), &overrides); err == nil {
			if to, ok := overrides["to"].(string); ok {
				emailRecipient = to
			}
		}
	}

	// Extract selected profile IDs
	selectedMailerProfileID := uint(0)
	if form.EmailDelivery != nil && form.EmailDelivery.MailerProfileID != nil {
		selectedMailerProfileID = *form.EmailDelivery.MailerProfileID
	}

	selectedCaptchaProfileID := uint(0)
	if form.CaptchaProfileID != nil {
		selectedCaptchaProfileID = *form.CaptchaProfileID
	}

	previewHTML := form.GeneratedHTML
	if strings.TrimSpace(previewHTML) == "" {
		if blank := GetTemplateByID("blank"); blank != nil {
			previewHTML = blank.RenderHTML(liveFormAction(form.Slug, form.Token))
		}
	}

	return ctx.Render("layouts/base", fiber.Map{
		"Title":                    "Edit endpoint",
		"Form":                     form,
		"EmailDelivery":            form.EmailDelivery,
		"WebhookDelivery":          form.WebhookDelivery,
		"EmailRecipient":           emailRecipient,
		"IsEdit":                   true,
		"MailerProfiles":           mailerProfiles,
		"CaptchaProfiles":          captchaProfiles,
		"SelectedMailerProfileID":  selectedMailerProfileID,
		"SelectedCaptchaProfileID": selectedCaptchaProfileID,
		"PreviewHTML":              previewHTML,
		"ContentView":              "admin/forms/new/content",
	}, "")
}

// AdminFormsUpdate persists changes to an existing form.
func AdminFormsUpdate(ctx *cartridge.Context) error {
	db := ctx.DB()
	logger := ctx.Logger

	id, err := strconv.Atoi(ctx.Params("id"))
	if err != nil {
		return fiber.ErrNotFound
	}

	// Parse mailer profile ID
	var mailerProfileID *uint
	if mailerIDStr := ctx.FormValue("mailer_profile_id"); mailerIDStr != "" {
		if pid, err := strconv.ParseUint(mailerIDStr, 10, 32); err == nil {
			uid := uint(pid)
			mailerProfileID = &uid
		}
	}

	// Parse captcha profile ID
	var captchaProfileID *uint
	if captchaIDStr := ctx.FormValue("captcha_profile_id"); captchaIDStr != "" {
		if pid, err := strconv.ParseUint(captchaIDStr, 10, 32); err == nil {
			uid := uint(pid)
			captchaProfileID = &uid
		}
	}

	params := forms.UpdateParams{
		ID:                 uint(id),
		Name:               ctx.FormValue("name"),
		AllowedOrigins:     ctx.FormValue("allowed_origins"),
		UseSDK:             ctx.FormValue("use_sdk") == "on",
		MailerProfileID:    mailerProfileID,
		CaptchaProfileID:   captchaProfileID,
		EmailRecipient:     ctx.FormValue("email_recipient"),
		EmailEnabled:       ctx.FormValue("email_enabled") == "on",
		WebhookEnabled:     ctx.FormValue("webhook_enabled") == "on",
		WebhookURL:         ctx.FormValue("webhook_url"),
		WebhookSecret:      ctx.FormValue("webhook_secret"),
		WebhookHeadersJSON: ctx.FormValue("webhook_headers"),
	}

	updatedForm, err := forms.Update(logger, db, params)
	if err != nil {
		// Handle validation errors
		if valErr, ok := err.(*forms.ValidationError); ok {
			form, _ := forms.GetByID(db, uint(id))
			return renderFormError(ctx, valErr.Message, form, form.EmailDelivery, form.WebhookDelivery, true, nil)
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.ErrNotFound
		}
		return fiber.ErrInternalServerError
	}

	return ctx.Redirect(fmt.Sprintf("/admin/forms/%d", updatedForm.ID))
}

func renderFormError(ctx *cartridge.Context, message string, form *forms.Form, emailDelivery *forms.EmailDelivery, webhookDelivery *forms.WebhookDelivery, isEdit bool, template *FormTemplate) error {
	// Load profiles for dropdowns
	db := ctx.DB()
	mailerProfiles, _ := integrations.ListMailerProfiles(db)
	captchaProfiles, _ := integrations.ListCaptchaProfiles(db)

	// Extract email recipient from overrides for display
	emailRecipient := ""
	if emailDelivery != nil && emailDelivery.OverridesJSON != "" {
		var overrides map[string]interface{}
		if err := json.Unmarshal([]byte(emailDelivery.OverridesJSON), &overrides); err == nil {
			if to, ok := overrides["to"].(string); ok {
				emailRecipient = to
			}
		}
	}

	// Extract selected profile IDs
	selectedMailerProfileID := uint(0)
	if emailDelivery != nil && emailDelivery.MailerProfileID != nil {
		selectedMailerProfileID = *emailDelivery.MailerProfileID
	}

	selectedCaptchaProfileID := uint(0)
	if form != nil && form.CaptchaProfileID != nil {
		selectedCaptchaProfileID = *form.CaptchaProfileID
	}

	data := fiber.Map{
		"Title":                    "New endpoint",
		"Error":                    message,
		"DefaultSlug":              forms.Slugify("New Form"),
		"EmailDelivery":            emailDelivery,
		"WebhookDelivery":          webhookDelivery,
		"EmailRecipient":           emailRecipient,
		"IsEdit":                   isEdit,
		"MailerProfiles":           mailerProfiles,
		"CaptchaProfiles":          captchaProfiles,
		"SelectedMailerProfileID":  selectedMailerProfileID,
		"SelectedCaptchaProfileID": selectedCaptchaProfileID,
		"PreviewHTML":              "",
		"ContentView":              "admin/forms/new/content",
	}
	if form != nil {
		data["Form"] = form
		if isEdit {
			data["Title"] = "Edit Form"
			data["DefaultSlug"] = form.Slug
		}
	}
	if template != nil {
		data["Template"] = template
		data["TemplateID"] = template.ID
		data["FormName"] = template.Name
		if !isEdit && template.Slug != "" {
			data["DefaultSlug"] = template.Slug
		}
		if data["PreviewHTML"] == "" {
			data["PreviewHTML"] = template.RenderHTML(exampleFormAction(template.Slug))
		}
	}

	if form != nil && strings.TrimSpace(form.GeneratedHTML) != "" {
		data["PreviewHTML"] = form.GeneratedHTML
	}

	if preview, ok := data["PreviewHTML"].(string); ok && strings.TrimSpace(preview) == "" {
		if blank := GetTemplateByID("blank"); blank != nil {
			action := ""
			if form != nil {
				action = liveFormAction(form.Slug, form.Token)
			} else if template != nil {
				action = exampleFormAction(template.Slug)
			}
			data["PreviewHTML"] = blank.RenderHTML(action)
		}
	}

	return ctx.Render("layouts/base", data, "")
}

func exampleFormAction(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "your-form"
	}
	return fmt.Sprintf("/forms/%s/submit?token=YOUR_FORM_TOKEN", slug)
}

func liveFormAction(slug, token string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		slug = "your-form"
	}
	token = strings.TrimSpace(token)
	if token == "" {
		token = "YOUR_FORM_TOKEN"
	}
	return fmt.Sprintf("/forms/%s/submit?token=%s", slug, token)
}
