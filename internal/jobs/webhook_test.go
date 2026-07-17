package jobs

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/karloscodes/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestWebhookDelivery(t *testing.T) {
	t.Run("posts signed submission payload", func(t *testing.T) {
		type receivedRequest struct {
			body, signature, source string
		}
		received := make(chan receivedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			received <- receivedRequest{
				body: string(body), signature: request.Header.Get("X-Miniform-Signature"),
				source: request.Header.Get("X-Source"),
			}
			response.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		db := testsupport.SetupTestDB(t)
		event := queuedWebhook(t, db, server.URL)
		cfg := &config.Config{}
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		ctx := jobContext(db)
		require.NoError(t, NewWebhookDispatcher(cfg).ProcessBatch(ctx))

		request := <-received
		assert.Contains(t, request.body, `"name":"Alice"`)
		assert.Equal(t, "test", request.source)
		assert.Equal(t, signWebhook([]byte(request.body), "webhook-secret"), request.signature)
		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusDelivered, stored.Status)
		assert.Equal(t, 1, stored.AttemptCount)
	})

	t.Run("schedules retry after remote failure", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusServiceUnavailable)
		}))
		defer server.Close()

		db := testsupport.SetupTestDB(t)
		event := queuedWebhook(t, db, server.URL)
		cfg := &config.Config{}
		cfg.Webhook.BackoffSchedule = "60"
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		require.NoError(t, NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)))

		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusRetrying, stored.Status)
		assert.Equal(t, 1, stored.AttemptCount)
		assert.NotNil(t, stored.NextAttemptAt)
		assert.Contains(t, stored.LastAttemptErr, "503")
	})
}

func queuedWebhook(t *testing.T, db *gorm.DB, endpoint string) *forms.WebhookEvent {
	t.Helper()
	form := &forms.Form{Name: "Contact", Slug: "contact-webhook", AllowedOrigins: "*"}
	require.NoError(t, db.Create(form).Error)
	require.NoError(t, db.Create(&forms.WebhookDelivery{
		FormID: form.ID, Enabled: true, URL: endpoint, Secret: "webhook-secret",
		HeadersJSON: `{"X-Source":"test"}`,
	}).Error)
	submission := &forms.Submission{FormID: form.ID, DataJSON: `{"name":"Alice"}`}
	require.NoError(t, db.Create(submission).Error)
	event := forms.NewWebhookEvent(submission.ID, time.Now())
	require.NoError(t, db.Create(event).Error)
	return event
}

func jobContext(db *gorm.DB) *cartridge.JobContext {
	return &cartridge.JobContext{
		Context: context.Background(), DB: db,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
