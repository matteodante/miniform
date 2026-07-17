package forms_test

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
			ID:                  form.ID,
			Name:                "Renamed",
			Slug:                "renamed-form",
			AllowedOrigins:      "*.example.com",
			UseSDK:              true,
			GeneratedHTML:       "<form><button>Send</button></form>",
			UpdateGeneratedHTML: true,
		})
		require.NoError(t, err)
		assert.Equal(t, "renamed-form", updated.Slug)
		assert.True(t, updated.UseSDK)
		assert.Contains(t, updated.GeneratedHTML, "<form>")

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
		assertNoFiles(t, dataDir)
	})

	t.Run("restores submission uploads when database deletion fails", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Restore", Slug: "restore", AllowedOrigins: "*"})
		require.NoError(t, err)
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test")
		require.NoError(t, err)
		files, err := forms.SaveFiles(dataDir, form.ID, submission.ID, []*forms.UploadedFile{{
			FieldName: "attachment",
			Filename:  "brief.txt",
			Data:      strings.NewReader("brief"),
		}})
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			if err := tx.Create(files[0]).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE TRIGGER fail_submission_delete
				BEFORE DELETE ON submissions
				BEGIN SELECT RAISE(ABORT, 'forced submission delete failure'); END`).Error
		}))

		err = forms.DeleteSubmission(logger, db, dataDir, submission.ID)

		assert.ErrorContains(t, err, "forced submission delete failure")
		_, err = forms.GetSubmissionByID(db, submission.ID)
		assert.NoError(t, err)
		preserved, err := forms.GetSubmissionFile(db, submission.ID, files[0].ID)
		require.NoError(t, err)
		source, err := forms.OpenSubmissionFile(dataDir, preserved)
		require.NoError(t, err)
		defer func() { _ = source.Close() }()
		content, err := io.ReadAll(source)
		require.NoError(t, err)
		assert.Equal(t, "brief", string(content))
	})

	t.Run("waits for an active upload before deleting its form or submission", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Uploading", Slug: "uploading", AllowedOrigins: "*"})
		require.NoError(t, err)
		uploadStarted := make(chan struct{})
		releaseUpload := make(chan struct{})
		uploadResult := make(chan submissionResult, 1)
		go func() {
			submission, err := forms.CreateSubmissionWithFiles(
				logger,
				db,
				form,
				map[string]any{"ok": true},
				"test",
				dataDir,
				[]*forms.UploadedFile{{
					FieldName: "attachment",
					Filename:  "brief.txt",
					Data: &gatedReader{
						started: uploadStarted,
						release: releaseUpload,
						reader:  strings.NewReader("brief"),
					},
				}},
			)
			uploadResult <- submissionResult{submission: submission, err: err}
		}()
		select {
		case <-uploadStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("upload did not start")
		}

		var submission forms.Submission
		require.NoError(t, db.Where("form_id = ?", form.ID).First(&submission).Error)
		formDeleteStarted := make(chan struct{})
		formDeleteResult := make(chan error, 1)
		go func() {
			close(formDeleteStarted)
			formDeleteResult <- forms.DeleteForm(logger, db, dataDir, form.ID)
		}()
		submissionDeleteStarted := make(chan struct{})
		submissionDeleteResult := make(chan error, 1)
		go func() {
			close(submissionDeleteStarted)
			submissionDeleteResult <- forms.DeleteSubmission(logger, db, dataDir, submission.ID)
		}()
		<-formDeleteStarted
		<-submissionDeleteStarted

		select {
		case err := <-formDeleteResult:
			t.Fatalf("form deletion completed during upload: %v", err)
		case err := <-submissionDeleteResult:
			t.Fatalf("submission deletion completed during upload: %v", err)
		case <-time.After(100 * time.Millisecond):
		}

		withoutFilesResult := make(chan submissionResult, 1)
		go func() {
			created, err := forms.CreateSubmission(logger, db, form, map[string]any{"without": "files"}, "test")
			withoutFilesResult <- submissionResult{submission: created, err: err}
		}()
		withoutFiles := receiveWithin(t, withoutFilesResult)
		require.NoError(t, withoutFiles.err)
		require.NotNil(t, withoutFiles.submission)

		close(releaseUpload)
		created := receiveWithin(t, uploadResult)
		require.NoError(t, created.err)
		require.NotNil(t, created.submission)
		assert.NoError(t, receiveWithin(t, formDeleteResult))
		submissionDeleteErr := receiveWithin(t, submissionDeleteResult)
		assert.True(t, submissionDeleteErr == nil || errors.Is(submissionDeleteErr, gorm.ErrRecordNotFound), submissionDeleteErr)
		_, err = forms.GetByID(db, form.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		_, err = forms.GetSubmissionByID(db, submission.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		assertNoFiles(t, dataDir)
	})

	t.Run("removes uploads when records disappear outside deletion coordination", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Bypass", Slug: "bypass", AllowedOrigins: "*"})
		require.NoError(t, err)
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			return tx.Exec(`CREATE TRIGGER fail_submission_file_insert
				BEFORE INSERT ON submission_files
				BEGIN SELECT RAISE(ABORT, 'forced submission file insert failure'); END`).Error
		}))
		uploadStarted := make(chan struct{})
		releaseUpload := make(chan struct{})
		uploadResult := make(chan submissionResult, 1)
		go func() {
			submission, err := forms.CreateSubmissionWithFiles(
				logger,
				db,
				form,
				map[string]any{"ok": true},
				"test",
				dataDir,
				[]*forms.UploadedFile{{
					FieldName: "attachment",
					Filename:  "brief.txt",
					Data: &gatedReader{
						started: uploadStarted,
						release: releaseUpload,
						reader:  strings.NewReader("brief"),
					},
				}},
			)
			uploadResult <- submissionResult{submission: submission, err: err}
		}()
		select {
		case <-uploadStarted:
		case <-time.After(5 * time.Second):
			t.Fatal("upload did not start")
		}

		var submission forms.Submission
		require.NoError(t, db.Where("form_id = ?", form.ID).First(&submission).Error)
		require.NoError(t, forms.Delete(logger, db, form.ID))
		close(releaseUpload)

		created := receiveWithin(t, uploadResult)
		assert.Error(t, created.err)
		assert.Nil(t, created.submission)
		_, err = forms.GetSubmissionByID(db, submission.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		assertNoFiles(t, dataDir)
	})

	t.Run("serializes concurrent submission deletions", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Concurrent", Slug: "concurrent", AllowedOrigins: "*"})
		require.NoError(t, err)
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test")
		require.NoError(t, err)
		filePath := filepath.Join(dataDir, "uploads", strconv.FormatUint(uint64(form.ID), 10), strconv.FormatUint(uint64(submission.ID), 10), "brief.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o700))
		require.NoError(t, os.WriteFile(filePath, []byte("brief"), 0o600))

		results := runConcurrently(t,
			func() error { return forms.DeleteSubmission(logger, db, dataDir, submission.ID) },
			func() error { return forms.DeleteSubmission(logger, db, dataDir, submission.ID) },
		)

		successes := 0
		notFound := 0
		for _, result := range results {
			switch {
			case result == nil:
				successes++
			case errors.Is(result, gorm.ErrRecordNotFound):
				notFound++
			default:
				assert.NoError(t, result)
			}
		}
		assert.Equal(t, 1, successes)
		assert.Equal(t, 1, notFound)
		_, err = forms.GetSubmissionByID(db, submission.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		assertNoFiles(t, dataDir)
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
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Disposable", Slug: "disposable", AllowedOrigins: "*"})
		require.NoError(t, err)
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test")
		require.NoError(t, err)
		filePath := filepath.Join(dataDir, "uploads", strconv.FormatUint(uint64(form.ID), 10), strconv.FormatUint(uint64(submission.ID), 10), "brief.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o700))
		require.NoError(t, os.WriteFile(filePath, []byte("brief"), 0o600))
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			return tx.Create(&forms.WebhookEvent{SubmissionID: submission.ID, Status: forms.WebhookStatusFailed}).Error
		}))

		require.NoError(t, forms.DeleteForm(logger, db, dataDir, form.ID))
		assertRecordCount(t, db, &forms.Form{}, 0)
		assertRecordCount(t, db, &forms.Submission{}, 0)
		assertRecordCount(t, db, &forms.WebhookDelivery{}, 0)
		assertRecordCount(t, db, &forms.EmailDelivery{}, 0)
		assertRecordCount(t, db, &forms.WebhookEvent{}, 0)
		assertNoFiles(t, dataDir)
	})

	t.Run("restores form uploads when database deletion fails", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Restore form", Slug: "restore-form", AllowedOrigins: "*"})
		require.NoError(t, err)
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test")
		require.NoError(t, err)
		files, err := forms.SaveFiles(dataDir, form.ID, submission.ID, []*forms.UploadedFile{{
			FieldName: "attachment",
			Filename:  "brief.txt",
			Data:      strings.NewReader("brief"),
		}})
		require.NoError(t, err)
		require.Len(t, files, 1)
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			if err := tx.Create(files[0]).Error; err != nil {
				return err
			}
			return tx.Exec(`CREATE TRIGGER fail_form_delete
				BEFORE DELETE ON forms
				BEGIN SELECT RAISE(ABORT, 'forced form delete failure'); END`).Error
		}))

		err = forms.DeleteForm(logger, db, dataDir, form.ID)

		assert.ErrorContains(t, err, "forced form delete failure")
		_, err = forms.GetByID(db, form.ID)
		assert.NoError(t, err)
		_, err = forms.GetSubmissionByID(db, submission.ID)
		assert.NoError(t, err)
		preserved, err := forms.GetSubmissionFile(db, submission.ID, files[0].ID)
		require.NoError(t, err)
		source, err := forms.OpenSubmissionFile(dataDir, preserved)
		require.NoError(t, err)
		defer func() { _ = source.Close() }()
		content, err := io.ReadAll(source)
		require.NoError(t, err)
		assert.Equal(t, "brief", string(content))
	})

	t.Run("serializes a failed form deletion with a submission deletion", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Partial restore", Slug: "partial-restore", AllowedOrigins: "*"})
		require.NoError(t, err)
		deletedSubmission, err := forms.CreateSubmission(logger, db, form, map[string]any{"kind": "deleted"}, "test")
		require.NoError(t, err)
		survivingSubmission, err := forms.CreateSubmission(logger, db, form, map[string]any{"kind": "surviving"}, "test")
		require.NoError(t, err)

		formDirectory := strconv.FormatUint(uint64(form.ID), 10)
		deletedFile := filepath.Join(dataDir, "uploads", formDirectory, strconv.FormatUint(uint64(deletedSubmission.ID), 10), "deleted.txt")
		survivingFile := filepath.Join(dataDir, "uploads", formDirectory, strconv.FormatUint(uint64(survivingSubmission.ID), 10), "surviving.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(deletedFile), 0o700))
		require.NoError(t, os.MkdirAll(filepath.Dir(survivingFile), 0o700))
		require.NoError(t, os.WriteFile(deletedFile, []byte("deleted"), 0o600))
		require.NoError(t, os.WriteFile(survivingFile, []byte("surviving"), 0o600))
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			return tx.Exec(`CREATE TRIGGER fail_form_delete
				BEFORE DELETE ON forms
				BEGIN SELECT RAISE(ABORT, 'forced form delete failure'); END`).Error
		}))

		results := runConcurrently(t,
			func() error { return forms.DeleteForm(logger, db, dataDir, form.ID) },
			func() error { return forms.DeleteSubmission(logger, db, dataDir, deletedSubmission.ID) },
		)

		assert.ErrorContains(t, results[0], "forced form delete failure")
		assert.NoError(t, results[1])
		_, err = forms.GetByID(db, form.ID)
		assert.NoError(t, err)
		_, err = forms.GetSubmissionByID(db, deletedSubmission.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		_, err = forms.GetSubmissionByID(db, survivingSubmission.ID)
		assert.NoError(t, err)
		assert.NoFileExists(t, deletedFile)
		content, err := os.ReadFile(survivingFile)
		require.NoError(t, err)
		assert.Equal(t, "surviving", string(content))
		var remainingFiles []string
		require.NoError(t, filepath.WalkDir(dataDir, func(path string, entry os.DirEntry, err error) error {
			if err == nil && !entry.IsDir() {
				remainingFiles = append(remainingFiles, path)
			}
			return err
		}))
		assert.Equal(t, []string{survivingFile}, remainingFiles)
	})
}

func runConcurrently(t *testing.T, operations ...func() error) []error {
	t.Helper()
	type result struct {
		index int
		err   error
	}
	start := make(chan struct{})
	resultChannel := make(chan result, len(operations))
	for index, operation := range operations {
		go func() {
			<-start
			resultChannel <- result{index: index, err: operation()}
		}()
	}
	close(start)

	results := make([]error, len(operations))
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for range operations {
		select {
		case result := <-resultChannel:
			results[result.index] = result.err
		case <-timer.C:
			t.Fatal("concurrent deletions did not finish")
		}
	}
	return results
}

func receiveWithin[T any](t *testing.T, channel <-chan T) T {
	t.Helper()
	select {
	case result := <-channel:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
		var zero T
		return zero
	}
}

func assertRecordCount(t *testing.T, db *gorm.DB, model any, expected int64) {
	t.Helper()
	var count int64
	require.NoError(t, db.Model(model).Count(&count).Error)
	assert.Equal(t, expected, count)
}

func assertNoFiles(t *testing.T, directory string) {
	t.Helper()
	var files []string
	require.NoError(t, filepath.WalkDir(directory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			files = append(files, path)
		}
		return nil
	}))
	assert.Empty(t, files)
}
