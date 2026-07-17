package jobs

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
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
		config:  cfg,
		client:  &http.Client{Timeout: 10 * time.Second},
		retries: newRetryPlan(cfg),
	}
}

func (dispatcher *WebhookDispatcher) ProcessBatch(ctx *cartridge.JobContext) error {
	var events []forms.WebhookEvent
	query := ctx.DB.Preload("Submission.Form.WebhookDelivery")
	if err := dueEvents(query, time.Now()).Limit(10).Find(&events).Error; err != nil {
		ctx.Logger.Error("query webhook queue", slog.Any("error", err))
		return err
	}
	for i := range events {
		dispatcher.deliver(ctx, &events[i])
	}
	return nil
}

func (dispatcher *WebhookDispatcher) deliver(ctx *cartridge.JobContext, event *forms.WebhookEvent) {
	if event.Submission == nil || event.Submission.Form == nil {
		applyWebhookState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, "submission unavailable"))
		return
	}

	delivery := event.Submission.Form.WebhookDelivery
	if delivery == nil || !delivery.Enabled || delivery.URL == "" {
		applyWebhookState(ctx, ctx.DB, event, finalState(forms.WebhookStatusFailed, "webhooks disabled for form"))
		return
	}

	payload, err := webhookPayload(event.Submission)
	if err == nil {
		err = dispatcher.post(ctx, delivery, payload)
	}
	if err != nil {
		applyWebhookState(ctx, ctx.DB, event, retryState(event.AttemptCount, dispatcher.retries, err))
		return
	}
	applyWebhookState(ctx, ctx.DB, event, deliveredState(event.AttemptCount))
}

func (dispatcher *WebhookDispatcher) post(ctx *cartridge.JobContext, delivery *forms.WebhookDelivery, payload []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, delivery.URL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create webhook request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("User-Agent", "Miniform/1.0")

	for key, value := range webhookHeaders(ctx.Logger, delivery) {
		request.Header.Set(key, value)
	}
	if delivery.Secret != "" {
		request.Header.Set(dispatcher.config.Webhook.SignatureHeader, signWebhook(payload, delivery.Secret))
	}

	response, err := dispatcher.client.Do(request)
	if err != nil {
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
