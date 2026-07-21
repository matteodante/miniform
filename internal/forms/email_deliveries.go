package forms

import (
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"net/mail"
	"strings"
	texttemplate "text/template"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

const (
	DefaultEmailDeliveryName = "Email notification"
	DefaultEmailSubject      = "New submission · {{.FormName}}"
	DefaultEmailText         = `New submission received
Form: {{.FormName}}
Received: {{.SubmittedAt}}

{{if .FieldList}}{{range .FieldList}}{{.Name}}:
{{.Value}}

{{end}}{{else}}No scalar fields were submitted.
{{end}}`
	DefaultEmailHTML = `<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>New submission</title></head>
<body style="margin:0;background:#f4f5f2;color:#17201b;font-family:Arial,sans-serif">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="background:#f4f5f2;padding:32px 16px">
<tr><td align="center">
<table role="presentation" width="100%" cellspacing="0" cellpadding="0" style="max-width:640px;background:#ffffff;border:1px solid #dfe4df;border-radius:12px">
<tr><td style="padding:28px 32px 20px;border-bottom:1px solid #dfe4df">
<p style="margin:0 0 8px;color:#55705d;font-size:12px;font-weight:bold;letter-spacing:.08em;text-transform:uppercase">Miniform</p>
<h1 style="margin:0;font-size:24px;line-height:1.3">New submission · {{.FormName}}</h1>
<p style="margin:10px 0 0;color:#66736a;font-size:13px">Received {{.SubmittedAt}}</p>
</td></tr>
<tr><td style="padding:24px 32px">
{{if .FieldList}}{{range .FieldList}}<div style="margin:0 0 20px">
<p style="margin:0 0 6px;color:#55705d;font-size:12px;font-weight:bold;text-transform:uppercase">{{.Name}}</p>
<pre style="margin:0;white-space:pre-wrap;overflow-wrap:anywhere;color:#17201b;font-family:Arial,sans-serif;font-size:15px;line-height:1.55">{{.Value}}</pre>
</div>{{end}}{{else}}<p style="margin:0;color:#66736a">No scalar fields were submitted.</p>{{end}}
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`
)

type EmailDeliveryParams struct {
	ID              uint
	FormID          uint
	Name            string
	Enabled         bool
	MailerProfileID *uint
	RecipientSource string
	Recipient       string
	ReplyToSource   string
	ReplyTo         string
	SubjectTemplate string
	Format          string
	TextTemplate    string
	HTMLTemplate    string
}

func CreateEmailDelivery(logger *slog.Logger, db *gorm.DB, params EmailDeliveryParams) (*EmailDelivery, error) {
	delivery, err := prepareEmailDelivery(params)
	if err != nil {
		return nil, err
	}
	delivery.FormID = params.FormID
	if delivery.FormID == 0 {
		return nil, invalid("form_id", "Form is required")
	}
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		var form Form
		if err := tx.Select("id", "captcha_profile_id").First(&form, delivery.FormID).Error; err != nil {
			return err
		}
		if err := validateDynamicRecipientSafeguard(delivery, form.CaptchaProfileID); err != nil {
			return err
		}
		if err := validateProfileReferences(tx, delivery.MailerProfileID, nil); err != nil {
			return err
		}
		return tx.Create(&delivery).Error
	}); err != nil {
		return nil, fmt.Errorf("create email notification: %w", err)
	}
	return GetEmailDelivery(db, delivery.FormID, delivery.ID)
}

