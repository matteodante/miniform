package cli

import (
	"strings"
	"time"

	"github.com/matteodante/miniform/internal/forms"
)

type webhookEventView struct {
	ID             uint       `json:"id"`
	SubmissionID   uint       `json:"submission_id"`
	FormID         uint       `json:"form_id,omitempty"`
	FormName       string     `json:"form_name,omitempty"`
	Status         string     `json:"status"`
	AttemptCount   int        `json:"attempt_count"`
	LastAttemptErr string     `json:"last_attempt_error,omitempty"`
	NextAttemptAt  *time.Time `json:"next_attempt_at,omitempty"`
	LastAttemptAt  *time.Time `json:"last_attempt_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type emailEventView struct {
	webhookEventView
	EmailDeliveryID   *uint  `json:"email_delivery_id,omitempty"`
	EmailDeliveryName string `json:"email_delivery_name,omitempty"`
}

func (r *Runner) runEvent(args []string) (any, error) {
	action, actionArgs, err := requireAction("event", args)
	if err != nil {
		return nil, err
	}
	switch action {
	case "list":
		return r.eventList(actionArgs)
	case "retry":
		return r.eventRetry(actionArgs)
	default:
		return nil, usageError("unknown event action: " + action)
	}
}

func (r *Runner) eventList(args []string) (any, error) {
	set := newFlagSet("event.list")
	eventType := set.String("type", "", "webhook or email")
	formID := set.Uint("form-id", 0, "filter by form id")
	status := set.String("status", "", "filter by delivery status")
	limit := set.Int("limit", 100, "maximum events, up to 500")
	if err := r.parseFlags(set, "event.list", args); err != nil {
		return nil, err
	}
	typeName := strings.ToLower(strings.TrimSpace(*eventType))
	if typeName != "webhook" && typeName != "email" {
		return nil, validationError("event type must be webhook or email")
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	if typeName == "webhook" {
		events, err := forms.ListWebhookEvents(r.DB, *formID, *status, *limit)
		if err != nil {
			return nil, err
		}
		views := make([]webhookEventView, 0, len(events))
		for i := range events {
			views = append(views, newWebhookEventView(&events[i]))
		}
		return views, nil
	}
	events, err := forms.ListEmailEvents(r.DB, *formID, *status, *limit)
	if err != nil {
		return nil, err
	}
	views := make([]emailEventView, 0, len(events))
	for i := range events {
		views = append(views, newEmailEventView(&events[i]))
	}
	return views, nil
}

func (r *Runner) eventRetry(args []string) (any, error) {
	set := newFlagSet("event.retry")
	eventType := set.String("type", "", "webhook or email")
	id := set.Uint("id", 0, "event id")
	yes := set.Bool("yes", false, "confirm redelivery")
	if err := r.parseFlags(set, "event.retry", args); err != nil {
		return nil, err
	}
	if err := requireUint(*id, "id"); err != nil {
		return nil, err
	}
	if !*yes {
		return nil, usageError("event retry requires --yes")
	}
	typeName := strings.ToLower(strings.TrimSpace(*eventType))
	if typeName != "webhook" && typeName != "email" {
		return nil, validationError("event type must be webhook or email")
	}
	if err := r.requireDatabase(); err != nil {
		return nil, err
	}
	if typeName == "webhook" {
		if err := forms.RetryWebhookEvent(r.Logger, r.DB, *id); err != nil {
			return nil, err
		}
	} else {
		if err := forms.RetryEmailEvent(r.Logger, r.DB, *id); err != nil {
			return nil, err
		}
	}
	return map[string]any{"id": *id, "type": typeName, "scheduled": true}, nil
}

func newWebhookEventView(event *forms.WebhookEvent) webhookEventView {
	view := webhookEventView{
		ID:             event.ID,
		SubmissionID:   event.SubmissionID,
		Status:         event.Status,
		AttemptCount:   event.AttemptCount,
		LastAttemptErr: event.LastAttemptErr,
		NextAttemptAt:  event.NextAttemptAt,
		LastAttemptAt:  event.LastAttemptAt,
		CreatedAt:      event.CreatedAt,
		UpdatedAt:      event.UpdatedAt,
	}
	if event.Submission != nil {
		view.FormID = event.Submission.FormID
		if event.Submission.Form != nil {
			view.FormName = event.Submission.Form.Name
		}
	}
	return view
}

func newEmailEventView(event *forms.EmailEvent) emailEventView {
	view := emailEventView{
		webhookEventView: webhookEventView{
			ID: event.ID, SubmissionID: event.SubmissionID, Status: event.Status,
			AttemptCount: event.AttemptCount, LastAttemptErr: event.LastAttemptErr,
			NextAttemptAt: event.NextAttemptAt, LastAttemptAt: event.LastAttemptAt,
			CreatedAt: event.CreatedAt, UpdatedAt: event.UpdatedAt,
		},
		EmailDeliveryID: event.EmailDeliveryID,
	}
	if event.EmailDelivery != nil {
		view.EmailDeliveryName = event.EmailDelivery.Name
	}
	if event.Submission != nil {
		view.FormID = event.Submission.FormID
		if event.Submission.Form != nil {
			view.FormName = event.Submission.Form.Name
		}
	}
	return view
}
