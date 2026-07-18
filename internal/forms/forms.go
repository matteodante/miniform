package forms

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"strings"

	"golang.org/x/net/http/httpguts"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
	"github.com/matteodante/miniform/internal/pkg/sqliteerr"
)

type CreateParams struct {
	Name               string
	Slug               string
	AllowedOrigins     string
	GeneratedHTML      string
	TemplateID         string
	MailerProfileID    *uint
	CaptchaProfileID   *uint
	EmailRecipient     string
	EmailFormat        string
	EmailEnabled       bool
	WebhookEnabled     bool
	WebhookURL         string
	WebhookSecret      string
	WebhookHeadersJSON string
}

type UpdateParams struct {
	ID                  uint
	Name                string
	Slug                string
	AllowedOrigins      string
	GeneratedHTML       string
	MailerProfileID     *uint
	CaptchaProfileID    *uint
	UpdateGeneratedHTML bool
	EmailRecipient      string
	EmailFormat         string
	EmailEnabled        bool
	WebhookEnabled      bool
	WebhookURL          string
	WebhookSecret       string
	WebhookHeadersJSON  string
}

type ValidationError struct {
	Field   string
	Message string
}

func (err *ValidationError) Error() string {
	return err.Field + ": " + err.Message
}

type deliveryValues struct {
	emailRecipient string
	emailFormat    string
	webhookURL     string
	webhookSecret  string
	webhookHeaders string
}

func Create(logger *slog.Logger, db *gorm.DB, params CreateParams) (*Form, error) {
	name := strings.TrimSpace(params.Name)
	slug := strings.TrimSpace(params.Slug)
	origins := strings.TrimSpace(params.AllowedOrigins)
	if name == "" {
		return nil, invalid("name", "Name is required")
	}
	if slug == "" {
		return nil, invalid("slug", "Slug is required")
	}
	if origins == "" {
		return nil, invalid("allowed_origins", "Allowed origins is required")
	}
	origins, err := normalizeAllowedOrigins(origins)
	if err != nil {
		return nil, err
	}

	delivery, err := prepareDelivery(params.EmailEnabled, params.MailerProfileID, params.EmailRecipient, params.EmailFormat, params.WebhookEnabled, params.WebhookURL, params.WebhookSecret, params.WebhookHeadersJSON)
	if err != nil {
		return nil, err
	}
	normalizedSlug, err := Slugify(slug)
	if err != nil {
		return nil, fmt.Errorf("generate form slug: %w", err)
	}
	token, err := generateToken(24)
	if err != nil {
		return nil, fmt.Errorf("generate form token: %w", err)
	}
	generatedHTML := strings.TrimSpace(params.GeneratedHTML)
	if templateID := strings.TrimSpace(params.TemplateID); generatedHTML == "" && templateID != "" {
		template := GetTemplateByID(templateID)
		if template == nil {
			return nil, invalid("template", "Unknown form template")
		}
		action := "/forms/" + url.PathEscape(normalizedSlug) + "/submit?token=" + url.QueryEscape(token)
		generatedHTML = template.RenderHTML(action)
	}
	if err := validateGeneratedHTML(generatedHTML); err != nil {
		return nil, err
	}

	form := &Form{
		Name:             name,
		Slug:             normalizedSlug,
		AllowedOrigins:   origins,
		GeneratedHTML:    generatedHTML,
		Token:            token,
		CaptchaProfileID: params.CaptchaProfileID,
		EmailDelivery: &EmailDelivery{
			Enabled:         params.EmailEnabled,
			MailerProfileID: params.MailerProfileID,
			Recipient:       delivery.emailRecipient,
			Format:          delivery.emailFormat,
		},
		WebhookDelivery: &WebhookDelivery{
			Enabled:     params.WebhookEnabled,
			URL:         delivery.webhookURL,
			Secret:      delivery.webhookSecret,
			HeadersJSON: delivery.webhookHeaders,
		},
	}

	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if err := validateProfileReferences(tx, params.MailerProfileID, params.CaptchaProfileID); err != nil {
			return err
		}
		return tx.Create(form).Error
	}); err != nil {
		if isUniqueConstraint(err) {
			return nil, invalid("slug", "Slug already exists")
		}
		return nil, fmt.Errorf("create form: %w", err)
	}
	return form, nil
}

