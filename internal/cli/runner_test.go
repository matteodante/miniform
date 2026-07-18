package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/iotest"

	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	appconfig "github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestRunner(t *testing.T) {
	t.Run("exposes a machine-readable command manifest without a database", func(t *testing.T) {
		runner, stdout, _ := newTestRunner(t, nil, "")

		exitCode := runner.Run([]string{"commands", "--json"})

		assert.Equal(t, ExitSuccess, exitCode)
		var envelope struct {
			OK      bool          `json:"ok"`
			Command string        `json:"command"`
			Data    []CommandSpec `json:"data"`
		}
		require.NoError(t, json.Unmarshal(stdout.Bytes(), &envelope))
		assert.True(t, envelope.OK)
		assert.Equal(t, "commands", envelope.Command)
		assert.Greater(t, len(envelope.Data), 25)
		assert.Contains(t, commandNames(envelope.Data), "backup")
		assert.Contains(t, commandNames(envelope.Data), "form create")
		for _, command := range envelope.Data {
			if command.Name == "form update" {
				assert.Contains(t, command.Flags, "--email-format text|html")
			}
		}
	})

	t.Run("recognizes global flags before the resource", func(t *testing.T) {
		assert.True(t, IsInvocation([]string{"--json", "form", "list"}))
		assert.True(t, RequiresDatabase([]string{"--json", "form", "list"}))
		assert.False(t, RequiresDatabase([]string{"form", "list", "--help"}))
		assert.False(t, RequiresDatabase([]string{"form", "list", "-h"}))
		assert.False(t, RequiresConfig([]string{"form", "list", "--help"}))
		assert.False(t, RequiresConfig([]string{"config", "show", "-h"}))
		assert.False(t, RequiresDatabase([]string{"--json", "commands"}))
		assert.False(t, RequiresDatabase([]string{"form", "unknown"}))
		assert.False(t, RequiresDatabase([]string{"form"}))
	})

	t.Run("validates command syntax before connecting to the database", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
		}{
			{name: "unknown flag", args: []string{"form", "list", "--bogus"}},
			{name: "malformed flag", args: []string{"form", "get", "--id", "not-a-number"}},
			{name: "unexpected positional", args: []string{"form", "list", "extra"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				connectCalls := 0
				runner := NewRunner(Dependencies{
					ConnectDatabase: func() (*gorm.DB, error) {
						connectCalls++
						return nil, errors.New("database should not be connected")
					},
					Stdout: io.Discard,
					Stderr: io.Discard,
				})

				exitCode := runner.Run(tt.args)

				assert.Equal(t, ExitUsage, exitCode)
				assert.Zero(t, connectCalls)
			})
		}
	})

	t.Run("validates local command semantics before connecting to the database", func(t *testing.T) {
		missingFile := filepath.Join(t.TempDir(), "missing-secret")
		tests := []struct {
			name     string
			args     []string
			stdin    string
			exitCode int
		}{
			{name: "required flag", args: []string{"form", "get"}, exitCode: ExitUsage},
			{name: "confirmation", args: []string{"form", "delete", "--id", "1"}, exitCode: ExitUsage},
			{name: "conflicting flags", args: []string{"form", "update", "--id", "1", "--clear-webhook-secret", "--webhook-secret-file", missingFile}, exitCode: ExitUsage},
			{name: "conflicting stdin files", args: []string{"account", "change-password", "--current-password-file", "-", "--new-password-file", "-"}, exitCode: ExitUsage},
			{name: "unreadable account file", args: []string{"account", "reset-password", "--new-password-file", missingFile}, exitCode: ExitValidation},
			{name: "unreadable form create file", args: []string{"form", "create", "--name", "Contact", "--slug", "contact", "--allowed-origins", "*", "--generated-html-file", missingFile}, exitCode: ExitValidation},
			{name: "unreadable form update file", args: []string{"form", "update", "--id", "1", "--webhook-headers-file", missingFile}, exitCode: ExitValidation},
			{name: "unreadable mailer file", args: []string{"mailer", "update", "--id", "1", "--smtp-password-file", missingFile}, exitCode: ExitValidation},
			{name: "unreadable captcha file", args: []string{"captcha", "update", "--id", "1", "--secret-key-file", missingFile}, exitCode: ExitValidation},
			{name: "invalid base URL", args: []string{"form", "code", "--id", "1", "--base-url", "javascript:alert(1)"}, exitCode: ExitValidation},
			{name: "invalid event type", args: []string{"event", "list", "--type", "unknown"}, exitCode: ExitValidation},
			{name: "invalid payload file", args: []string{"submission", "create", "--form-id", "1", "--data-file", "-"}, stdin: "[", exitCode: ExitValidation},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				connectCalls := 0
				runner := NewRunner(Dependencies{
					ConnectDatabase: func() (*gorm.DB, error) {
						connectCalls++
						return nil, errors.New("database should not be connected")
					},
					Stdin:  strings.NewReader(tt.stdin),
					Stdout: io.Discard,
					Stderr: io.Discard,
				})

				exitCode := runner.Run(tt.args)

				assert.Equal(t, tt.exitCode, exitCode)
				assert.Zero(t, connectCalls)
			})
		}
	})

	t.Run("connects once for valid database commands", func(t *testing.T) {
		tests := []struct {
			name string
			args []string
		}{
			{name: "forms", args: []string{"form", "list"}},
			{name: "mailers", args: []string{"mailer", "list"}},
			{name: "captcha", args: []string{"captcha", "list"}},
			{name: "submissions", args: []string{"submission", "list"}},
			{name: "events", args: []string{"event", "list", "--type", "webhook"}},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				db := testsupport.SetupTestDB(t)
				connectCalls := 0
				runner := NewRunner(Dependencies{
					ConnectDatabase: func() (*gorm.DB, error) {
						connectCalls++
						return db, nil
					},
					Stdout: io.Discard,
					Stderr: io.Discard,
				})

				exitCode := runner.Run(tt.args)

				assert.Equal(t, ExitSuccess, exitCode)
				assert.Equal(t, 1, connectCalls)
			})
		}
	})

	t.Run("reports the database connection failure", func(t *testing.T) {
		stderr := &bytes.Buffer{}
		runner := NewRunner(Dependencies{
			ConnectDatabase: func() (*gorm.DB, error) {
				return nil, errors.New("permission denied")
			},
			Stdout: io.Discard,
			Stderr: stderr,
		})

		exitCode := runner.Run([]string{"form", "list"})

		assert.Equal(t, ExitInternal, exitCode)
		assert.Contains(t, stderr.String(), "connect database: permission denied")
	})

	t.Run("does not connect for help or built-in templates", func(t *testing.T) {
		connectCalls := 0
		runner := NewRunner(Dependencies{
			ConnectDatabase: func() (*gorm.DB, error) {
				connectCalls++
				return nil, errors.New("database should not be connected")
			},
			Stdout: io.Discard,
			Stderr: io.Discard,
		})

		assert.Equal(t, ExitSuccess, runner.Run([]string{"help", "form"}))
		assert.Equal(t, ExitSuccess, runner.Run([]string{"form", "list", "--help"}))
		assert.Equal(t, ExitSuccess, runner.Run([]string{"form", "template-list"}))
		assert.Zero(t, connectCalls)
	})

	t.Run("returns stable JSON usage errors", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		runner, _, stderr := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{"form", "get", "--json"})

		assert.Equal(t, ExitUsage, exitCode)
		var envelope errorEnvelope
		require.NoError(t, json.Unmarshal(stderr.Bytes(), &envelope))
		assert.False(t, envelope.OK)
		assert.Equal(t, "usage_error", envelope.Error.Code)
	})

	t.Run("writes machine-readable startup failures", func(t *testing.T) {
		stderr := &bytes.Buffer{}

		exitCode := WriteStartupFailure([]string{"--json", "form", "list"}, stderr, "connect database", errors.New("permission denied"))

		assert.Equal(t, ExitInternal, exitCode)
		var envelope errorEnvelope
		require.NoError(t, json.Unmarshal(stderr.Bytes(), &envelope))
		assert.Equal(t, "form.list", envelope.Command)
		assert.Equal(t, "internal_error", envelope.Error.Code)
		assert.Contains(t, envelope.Error.Message, "permission denied")
	})

	t.Run("classifies an unchanged password as validation", func(t *testing.T) {
		failure := classifyError(accounts.ErrPasswordUnchanged)

		assert.Equal(t, ExitValidation, failure.ExitCode)
		assert.Equal(t, "validation_error", failure.Code)
	})

	t.Run("keeps an existing file intact when forced copy fails", func(t *testing.T) {
		directory := t.TempDir()
		destination := filepath.Join(directory, "submission.txt")
		require.NoError(t, os.WriteFile(destination, []byte("original"), 0o600))
		source := io.MultiReader(strings.NewReader("partial"), iotest.ErrReader(errors.New("copy failed")))

		err := copySubmissionFile(source, destination, true)

		require.Error(t, err)
		content, readErr := os.ReadFile(destination)
		require.NoError(t, readErr)
		assert.Equal(t, "original", string(content))
		entries, readErr := os.ReadDir(directory)
		require.NoError(t, readErr)
		assert.Len(t, entries, 1)
	})

	t.Run("creates forms and redacts submission credentials by default", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		runner, stdout, _ := newTestRunner(t, db, "webhook-secret\n")

		exitCode := runner.Run([]string{
			"form", "create", "--json",
			"--name", "Contact", "--slug", "contact", "--allowed-origins", "*",
			"--webhook-enabled", "--webhook-url", "https://example.com/hook", "--webhook-secret-file", "-",
		})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), `"token":"[REDACTED]"`)
		assert.Contains(t, stdout.String(), `"secret":"[REDACTED]"`)
		form, err := forms.GetBySlug(db, "contact")
		require.NoError(t, err)
		assert.Equal(t, "webhook-secret", form.WebhookDelivery.Secret)
	})

	t.Run("mailer update preserves omitted secrets", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		profile, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name:             "Primary",
			DefaultFromEmail: "sender@example.com",
			SMTPHost:         "smtp.example.com",
			SMTPPort:         587,
			SMTPPassword:     "original-secret",
			SMTPEncryption:   "starttls",
		})
		require.NoError(t, err)
		runner, _, _ := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{"mailer", "update", "--id", uintString(profile.ID), "--name", "Renamed"})

		assert.Equal(t, ExitSuccess, exitCode)
		updated, err := integrations.GetMailerProfileByID(db, profile.ID)
		require.NoError(t, err)
		assert.Equal(t, "Renamed", updated.Name)
		assert.Equal(t, "original-secret", updated.SMTPPassword)
	})

	t.Run("creates an SMTP mailer from protected password input", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		runner, stdout, _ := newTestRunner(t, db, "smtp-secret\n")

		exitCode := runner.Run([]string{
			"mailer", "create", "--json", "--name", "SMTP",
			"--default-from-email", "sender@example.com",
			"--smtp-host", "smtp.example.com", "--smtp-password-file", "-",
		})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), `"smtp_password":"[REDACTED]"`)
		profiles, err := integrations.ListMailerProfiles(db)
		require.NoError(t, err)
		require.Len(t, profiles, 1)
		assert.Equal(t, "smtp.example.com", profiles[0].SMTPHost)
		assert.Equal(t, "smtp-secret", profiles[0].SMTPPassword)
	})

	t.Run("account reset hashes the replacement password", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		user := createCLIUser(t, logger, db)
		runner, _, _ := newTestRunner(t, db, "replacement-password\n")

		exitCode := runner.Run([]string{"account", "reset-password", "--email", user.Email, "--new-password-file", "-"})

		assert.Equal(t, ExitSuccess, exitCode)
		result, err := accounts.Authenticate(logger, db, user.Email, "replacement-password")
		require.NoError(t, err)
		assert.Equal(t, user.ID, result.ID)
	})

	t.Run("submission list returns parsed payload and pagination", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Inbox", Slug: "inbox", AllowedOrigins: "*"})
		require.NoError(t, err)
		_, err = forms.CreateSubmissionWithFiles(logger, db, form, map[string]any{"email": "user@example.com"}, "test-agent", "", nil)
		require.NoError(t, err)
		runner, stdout, _ := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{"submission", "list", "--json", "--form-id", uintString(form.ID)})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), `"email":"user@example.com"`)
		assert.Contains(t, stdout.String(), `"total_count":1`)
	})

	t.Run("configures multiple email recipients and message format", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		profile, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "SMTP", DefaultFromEmail: "forms@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)
		runner, stdout, _ := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{
			"form", "create", "--json", "--name", "Email", "--slug", "email", "--allowed-origins", "*",
			"--email-enabled", "--mailer-profile-id", uintString(profile.ID),
			"--email-recipient", "owner@example.com, archive@example.com", "--email-format", "html",
		})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), `"recipient":"owner@example.com, archive@example.com"`)
		assert.Contains(t, stdout.String(), `"format":"html"`)
		created, err := forms.GetBySlug(db, "email")
		require.NoError(t, err)
		require.NotNil(t, created.EmailDelivery)
		assert.Equal(t, forms.EmailFormatHTML, created.EmailDelivery.Format)

		runner, _, _ = newTestRunner(t, db, "")
		exitCode = runner.Run([]string{"form", "update", "--id", uintString(created.ID), "--email-format", "text"})
		assert.Equal(t, ExitSuccess, exitCode)
		updated, err := forms.GetByID(db, created.ID)
		require.NoError(t, err)
		assert.Equal(t, forms.EmailFormatText, updated.EmailDelivery.Format)
		assert.Equal(t, "owner@example.com, archive@example.com", updated.EmailDelivery.Recipient)
	})

	t.Run("form code redacts the token and emits native HTML", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name:           "Embed",
			Slug:           "embed",
			AllowedOrigins: "*",
		})
		require.NoError(t, err)
		runner, stdout, _ := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{"form", "code", "--json", "--id", uintString(form.ID)})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), "YOUR_FORM_TOKEN")
		assert.NotContains(t, stdout.String(), form.Token)
		assert.NotContains(t, stdout.String(), "<script")

		runner, stdout, _ = newTestRunner(t, db, "")
		exitCode = runner.Run([]string{
			"form", "code", "--json", "--show-secrets", "--base-url", "https://forms.example.com", "--id", uintString(form.ID),
		})
		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), form.Token)
		assert.Contains(t, stdout.String(), "https://forms.example.com/forms/embed/submit")
		assert.NotContains(t, stdout.String(), "includes_sdk")

		runner, _, _ = newTestRunner(t, db, "")
		exitCode = runner.Run([]string{
			"form", "code", "--base-url", "javascript:alert(1)", "--id", uintString(form.ID),
		})
		assert.Equal(t, ExitValidation, exitCode)
	})

	t.Run("submission get renders nested delivery events", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name:           "Delivery inbox",
			Slug:           "delivery-inbox",
			AllowedOrigins: "*",
			WebhookEnabled: true,
			WebhookURL:     "https://example.com/hook",
		})
		require.NoError(t, err)
		submission, err := forms.CreateSubmissionWithFiles(logger, db, form, map[string]any{"ok": true}, "test-agent", "", nil)
		require.NoError(t, err)
		runner, stdout, _ := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{"submission", "get", "--json", "--id", uintString(submission.ID)})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), `"webhook_events"`)
		assert.Contains(t, stdout.String(), `"form_name":"Delivery inbox"`)
	})

	t.Run("submission file copy cannot escape upload storage", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name:           "Files",
			Slug:           "files",
			AllowedOrigins: "*",
		})
		require.NoError(t, err)
		submission, err := forms.CreateSubmissionWithFiles(logger, db, form, map[string]any{"ok": true}, "test-agent", "", nil)
		require.NoError(t, err)

		baseDirectory := t.TempDir()
		storageDirectory := filepath.Join(baseDirectory, "storage")
		require.NoError(t, os.Mkdir(storageDirectory, 0o750))
		require.NoError(t, os.WriteFile(filepath.Join(baseDirectory, "secret.txt"), []byte("secret"), 0o600))
		storedFile := &forms.SubmissionFile{
			SubmissionID: submission.ID,
			FieldName:    "attachment",
			Filename:     "secret.txt",
			Size:         6,
			StoragePath:  "../secret.txt",
		}
		require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
			return tx.Create(storedFile).Error
		}))

		runner, _, _ := newTestRunner(t, db, "")
		runner.Config.DataDirectory = storageDirectory
		destination := filepath.Join(t.TempDir(), "copy.txt")

		exitCode := runner.Run([]string{
			"submission", "file-copy", "--id", uintString(submission.ID),
			"--file-id", uintString(storedFile.ID), "--output", destination,
		})

		assert.Equal(t, ExitInternal, exitCode)
		_, err = os.Stat(destination)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})
}

func newTestRunner(t *testing.T, db *gorm.DB, stdin string) (*Runner, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	dataDir := t.TempDir()
	cfg := &appconfig.Config{Config: &cartridgeconfig.Config{DataDirectory: dataDir}}
	runner := NewRunner(Dependencies{
		DB:     db,
		Config: cfg,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Stdin:  strings.NewReader(stdin),
		Stdout: stdout,
		Stderr: stderr,
	})
	return runner, stdout, stderr
}

func commandNames(specs []CommandSpec) []string {
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.Name)
	}
	return names
}

func createCLIUser(t *testing.T, logger *slog.Logger, db *gorm.DB) *accounts.User {
	t.Helper()
	// ResetPassword owns hashing, so create a minimal recovery target with a valid placeholder hash.
	user := &accounts.User{Email: "admin@example.com", PasswordHash: "$2a$10$Q1pg.L2uyfJ2QportzoH9.UPdkdy2skSFqtGaRfOXpO0SBGCQ1qIW"}
	require.NoError(t, dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error { return tx.Create(user).Error }))
	return user
}

func uintString(value uint) string {
	return strconv.FormatUint(uint64(value), 10)
}
