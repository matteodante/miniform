package jobs

import (
	"encoding/json"
	"fmt"
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
	query := ctx.DB.Preload("Submission.Form.EmailDelivery.MailerProfile")
	if err := dueEvents(query, time.Now()).Limit(10).Find(&events).Error; err != nil {
		ctx.Logger.Error("query email queue", slog.Any("error", err))
		return err
	}
	for i := range events {
		dispatcher.deliver(ctx, &events[i])
	}
	return nil
}

func (dispatcher *EmailDispatcher) deliver(ctx *cartridge.JobContext, event *forms.EmailEvent) {
	if event.Submission == nil || event.Submission.Form == nil {
		applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, "submission unavailable"))
		return
	}

	delivery := event.Submission.Form.EmailDelivery
	profile, from, to, err := emailSettings(delivery)
	if err != nil {
		applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
		return
	}

	subject := "New submission · " + event.Submission.Form.Name
	body := emailBody(event.Submission.DataJSON)
	settings, err := smtpSettings(profile, from, to)
	if err == nil {
		err = sendSMTP(ctx, settings, buildSMTPMessage(from, to, subject, body))
	}

	if err != nil {
		applyEmailState(ctx, ctx.DB, event, retryState(event.AttemptCount, dispatcher.retries, err))
		return
	}
	applyEmailState(ctx, ctx.DB, event, deliveredState(event.AttemptCount))
}

func emailSettings(delivery *forms.EmailDelivery) (*integrations.MailerProfile, string, string, error) {
	if delivery == nil || !delivery.Enabled {
		return nil, "", "", fmt.Errorf("email forwarding disabled")
	}
	profile := delivery.MailerProfile
	if profile == nil {
		return nil, "", "", fmt.Errorf("mailer configuration missing")
	}

	fromAddress, err := mail.ParseAddress(strings.TrimSpace(profile.DefaultFromEmail))
	if err != nil {
		return nil, "", "", fmt.Errorf("mailer sender missing or invalid")
	}
	fromAddress.Name = strings.TrimSpace(profile.DefaultFromName)

	to := strings.TrimSpace(delivery.Recipient)
	if to == "" {
		return nil, "", "", fmt.Errorf("email recipient missing")
	}
	return profile, fromAddress.String(), to, nil
}

func smtpSettings(profile *integrations.MailerProfile, from, to string) (*smtpConfig, error) {
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
		From: from, To: to,
	}, nil
}

func emailBody(rawJSON string) string {
	var fields map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &fields); err != nil {
		fields = map[string]any{"raw": rawJSON}
	}
	keys := make([]string, 0, len(fields))
	for key := range fields {
		keys = append(keys, key)
	}
	slices.Sort(keys)

	var body strings.Builder
	body.WriteString("New submission received:\n\n")
	for _, key := range keys {
		value, err := json.MarshalIndent(fields[key], "", "  ")
		if err != nil {
			value = fmt.Appendf(nil, "%v", fields[key])
		}
		fmt.Fprintf(&body, "%s:\n%s\n\n", key, value)
	}
	return body.String()
}
