package jobs

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
)

type WebhookDispatcher struct {
	config  *config.Config
	client  *http.Client
	retries retryPlan
}

func NewWebhookDispatcher(cfg *config.Config) *WebhookDispatcher {
	return &WebhookDispatcher{
		config: cfg,
		client: &http.Client{
			Timeout:       10 * time.Second,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		retries: newRetryPlan(cfg),
	}
}

func (dispatcher *WebhookDispatcher) ProcessBatch(ctx *cartridge.JobContext) error {
	now := time.Now().UTC()
	eventIDs, err := dueEventIDs(ctx.DB.WithContext(ctx), &forms.WebhookEvent{}, now)
	if err != nil {
		ctx.Logger.Error("query webhook queue", slog.Any("error", err))
		return err
	}
	for _, eventID := range eventIDs {
		leaseUntil, err := claimEvent(ctx, ctx.DB, &forms.WebhookEvent{}, eventID, time.Now().UTC())
		if err != nil {
			return err
		}
		if leaseUntil == nil {
			continue
		}

		var event forms.WebhookEvent
		if err := ctx.DB.WithContext(ctx).
			Preload("Submission.Form.WebhookDelivery").
			First(&event, eventID).Error; err != nil {
			ctx.Logger.Error("load claimed webhook event", slog.Uint64("id", uint64(eventID)), slog.Any("error", err))
			return err
		}
		event.Status = forms.WebhookStatusDelivering
		event.NextAttemptAt = leaseUntil
		if err := dispatcher.deliver(ctx, &event); err != nil {
			return err
		}
	}
	return nil
}

func (dispatcher *WebhookDispatcher) deliver(ctx *cartridge.JobContext, event *forms.WebhookEvent) error {
	if event.Submission == nil || event.Submission.Form == nil {
		return applyWebhookState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, "submission unavailable"))
	}

	delivery := event.Submission.Form.WebhookDelivery
	if delivery == nil || !delivery.Enabled || delivery.URL == "" {
		return applyWebhookState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, "webhooks disabled for form"))
	}

	payload, err := webhookPayload(event.Submission)
	if err == nil {
		err = dispatcher.post(ctx, event, delivery, payload)
	}
	if err != nil {
		if stateErr := applyWebhookState(ctx, ctx.DB, event, retryState(event.AttemptCount, dispatcher.retries, err)); stateErr != nil {
			return stateErr
		}
		ctx.Logger.Warn("webhook delivery failed",
			slog.Uint64("event_id", uint64(event.ID)),
			slog.Int("attempt", event.AttemptCount),
			slog.String("status", event.Status),
		)
		return nil
	}
	return applyWebhookState(ctx, ctx.DB, event, deliveredState(event.AttemptCount))
}

func (dispatcher *WebhookDispatcher) post(ctx *cartridge.JobContext, event *forms.WebhookEvent, delivery *forms.WebhookDelivery, payload []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Miniform/1.0")

	for key, value := range webhookHeaders(ctx.Logger, delivery) {
		request.Header.Set(key, value)
	}
	request.Header.Set("Idempotency-Key", fmt.Sprintf("miniform-%s-webhook-%d", event.Submission.Form.PublicID, event.ID))
	if delivery.Secret != "" {
		request.Header.Set(dispatcher.config.Webhook.SignatureHeader, signWebhook(payload, delivery.Secret))
	}

	response, err := dispatcher.client.Do(request)
	if err != nil {
		var urlError *url.Error
		if errors.As(err, &urlError) {
			err = urlError.Err
		}
		return fmt.Errorf("send webhook: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if err := discardBody(response.Body); err != nil {
		return fmt.Errorf("read webhook response: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("webhook returned status %d", response.StatusCode)
	}
	return nil
}

func webhookPayload(submission *forms.Submission) ([]byte, error) {
	var data any
	if err := json.Unmarshal([]byte(submission.DataJSON), &data); err != nil {
		data = submission.DataJSON
	}
	form := submission.Form
	return json.Marshal(map[string]any{
		"form": map[string]any{
			"id": form.ID, "public_id": form.PublicID, "name": form.Name,
			"slug": form.Slug, "created_at": form.CreatedAt.UTC(),
		},
		"submission": map[string]any{
			"id": submission.ID, "data": data, "received_at": submission.CreatedAt.UTC(),
			"user_agent": submission.UserAgent,
		},
	})
}

func webhookHeaders(logger *slog.Logger, delivery *forms.WebhookDelivery) map[string]string {
	if delivery.HeadersJSON == "" {
		return nil
	}
	var headers map[string]string
	if err := json.Unmarshal([]byte(delivery.HeadersJSON), &headers); err != nil {
		logger.Warn("invalid webhook headers", slog.Uint64("form_id", uint64(delivery.FormID)), slog.Any("error", err))
		return nil
	}
	return headers
}

func signWebhook(payload []byte, secret string) string {
	signature := hmac.New(sha256.New, []byte(secret))
	_, _ = signature.Write(payload)
	return hex.EncodeToString(signature.Sum(nil))
}