func Update(logger *slog.Logger, db *gorm.DB, params UpdateParams) (*Form, error) {
	name := strings.TrimSpace(params.Name)
	if name == "" {
		return nil, invalid("name", "Name is required")
	}

	form, err := GetByID(db, params.ID)
	if err != nil {
		return nil, err
	}
	if form.EmailDelivery == nil || form.WebhookDelivery == nil {
		return nil, fmt.Errorf("form %d has incomplete delivery configuration", params.ID)
	}
	delivery, err := prepareDelivery(params.EmailEnabled, params.MailerProfileID, params.EmailRecipient, params.EmailFormat, params.WebhookEnabled, params.WebhookURL, params.WebhookSecret, params.WebhookHeadersJSON)
	if err != nil {
		return nil, err
	}

	origins := form.AllowedOrigins
	if value := strings.TrimSpace(params.AllowedOrigins); value != "" {
		origins, err = normalizeAllowedOrigins(value)
		if err != nil {
			return nil, err
		}
	}
	slug := form.Slug
	if value := strings.TrimSpace(params.Slug); value != "" {
		slug, err = Slugify(value)
		if err != nil {
			return nil, fmt.Errorf("generate form slug: %w", err)
		}
	}
	generatedHTML := form.GeneratedHTML
	if params.UpdateGeneratedHTML {
		generatedHTML = strings.TrimSpace(params.GeneratedHTML)
		if err := validateGeneratedHTML(generatedHTML); err != nil {
			return nil, err
		}
	}
	err = dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if err := validateProfileReferences(tx, params.MailerProfileID, params.CaptchaProfileID); err != nil {
			return err
		}
		if err := tx.Model(&Form{}).Where("id = ?", params.ID).Updates(map[string]any{
			"name": name, "slug": slug, "allowed_origins": origins,
			"generated_html":     generatedHTML,
			"captcha_profile_id": params.CaptchaProfileID,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&EmailDelivery{}).Where("id = ?", form.EmailDelivery.ID).Updates(map[string]any{
			"enabled": params.EmailEnabled, "mailer_profile_id": params.MailerProfileID,
			"recipient": delivery.emailRecipient, "format": delivery.emailFormat,
		}).Error; err != nil {
			return err
		}
		return tx.Model(&WebhookDelivery{}).Where("id = ?", form.WebhookDelivery.ID).Updates(map[string]any{
			"enabled": params.WebhookEnabled, "url": delivery.webhookURL,
			"secret": delivery.webhookSecret, "headers_json": delivery.webhookHeaders,
		}).Error
	})
	if isUniqueConstraint(err) {
		return nil, invalid("slug", "Slug already exists")
	}
	if err != nil {
		return nil, fmt.Errorf("update form %d: %w", params.ID, err)
	}
	return GetByID(db, params.ID)
}

func GetByID(db *gorm.DB, id uint) (*Form, error) {
	return loadForm(db.Where("id = ?", id))
}

func GetBySlug(db *gorm.DB, slug string) (*Form, error) {
	return loadForm(db.Where("slug = ?", slug))
}

func List(db *gorm.DB) ([]Form, error) {
	var result []Form
	if err := db.Preload("EmailDelivery").Preload("WebhookDelivery").Order("name ASC").Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list forms: %w", err)
	}
	return result, nil
}

func GetSubmissions(db *gorm.DB, formID uint, limit int) ([]Submission, error) {
	var result []Submission
	if err := db.Where("form_id = ?", formID).Order("created_at DESC, id DESC").Limit(limit).Find(&result).Error; err != nil {
		return nil, fmt.Errorf("list form submissions: %w", err)
	}
	return result, nil
}

func GetWebhookEvents(db *gorm.DB, formID uint, limit int) ([]WebhookEvent, error) {
	return recentEvents[WebhookEvent](db, formID, limit)
}

func GetEmailEvents(db *gorm.DB, formID uint, limit int) ([]EmailEvent, error) {
	return recentEvents[EmailEvent](db, formID, limit)
}

