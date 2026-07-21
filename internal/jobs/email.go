package jobs

import (
	"bytes"
	"encoding/json"
	"fmt"
	htmltemplate "html/template"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
	texttemplate "text/template"
	"time"

	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

type EmailDispatcher struct {
	retries retryPlan
}

func NewEmailDispatcher(cfg *config.Config) *EmailDispatcher {
	return &EmailDispatcher{
		retries: newRetryPlan(cfg),
	}
}

func (dispatcher *EmailDispatcher) ProcessBatch(ctx *cartridge.JobContext) error {
	var events []forms.EmailEvent
	now := time.Now().UTC()
	query := ctx.DB.WithContext(ctx).
		Preload("Submission.Form").
		Preload("EmailDelivery.MailerProfile")
	if err := dueEvents(query, now).Limit(10).Find(&events).Error; err != nil {
		ctx.Logger.Error("query email queue", slog.Any("error", err))
		return err
	}
	for i := range events {
		leaseUntil, err := claimEvent(ctx, ctx.DB, &forms.EmailEvent{}, events[i].ID, time.Now().UTC())
		if err != nil {
			return err
		}
		if leaseUntil == nil {
			continue
		}
		events[i].Status = forms.WebhookStatusDelivering
		events[i].NextAttemptAt = leaseUntil
		if err := dispatcher.deliver(ctx, &events[i]); err != nil {
			return err
		}
	}
	return nil
}

func (dispatcher *EmailDispatcher) deliver(ctx *cartridge.JobContext, event *forms.EmailEvent) error {
	if event.Submission == nil || event.Submission.Form == nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, "submission unavailable"))
	}

	delivery := event.EmailDelivery
	if delivery == nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, "email notification unavailable"))
	}
	templateData := newEmailTemplateData(event.Submission)
	profile, from, recipients, replyTo, format, err := emailSettings(delivery, templateData.Fields)
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}

	subject, content, err := renderEmail(delivery, templateData, format)
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}
	settings, err := smtpSettings(profile, from, recipients)
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}
	message, err := buildSMTPMessage(outboundEmail{
		From: from, To: recipients, ReplyTo: replyTo, Subject: subject, Format: format,
		TextBody: content.text, HTMLBody: content.html,
	})
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}
	err = sendSMTP(ctx, settings, message)

	if err != nil {
		if stateErr := applyEmailState(ctx, ctx.DB, event, retryState(event.AttemptCount, dispatcher.retries, err)); stateErr != nil {
			return stateErr
		}
		ctx.Logger.Warn("email delivery failed",
			slog.Uint64("event_id", uint64(event.ID)),
			slog.Int("attempt", event.AttemptCount),
			slog.String("status", event.Status),
		)
		return nil
	}
	return applyEmailState(ctx, ctx.DB, event, deliveredState(event.AttemptCount))
}

func emailSettings(delivery *forms.EmailDelivery, fields map[string]string) (*integrations.MailerProfile, string, []string, string, string, error) {
	if delivery == nil || !delivery.Enabled {
		return nil, "", nil, "", "", fmt.Errorf("email notification disabled")
	}
	profile := delivery.MailerProfile
	if profile == nil {
		return nil, "", nil, "", "", fmt.Errorf("mailer configuration missing")
	}

	fromAddress, err := mail.ParseAddress(strings.TrimSpace(profile.DefaultFromEmail))
	if err != nil {
		return nil, "", nil, "", "", fmt.Errorf("mailer sender missing or invalid")
	}
	fromAddress.Name = strings.TrimSpace(profile.DefaultFromName)

	recipients, err := resolveEmailRecipients(delivery, fields)
	if err != nil {
		return nil, "", nil, "", "", err
	}
	replyTo, err := resolveEmailReplyTo(delivery, fields)
	if err != nil {
		return nil, "", nil, "", "", err
	}
	format, err := forms.NormalizeEmailFormat(delivery.Format)
	if err != nil {
		return nil, "", nil, "", "", fmt.Errorf("email format invalid")
	}
	return profile, fromAddress.String(), recipients, replyTo, format, nil
}

func smtpSettings(profile *integrations.MailerProfile, from string, recipients []string) (*smtpConfig, error) {
	if profile.SMTPHost == "" || profile.SMTPPort == 0 {
		return nil, fmt.Errorf("smtp configuration missing")
	}
	encryption := strings.ToLower(strings.TrimSpace(profile.SMTPEncryption))
	if encryption == "" {
		encryption = "starttls"
	}
	if !slices.Contains([]string{"starttls", "tls", "none"}, encryption) {
		return nil, fmt.Errorf("smtp encryption mode invalid")
	}
	return &smtpConfig{
		Host: profile.SMTPHost, Port: profile.SMTPPort,
		Username: profile.SMTPUsername, Password: profile.SMTPPassword, Encryption: encryption,
		From: from, Recipients: recipients,
	}, nil
}