func UpdateEmailDelivery(logger *slog.Logger, db *gorm.DB, params EmailDeliveryParams) (*EmailDelivery, error) {
	if params.ID == 0 || params.FormID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	delivery, err := prepareEmailDelivery(params)
	if err != nil {
		return nil, err
	}
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		var form Form
		if err := tx.Select("id", "captcha_profile_id").First(&form, params.FormID).Error; err != nil {
			return err
		}
		if err := validateDynamicRecipientSafeguard(delivery, form.CaptchaProfileID); err != nil {
			return err
		}
		if err := validateProfileReferences(tx, delivery.MailerProfileID, nil); err != nil {
			return err
		}
		result := tx.Model(&EmailDelivery{}).
			Where("id = ? AND form_id = ?", params.ID, params.FormID).
			Updates(map[string]any{
				"name": delivery.Name, "enabled": delivery.Enabled,
				"mailer_profile_id": delivery.MailerProfileID,
				"recipient_source":  delivery.RecipientSource, "recipient": delivery.Recipient,
				"reply_to_source": delivery.ReplyToSource, "reply_to": delivery.ReplyTo,
				"subject_template": delivery.SubjectTemplate, "format": delivery.Format,
				"text_template": delivery.TextTemplate, "html_template": delivery.HTMLTemplate,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("update email notification: %w", err)
	}
	return GetEmailDelivery(db, params.FormID, params.ID)
}

func DeleteEmailDelivery(logger *slog.Logger, db *gorm.DB, formID, id uint) error {
	if formID == 0 || id == 0 {
		return gorm.ErrRecordNotFound
	}
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		result := tx.Where("id = ? AND form_id = ?", id, formID).Delete(&EmailDelivery{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func GetEmailDelivery(db *gorm.DB, formID, id uint) (*EmailDelivery, error) {
	var delivery EmailDelivery
	if err := db.Preload("MailerProfile").Where("id = ? AND form_id = ?", id, formID).First(&delivery).Error; err != nil {
		return nil, err
	}
	return &delivery, nil
}

func GetEmailDeliveryByID(db *gorm.DB, id uint) (*EmailDelivery, error) {
	var delivery EmailDelivery
	if err := db.Preload("MailerProfile").First(&delivery, id).Error; err != nil {
		return nil, err
	}
	return &delivery, nil
}

func ListEmailDeliveries(db *gorm.DB, formID uint) ([]EmailDelivery, error) {
	var deliveries []EmailDelivery
	if err := db.Preload("MailerProfile").Where("form_id = ?", formID).Order("id ASC").Find(&deliveries).Error; err != nil {
		return nil, fmt.Errorf("list email notifications: %w", err)
	}
	return deliveries, nil
}

func PrimaryEmailDelivery(form *Form) *EmailDelivery {
	if form == nil || len(form.EmailDeliveries) == 0 {
		return nil
	}
	return &form.EmailDeliveries[0]
}

func EffectiveEmailSubject(delivery *EmailDelivery) string {
	if delivery == nil || strings.TrimSpace(delivery.SubjectTemplate) == "" {
		return DefaultEmailSubject
	}
	return delivery.SubjectTemplate
}

func EffectiveEmailText(delivery *EmailDelivery) string {
	if delivery == nil || strings.TrimSpace(delivery.TextTemplate) == "" {
		return DefaultEmailText
	}
	return delivery.TextTemplate
}

func EffectiveEmailHTML(delivery *EmailDelivery) string {
	if delivery == nil || strings.TrimSpace(delivery.HTMLTemplate) == "" {
		return DefaultEmailHTML
	}
	return delivery.HTMLTemplate
}

func prepareEmailDelivery(params EmailDeliveryParams) (EmailDelivery, error) {
	delivery := EmailDelivery{
		Name: strings.TrimSpace(params.Name), Enabled: params.Enabled,
		MailerProfileID: params.MailerProfileID,
		RecipientSource: strings.ToLower(strings.TrimSpace(params.RecipientSource)),
		Recipient:       strings.TrimSpace(params.Recipient),
		ReplyToSource:   strings.ToLower(strings.TrimSpace(params.ReplyToSource)),
		ReplyTo:         strings.TrimSpace(params.ReplyTo),
		SubjectTemplate: strings.TrimSpace(params.SubjectTemplate),
		Format:          params.Format,
		TextTemplate:    params.TextTemplate,
		HTMLTemplate:    params.HTMLTemplate,
	}
	if delivery.Name == "" {
		delivery.Name = DefaultEmailDeliveryName
	}
	if delivery.RecipientSource == "" {
		delivery.RecipientSource = EmailRecipientStatic
	}
	if delivery.ReplyToSource == "" {
		if delivery.ReplyTo == "" {
			delivery.ReplyToSource = EmailReplyToNone
		} else {
			delivery.ReplyToSource = EmailReplyToStatic
		}
	}

	format, err := NormalizeEmailFormat(delivery.Format)
	if err != nil {
		return delivery, invalid("email_format", "Email format must be text or html")
	}
	delivery.Format = format

	switch delivery.RecipientSource {
	case EmailRecipientStatic:
		if delivery.Recipient != "" {
			delivery.Recipient, err = normalizeEmailRecipients(delivery.Recipient)
		}
	case EmailRecipientField:
		if delivery.Recipient == "" {
			err = fmt.Errorf("field missing")
		}
	default:
		return delivery, invalid("recipient_source", "Recipient source must be static or field")
	}
	if err != nil {
		return delivery, invalid("email", "Email recipient must be a valid address list or field name")
	}
	if delivery.Enabled && (delivery.MailerProfileID == nil || delivery.Recipient == "") {
		return delivery, invalid("email", "Mailer profile and recipient are required when email is enabled")
	}

	switch delivery.ReplyToSource {
	case EmailReplyToNone:
		delivery.ReplyTo = ""
	case EmailReplyToStatic:
		if delivery.ReplyTo == "" {
			return delivery, invalid("email_reply_to", "Reply-To address is required")
		}
		delivery.ReplyTo, err = normalizeEmailAddress(delivery.ReplyTo)
	case EmailReplyToField:
		if delivery.ReplyTo == "" {
			err = fmt.Errorf("field missing")
		}
	default:
		return delivery, invalid("reply_to_source", "Reply-To source must be none, static, or field")
	}
	if err != nil {
		return delivery, invalid("email_reply_to", "Reply-To must be a valid address or field name")
	}

	if delivery.SubjectTemplate == "" {
		delivery.SubjectTemplate = DefaultEmailSubject
	}
	if strings.TrimSpace(delivery.TextTemplate) == "" {
		delivery.TextTemplate = DefaultEmailText
	}
	if strings.TrimSpace(delivery.HTMLTemplate) == "" {
		delivery.HTMLTemplate = DefaultEmailHTML
	}
	if err := validateEmailTemplates(&delivery); err != nil {
		return delivery, err
	}
	return delivery, nil
}

func normalizeEmailRecipients(value string) (string, error) {
	recipients, err := ParseEmailRecipients(value)
	if err != nil {
		return "", err
	}
	formatted := make([]string, len(recipients))
	for i := range recipients {
		formatted[i] = FormatEmailRecipient(recipients[i])
	}
	return strings.Join(formatted, ", "), nil
}

func normalizeEmailAddress(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("email address contains a control character")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return FormatEmailRecipient(address), nil
}

func validateEmailTemplates(delivery *EmailDelivery) error {
	if _, err := texttemplate.New("subject").Option("missingkey=error").Parse(delivery.SubjectTemplate); err != nil {
		return invalid("email_subject", "Email subject template is invalid")
	}
	if _, err := texttemplate.New("text").Option("missingkey=error").Parse(delivery.TextTemplate); err != nil {
		return invalid("email_text", "Email text template is invalid")
	}
	if _, err := htmltemplate.New("html").Option("missingkey=error").Parse(delivery.HTMLTemplate); err != nil {
		return invalid("email_html", "Email HTML template is invalid")
	}
	return nil
}
