package integrations_test

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestProfiles(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("mailer lifecycle preserves SMTP settings", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name:            "  Transactional SMTP  ",
			DefaultFromName: "Forms", DefaultFromEmail: "forms@example.com",
			SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: " smtp user ",
			SMTPPassword: " secret ", SMTPEncryption: "starttls",
		})
		require.NoError(t, err)
		assert.Equal(t, "Transactional SMTP", created.Name)
		assert.Equal(t, " smtp user ", created.SMTPUsername)
		assert.Equal(t, " secret ", created.SMTPPassword)
		assert.NotZero(t, created.ID)

		updated, err := integrations.UpdateMailerProfile(logger, db, created.ID, integrations.MailerProfileParams{
			Name: "Primary SMTP", SMTPHost: "mail.example.org",
			SMTPPort: 465, SMTPEncryption: "tls", DefaultFromEmail: "forms@example.com",
		})
		require.NoError(t, err)
		assert.Equal(t, "Primary SMTP", updated.Name)
		assert.Equal(t, "mail.example.org", updated.SMTPHost)
		assert.Equal(t, 465, updated.SMTPPort)
		assert.Empty(t, updated.SMTPPassword)

		require.NoError(t, integrations.DeleteMailerProfile(logger, db, created.ID))
		_, err = integrations.GetMailerProfileByID(db, created.ID)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
		assert.ErrorIs(t, integrations.DeleteMailerProfile(logger, db, created.ID), gorm.ErrRecordNotFound)
	})

	t.Run("mailer schema contains only SMTP configuration", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		for _, column := range []string{"provider", "api_key", "domain", "defaults_json"} {
			assert.False(t, db.Migrator().HasColumn(&integrations.MailerProfile{}, column), column)
		}
	})

	t.Run("mailer validation rejects malformed input and duplicate names", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		_, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{})
		assertValidation(t, err, "name")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: "Missing host"})
		assertValidation(t, err, "smtp_host")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Bad port", SMTPHost: "smtp.example.com", SMTPPort: 70000,
		})
		assertValidation(t, err, "smtp_port")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Bad encryption", SMTPHost: "smtp.example.com", SMTPEncryption: "plain",
		})
		assertValidation(t, err, "smtp_encryption")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Bad sender", DefaultFromEmail: "not-an-email", SMTPHost: "smtp.example.com",
		})
		assertValidation(t, err, "default_from_email")

		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: "Unique", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com"})
		require.NoError(t, err)
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: "Unique", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com"})
		assertValidation(t, err, "name")

		second, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: "Second", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com"})
		require.NoError(t, err)
		_, err = integrations.UpdateMailerProfile(logger, db, second.ID, integrations.MailerProfileParams{Name: "Unique", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com"})
		assertValidation(t, err, "name")
		_, err = integrations.UpdateMailerProfile(logger, db, 9999, integrations.MailerProfileParams{Name: "Missing", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com"})
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})

	t.Run("captcha lifecycle stores explicit Turnstile credentials", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created, err := integrations.CreateCaptchaProfile(logger, db, integrations.CaptchaProfileParams{
			Name: "Turnstile", SiteKey: "public", SecretKey: "secret",
		})
		require.NoError(t, err)
		assert.Equal(t, "public", created.SiteKey)
		assert.Equal(t, "secret", created.SecretKey)

		updated, err := integrations.UpdateCaptchaProfile(logger, db, created.ID, integrations.CaptchaProfileParams{
			Name: "Turnstile Production", SiteKey: "rotated-public", SecretKey: "rotated-secret",
		})
		require.NoError(t, err)
		assert.Equal(t, "rotated-public", updated.SiteKey)
		assert.Equal(t, "rotated-secret", updated.SecretKey)

		for _, test := range []struct {
			name, field string
			params      integrations.CaptchaProfileParams
		}{
			{"missing name", "name", integrations.CaptchaProfileParams{}},
			{"missing site key", "site_key", integrations.CaptchaProfileParams{Name: "Missing site key", SecretKey: "secret"}},
			{"missing secret key", "secret_key", integrations.CaptchaProfileParams{Name: "Missing secret key", SiteKey: "public"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				_, err := integrations.CreateCaptchaProfile(logger, db, test.params)
				assertValidation(t, err, test.field)
			})
		}
		require.NoError(t, integrations.DeleteCaptchaProfile(logger, db, created.ID))
	})

	t.Run("captcha schema contains only explicit Turnstile credentials", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		assert.True(t, db.Migrator().HasColumn(&integrations.CaptchaProfile{}, "site_key"))
		for _, column := range []string{"provider", "site_keys_json", "policy_json"} {
			assert.False(t, db.Migrator().HasColumn(&integrations.CaptchaProfile{}, column), column)
		}
	})

	t.Run("captcha validation rejects duplicate names", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		params := integrations.CaptchaProfileParams{Name: "Unique", SiteKey: "public", SecretKey: "secret"}
		_, err := integrations.CreateCaptchaProfile(logger, db, params)
		require.NoError(t, err)
		_, err = integrations.CreateCaptchaProfile(logger, db, params)
		assertValidation(t, err, "name")

		second, err := integrations.CreateCaptchaProfile(logger, db, integrations.CaptchaProfileParams{
			Name: "Second", SiteKey: "other-public", SecretKey: "other-secret",
		})
		require.NoError(t, err)
		_, err = integrations.UpdateCaptchaProfile(logger, db, second.ID, params)
		assertValidation(t, err, "name")
		_, err = integrations.UpdateCaptchaProfile(logger, db, 9999, params)
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})

	t.Run("refuses to delete profiles referenced by a form", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		mailer, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "In use", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)
		captcha, err := integrations.CreateCaptchaProfile(logger, db, integrations.CaptchaProfileParams{
			Name: "In use", SiteKey: "site", SecretKey: "secret",
		})
		require.NoError(t, err)
		form, err := forms.Create(logger, db, forms.CreateParams{
			Name: "Protected", Slug: "protected", AllowedOrigins: "*",
			MailerProfileID: &mailer.ID, EmailEnabled: true, EmailRecipient: "owner@example.com",
			CaptchaProfileID: &captcha.ID,
		})
		require.NoError(t, err)

		assert.ErrorIs(t, integrations.DeleteMailerProfile(logger, db, mailer.ID), integrations.ErrProfileInUse)
		assert.ErrorIs(t, integrations.DeleteCaptchaProfile(logger, db, captcha.ID), integrations.ErrProfileInUse)

		preserved, err := forms.GetByID(db, form.ID)
		require.NoError(t, err)
		delivery := forms.PrimaryEmailDelivery(preserved)
		require.NotNil(t, delivery)
		require.NotNil(t, delivery.MailerProfileID)
		assert.Equal(t, mailer.ID, *delivery.MailerProfileID)
		require.NotNil(t, preserved.CaptchaProfileID)
		assert.Equal(t, captcha.ID, *preserved.CaptchaProfileID)
	})

	t.Run("lists profiles alphabetically", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		for _, name := range []string{"Zulu", "Alpha", "Middle"} {
			_, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: name, DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com"})
			require.NoError(t, err)
		}
		mailers, err := integrations.ListMailerProfiles(db)
		require.NoError(t, err)
		assert.Equal(t, []string{"Alpha", "Middle", "Zulu"}, []string{mailers[0].Name, mailers[1].Name, mailers[2].Name})

		for _, name := range []string{"Guard B", "Guard A"} {
			_, err := integrations.CreateCaptchaProfile(logger, db, integrations.CaptchaProfileParams{
				Name: name, SiteKey: "public", SecretKey: "secret",
			})
			require.NoError(t, err)
		}
		captchas, err := integrations.ListCaptchaProfiles(db)
		require.NoError(t, err)
		assert.Equal(t, []string{"Guard A", "Guard B"}, []string{captchas[0].Name, captchas[1].Name})
	})
}

func assertValidation(t *testing.T, err error, field string) {
	t.Helper()
	var validation *integrations.ValidationError
	require.True(t, errors.As(err, &validation), "error %v must be a ValidationError", err)
	assert.Equal(t, field, validation.Field)
}
