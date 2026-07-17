package jobs

import (
	"log/slog"
	"strings"
	"time"

	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

const storedErrorLimit = 500

type retryPlan struct {
	delays []time.Duration
	limit  int
}

func newRetryPlan(cfg *config.Config) retryPlan {
	seconds := cfg.WebhookBackoff()
	plan := retryPlan{delays: make([]time.Duration, len(seconds)), limit: cfg.Webhook.RetryLimit}
	if plan.limit < 1 {
		plan.limit = forms.DefaultRetryLimit
	}
	for i, delay := range seconds {
		plan.delays[i] = time.Duration(delay) * time.Second
	}
	return plan
}

func (plan retryPlan) next(attempt int, now time.Time) *time.Time {
	if len(plan.delays) == 0 {
		return nil
	}
	index := max(attempt-1, 0)
	index = min(index, len(plan.delays)-1)
	next := now.UTC().Add(plan.delays[index])
	return &next
}

func dueEvents(db *gorm.DB, now time.Time) *gorm.DB {
	return db.Where("next_attempt_at <= ?", now.UTC()).Order("next_attempt_at, created_at, id")
}

type deliveryState struct {
	status        string
	attemptedAt   time.Time
	message       string
	nextAttemptAt *time.Time
	attemptCount  *int
}

func deliveredState(previousAttempts int) deliveryState {
	attempts := previousAttempts + 1
	return deliveryState{status: forms.WebhookStatusDelivered, attemptedAt: time.Now().UTC(), attemptCount: &attempts}
}

func finalState(status, message string) deliveryState {
	return deliveryState{status: status, attemptedAt: time.Now().UTC(), message: message}
}

func retryState(previousAttempts int, plan retryPlan, cause error) deliveryState {
	attempts := previousAttempts + 1
	now := time.Now().UTC()
	state := deliveryState{
		status:       forms.WebhookStatusRetrying,
		attemptedAt:  now,
		message:      compactError(cause),
		attemptCount: &attempts,
	}
	if attempts >= plan.limit {
		state.status = forms.WebhookStatusFailed
		return state
	}
	state.nextAttemptAt = plan.next(attempts, now)
	return state
}

func compactError(err error) string {
	if err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if len(message) > storedErrorLimit {
		return message[:storedErrorLimit]
	}
	return message
}

func saveState(ctx *cartridge.JobContext, db *gorm.DB, model any, id uint, state deliveryState) error {
	values := map[string]any{
		"status":           state.status,
		"last_attempt_at":  state.attemptedAt.UTC(),
		"last_attempt_err": state.message,
		"next_attempt_at":  state.nextAttemptAt,
	}
	if state.attemptCount != nil {
		values["attempt_count"] = *state.attemptCount
	}
	return dbtxn.WithRetry(ctx.Logger, db, func(tx *gorm.DB) error {
		return tx.Model(model).Where("id = ?", id).Updates(values).Error
	})
}

func applyWebhookState(ctx *cartridge.JobContext, db *gorm.DB, event *forms.WebhookEvent, state deliveryState) {
	if err := saveState(ctx, db, &forms.WebhookEvent{}, event.ID, state); err != nil {
		ctx.Logger.Error("update webhook event", slog.Uint64("id", uint64(event.ID)), slog.Any("error", err))
		return
	}
	event.Status = state.status
	event.LastAttemptAt = &state.attemptedAt
	event.LastAttemptErr = state.message
	event.NextAttemptAt = state.nextAttemptAt
	if state.attemptCount != nil {
		event.AttemptCount = *state.attemptCount
	}
}

func applyEmailState(ctx *cartridge.JobContext, db *gorm.DB, event *forms.EmailEvent, state deliveryState) {
	if err := saveState(ctx, db, &forms.EmailEvent{}, event.ID, state); err != nil {
		ctx.Logger.Error("update email event", slog.Uint64("id", uint64(event.ID)), slog.Any("error", err))
		return
	}
	event.Status = state.status
	event.LastAttemptAt = &state.attemptedAt
	event.LastAttemptErr = state.message
	event.NextAttemptAt = state.nextAttemptAt
	if state.attemptCount != nil {
		event.AttemptCount = *state.attemptCount
	}
}