type emailContent struct {
	text string
	html string
}

type emailTemplateData struct {
	FormName    string
	SubmittedAt string
	Fields      map[string]string
	FieldList   []emailField
}

type emailField struct {
	Name  string
	Value string
}

func newEmailTemplateData(submission *forms.Submission) emailTemplateData {
	fields, fieldList := emailFields(submission.DataJSON)
	return emailTemplateData{
		FormName: submission.Form.Name, SubmittedAt: submission.CreatedAt.UTC().Format(time.RFC3339),
		Fields: fields, FieldList: fieldList,
	}
}

func renderEmail(delivery *forms.EmailDelivery, data emailTemplateData, format string) (string, emailContent, error) {
	subject, err := executeTextTemplate("email subject", forms.EffectiveEmailSubject(delivery), data)
	if err != nil {
		return "", emailContent{}, err
	}
	subject = strings.TrimSpace(subject)
	if subject == "" || strings.ContainsAny(subject, "\r\n\x00") {
		return "", emailContent{}, fmt.Errorf("render email subject: result must be one non-empty line")
	}
	textBody, err := executeTextTemplate("email text", forms.EffectiveEmailText(delivery), data)
	if err != nil {
		return "", emailContent{}, err
	}
	content := emailContent{text: textBody}
	if format == forms.EmailFormatHTML {
		htmlTemplate, err := htmltemplate.New("email html").Option("missingkey=error").Parse(forms.EffectiveEmailHTML(delivery))
		if err != nil {
			return "", emailContent{}, fmt.Errorf("parse email HTML template: %w", err)
		}
		var htmlBody bytes.Buffer
		if err := htmlTemplate.Execute(&htmlBody, data); err != nil {
			return "", emailContent{}, fmt.Errorf("render email HTML template: %w", err)
		}
		content.html = htmlBody.String()
	}
	return subject, content, nil
}

func executeTextTemplate(name, source string, data emailTemplateData) (string, error) {
	tmpl, err := texttemplate.New(name).Option("missingkey=error").Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse %s template: %w", name, err)
	}
	var output bytes.Buffer
	if err := tmpl.Execute(&output, data); err != nil {
		return "", fmt.Errorf("render %s template: %w", name, err)
	}
	return output.String(), nil
}

func emailFields(rawJSON string) (map[string]string, []emailField) {
	var fields map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
		fields = map[string]any{"raw": rawJSON}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	values := make(map[string]string, len(keys))
	result := make([]emailField, 0, len(keys))
	for _, key := range keys {
		value := emailFieldValue(fields[key])
		values[key] = value
		result = append(result, emailField{Name: key, Value: value})
	}
	return values, result
}

func emailFieldValue(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprint(value)
	}
	return string(encoded)
}

func resolveEmailRecipients(delivery *forms.EmailDelivery, fields map[string]string) ([]string, error) {
	value := delivery.Recipient
	if delivery.RecipientSource == forms.EmailRecipientField {
		value = fields[delivery.Recipient]
		address, err := parseDynamicEmailAddress(value)
		if err != nil {
			return nil, fmt.Errorf("email recipient field %q missing or invalid", delivery.Recipient)
		}
		return []string{address}, nil
	}
	addresses, err := forms.ParseEmailRecipients(value)
	if err != nil {
		return nil, fmt.Errorf("email recipients missing or invalid")
	}
	recipients := make([]string, len(addresses))
	for i := range addresses {
		recipients[i] = forms.FormatEmailRecipient(addresses[i])
	}
	return recipients, nil
}

func resolveEmailReplyTo(delivery *forms.EmailDelivery, fields map[string]string) (string, error) {
	switch delivery.ReplyToSource {
	case "", forms.EmailReplyToNone:
		return "", nil
	case forms.EmailReplyToStatic:
		address, err := parseDynamicEmailAddress(delivery.ReplyTo)
		if err != nil {
			return "", fmt.Errorf("email Reply-To missing or invalid")
		}
		return address, nil
	case forms.EmailReplyToField:
		address, err := parseDynamicEmailAddress(fields[delivery.ReplyTo])
		if err != nil {
			return "", fmt.Errorf("email Reply-To field %q missing or invalid", delivery.ReplyTo)
		}
		return address, nil
	default:
		return "", fmt.Errorf("email Reply-To source invalid")
	}
}

func parseDynamicEmailAddress(value string) (string, error) {
	if strings.ContainsAny(value, "\r\n\x00") {
		return "", fmt.Errorf("email address contains a control character")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	if err != nil {
		return "", err
	}
	return forms.FormatEmailRecipient(address), nil
}