func deleteFormRecords(tx *gorm.DB, id uint) error {
	submissionIDs := tx.Model(&Submission{}).Select("id").Where("form_id = ?", id)
	for _, model := range []any{&WebhookEvent{}, &EmailEvent{}, &SubmissionFile{}} {
		if err := tx.Where("submission_id IN (?)", submissionIDs).Delete(model).Error; err != nil {
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
	deleted := tx.Delete(&Form{}, id)
	if deleted.Error != nil {
		return deleted.Error
	}
	if deleted.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// ReconcileDeliveryRecords restores the one-to-one delivery rows required by every form.
func ReconcileDeliveryRecords(logger *slog.Logger, db *gorm.DB) error {
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		var missingEmail []uint
		if err := tx.Model(&Form{}).
			Where("NOT EXISTS (SELECT 1 FROM email_deliveries WHERE email_deliveries.form_id = forms.id)").
			Pluck("id", &missingEmail).Error; err != nil {
			return err
		}
		for _, formID := range missingEmail {
			if err := tx.Create(&EmailDelivery{FormID: formID}).Error; err != nil {
				return err
			}
		}

		var missingWebhooks []uint
		if err := tx.Model(&Form{}).
			Where("NOT EXISTS (SELECT 1 FROM webhook_deliveries WHERE webhook_deliveries.form_id = forms.id)").
			Pluck("id", &missingWebhooks).Error; err != nil {
			return err
		}
		for _, formID := range missingWebhooks {
			if err := tx.Create(&WebhookDelivery{FormID: formID}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func loadForm(query *gorm.DB) (*Form, error) {
	var form Form
	err := query.
		Preload("EmailDelivery.MailerProfile").
		Preload("WebhookDelivery").
		Preload("CaptchaProfile").
		First(&form).Error
	if err != nil {
		return nil, fmt.Errorf("load form: %w", err)
	}
	return &form, nil
}

func recentEvents[T any](db *gorm.DB, formID uint, limit int) ([]T, error) {
	var events []T
	submissionIDs := db.Model(&Submission{}).Select("id").Where("form_id = ?", formID)
	if err := db.Preload("Submission").Where("submission_id IN (?)", submissionIDs).
		Order("created_at DESC, id DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, fmt.Errorf("list delivery events: %w", err)
	}
	return events, nil
}

func prepareDelivery(emailEnabled bool, mailerID *uint, recipient, emailFormat string, webhookEnabled bool, webhookURL, webhookSecret, webhookHeaders string) (deliveryValues, error) {
	values := deliveryValues{
		webhookURL:    strings.TrimSpace(webhookURL),
		webhookSecret: strings.TrimSpace(webhookSecret),
	}
	format, err := NormalizeEmailFormat(emailFormat)
	if err != nil {
		return values, invalid("email_format", "Email format must be text or html")
	}
	values.emailFormat = format

	recipient = strings.TrimSpace(recipient)
	if recipient != "" {
		recipients, err := ParseEmailRecipients(recipient)
		if err != nil {
			return values, invalid("email", "Email recipients must be valid addresses")
		}
		formatted := make([]string, len(recipients))
		for i := range recipients {
			formatted[i] = FormatEmailRecipient(recipients[i])
		}
		values.emailRecipient = strings.Join(formatted, ", ")
	}
	if emailEnabled && (mailerID == nil || values.emailRecipient == "") {
		return values, invalid("email", "Mailer profile and email recipient required when email forwarding is enabled")
	}
	if webhookEnabled && values.webhookURL == "" {
		return values, invalid("webhook", "Webhook URL required when webhook delivery is enabled")
	}
	if webhookEnabled {
		endpoint, err := url.Parse(values.webhookURL)
		if err != nil || endpoint.Host == "" ||
			(!strings.EqualFold(endpoint.Scheme, "http") && !strings.EqualFold(endpoint.Scheme, "https")) {
			return values, invalid("webhook", "Webhook URL must be an absolute HTTP or HTTPS URL")
		}
	}

	values.webhookHeaders, err = canonicalWebhookHeaders(webhookHeaders)
	return values, err
}

func NormalizeEmailFormat(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return EmailFormatText, nil
	}
	if value != EmailFormatText && value != EmailFormatHTML {
		return "", fmt.Errorf("unsupported email format %q", value)
	}
	return value, nil
}

func ParseEmailRecipients(value string) ([]*mail.Address, error) {
	lines := strings.FieldsFunc(value, func(r rune) bool { return r == '\r' || r == '\n' })
	if len(lines) == 0 {
		return nil, fmt.Errorf("email recipients missing")
	}
	recipients, err := mail.ParseAddressList(strings.Join(lines, ","))
	if err != nil {
		return nil, err
	}
	if len(recipients) == 0 {
		return nil, fmt.Errorf("email recipients missing")
	}

	unique := recipients[:0]
	seen := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		key := strings.ToLower(recipient.Address)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, recipient)
	}
	return unique, nil
}

func FormatEmailRecipient(recipient *mail.Address) string {
	if recipient.Name == "" {
		return recipient.Address
	}
	return recipient.String()
}

func canonicalWebhookHeaders(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(value), &headers); err != nil {
		return "", invalid("webhook_headers", "Webhook headers must be valid JSON")
	}
	for name, value := range headers {
		if !httpguts.ValidHeaderFieldName(name) {
			return "", invalid("webhook_headers", "Webhook header names must be valid HTTP header names")
		}
		if !httpguts.ValidHeaderFieldValue(value) {
			return "", invalid("webhook_headers", "Webhook header values must be valid HTTP header values")
		}
	}
	normalized, err := json.Marshal(headers)
	if err != nil {
		return "", fmt.Errorf("normalize webhook headers: %w", err)
	}
	return string(normalized), nil
}

func invalid(field, message string) *ValidationError {
	return &ValidationError{Field: field, Message: message}
}

func isUniqueConstraint(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	return sqliteerr.IsUniqueOrPrimaryConstraint(err)
}
