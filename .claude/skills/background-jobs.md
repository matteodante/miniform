# Background Job Pattern

## Overview

Background jobs process webhooks and emails asynchronously with retry logic and state management.

Each enabled email notification creates its own event linked by `email_delivery_id`. Never collapse sibling notifications into one event: their recipients, templates, status, and retries are independent.

## Location

`internal/jobs/`

## Job processor structure

```go
type WebhookDispatcher struct {
    config  *config.Config
    client  *http.Client
    retries retryPlan
}

func (dispatcher *WebhookDispatcher) ProcessBatch(ctx *cartridge.JobContext) error {
    var events []forms.WebhookEvent
    now := time.Now().UTC()
    query := ctx.DB.WithContext(ctx).Preload("Submission.Form.WebhookDelivery")
    if err := dueEvents(query, now).Limit(10).Find(&events).Error; err != nil {
        return err
    }

    for i := range events {
        leaseUntil, err := claimEvent(ctx, ctx.DB, &forms.WebhookEvent{}, events[i].ID, time.Now().UTC())
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
```

## State Machine

Events follow a state machine:

```
pending/retrying/expired delivering -> delivering lease -> delivered
                                                   \----> retrying
                                                   \----> failed
```

Status constants:

```go
const (
    WebhookStatusPending    = "pending"
    WebhookStatusDelivering = "delivering"
    WebhookStatusDelivered  = "delivered"
    WebhookStatusRetrying   = "retrying"
    WebhookStatusFailed     = "failed"

    DefaultRetryLimit = 3
)
```

`next_attempt_at` is the eligibility time for pending/retrying work and the one-minute lease deadline while delivering. Expired delivering rows are reclaimable.

## Claim and completion

```go
leaseUntil := now.UTC().Add(time.Minute)
err := dbtxn.WithRetry(ctx.Logger, db.WithContext(ctx), func(tx *gorm.DB) error {
    return tx.Model(model).
        Where("id = ?", id).
        Where("status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", eligible, now.UTC()).
        Updates(map[string]any{
            "status": forms.WebhookStatusDelivering,
            "next_attempt_at": leaseUntil,
        }).Error
})

// After network I/O, update only the lease still owned by this worker.
err = dbtxn.WithRetry(ctx.Logger, db.WithContext(ctx), func(tx *gorm.DB) error {
    result := tx.Model(model).
        Where("id = ? AND status = ? AND next_attempt_at = ?", id, forms.WebhookStatusDelivering, leaseUntil).
        Updates(values)
    if result.Error != nil {
        return result.Error
    }
    if result.RowsAffected != 1 {
        return errDeliveryClaimLost
    }
    return nil
})
```

Network I/O always happens outside a database transaction. Webhooks carry a stable `Idempotency-Key` derived from the form public ID and event ID. Both SMTP and webhook requests use the configured retry limit and backoff schedule and honor cancellation.

## Key principles

1. **Bounded batches**: Process at most the current batch limit in deterministic order
2. **Durable claims**: Claim with an expiring lease before network I/O
3. **Compare-and-set**: Never let a stale worker overwrite a reclaimed event
4. **Idempotency**: Preserve stable webhook identity across attempts
5. **State tracking**: Record status, count, compact error, attempt time, and next eligibility
6. **Retry transactions**: Use `dbtxn.WithRetry` for every state mutation
7. **Cancellation**: Propagate the runner context into database, HTTP, and SMTP work

## Verification

Run focused dispatcher tests and `make test-race` for claim, cancellation, or shutdown changes. Run `make test-stress` when webhook or email queue throughput, retries, restart recovery, or resource behavior may change; its SMTP capture is local-only and blocks recipients outside `.invalid`.
