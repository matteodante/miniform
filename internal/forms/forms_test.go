package forms_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/iotest"
	"time"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	"github.com/matteodante/miniform/internal/pkg/testsupport"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestForms(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("creates, loads, and rejects duplicate forms", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		profile, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Primary mailer", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)

		created, err := forms.Create(logger, db, forms.CreateParams{
			Name: "  Contact us  ", Slug: "Contact Form", AllowedOrigins: " example.com ",
			MailerProfileID: &profile.ID, EmailRecipient: "Owner <owner@example.com>\nteam@example.com, OWNER@example.com",
			EmailFormat: forms.EmailFormatHTML, EmailEnabled: true,
			WebhookEnabled: true, WebhookURL: "https://hooks.example.com/forms",
			WebhookHeadersJSON: `{"X-Source":"miniform"}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "Contact us", created.Name)
		assert.Equal(t, "contact-form", created.Slug)
		assert.NotEmpty(t, created.PublicID)
		assert.NotEmpty(t, created.Token)

		loaded, err := forms.GetBySlug(db, created.Slug)
		require.NoError(t, err)
		require.NotNil(t, loaded.EmailDelivery)
		require.NotNil(t, loaded.WebhookDelivery)
		assert.True(t, loaded.EmailDelivery.Enabled)
		assert.Equal(t, `"Owner" <owner@example.com>, team@example.com`, loaded.EmailDelivery.Recipient)
		assert.Equal(t, forms.EmailFormatHTML, loaded.EmailDelivery.Format)
		assert.True(t, loaded.WebhookDelivery.Enabled)

		_, err = forms.Create(logger, db, forms.CreateParams{
			Name: "Duplicate", Slug: "contact-form", AllowedOrigins: "example.com",
		})
		var validation *forms.ValidationError
		require.ErrorAs(t, err, &validation)
		assert.Equal(t, "slug", validation.Field)

		_, err = forms.GetBySlug(db, "missing")
		assert.Error(t, err)
	})

	t.Run("normalizes email formats and recipient lists", func(t *testing.T) {
		format, err := forms.NormalizeEmailFormat(" HTML ")
		require.NoError(t, err)
		assert.Equal(t, forms.EmailFormatHTML, format)
		format, err = forms.NormalizeEmailFormat("")
		require.NoError(t, err)
		assert.Equal(t, forms.EmailFormatText, format)
		_, err = forms.NormalizeEmailFormat("markdown")
		assert.Error(t, err)

		recipients, err := forms.ParseEmailRecipients("Alice <alice@example.com>\r\nbob@example.com, ALICE@example.com")
		require.NoError(t, err)
		require.Len(t, recipients, 2)
		assert.Equal(t, "alice@example.com", recipients[0].Address)
		assert.Equal(t, "bob@example.com", recipients[1].Address)
		_, err = forms.ParseEmailRecipients("not-an-address")
		assert.Error(t, err)
		_, err = forms.ParseEmailRecipients("Undisclosed:;")
		assert.Error(t, err)
	})

	t.Run("rejects invalid webhook header names and values on create and update", func(t *testing.T) {
		tests := []struct {
			name    string
			headers string
		}{
			{name: "invalid name", headers: `{"Bad Header":"value"}`},
			{name: "invalid value", headers: `{"X-Valid":"line\r\nbreak"}`},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db := testsupport.SetupTestDB(t)

				_, err := forms.Create(logger, db, forms.CreateParams{
					Name: "Invalid", Slug: "invalid", AllowedOrigins: "*",
					WebhookEnabled: true, WebhookURL: "https://example.com/hook", WebhookHeadersJSON: tt.headers,
				})
				var validation *forms.ValidationError
				require.ErrorAs(t, err, &validation)
				assert.Equal(t, "webhook_headers", validation.Field)

				created, err := forms.Create(logger, db, forms.CreateParams{
					Name: "Before", Slug: "invalid", AllowedOrigins: "*",
					WebhookEnabled: true, WebhookURL: "https://example.com/hook", WebhookHeadersJSON: `{"X-Valid":"before"}`,
				})
				require.NoError(t, err)

				_, err = forms.Update(logger, db, forms.UpdateParams{
					ID: created.ID, Name: "After", AllowedOrigins: "*",
					WebhookEnabled: true, WebhookURL: "https://example.com/hook", WebhookHeadersJSON: tt.headers,
				})
				require.ErrorAs(t, err, &validation)
				assert.Equal(t, "webhook_headers", validation.Field)

				preserved, err := forms.GetByID(db, created.ID)
				require.NoError(t, err)
				assert.Equal(t, "Before", preserved.Name)
				require.NotNil(t, preserved.WebhookDelivery)
				assert.JSONEq(t, `{"X-Valid":"before"}`, preserved.WebhookDelivery.HeadersJSON)
			})
		}
	})

	t.Run("rejects references to missing integration profiles", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		missingProfileID := uint(999)

		_, err := forms.Create(logger, db, forms.CreateParams{
			Name: "Invalid", Slug: "invalid", AllowedOrigins: "*", CaptchaProfileID: &missingProfileID,
		})
		var validation *forms.ValidationError
		require.ErrorAs(t, err, &validation)
		assert.Equal(t, "captcha_profile_id", validation.Field)
		var formCount int64
		require.NoError(t, db.Model(&forms.Form{}).Count(&formCount).Error)
		assert.Zero(t, formCount)

		created, err := forms.Create(logger, db, forms.CreateParams{Name: "Valid", Slug: "valid", AllowedOrigins: "*"})
		require.NoError(t, err)
		_, err = forms.Update(logger, db, forms.UpdateParams{
			ID: created.ID, Name: "Changed", AllowedOrigins: "*", MailerProfileID: &missingProfileID,
		})
		require.ErrorAs(t, err, &validation)
		assert.Equal(t, "mailer_profile_id", validation.Field)
		preserved, err := forms.GetByID(db, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "Valid", preserved.Name)
	})

	t.Run("normalizes valid origins and rejects dead origin configurations", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created, err := forms.Create(logger, db, forms.CreateParams{
			Name: "Origins", Slug: "origins", AllowedOrigins: " HTTPS://Example.COM/path, *.Example.org, example.com ",
		})
		require.NoError(t, err)
		assert.Equal(t, "example.com, *.example.org", created.AllowedOrigins)

		for _, value := range []string{",", "example.com,", "bad host", "ftp://example.com", "*example.com", "https://user@example.com"} {
			_, err := forms.Create(logger, db, forms.CreateParams{Name: "Invalid", Slug: "invalid-" + strings.ReplaceAll(value, ".", "-"), AllowedOrigins: value})
			var validation *forms.ValidationError
			require.ErrorAs(t, err, &validation, value)
			assert.Equal(t, "allowed_origins", validation.Field, value)
		}
	})

	t.Run("rejects generated HTML without a usable form", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		_, err := forms.Create(logger, db, forms.CreateParams{
			Name: "Broken", Slug: "broken", AllowedOrigins: "*", GeneratedHTML: "<section>No form</section>",
		})
		var validation *forms.ValidationError
		require.ErrorAs(t, err, &validation)
		assert.Equal(t, "generated_html", validation.Field)

		created, err := forms.Create(logger, db, forms.CreateParams{Name: "Valid", Slug: "valid", AllowedOrigins: "*"})
		require.NoError(t, err)
		_, err = forms.Update(logger, db, forms.UpdateParams{
			ID: created.ID, Name: "Changed", AllowedOrigins: "*", UpdateGeneratedHTML: true, GeneratedHTML: "<p>Still no form</p>",
		})
		require.ErrorAs(t, err, &validation)
		assert.Equal(t, "generated_html", validation.Field)
		preserved, err := forms.GetByID(db, created.ID)
		require.NoError(t, err)
		assert.Equal(t, "Valid", preserved.Name)
	})

	t.Run("stores a generated template during form creation", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)

		created, err := forms.Create(logger, db, forms.CreateParams{
			Name: "Contact", Slug: "contact", AllowedOrigins: "example.com", TemplateID: "contact",
		})

		require.NoError(t, err)
		assert.Contains(t, created.GeneratedHTML, "/forms/contact/submit?token="+created.Token)
		loaded, err := forms.GetByID(db, created.ID)
		require.NoError(t, err)
		assert.Equal(t, created.GeneratedHTML, loaded.GeneratedHTML)
	})

	t.Run("stores nested submissions and schedules enabled deliveries", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		form := forms.Form{Name: "Orders", Slug: "orders"}
		require.NoError(t, db.Create(&form).Error)
		require.NoError(t, db.Create(&forms.WebhookDelivery{
			FormID: form.ID, Enabled: true, URL: "https://example.com/hook",
		}).Error)
		require.NoError(t, db.Create(&forms.EmailDelivery{
			FormID: form.ID, Enabled: true, Recipient: "ops@example.com",
		}).Error)
		require.NoError(t, db.Preload("WebhookDelivery").Preload("EmailDelivery").First(&form, form.ID).Error)

		payload := map[string]any{
			"customer": map[string]any{"name": "Ada", "city": "Rome"},
			"items":    []string{"notebook", "pen"},
		}
		submission, err := forms.CreateSubmissionWithFiles(logger, db, &form, payload, "Browser/1.0", "", nil)
		require.NoError(t, err)
		assert.False(t, submission.IsSpam)
		assert.Equal(t, "Browser/1.0", submission.UserAgent)
		assert.Equal(t, time.UTC, submission.CreatedAt.Location())

		var decoded map[string]any
		require.NoError(t, json.Unmarshal([]byte(submission.DataJSON), &decoded))
		assert.Equal(t, "Rome", decoded["customer"].(map[string]any)["city"])
		assert.Len(t, decoded["items"], 2)

		var webhookCount, emailCount int64
		require.NoError(t, db.Model(&forms.WebhookEvent{}).Where("submission_id = ?", submission.ID).Count(&webhookCount).Error)
		require.NoError(t, db.Model(&forms.EmailEvent{}).Where("submission_id = ?", submission.ID).Count(&emailCount).Error)
		assert.Equal(t, int64(1), webhookCount)
		assert.Equal(t, int64(1), emailCount)

		var storedTimestamp string
		require.NoError(t, db.Raw("SELECT CAST(created_at AS TEXT) FROM submissions WHERE id = ?", submission.ID).Scan(&storedTimestamp).Error)
		assert.True(t, strings.HasSuffix(storedTimestamp, "+00:00"), storedTimestamp)
	})

	t.Run("rejects an empty submission after scrubbing an empty honeypot", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		form := forms.Form{Name: "Contact", Slug: "contact"}
		require.NoError(t, db.Create(&form).Error)

		for _, payload := range []map[string]any{
			{},
			{forms.HoneypotField: ""},
			{"__fl_hp": ""},
		} {
			submission, err := forms.CreateSubmissionWithFiles(logger, db, &form, payload, "Browser", "", nil)
			assert.ErrorIs(t, err, forms.ErrEmptySubmission)
			assert.Nil(t, submission)
		}

		var count int64
		require.NoError(t, db.Model(&forms.Submission{}).Count(&count).Error)
		assert.Zero(t, count)
	})

	t.Run("schedules deliveries only after uploads are stored", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		root := t.TempDir()
		form := forms.Form{
			Name: "Uploads", Slug: "uploads",
			WebhookDelivery: &forms.WebhookDelivery{Enabled: true, URL: "https://example.com/hook"},
		}
		require.NoError(t, db.Create(&form).Error)

		started := make(chan struct{})
		release := make(chan struct{})
		result := make(chan submissionResult, 1)
		go func() {
			submission, err := forms.CreateSubmissionWithFiles(
				logger,
				db,
				&form,
				map[string]any{"message": "hello"},
				"Browser",
				root,
				[]*forms.UploadedFile{{
					FieldName: "attachment",
					Filename:  "brief.txt",
					Data: &gatedReader{
						started: started,
						release: release,
						reader:  bytes.NewBufferString("brief"),
					},
				}},
			)
			result <- submissionResult{submission: submission, err: err}
		}()

		select {
		case <-started:
		case early := <-result:
			require.NoError(t, early.err)
			t.Fatal("upload completed before the reader was released")
		case <-time.After(2 * time.Second):
			t.Fatal("upload did not reach the reader")
		}

		var submissionCount, fileCount, eventCount int64
		require.NoError(t, db.Model(&forms.Submission{}).Count(&submissionCount).Error)
		require.NoError(t, db.Model(&forms.SubmissionFile{}).Count(&fileCount).Error)
		require.NoError(t, db.Model(&forms.WebhookEvent{}).Count(&eventCount).Error)
		assert.Zero(t, submissionCount)
		assert.Zero(t, fileCount)
		assert.Zero(t, eventCount)

		close(release)
		created := <-result
		require.NoError(t, created.err)
		require.NotNil(t, created.submission)
		require.NoError(t, db.Model(&forms.SubmissionFile{}).Count(&fileCount).Error)
		require.NoError(t, db.Model(&forms.WebhookEvent{}).Count(&eventCount).Error)
		require.NoError(t, db.Model(&forms.Submission{}).Count(&submissionCount).Error)
		assert.Equal(t, int64(1), submissionCount)
		assert.Equal(t, int64(1), fileCount)
		assert.Equal(t, int64(1), eventCount)
	})

	t.Run("rolls back a submission when storing an upload fails", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		root := t.TempDir()
		form := forms.Form{
			Name: "Broken upload", Slug: "broken-upload",
			WebhookDelivery: &forms.WebhookDelivery{Enabled: true, URL: "https://example.com/hook"},
		}
		require.NoError(t, db.Create(&form).Error)

		submission, err := forms.CreateSubmissionWithFiles(
			logger,
			db,
			&form,
			map[string]any{"message": "hello"},
			"Browser",
			root,
			[]*forms.UploadedFile{{Filename: "broken.txt", Data: iotest.ErrReader(errors.New("read failed"))}},
		)

		assert.Error(t, err)
		assert.Nil(t, submission)
		for name, model := range map[string]any{
			"submissions": &forms.Submission{},
			"files":       &forms.SubmissionFile{},
			"events":      &forms.WebhookEvent{},
		} {
			var count int64
			require.NoError(t, db.Model(model).Count(&count).Error, name)
			assert.Zero(t, count, name)
		}
		entries, err := os.ReadDir(filepath.Join(root, "uploads", "1"))
		if !os.IsNotExist(err) {
			require.NoError(t, err)
			assert.Empty(t, entries)
		}
	})

	t.Run("scrubs the honeypot and skips files and deliveries", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		root := t.TempDir()
		form := forms.Form{
			Name: "Protected", Slug: "protected",
			WebhookDelivery: &forms.WebhookDelivery{Enabled: true, URL: "https://example.com/hook"},
			EmailDelivery:   &forms.EmailDelivery{Enabled: true, Recipient: "owner@example.com"},
		}
		require.NoError(t, db.Create(&form).Error)

		payload := map[string]any{forms.HoneypotField: "", "__fl_hp": "filled"}
		submission, err := forms.CreateSubmissionWithFiles(logger, db, &form, payload, "Bot", root, []*forms.UploadedFile{{
			FieldName: "attachment", Filename: "payload.pdf", Data: bytes.NewBufferString("junk"),
		}})
		require.NoError(t, err)
		assert.True(t, submission.IsSpam)
		assert.Empty(t, submission.Files)
		assert.NotContains(t, submission.DataJSON, forms.HoneypotField)
		assert.NotContains(t, submission.DataJSON, "__fl_hp")

		var fileCount, webhookCount, emailCount int64
		require.NoError(t, db.Model(&forms.SubmissionFile{}).Count(&fileCount).Error)
		require.NoError(t, db.Model(&forms.WebhookEvent{}).Count(&webhookCount).Error)
		require.NoError(t, db.Model(&forms.EmailEvent{}).Count(&emailCount).Error)
		assert.Zero(t, fileCount)
		assert.Zero(t, webhookCount)
		assert.Zero(t, emailCount)
		entries, err := os.ReadDir(root)
		require.NoError(t, err)
		assert.Empty(t, entries)
	})

	t.Run("produces stable readable slugs", func(t *testing.T) {
		for input, want := range map[string]string{
			"Hello World":         "hello-world",
			" Form_Name 123! ":    "form-name-123",
			"Pre-Existing-Dashes": "pre-existing-dashes",
		} {
			slug, err := forms.Slugify(input)
			require.NoError(t, err)
			assert.Equal(t, want, slug)
		}
		for _, input := range []string{"!!!", ""} {
			slug, err := forms.Slugify(input)
			require.NoError(t, err)
			assert.Len(t, slug, 20)
		}
	})

	t.Run("updates form and delivery settings atomically", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		profile, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Transactional mailer", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name: "Before", Slug: "stable-slug", AllowedOrigins: "old.example.com",
		})
		require.NoError(t, err)

		updated, err := forms.Update(logger, db, forms.UpdateParams{
			ID: form.ID, Name: "After", AllowedOrigins: "new.example.com",
			EmailEnabled: true, MailerProfileID: &profile.ID,
			EmailRecipient: "team@example.com\narchive@example.com", EmailFormat: forms.EmailFormatHTML,
			WebhookEnabled: true, WebhookURL: "https://new.example.com/hook",
			WebhookSecret: "secret", WebhookHeadersJSON: `{"Authorization":"Bearer token"}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "After", updated.Name)
		assert.Equal(t, "stable-slug", updated.Slug)
		assert.Equal(t, "new.example.com", updated.AllowedOrigins)
		require.NotNil(t, updated.EmailDelivery)
		require.NotNil(t, updated.WebhookDelivery)
		assert.True(t, updated.EmailDelivery.Enabled)
		assert.Equal(t, "team@example.com, archive@example.com", updated.EmailDelivery.Recipient)
		assert.Equal(t, forms.EmailFormatHTML, updated.EmailDelivery.Format)
		assert.Equal(t, "secret", updated.WebhookDelivery.Secret)
		assert.JSONEq(t, `{"Authorization":"Bearer token"}`, updated.WebhookDelivery.HeadersJSON)

		checks := []struct {
			name, field string
			params      forms.UpdateParams
		}{
			{"blank name", "name", forms.UpdateParams{ID: form.ID, Name: "  "}},
			{"incomplete email", "email", forms.UpdateParams{ID: form.ID, Name: "After", EmailEnabled: true}},
			{"invalid email recipient", "email", forms.UpdateParams{ID: form.ID, Name: "After", EmailEnabled: true, MailerProfileID: &profile.ID, EmailRecipient: "not-an-email"}},
			{"invalid email format", "email_format", forms.UpdateParams{ID: form.ID, Name: "After", EmailFormat: "markdown"}},
			{"missing webhook URL", "webhook", forms.UpdateParams{ID: form.ID, Name: "After", WebhookEnabled: true}},
			{"invalid webhook URL", "webhook", forms.UpdateParams{ID: form.ID, Name: "After", WebhookEnabled: true, WebhookURL: "://missing"}},
			{"invalid headers", "webhook_headers", forms.UpdateParams{ID: form.ID, Name: "After", WebhookEnabled: true, WebhookURL: "https://example.com", WebhookHeadersJSON: "{"}},
		}
		for _, check := range checks {
			t.Run(check.name, func(t *testing.T) {
				_, err := forms.Update(logger, db, check.params)
				var validation *forms.ValidationError
				require.ErrorAs(t, err, &validation)
				assert.Equal(t, check.field, validation.Field)
			})
		}
	})

	t.Run("rejects an incomplete delivery configuration", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		form := forms.Form{Name: "Incomplete", Slug: "incomplete", AllowedOrigins: "*"}
		require.NoError(t, db.Create(&form).Error)

		_, err := forms.Update(logger, db, forms.UpdateParams{ID: form.ID, Name: "Updated"})
		assert.ErrorContains(t, err, "incomplete delivery configuration")
	})

	t.Run("deletes a form-owned record graph", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		dataDir := t.TempDir()
		form := forms.Form{
			Name: "Disposable", Slug: "disposable",
			WebhookDelivery: &forms.WebhookDelivery{Enabled: true, URL: "https://example.com/hook"},
			EmailDelivery:   &forms.EmailDelivery{Enabled: true, Recipient: "owner@example.com"},
		}
		require.NoError(t, db.Create(&form).Error)
		submission, err := forms.CreateSubmissionWithFiles(logger, db, &form, map[string]any{"message": "hello"}, "Browser", "", nil)
		require.NoError(t, err)
		require.NoError(t, db.Create(&forms.SubmissionFile{
			SubmissionID: submission.ID, FieldName: "file", Filename: "note.txt", StoragePath: "uploads/note.txt",
		}).Error)

		require.NoError(t, forms.DeleteForm(logger, db, dataDir, form.ID))
		for name, model := range map[string]any{
			"forms": &forms.Form{}, "submissions": &forms.Submission{}, "files": &forms.SubmissionFile{},
			"email deliveries": &forms.EmailDelivery{}, "webhook deliveries": &forms.WebhookDelivery{},
			"email events": &forms.EmailEvent{}, "webhook events": &forms.WebhookEvent{},
		} {
			var count int64
			require.NoError(t, db.Model(model).Count(&count).Error, name)
			assert.Zero(t, count, name)
		}
		assert.ErrorIs(t, forms.DeleteForm(logger, db, dataDir, form.ID), gorm.ErrRecordNotFound)
	})
}

type submissionResult struct {
	submission *forms.Submission
	err        error
}

type gatedReader struct {
	started chan<- struct{}
	release <-chan struct{}
	reader  io.Reader
	once    sync.Once
}

func (reader *gatedReader) Read(buffer []byte) (int, error) {
	reader.once.Do(func() { close(reader.started) })
	<-reader.release
	return reader.reader.Read(buffer)
}
