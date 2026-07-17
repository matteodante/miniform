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

var ErrEmptySubmission = errors.New("submission payload empty")

func CreateSubmissionWithFiles(logger *slog.Logger, db *gorm.DB, form *Form, payload map[string]any, userAgent, dataDir string, files []*UploadedFile) (*Submission, error) {
	spam := consumeHoneypot(payload)
	if spam {
		logger.Info("honeypot triggered", slog.Uint64("form_id", uint64(form.ID)), slog.String("form_slug", form.Slug))
	}
	if !spam && len(payload) == 0 && len(files) == 0 {
		CloseFiles(files)
		return nil, ErrEmptySubmission
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
	var records []*SubmissionFile
	var staged *stagedUploadDeletion
	if storeFiles {
		records, staged, err = stageUnassignedFiles(dataDir, files)
		if err != nil {
			logger.Error("store submission files", slog.Any("error", err), slog.Uint64("form_id", uint64(form.ID)))
			return nil, errors.New("failed to save submission")
		}
	} else {
		CloseFiles(files)
	}

	writeErr := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		var formID uint
		if err := tx.Model(&Form{}).Select("id").Where("id = ?", form.ID).Scan(&formID).Error; err != nil {
			return err
		}
		if formID == 0 {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Create(submission).Error; err != nil {
			return err
		}
		if spam {
			return nil
		}
		for _, record := range records {
			record.SubmissionID = submission.ID
		}
		if len(records) > 0 {
			if err := tx.Create(&records).Error; err != nil {
				return err
			}
		}
		return createDeliveryEvents(tx, form, submission.ID)
	})
	cleanupErr := staged.finish(db)
	if writeErr != nil {
		if cleanupErr != nil {
			writeErr = errors.Join(writeErr, cleanupErr)
		}
		logger.Error("store submission", slog.Any("error", writeErr), slog.Uint64("form_id", uint64(form.ID)))
		return nil, errors.New("failed to save submission")
	}
	if cleanupErr != nil {
		logger.Error("finalize submission uploads; restart required for recovery",
			slog.Any("error", cleanupErr), slog.Uint64("submission_id", uint64(submission.ID)))
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
	if form.EmailDelivery != nil && form.EmailDelivery.Enabled && form.EmailDelivery.Recipient != "" {
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
