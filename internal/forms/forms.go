package forms

import (
	"encoding/json"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"log/slog"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

// CreateParams holds parameters for creating a new form
type CreateParams struct {
	Name                 string
	Slug                 string
	AllowedOrigins       string
	UseSDK               bool
	GeneratedHTML        string
	MailerProfileID      *uint
	CaptchaProfileID     *uint
	CaptchaOverridesJSON string
	EmailRecipient       string
	EmailEnabled         bool
	WebhookEnabled       bool
	WebhookURL           string
	WebhookSecret        string
	WebhookHeadersJSON   string
	TemplateID           string
}

// UpdateParams holds parameters for updating a form
type UpdateParams struct {
	ID                     uint
	Name                   string
	Slug                   string
	AllowedOrigins         string
	UseSDK                 bool
	GeneratedHTML          string
	MailerProfileID        *uint
	CaptchaProfileID       *uint
	CaptchaOverridesJSON   string
	UpdateGeneratedHTML    bool
	UpdateCaptchaOverrides bool
	EmailRecipient         string
	EmailEnabled           bool
	WebhookEnabled         bool
	WebhookURL             string
	WebhookSecret          string
	WebhookHeadersJSON     string
}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// Create creates a new form with the given parameters
func Create(logger *slog.Logger, db *gorm.DB, params CreateParams) (*Form, error) {
	// Validate required fields
	if strings.TrimSpace(params.Name) == "" {
		return nil, &ValidationError{Field: "name", Message: "Name is required"}
	}

	// Validate slug is provided
	slug := strings.TrimSpace(params.Slug)
	if slug == "" {
		return nil, &ValidationError{Field: "slug", Message: "Slug is required"}
	}
	slug = Slugify(slug)

	// Validate allowed origins
	if strings.TrimSpace(params.AllowedOrigins) == "" {
		return nil, &ValidationError{Field: "allowed_origins", Message: "Allowed origins is required"}
	}

	// Build email overrides JSON
	emailOverrides := make(map[string]interface{})
	if recipient := strings.TrimSpace(params.EmailRecipient); recipient != "" {
		emailOverrides["to"] = recipient
	}
	emailOverridesJSON := ""
	if len(emailOverrides) > 0 {
		if data, err := json.Marshal(emailOverrides); err == nil {
			emailOverridesJSON = string(data)
		}
	}

	// Validate email delivery settings
	if params.EmailEnabled && (params.MailerProfileID == nil || emailOverridesJSON == "") {
		return nil, &ValidationError{
			Field:   "email",
			Message: "Mailer profile and email recipient required when email forwarding is enabled",
		}
	}

	// Validate webhook delivery settings
	if params.WebhookEnabled && strings.TrimSpace(params.WebhookURL) == "" {
		return nil, &ValidationError{
			Field:   "webhook",
			Message: "Webhook URL required when webhook delivery is enabled",
		}
	}

	// Validate webhook headers JSON
	webhookHeadersJSON := strings.TrimSpace(params.WebhookHeadersJSON)
	if webhookHeadersJSON != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(webhookHeadersJSON), &headers); err != nil {
			return nil, &ValidationError{
				Field:   "webhook_headers",
				Message: "Webhook headers must be valid JSON",
			}
		}
		normalized, _ := json.Marshal(headers)
		webhookHeadersJSON = string(normalized)
	}

	captchaOverridesJSON := strings.TrimSpace(params.CaptchaOverridesJSON)
	if captchaOverridesJSON != "" {
		var overrides map[string]any
		if err := json.Unmarshal([]byte(captchaOverridesJSON), &overrides); err != nil {
			return nil, &ValidationError{Field: "captcha_overrides", Message: "Captcha overrides must be valid JSON"}
		}
		normalized, _ := json.Marshal(overrides)
		captchaOverridesJSON = string(normalized)
	}

	// Create form model
	form := &Form{
		Name:                 strings.TrimSpace(params.Name),
		Slug:                 slug,
		AllowedOrigins:       strings.TrimSpace(params.AllowedOrigins),
		UseSDK:               params.UseSDK,
		GeneratedHTML:        strings.TrimSpace(params.GeneratedHTML),
		CaptchaProfileID:     params.CaptchaProfileID,
		CaptchaOverridesJSON: captchaOverridesJSON,
	}

	// Create delivery records
	form.EmailDelivery = &EmailDelivery{
		Enabled:         params.EmailEnabled,
		MailerProfileID: params.MailerProfileID,
		OverridesJSON:   emailOverridesJSON,
	}

	form.WebhookDelivery = &WebhookDelivery{
		Enabled:     params.WebhookEnabled,
		URL:         strings.TrimSpace(params.WebhookURL),
		Secret:      strings.TrimSpace(params.WebhookSecret),
		HeadersJSON: webhookHeadersJSON,
	}

	// Persist to database
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		return tx.Create(form).Error
	}); err != nil {
		if isUniqueConstraint(err) {
			return nil, &ValidationError{Field: "slug", Message: "Slug already exists"}
		}
		logger.Error("failed to create form", slog.Any("error", err))
		return nil, err
	}

	return form, nil
}

