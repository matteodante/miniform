package forms

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

// submissionFilesMutex lets uploads run together while serializing deletion across database and filesystem.
var submissionFilesMutex sync.RWMutex

// DeleteForm deletes a form and removes all upload directories owned by it.
func DeleteForm(logger *slog.Logger, db *gorm.DB, dataDir string, id uint) error {
	submissionFilesMutex.Lock()
	defer submissionFilesMutex.Unlock()

	var staged *stagedUploadDeletion
	deleteErr := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if staged != nil {
			if err := staged.finish(tx); err != nil {
				return err
			}
			staged = nil
		}
		var form Form
		if err := tx.Select("id").First(&form, id).Error; err != nil {
			return err
		}
		var files []*SubmissionFile
		submissionIDs := tx.Model(&Submission{}).Select("id").Where("form_id = ?", id)
		if err := tx.Where("submission_id IN (?)", submissionIDs).Find(&files).Error; err != nil {
			return fmt.Errorf("list form uploads for deletion: %w", err)
		}
		var err error
		staged, err = stageStoredFiles(dataDir, files)
		if err != nil {
			return fmt.Errorf("stage form uploads: %w", err)
		}
		return deleteFormRecords(tx, id)
	})
	cleanupErr := staged.finish(db)
	if deleteErr != nil {
		return errors.Join(fmt.Errorf("delete form: %w", deleteErr), cleanupErr)
	}
	if cleanupErr != nil {
		return fmt.Errorf("form deleted but quarantined upload cleanup failed: %w", cleanupErr)
	}
	return nil
}

// RotateToken replaces the public submission token for a form.
func RotateToken(logger *slog.Logger, db *gorm.DB, id uint) (*Form, error) {
	if _, err := GetByID(db, id); err != nil {
		return nil, err
	}
	token, err := generateToken(24)
	if err != nil {
		return nil, fmt.Errorf("generate form token: %w", err)
	}
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		return tx.Model(&Form{}).Where("id = ?", id).Update("token", token).Error
	}); err != nil {
		return nil, fmt.Errorf("rotate form token: %w", err)
	}
	return GetByID(db, id)
}

// SubmissionFilter defines the searchable inbox view shared by non-HTTP clients.
type SubmissionFilter struct {
	FormID  uint
	Range   string
	Query   string
	Page    int
	PerPage int
	IsSpam  *bool
}

// SubmissionPage contains one page of inbox results and pagination metadata.
type SubmissionPage struct {
	Items      []Submission
	Page       int
	PerPage    int
	TotalCount int64
	TotalPages int
}

type InboxSummary struct {
	Forms         []Form
	FormCount     int64
	EntriesLast24 int64
}

func GetInboxSummary(db *gorm.DB, now time.Time) (*InboxSummary, error) {
	summary := &InboxSummary{}
	if err := db.Select("id, name").Order("name").Find(&summary.Forms).Error; err != nil {
		return nil, fmt.Errorf("list inbox forms: %w", err)
	}
	if err := db.Model(&Form{}).Count(&summary.FormCount).Error; err != nil {
		return nil, fmt.Errorf("count inbox forms: %w", err)
	}
	if err := db.Model(&Submission{}).Where("created_at > ?", now.UTC().Add(-24*time.Hour)).Count(&summary.EntriesLast24).Error; err != nil {
		return nil, fmt.Errorf("count recent submissions: %w", err)
	}
	return summary, nil
}

// ListSubmissions returns submissions matching the same filters used by the inbox.
func ListSubmissions(db *gorm.DB, filter SubmissionFilter) (*SubmissionPage, error) {
	page := filter.Page
	if page < 1 {
		page = 1
	}
	perPage := filter.PerPage
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 200 {
		perPage = 200
	}

	query, err := filteredSubmissions(db, filter, time.Now().UTC())
	if err != nil {
		return nil, err
	}

	var totalCount int64
	if err := query.Count(&totalCount).Error; err != nil {
		return nil, fmt.Errorf("count submissions: %w", err)
	}

	var items []Submission
	if err := query.
		Order("created_at DESC, id DESC").
		Limit(perPage).
		Offset((page - 1) * perPage).
		Find(&items).Error; err != nil {
		return nil, fmt.Errorf("list submissions: %w", err)
	}

	totalPages := 0
	if totalCount > 0 {
		totalPages = (int(totalCount) + perPage - 1) / perPage
	}

	return &SubmissionPage{
		Items:      items,
		Page:       page,
		PerPage:    perPage,
		TotalCount: totalCount,
		TotalPages: totalPages,
	}, nil
}

func filteredSubmissions(db *gorm.DB, filter SubmissionFilter, now time.Time) (*gorm.DB, error) {
	query, err := submissionFilterQuery(db, filter, now)
	if err != nil {
		return nil, err
	}
	return query.Preload("Form"), nil
}

func submissionFilterQuery(db *gorm.DB, filter SubmissionFilter, now time.Time) (*gorm.DB, error) {
	query := db.Model(&Submission{})
	if filter.FormID > 0 {
		query = query.Where("form_id = ?", filter.FormID)
	}
	if search := strings.TrimSpace(filter.Query); search != "" {
		query = query.Where("data_json LIKE ?", "%"+search+"%")
	}
	if filter.IsSpam != nil {
		query = query.Where("is_spam = ?", *filter.IsSpam)
	}

	startTime, err := submissionRangeStart(filter.Range, now)
	if err != nil {
		return nil, err
	}
	if !startTime.IsZero() {
		query = query.Where("created_at >= ?", startTime)
	}
	return query, nil
}

