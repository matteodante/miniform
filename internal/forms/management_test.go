package forms_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestManagement(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("updates advanced form fields and rotates its token", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name:           "Original",
			Slug:           "original",
			AllowedOrigins: "example.com",
		})
		require.NoError(t, err)
		originalToken := form.Token

		updated, err := forms.Update(logger, db, forms.UpdateParams{
			ID:                     form.ID,
			Name:                   "Renamed",
			Slug:                   "renamed-form",
			AllowedOrigins:         "*.example.com",
			UseSDK:                 true,
			GeneratedHTML:          "<form><button>Send</button></form>",
			UpdateGeneratedHTML:    true,
			CaptchaOverridesJSON:   `{"theme":"dark"}`,
			UpdateCaptchaOverrides: true,
		})
		require.NoError(t, err)
		assert.Equal(t, "renamed-form", updated.Slug)
		assert.True(t, updated.UseSDK)
		assert.Contains(t, updated.GeneratedHTML, "<form>")
		assert.JSONEq(t, `{"theme":"dark"}`, updated.CaptchaOverridesJSON)

		rotated, err := forms.RotateToken(logger, db, form.ID)
		require.NoError(t, err)
		assert.NotEqual(t, originalToken, rotated.Token)
	})

	t.Run("deletes a submission with events metadata and uploaded files", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Inbox", Slug: "inbox", AllowedOrigins: "*"})
		require.NoError(t, err)
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"email": "user@example.com"}, "test")
		require.NoError(t, err)

		formDirectory := strconv.FormatUint(uint64(form.ID), 10)
		submissionDirectory := strconv.FormatUint(uint64(submission.ID), 10)
		filePath := filepath.Join("uploads", formDirectory, submissionDirectory, "brief.txt")
		require.NoError(t, os.MkdirAll(filepath.Join(dataDir, filepath.Dir(filePath)), 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(dataDir, filePath), []byte("brief"), 0o600))
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			if err := tx.Create(&forms.WebhookEvent{SubmissionID: submission.ID, Status: forms.WebhookStatusFailed}).Error; err != nil {
				return err
			}
			if err := tx.Create(&forms.EmailEvent{SubmissionID: submission.ID, Status: forms.WebhookStatusFailed}).Error; err != nil {
				return err
			}
			return tx.Create(&forms.SubmissionFile{
				SubmissionID: submission.ID,
				FieldName:    "attachment",
				Filename:     "brief.txt",
				Size:         5,
				StoragePath:  filePath,
				CreatedAt:    time.Now().UTC(),
			}).Error
		}))

		require.NoError(t, forms.DeleteSubmission(logger, db, dataDir, submission.ID))
		assertRecordCount(t, db, &forms.Submission{}, 0)
		assertRecordCount(t, db, &forms.WebhookEvent{}, 0)
		assertRecordCount(t, db, &forms.EmailEvent{}, 0)
		assertRecordCount(t, db, &forms.SubmissionFile{}, 0)
		_, err = os.Stat(filepath.Join(dataDir, "uploads", formDirectory, submissionDirectory))
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("keeps a submission retryable when deleting its files fails", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		outsideDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Retry", Slug: "retry", AllowedOrigins: "*"})
		require.NoError(t, err)
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test")
		require.NoError(t, err)

		formDirectory := strconv.FormatUint(uint64(form.ID), 10)
		require.NoError(t, os.MkdirAll(filepath.Join(dataDir, "uploads"), 0o700))
		linkPath := filepath.Join(dataDir, "uploads", formDirectory)
		require.NoError(t, os.Symlink(outsideDir, linkPath))
		outsideFile := filepath.Join(outsideDir, "keep.txt")
		require.NoError(t, os.WriteFile(outsideFile, []byte("keep"), 0o600))

		err = forms.DeleteSubmission(logger, db, dataDir, submission.ID)

		assert.Error(t, err)
		_, err = forms.GetSubmissionByID(db, submission.ID)
		assert.NoError(t, err)
		assert.FileExists(t, outsideFile)

		require.NoError(t, os.Remove(linkPath))
		require.NoError(t, forms.DeleteSubmission(logger, db, dataDir, submission.ID))
		_, err = forms.GetSubmissionByID(db, submission.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})

	t.Run("deletes a form and all owned records", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Disposable", Slug: "disposable", AllowedOrigins: "*"})
		require.NoError(t, err)
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test")
		require.NoError(t, err)
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			return tx.Create(&forms.WebhookEvent{SubmissionID: submission.ID, Status: forms.WebhookStatusFailed}).Error
		}))

		require.NoError(t, forms.DeleteForm(logger, db, t.TempDir(), form.ID))
		assertRecordCount(t, db, &forms.Form{}, 0)
		assertRecordCount(t, db, &forms.Submission{}, 0)
		assertRecordCount(t, db, &forms.WebhookDelivery{}, 0)
		assertRecordCount(t, db, &forms.EmailDelivery{}, 0)
		assertRecordCount(t, db, &forms.WebhookEvent{}, 0)
	})
}

func assertRecordCount(t *testing.T, db *gorm.DB, model any, expected int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Count(&count).Error)
	assert.Equal(t, expected, count)
}
