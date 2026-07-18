package jobs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/mail"
	"slices"
	"strings"
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
	query := ctx.DB.WithContext(ctx).Preload("Submission.Form.EmailDelivery.MailerProfile")
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

	delivery := event.Submission.Form.EmailDelivery
	profile, from, recipients, format, err := emailSettings(delivery)
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}

	subject := "New submission · " + event.Submission.Form.Name
	content, err := renderEmail(event.Submission)
	settings, settingsErr := smtpSettings(profile, from, recipients)
	if err == nil {
		err = settingsErr
	}
	if err == nil {
		message, messageErr := buildSMTPMessage(outboundEmail{
			From: from, To: recipients, Subject: subject, Format: format,
			TextBody: content.text, HTMLBody: content.html,
		})
		if messageErr != nil {
			err = messageErr
		} else {
			err = sendSMTP(ctx, settings, message)
		}
	}

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

func emailSettings(delivery *forms.EmailDelivery) (*integrations.MailerProfile, string, []string, string, error) {
	if delivery == nil || !delivery.Enabled {
		return nil, "", nil, "", fmt.Errorf("email forwarding disabled")
	}
	profile := delivery.MailerProfile
	if profile == nil {
		return nil, "", nil, "", fmt.Errorf("mailer configuration missing")
	}

	fromAddress, err := mail.ParseAddress(strings.TrimSpace(profile.DefaultFromEmail))
	if err != nil {
		return nil, "", nil, "", fmt.Errorf("mailer sender missing or invalid")
	}
	fromAddress.Name = strings.TrimSpace(profile.DefaultFromName)

	addresses, err := forms.ParseEmailRecipients(delivery.Recipient)
	if err != nil {
		return nil, "", nil, "", fmt.Errorf("email recipients missing or invalid")
	}
	recipients := make([]string, len(addresses))
	for i := range addresses {
		recipients[i] = forms.FormatEmailRecipient(addresses[i])
	}
	format, err := forms.NormalizeEmailFormat(delivery.Format)
	if err != nil {
		return nil, "", nil, "", fmt.Errorf("email format invalid")
	}
	return profile, fromAddress.String(), recipients, format, nil
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
	Fields      []emailField
}

type emailField struct {
	Name  string
	Value string
}

func renderEmail(submission *forms.Submission) (emailContent, error) {
	data := emailTemplateData{
		FormName:    submission.Form.Name,
		SubmittedAt: submission.CreatedAt.UTC().Format(time.RFC3339),
		Fields:      emailFields(submission.DataJSON),
	}

	var textBody strings.Builder
	fmt.Fprintf(&textBody, "New submission received\nForm: %s\nReceived: %s\n\n", data.FormName, data.SubmittedAt)
	if len(data.Fields) == 0 {
		textBody.WriteString("No scalar fields were submitted.\n")
	}
	for _, field := range data.Fields {
		fmt.Fprintf(&textBody, "%s:\n%s\n\n", field.Name, field.Value)
	}

	tmpl, err := template.New("email-notification").Parse(emailHTMLTemplate)
	if err != nil {
		return emailContent{}, fmt.Errorf("parse email template: %w", err)
	}
	var htmlBody bytes.Buffer
	if err := tmpl.Execute(&htmlBody, data); err != nil {
		return emailContent{}, fmt.Errorf("render email template: %w", err)
	}
	return emailContent{text: textBody.String(), html: htmlBody.String()}, nil
}

func emailFields(rawJSON string) []emailField {
	var fields map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
		fields = map[string]any{"raw": rawJSON}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	result := make([]emailField, 0, len(keys))
	for _, key := range keys {
		result = append(result, emailField{Name: key, Value: emailFieldValue(fields[key])})
	}
	return result
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

const emailHTMLTemplate = `<!doctype html>
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
{{if .Fields}}{{range .Fields}}<div style="margin:0 0 20px">
<p style="margin:0 0 6px;color:#55705d;font-size:12px;font-weight:bold;text-transform:uppercase">{{.Name}}</p>
<pre style="margin:0;white-space:pre-wrap;overflow-wrap:anywhere;color:#17201b;font-family:Arial,sans-serif;font-size:15px;line-height:1.55">{{.Value}}</pre>
</div>{{end}}{{else}}<p style="margin:0;color:#66736a">No scalar fields were submitted.</p>{{end}}
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`
