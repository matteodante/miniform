package forms

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

const (
	HoneypotField       = "__mf_hp"
	legacyHoneypotField = "__fl_hp"
)

func CreateSubmission(logger *slog.Logger, db *gorm.DB, form *Form, payload map[string]any, userAgent string) (*Submission, error) {
	return CreateSubmissionWithFiles(logger, db, form, payload, userAgent, "", nil)
}

func CreateSubmissionWithFiles(logger *slog.Logger, db *gorm.DB, form *Form, payload map[string]any, userAgent, dataDir string, files []*UploadedFile) (*Submission, error) {
	spam := consumeHoneypot(payload)
	if spam {
		logger.Info("honeypot triggered", slog.Uint64("form_id", uint64(form.ID)), slog.String("form_slug", form.Slug))
	}

	data, err := json.Marshal(payload)
	if err != nil {
		CloseFiles(files)
		return nil, errors.New("failed to encode submission payload")
	}
	submission := &Submission{
		FormID: form.ID, DataJSON: string(data), UserAgent: userAgent, IsSpam: spam,
	}
	storeFiles := !spam && len(files) > 0 && strings.TrimSpace(dataDir) != ""
	if storeFiles {
		submissionFilesMutex.RLock()
		defer submissionFilesMutex.RUnlock()
	}

	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if err := tx.Create(submission).Error; err != nil {
			return err
		}
		if spam || storeFiles {
			return nil
		}
		return createDeliveryEvents(tx, form, submission.ID)
	}); err != nil {
		CloseFiles(files)
		logger.Error("store submission", slog.Any("error", err), slog.Uint64("form_id", uint64(form.ID)))
		return nil, errors.New("failed to save submission")
	}

	if !storeFiles {
		CloseFiles(files)
		return submission, nil
	}

	records, err := SaveFiles(dataDir, form.ID, submission.ID, files)
	if err == nil {
		err = dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			if err := tx.Create(&records).Error; err != nil {
				return err
			}
			return createDeliveryEvents(tx, form, submission.ID)
		})
	}
	if err != nil {
		cleanupSubmission(logger, db, form.ID, submission.ID, dataDir)
		logger.Error("store submission files", slog.Any("error", err), slog.Uint64("submission_id", uint64(submission.ID)))
		return nil, errors.New("failed to save submission")
	}

	submission.Files = records
	return submission, nil
}

func createDeliveryEvents(tx *gorm.DB, form *Form, submissionID uint) error {
	now := time.Now().UTC()
	if form.WebhookDelivery != nil && form.WebhookDelivery.Enabled && form.WebhookDelivery.URL != "" {
		if err := tx.Create(NewWebhookEvent(submissionID, now)).Error; err != nil {
			return err
		}
	}
	if form.EmailDelivery != nil && form.EmailDelivery.Enabled && extractEmailRecipient(form.EmailDelivery) != "" {
		return tx.Create(NewEmailEvent(submissionID, now)).Error
	}
	return nil
}

func consumeHoneypot(payload map[string]any) bool {
	spam := false
	for _, field := range []string{HoneypotField, legacyHoneypotField} {
		value, found := payload[field]
		if !found {
			continue
		}
		delete(payload, field)
		if text, ok := value.(string); ok {
			spam = spam || strings.TrimSpace(text) != ""
		} else {
			spam = spam || value != nil
		}
	}
	return spam
}

func extractEmailRecipient(delivery *EmailDelivery) string {
	if delivery == nil || delivery.OverridesJSON == "" {
		return ""
	}
	var overrides struct {
		To string `json:"to"`
	}
	if json.Unmarshal([]byte(delivery.OverridesJSON), &overrides) != nil {
		return ""
	}
	return overrides.To
}

func cleanupSubmission(logger *slog.Logger, db *gorm.DB, formID, submissionID uint, dataDir string) {
	err := deleteSubmission(logger, db, dataDir, submissionID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		err = DeleteSubmissionFiles(dataDir, formID, submissionID)
	}
	if err != nil {
		logger.Error("clean up incomplete submission", slog.Any("error", err), slog.Uint64("submission_id", uint64(submissionID)))
	}
}
