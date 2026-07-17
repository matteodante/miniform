package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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
		assert.Contains(t, commandNames(envelope.Data), "form create")
	})

	t.Run("recognizes global flags before the resource", func(t *testing.T) {
		assert.True(t, IsInvocation([]string{"--json", "form", "list"}))
		assert.True(t, RequiresDatabase([]string{"--json", "form", "list"}))
		assert.False(t, RequiresDatabase([]string{"--json", "commands"}))
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

		exitCode := WriteStartupFailure([]string{"--json", "form", "list"}, stderr, "connect database")

		assert.Equal(t, ExitInternal, exitCode)
		var envelope errorEnvelope
		require.NoError(t, json.Unmarshal(stderr.Bytes(), &envelope))
		assert.Equal(t, "form.list", envelope.Command)
		assert.Equal(t, "internal_error", envelope.Error.Code)
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
			Name:           "Primary",
			SMTPHost:       "smtp.example.com",
			SMTPPort:       587,
			SMTPPassword:   "original-secret",
			SMTPEncryption: "starttls",
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

	t.Run("config set writes secrets from stdin and config unset preserves other lines", func(t *testing.T) {
		envPath := filepath.Join(t.TempDir(), ".env")
		require.NoError(t, os.WriteFile(envPath, []byte("# keep\nMINIFORM_PORT=9000\n"), 0o644))
		runner, _, _ := newTestRunner(t, nil, "new-session-secret\n")

		exitCode := runner.Run([]string{
			"config", "set", "--key", "MINIFORM_SESSION_SECRET", "--value-file", "-", "--env-file", envPath,
		})
		assert.Equal(t, ExitSuccess, exitCode)

		content, err := os.ReadFile(envPath)
		require.NoError(t, err)
		assert.Contains(t, string(content), "# keep")
		assert.Contains(t, string(content), "MINIFORM_PORT=9000")
		assert.Contains(t, string(content), "MINIFORM_SESSION_SECRET=new-session-secret")
		info, err := os.Stat(envPath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		runner, _, _ = newTestRunner(t, nil, "")
		exitCode = runner.Run([]string{"config", "unset", "--key", "MINIFORM_PORT", "--env-file", envPath})
		assert.Equal(t, ExitSuccess, exitCode)
		content, err = os.ReadFile(envPath)
		require.NoError(t, err)
		assert.NotContains(t, string(content), "MINIFORM_PORT")
		assert.Contains(t, string(content), "MINIFORM_SESSION_SECRET")
	})

	t.Run("config set creates private parent directories", func(t *testing.T) {
		directory := filepath.Join(t.TempDir(), "config")
		envPath := filepath.Join(directory, ".env")
		runner, _, _ := newTestRunner(t, nil, "")

		exitCode := runner.Run([]string{
			"config", "set", "--key", "MINIFORM_PORT", "--value", "9000", "--env-file", envPath,
		})

		assert.Equal(t, ExitSuccess, exitCode)
		info, err := os.Stat(directory)
		require.NoError(t, err)
		assert.Zero(t, info.Mode().Perm()&0o027)
	})

	t.Run("account reset hashes the replacement password", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		require.NoError(t, db.AutoMigrate(&accounts.Settings{}))
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		user := createCLIUser(t, logger, db)
		runner, _, _ := newTestRunner(t, db, "replacement-password\n")

		exitCode := runner.Run([]string{"account", "reset-password", "--email", user.Email, "--new-password-file", "-"})

		assert.Equal(t, ExitSuccess, exitCode)
		result, err := accounts.Authenticate(logger, db, user.Email, "replacement-password")
		require.NoError(t, err)
		assert.Equal(t, user.ID, result.User.ID)
	})

	t.Run("submission list returns parsed payload and pagination", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		form, err := forms.Create(logger, db, forms.CreateParams{Name: "Inbox", Slug: "inbox", AllowedOrigins: "*"})
		require.NoError(t, err)
		_, err = forms.CreateSubmission(logger, db, form, map[string]any{"email": "user@example.com"}, "test-agent")
		require.NoError(t, err)
		runner, stdout, _ := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{"submission", "list", "--json", "--form-id", uintString(form.ID)})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), `"email":"user@example.com"`)
		assert.Contains(t, stdout.String(), `"total_count":1`)
	})

	t.Run("form code redacts the token and includes configured SDK", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name:           "Embed",
			Slug:           "embed",
			AllowedOrigins: "*",
			UseSDK:         true,
		})
		require.NoError(t, err)
		runner, stdout, _ := newTestRunner(t, db, "")

		exitCode := runner.Run([]string{"form", "code", "--json", "--id", uintString(form.ID)})

		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), "YOUR_FORM_TOKEN")
		assert.NotContains(t, stdout.String(), form.Token)
		assert.Contains(t, stdout.String(), "/assets/miniform.js")

		runner, stdout, _ = newTestRunner(t, db, "")
		exitCode = runner.Run([]string{"form", "code", "--json", "--show-secrets", "--id", uintString(form.ID)})
		assert.Equal(t, ExitSuccess, exitCode)
		assert.Contains(t, stdout.String(), form.Token)
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
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test-agent")
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
		submission, err := forms.CreateSubmission(logger, db, form, map[string]any{"ok": true}, "test-agent")
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
