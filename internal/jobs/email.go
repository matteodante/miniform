package jobs

import (
	"fmt"
	"log/slog"
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
	fields := forms.EmailTemplateFields(event.Submission.DataJSON)
	profile, from, recipients, replyTo, err := emailSettings(delivery, fields)
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}

	rendered, err := forms.RenderEmail(delivery, event.Submission)
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}
	settings, err := smtpSettings(profile, from, recipients)
	if err != nil {
		return applyEmailState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, err.Error()))
	}
	message, err := buildSMTPMessage(outboundEmail{
		From: from, To: recipients, ReplyTo: replyTo, Subject: rendered.Subject, Format: rendered.Format,
		TextBody: rendered.TextBody, HTMLBody: rendered.HTMLBody,
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

func emailSettings(delivery *forms.EmailDelivery, fields map[string]string) (*integrations.MailerProfile, string, []string, string, error) {
	if delivery == nil || !delivery.Enabled {
		return nil, "", nil, "", fmt.Errorf("email notification disabled")
	}
	profile := delivery.MailerProfile
	if profile == nil {
		return nil, "", nil, "", fmt.Errorf("mailer configuration missing")
	}

	from, err := integrations.MailerSender(profile)
	if err != nil {
		return nil, "", nil, "", err
	}

	recipients, err := forms.ResolveEmailRecipients(delivery, fields)
	if err != nil {
		return nil, "", nil, "", err
	}
	replyTo, err := forms.ResolveEmailReplyTo(delivery, fields)
	if err != nil {
		return nil, "", nil, "", err
	}
	return profile, from, recipients, replyTo, nil
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
