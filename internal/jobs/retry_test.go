package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/karloscodes/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestRetryQueue(t *testing.T) {
	t.Run("builds retry plan from configuration", func(t *testing.T) {
		cfg := &config.Config{}
		cfg.Webhook.BackoffSchedule = "60,300,900"
		plan := newRetryPlan(cfg)
		assert.Equal(t, []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}, plan.delays)
		assert.Equal(t, forms.DefaultRetryLimit, plan.limit)

		now := time.Date(2026, time.July, 16, 10, 0, 0, 0, time.UTC)
		assert.Equal(t, now.Add(time.Minute), *plan.next(0, now))
		assert.Equal(t, now.Add(5*time.Minute), *plan.next(2, now))
		assert.Equal(t, now.Add(15*time.Minute), *plan.next(99, now))
		assert.Nil(t, retryPlan{}.next(1, now))
	})

	t.Run("trims and limits stored errors", func(t *testing.T) {
		assert.Empty(t, compactError(nil))
		assert.Equal(t, "problem", compactError(errors.New("  problem  ")))
		assert.Len(t, compactError(errors.New(strings.Repeat("x", 600))), storedErrorLimit)
	})

	t.Run("transitions from retrying to failed", func(t *testing.T) {
		plan := retryPlan{delays: []time.Duration{time.Minute}, limit: forms.DefaultRetryLimit}
		first := retryState(0, plan, errors.New("temporary"))
		assert.Equal(t, forms.WebhookStatusRetrying, first.status)
		require.NotNil(t, first.nextAttemptAt)
		require.NotNil(t, first.attemptCount)
		assert.Equal(t, 1, *first.attemptCount)

		last := retryState(forms.DefaultRetryLimit-1, plan, errors.New("still broken"))
		assert.Equal(t, forms.WebhookStatusFailed, last.status)
		assert.Nil(t, last.nextAttemptAt)
	})

	t.Run("selects only due events in schedule order", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		form := &forms.Form{Name: "Queue", Slug: "queue", AllowedOrigins: "*"}
		require.NoError(t, db.Create(form).Error)
		submission := &forms.Submission{FormID: form.ID, DataJSON: `{}`}
		require.NoError(t, db.Create(submission).Error)

		now := time.Now().UTC()
		firstAt, secondAt, futureAt := now.Add(-2*time.Minute), now.Add(-time.Minute), now.Add(time.Minute)
		first := forms.NewEmailEvent(submission.ID, firstAt)
		second := forms.NewEmailEvent(submission.ID, secondAt)
		future := forms.NewEmailEvent(submission.ID, futureAt)
		require.NoError(t, db.Create(first).Error)
		require.NoError(t, db.Create(second).Error)
		require.NoError(t, db.Create(future).Error)

		var events []forms.EmailEvent
		require.NoError(t, dueEvents(db, now).Find(&events).Error)
		require.Len(t, events, 2)
		assert.Equal(t, []uint{first.ID, second.ID}, []uint{events[0].ID, events[1].ID})
	})

	t.Run("persists and mirrors event state", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		form := &forms.Form{Name: "State", Slug: "state", AllowedOrigins: "*"}
		require.NoError(t, db.Create(form).Error)
		submission := &forms.Submission{FormID: form.ID, DataJSON: `{}`}
		require.NoError(t, db.Create(submission).Error)
		event := forms.NewEmailEvent(submission.ID, time.Now())
		require.NoError(t, db.Create(event).Error)

		ctx := &cartridge.JobContext{
			Context: context.Background(), DB: db,
			Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		}
		state := deliveredState(event.AttemptCount)
		applyEmailState(ctx, db, event, state)
		assert.Equal(t, forms.WebhookStatusDelivered, event.Status)
		assert.Equal(t, 1, event.AttemptCount)
		assert.Equal(t, time.UTC, event.LastAttemptAt.Location())

		var stored forms.EmailEvent
		require.NoError(t, db.First(&stored, event.ID).Error)
		assert.Equal(t, event.Status, stored.Status)
		assert.Equal(t, event.AttemptCount, stored.AttemptCount)
		assert.Nil(t, stored.NextAttemptAt)
	})
}