// GetSubmissionByID returns a submission with its endpoint, events, and files.
func GetSubmissionByID(db *gorm.DB, id uint) (*Submission, error) {
	var submission Submission
	if err := db.
		Preload("Form").
		Preload("WebhookEvents").
		Preload("EmailEvents.EmailDelivery").
		Preload("Files").
		First(&submission, id).Error; err != nil {
		return nil, err
	}
	return &submission, nil
}

// GetSubmissionFile returns file metadata scoped to its submission.
func GetSubmissionFile(db *gorm.DB, submissionID, fileID uint) (*SubmissionFile, error) {
	var file SubmissionFile
	if err := db.Where("id = ? AND submission_id = ?", fileID, submissionID).First(&file).Error; err != nil {
		return nil, err
	}
	return &file, nil
}

// DeleteSubmission removes a submission and its uploads.
func DeleteSubmission(logger *slog.Logger, db *gorm.DB, dataDir string, id uint) error {
	submissionFilesMutex.Lock()
	defer submissionFilesMutex.Unlock()
	return deleteSubmission(logger, db, dataDir, id)
}

func deleteSubmission(logger *slog.Logger, db *gorm.DB, dataDir string, id uint) error {
	var staged *stagedUploadDeletion
	deleteErr := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if staged != nil {
			if err := staged.finish(tx); err != nil {
				return err
			}
			staged = nil
		}
		var submission Submission
		if err := tx.Preload("Files").First(&submission, id).Error; err != nil {
			return err
		}
		var err error
		staged, err = stageStoredFiles(dataDir, submission.Files)
		if err != nil {
			return fmt.Errorf("stage submission uploads: %w", err)
		}
		if err := tx.Where("submission_id = ?", id).Delete(&WebhookEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("submission_id = ?", id).Delete(&EmailEvent{}).Error; err != nil {
			return err
		}
		if err := tx.Where("submission_id = ?", id).Delete(&SubmissionFile{}).Error; err != nil {
			return err
		}
		result := tx.Delete(&Submission{}, id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
	cleanupErr := staged.finish(db)
	if deleteErr != nil {
		return errors.Join(fmt.Errorf("delete submission: %w", deleteErr), cleanupErr)
	}
	if cleanupErr != nil {
		return fmt.Errorf("submission deleted but quarantined upload cleanup failed: %w", cleanupErr)
	}
	return nil
}

// ListWebhookEvents returns webhook delivery events, optionally scoped to a form.
func ListWebhookEvents(db *gorm.DB, formID uint, status string, limit int) ([]WebhookEvent, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	query := db.Preload("Submission").Preload("Submission.Form")
	if formID > 0 {
		query = query.Where("submission_id IN (?)", db.Model(&Submission{}).Select("id").Where("form_id = ?", formID))
	}
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("status = ?", status)
	}

	var events []WebhookEvent
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// ListEmailEvents returns email delivery events, optionally scoped to a form.
func ListEmailEvents(db *gorm.DB, formID uint, status string, limit int) ([]EmailEvent, error) {
	if limit < 1 || limit > 500 {
		limit = 100
	}
	query := db.Preload("Submission").Preload("Submission.Form").Preload("EmailDelivery")
	if formID > 0 {
		query = query.Where("submission_id IN (?)", db.Model(&Submission{}).Select("id").Where("form_id = ?", formID))
	}
	if status = strings.TrimSpace(status); status != "" {
		query = query.Where("status = ?", status)
	}

	var events []EmailEvent
	if err := query.Order("created_at DESC, id DESC").Limit(limit).Find(&events).Error; err != nil {
		return nil, err
	}
	return events, nil
}

// RetryWebhookEvent schedules a webhook event for immediate redelivery.
func RetryWebhookEvent(logger *slog.Logger, db *gorm.DB, id uint) error {
	return retryDeliveryEvent(logger, db, &WebhookEvent{}, id)
}

// RetryEmailEvent schedules an email event for immediate redelivery.
func RetryEmailEvent(logger *slog.Logger, db *gorm.DB, id uint) error {
	return retryDeliveryEvent(logger, db, &EmailEvent{}, id)
}

func retryDeliveryEvent(logger *slog.Logger, db *gorm.DB, model any, id uint) error {
	now := time.Now().UTC()
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		result := tx.Model(model).
			Where("id = ? AND status = ?", id, WebhookStatusFailed).
			Updates(map[string]any{
				"status":           WebhookStatusPending,
				"attempt_count":    0,
				"last_attempt_err": "",
				"next_attempt_at":  &now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func submissionRangeStart(value string, now time.Time) (time.Time, error) {
	switch strings.TrimSpace(value) {
	case "", "all":
		return time.Time{}, nil
	case "7d":
		return now.AddDate(0, 0, -7), nil
	case "30d":
		return now.AddDate(0, 0, -30), nil
	case "90d":
		return now.AddDate(0, 0, -90), nil
	default:
		return time.Time{}, &ValidationError{Field: "range", Message: "Range must be one of all, 7d, 30d, or 90d"}
	}
}