// GetByID retrieves a form with all its relations
func GetByID(db *gorm.DB, id uint) (*Form, error) {
	var form Form
	if err := db.Where("id = ?", id).
		Preload("WebhookDelivery").
		Preload("EmailDelivery").
		Preload("EmailDelivery.MailerProfile").
		Preload("CaptchaProfile").
		First(&form).Error; err != nil {
		return nil, err
	}
	return &form, nil
}

// List retrieves all forms ordered by name
func List(db *gorm.DB) ([]Form, error) {
	var forms []Form
	if err := db.Order("name ASC").Find(&forms).Error; err != nil {
		return nil, err
	}
	return forms, nil
}

// GetSubmissions retrieves submissions for a form
func GetSubmissions(db *gorm.DB, formID uint, limit int) ([]Submission, error) {
	var submissions []Submission
	if err := db.Where("form_id = ?", formID).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&submissions).Error; err != nil {
		return nil, err
	}
	return submissions, nil
}

// GetWebhookEvents retrieves recent webhook events for a form
func GetWebhookEvents(db *gorm.DB, formID uint, limit int) ([]WebhookEvent, error) {
	var events []WebhookEvent
	if err := db.Preload("Submission").
		Where("submission_id IN (?)", db.Model(&Submission{}).Select("id").Where("form_id = ?", formID)).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// GetEmailEvents retrieves recent email events for a form
func GetEmailEvents(db *gorm.DB, formID uint, limit int) ([]EmailEvent, error) {
	var events []EmailEvent
	if err := db.Preload("Submission").
		Where("submission_id IN (?)", db.Model(&Submission{}).Select("id").Where("form_id = ?", formID)).
		Order("created_at DESC, id DESC").
		Limit(limit).
		Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// Delete deletes a form
func Delete(logger *slog.Logger, db *gorm.DB, id uint) error {
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		var submissionIDs []uint
		if err := tx.Model(&Submission{}).Where("form_id = ?", id).Pluck("id", &submissionIDs).Error; err != nil {
			return err
		}
		if len(submissionIDs) > 0 {
			if err := tx.Where("submission_id IN ?", submissionIDs).Delete(&WebhookEvent{}).Error; err != nil {
				return err
			}
			if err := tx.Where("submission_id IN ?", submissionIDs).Delete(&EmailEvent{}).Error; err != nil {
				return err
			}
			if err := tx.Where("submission_id IN ?", submissionIDs).Delete(&SubmissionFile{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("form_id = ?", id).Delete(&Submission{}).Error; err != nil {
			return err
		}
		if err := tx.Where("form_id = ?", id).Delete(&EmailDelivery{}).Error; err != nil {
			return err
		}
		if err := tx.Where("form_id = ?", id).Delete(&WebhookDelivery{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&Form{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

// GetBySlug retrieves a form by slug
func GetBySlug(db *gorm.DB, slug string) (*Form, error) {
	var form Form
	if err := db.Where("slug = ?", slug).
		Preload("WebhookDelivery").
		Preload("EmailDelivery").
		Preload("EmailDelivery.MailerProfile").
		Preload("CaptchaProfile").
		First(&form).Error; err != nil {
		return nil, err
	}
	return &form, nil
}

// EnsureDeliveryRecords creates delivery records if they don't exist
func EnsureDeliveryRecords(logger *slog.Logger, db *gorm.DB, form *Form) error {
	if form.EmailDelivery == nil {
		form.EmailDelivery = &EmailDelivery{FormID: form.ID}
		if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			return tx.Create(form.EmailDelivery).Error
		}); err != nil {
			logger.Error("failed to create email delivery", slog.Any("error", err), slog.Uint64("form_id", uint64(form.ID)))
			return err
		}
	}
	if form.WebhookDelivery == nil {
		form.WebhookDelivery = &WebhookDelivery{FormID: form.ID}
		if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			return tx.Create(form.WebhookDelivery).Error
		}); err != nil {
			logger.Error("failed to create webhook delivery", slog.Any("error", err), slog.Uint64("form_id", uint64(form.ID)))
			return err
		}
	}
	return nil
}

// Update updates an existing form
func Update(logger *slog.Logger, db *gorm.DB, params UpdateParams) (*Form, error) {
	// Validate required fields
	if strings.TrimSpace(params.Name) == "" {
		return nil, &ValidationError{Field: "name", Message: "Name is required"}
	}

	// Get existing form
	form, err := GetByID(db, params.ID)
	if err != nil {
		return nil, err
	}

	// Ensure delivery records exist
	if err := EnsureDeliveryRecords(logger, db, form); err != nil {
		return nil, err
	}

	allowedOrigins := form.AllowedOrigins
	if strings.TrimSpace(params.AllowedOrigins) != "" {
		allowedOrigins = strings.TrimSpace(params.AllowedOrigins)
	}

	slug := form.Slug
	if strings.TrimSpace(params.Slug) != "" {
		slug = Slugify(params.Slug)
	}
	generatedHTML := form.GeneratedHTML
	if params.UpdateGeneratedHTML {
		generatedHTML = strings.TrimSpace(params.GeneratedHTML)
	}
	captchaOverridesJSON := form.CaptchaOverridesJSON
	if params.UpdateCaptchaOverrides {
		captchaOverridesJSON = strings.TrimSpace(params.CaptchaOverridesJSON)
		if captchaOverridesJSON != "" {
			var overrides map[string]any
			if err := json.Unmarshal([]byte(captchaOverridesJSON), &overrides); err != nil {
				return nil, &ValidationError{Field: "captcha_overrides", Message: "Captcha overrides must be valid JSON"}
			}
			normalized, _ := json.Marshal(overrides)
			captchaOverridesJSON = string(normalized)
		}
	}

	// Build email overrides JSON
	emailOverrides := make(map[string]interface{})
	if recipient := strings.TrimSpace(params.EmailRecipient); recipient != "" {
		emailOverrides["to"] = recipient
	}
	emailOverridesJSON := ""
	if len(emailOverrides) > 0 {
		if data, err := json.Marshal(emailOverrides); err == nil {
			emailOverridesJSON = string(data)
		}
	}

	// Validate email delivery if enabled
	if params.EmailEnabled && (params.MailerProfileID == nil || emailOverridesJSON == "") {
		return nil, &ValidationError{
			Field:   "email",
			Message: "Mailer profile and email recipient required when email forwarding is enabled",
		}
	}

	// Validate webhook delivery if enabled
	if params.WebhookEnabled && strings.TrimSpace(params.WebhookURL) == "" {
		return nil, &ValidationError{
			Field:   "webhook",
			Message: "Webhook URL required when webhook delivery is enabled",
		}
	}

	// Validate webhook headers JSON if provided
	webhookHeadersJSON := strings.TrimSpace(params.WebhookHeadersJSON)
	if webhookHeadersJSON != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(webhookHeadersJSON), &headers); err != nil {
			return nil, &ValidationError{
				Field:   "webhook_headers",
				Message: "Webhook headers must be valid JSON",
			}
		}
		normalized, _ := json.Marshal(headers)
		webhookHeadersJSON = string(normalized)
	}

	// Update in transaction
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		// Update form fields
		if err := tx.Model(&Form{}).
			Where("id = ?", params.ID).
			Updates(map[string]any{
				"name":                   strings.TrimSpace(params.Name),
				"slug":                   slug,
				"captcha_profile_id":     params.CaptchaProfileID,
				"captcha_overrides_json": captchaOverridesJSON,
				"allowed_origins":        allowedOrigins,
				"use_sdk":                params.UseSDK,
				"generated_html":         generatedHTML,
			}).Error; err != nil {
			return err
		}

		// Update email delivery
		if err := tx.Model(&EmailDelivery{}).
			Where("id = ?", form.EmailDelivery.ID).
			Updates(map[string]any{
				"enabled":           params.EmailEnabled,
				"mailer_profile_id": params.MailerProfileID,
				"overrides_json":    emailOverridesJSON,
			}).Error; err != nil {
			return err
		}

		// Update webhook delivery
		if err := tx.Model(&WebhookDelivery{}).
			Where("id = ?", form.WebhookDelivery.ID).
			Updates(map[string]any{
				"enabled":      params.WebhookEnabled,
				"url":          strings.TrimSpace(params.WebhookURL),
				"secret":       strings.TrimSpace(params.WebhookSecret),
				"headers_json": webhookHeadersJSON,
			}).Error; err != nil {
			return err
		}

		return nil
	}); err != nil {
		if isUniqueConstraint(err) {
			return nil, &ValidationError{Field: "slug", Message: "Slug already exists"}
		}
		logger.Error("failed to update form", slog.Any("error", err), slog.Uint64("form_id", uint64(params.ID)))
		return nil, err
	}

	// Reload form with updated data
	return GetByID(db, params.ID)
}

// isUniqueConstraint checks if an error is a unique constraint violation
func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "unique") ||
		strings.Contains(errStr, "duplicate") ||
		strings.Contains(errStr, "constraint")
}
