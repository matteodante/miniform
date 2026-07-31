package jobs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/karloscodes/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestWebhookDelivery(t *testing.T) {
	t.Run("posts signed submission payload", func(t *testing.T) {
		type receivedRequest struct {
			body, signature, source, idempotencyKey string
		}
		received := make(chan receivedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			received <- receivedRequest{
				body: string(body), signature: request.Header.Get("X-Miniform-Signature"),
				source: request.Header.Get("X-Source"), idempotencyKey: request.Header.Get("Idempotency-Key"),
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
		assert.Equal(t, fmt.Sprintf("miniform-%s-webhook-%d", event.Submission.Form.PublicID, event.ID), request.idempotencyKey)
		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusDelivered, stored.Status)
		assert.Equal(t, 1, stored.AttemptCount)
	})

	t.Run("does not silently disable a whitespace signing key", func(t *testing.T) {
		type receivedRequest struct {
			body, signature string
		}
		received := make(chan receivedRequest, 1)
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			body, _ := io.ReadAll(request.Body)
			received <- receivedRequest{body: string(body), signature: request.Header.Get("X-Miniform-Signature")}
			response.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.DiscardHandler)
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name: "Signed", Slug: "signed", AllowedOrigins: "*",
			WebhookEnabled: true, WebhookURL: server.URL, WebhookSecret: "  ",
		})
		require.NoError(t, err)
		_, err = forms.CreateSubmissionWithFiles(logger, db, form, map[string]any{"name": "Alice"}, "test", "", nil)
		require.NoError(t, err)

		cfg := &config.Config{}
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		require.NoError(t, NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)))

		request := <-received
		assert.Equal(t, signWebhook([]byte(request.body), "  "), request.signature)
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

	t.Run("delivers an event once with concurrent processors", func(t *testing.T) {
		var requests atomic.Int32
		firstRequest := make(chan struct{})
		releaseFirst := make(chan struct{})
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			if requests.Add(1) == 1 {
				close(firstRequest)
				<-releaseFirst
			}
			response.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		db := testsupport.SetupTestDB(t)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		sqlDB.SetMaxOpenConns(1)
		event := queuedWebhook(t, db, server.URL)
		cfg := &config.Config{}
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"

		firstDone := make(chan error, 1)
		go func() { firstDone <- NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)) }()
		select {
		case <-firstRequest:
		case <-time.After(time.Second):
			t.Fatal("first webhook request did not start")
		}

		secondDone := make(chan error, 1)
		go func() { secondDone <- NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)) }()
		select {
		case err := <-secondDone:
			require.NoError(t, err)
		case <-time.After(time.Second):
			t.Fatal("second processor did not finish")
		}
		close(releaseFirst)
		require.NoError(t, <-firstDone)

		assert.Equal(t, int32(1), requests.Load())
		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusDelivered, stored.Status)
		require.NoError(t, NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)))
		assert.Equal(t, int32(1), requests.Load())
	})

	t.Run("reloads webhook configuration after each claim", func(t *testing.T) {
		firstRequest := make(chan struct{})
		releaseFirst := make(chan struct{})
		t.Cleanup(func() {
			select {
			case <-releaseFirst:
			default:
				close(releaseFirst)
			}
		})
		var oldRequests atomic.Int32
		oldServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			if oldRequests.Add(1) == 1 {
				close(firstRequest)
				<-releaseFirst
			}
			response.WriteHeader(http.StatusNoContent)
		}))
		defer oldServer.Close()

		var newRequests atomic.Int32
		newServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			newRequests.Add(1)
			response.WriteHeader(http.StatusNoContent)
		}))
		defer newServer.Close()

		db := testsupport.SetupTestDB(t)
		firstEvent := queuedWebhook(t, db, oldServer.URL)
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			submission := &forms.Submission{FormID: firstEvent.Submission.FormID, DataJSON: `{"name":"Bob"}`}
			if err := tx.Create(submission).Error; err != nil {
				return err
			}
			return tx.Create(forms.NewWebhookEvent(submission.ID, time.Now().UTC())).Error
		}))

		cfg := &config.Config{}
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		done := make(chan error, 1)
		go func() { done <- NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)) }()
		select {
		case <-firstRequest:
		case <-time.After(time.Second):
			t.Fatal("first webhook request did not start")
		}
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			return tx.Model(&forms.WebhookDelivery{}).Where("form_id = ?", firstEvent.Submission.FormID).
				Update("url", newServer.URL).Error
		}))
		close(releaseFirst)
		require.NoError(t, <-done)

		assert.Equal(t, int32(1), oldRequests.Load())
		assert.Equal(t, int32(1), newRequests.Load())
	})

	t.Run("returns a state persistence failure", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		sqlDB, err := db.DB()
		require.NoError(t, err)
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_ = sqlDB.Close()
			response.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		queuedWebhook(t, db, server.URL)

		cfg := &config.Config{}
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		err = NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db))
		assert.ErrorContains(t, err, "database is closed")
	})

	t.Run("does not follow redirects", func(t *testing.T) {
		var redirectedRequests atomic.Int32
		target := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			redirectedRequests.Add(1)
			response.WriteHeader(http.StatusNoContent)
		}))
		defer target.Close()
		redirect := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			http.Redirect(response, request, target.URL, http.StatusFound)
		}))
		defer redirect.Close()

		db := testsupport.SetupTestDB(t)
		event := queuedWebhook(t, db, redirect.URL)
		cfg := &config.Config{}
		cfg.Webhook.BackoffSchedule = "60"
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		require.NoError(t, NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)))

		assert.Zero(t, redirectedRequests.Load())
		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusRetrying, stored.Status)
		assert.Contains(t, stored.LastAttemptErr, "302")
	})

	t.Run("does not overwrite an event after losing its claim", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		var event *forms.WebhookEvent
		renewedLease := time.Now().UTC().Add(2 * deliveryLease)
		mutationErr := make(chan error, 1)
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			mutationErr <- dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
				return tx.Model(&forms.WebhookEvent{}).Where("id = ?", event.ID).Update("next_attempt_at", renewedLease).Error
			})
			response.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()
		event = queuedWebhook(t, db, server.URL)

		cfg := &config.Config{}
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		err := NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db))
		require.NoError(t, <-mutationErr)
		assert.ErrorContains(t, err, "claim")

		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusDelivering, stored.Status)
		require.NotNil(t, stored.NextAttemptAt)
		assert.WithinDuration(t, renewedLease, *stored.NextAttemptAt, time.Millisecond)
	})

	t.Run("recovers an expired delivery lease", func(t *testing.T) {
		var requests atomic.Int32
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			requests.Add(1)
			response.WriteHeader(http.StatusNoContent)
		}))
		defer server.Close()

		db := testsupport.SetupTestDB(t)
		event := queuedWebhook(t, db, server.URL)
		expired := time.Now().UTC().Add(-time.Minute)
		require.NoError(t, dbtxn.WithRetry(slog.Default(), db, func(tx *gorm.DB) error {
			return tx.Model(&forms.WebhookEvent{}).Where("id = ?", event.ID).Updates(map[string]any{
				"status": forms.WebhookStatusDelivering, "next_attempt_at": expired,
			}).Error
		}))

		cfg := &config.Config{}
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		require.NoError(t, NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)))
		assert.Equal(t, int32(1), requests.Load())
		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, forms.WebhookStatusDelivered, stored.Status)
	})

	t.Run("does not persist webhook URL secrets in errors", func(t *testing.T) {
		listener, err := new(net.ListenConfig).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		endpoint := "http://" + listener.Addr().String() + "/hook?token=webhook-top-secret"
		require.NoError(t, listener.Close())

		db := testsupport.SetupTestDB(t)
		event := queuedWebhook(t, db, endpoint)
		cfg := &config.Config{}
		cfg.Webhook.BackoffSchedule = "60"
		cfg.Webhook.SignatureHeader = "X-Miniform-Signature"
		require.NoError(t, NewWebhookDispatcher(cfg).ProcessBatch(jobContext(db)))

		var stored forms.WebhookEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.NotContains(t, stored.LastAttemptErr, "webhook-top-secret")
		assert.Contains(t, stored.LastAttemptErr, "send webhook")
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
	submission.Form = form
	event.Submission = submission
	return event
}

func jobContext(db *gorm.DB) *cartridge.JobContext {
	return &cartridge.JobContext{
		Context: context.Background(), DB: db,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
}
