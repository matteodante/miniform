package integrations_test

import (
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/integrations"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestProfiles(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("mailer lifecycle preserves provider settings", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "  Transactional SMTP  ", Provider: "smtp", DefaultsJSON: `{"headers":{"X-App":"Miniform"}}`,
			DefaultFromName: "Forms", DefaultFromEmail: "forms@example.com",
			SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPUsername: "user",
			SMTPPassword: "secret", SMTPEncryption: "starttls",
		})
		require.NoError(t, err)
		assert.Equal(t, "Transactional SMTP", created.Name)
		assert.NotZero(t, created.ID)

		updated, err := integrations.UpdateMailerProfile(logger, db, created.ID, integrations.MailerProfileParams{
			Name: "Primary SMTP", Provider: "smtp", SMTPHost: "mail.example.org",
			SMTPPort: 465, SMTPEncryption: "tls",
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

	t.Run("mailer validation rejects malformed input and duplicate names", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		_, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{})
		assertValidation(t, err, "name")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Broken JSON", DefaultsJSON: `{broken`,
		})
		assertValidation(t, err, "defaults_json")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Wrong JSON shape", DefaultsJSON: `[]`,
		})
		assertValidation(t, err, "defaults_json")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Unknown provider", Provider: "sendmail",
		})
		assertValidation(t, err, "provider")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Bad port", SMTPPort: 70000,
		})
		assertValidation(t, err, "smtp_port")
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{
			Name: "Bad encryption", SMTPEncryption: "plain",
		})
		assertValidation(t, err, "smtp_encryption")

		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: "Unique"})
		require.NoError(t, err)
		_, err = integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: "Unique"})
		assertValidation(t, err, "name")

		second, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: "Second"})
		require.NoError(t, err)
		_, err = integrations.UpdateMailerProfile(logger, db, second.ID, integrations.MailerProfileParams{Name: "Unique"})
		assertValidation(t, err, "name")
		_, err = integrations.UpdateMailerProfile(logger, db, 9999, integrations.MailerProfileParams{Name: "Missing"})
		assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
	})

	t.Run("captcha lifecycle validates both JSON documents", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created, err := integrations.CreateCaptchaProfile(logger, db, integrations.CaptchaProfileParams{
			Name: "Turnstile", Provider: "turnstile", SecretKey: "secret",
			SiteKeysJSON: `[{"host_pattern":"*.example.com","site_key":"public"}]`,
			PolicyJSON:   `{"required":true,"action":"submit"}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "turnstile", created.Provider)

		updated, err := integrations.UpdateCaptchaProfile(logger, db, created.ID, integrations.CaptchaProfileParams{
			Name: "Turnstile Production", Provider: "turnstile", SecretKey: "rotated",
			SiteKeysJSON: `[]`, PolicyJSON: `{}`,
		})
		require.NoError(t, err)
		assert.Equal(t, "rotated", updated.SecretKey)

		for _, test := range []struct {
			name, field string
			params      integrations.CaptchaProfileParams
		}{
			{"missing name", "name", integrations.CaptchaProfileParams{}},
			{"bad site keys", "site_keys_json", integrations.CaptchaProfileParams{Name: "Bad keys", SiteKeysJSON: `{`}},
			{"site keys object", "site_keys_json", integrations.CaptchaProfileParams{Name: "Object keys", SiteKeysJSON: `{}`}},
			{"incomplete site key", "site_keys_json", integrations.CaptchaProfileParams{Name: "Incomplete key", SiteKeysJSON: `[{"host_pattern":"*"}]`}},
			{"bad policy", "policy_json", integrations.CaptchaProfileParams{Name: "Bad policy", PolicyJSON: `[`}},
			{"policy array", "policy_json", integrations.CaptchaProfileParams{Name: "Array policy", PolicyJSON: `[]`}},
			{"policy value type", "policy_json", integrations.CaptchaProfileParams{Name: "Typed policy", PolicyJSON: `{"required":"yes"}`}},
			{"invalid action", "policy_json", integrations.CaptchaProfileParams{Name: "Bad action", PolicyJSON: `{"action":"not allowed"}`}},
			{"invalid theme", "policy_json", integrations.CaptchaProfileParams{Name: "Bad theme", PolicyJSON: `{"theme":"sepia"}`}},
			{"invalid size", "policy_json", integrations.CaptchaProfileParams{Name: "Bad size", PolicyJSON: `{"size":"invisible"}`}},
			{"unknown provider", "provider", integrations.CaptchaProfileParams{Name: "Other provider", Provider: "recaptcha"}},
		} {
			t.Run(test.name, func(t *testing.T) {
				_, err := integrations.CreateCaptchaProfile(logger, db, test.params)
				assertValidation(t, err, test.field)
			})
		}
		require.NoError(t, integrations.DeleteCaptchaProfile(logger, db, created.ID))
	})

	t.Run("lists profiles alphabetically", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		for _, name := range []string{"Zulu", "Alpha", "Middle"} {
			_, err := integrations.CreateMailerProfile(logger, db, integrations.MailerProfileParams{Name: name})
			require.NoError(t, err)
		}
		mailers, err := integrations.ListMailerProfiles(db)
		require.NoError(t, err)
		assert.Equal(t, []string{"Alpha", "Middle", "Zulu"}, []string{mailers[0].Name, mailers[1].Name, mailers[2].Name})

		for _, name := range []string{"Guard B", "Guard A"} {
			_, err := integrations.CreateCaptchaProfile(logger, db, integrations.CaptchaProfileParams{
				Name: name, SiteKeysJSON: `[]`,
			})
			require.NoError(t, err)
		}
		captchas, err := integrations.ListCaptchaProfiles(db)
		require.NoError(t, err)
		assert.Equal(t, []string{"Guard A", "Guard B"}, []string{captchas[0].Name, captchas[1].Name})
	})

	t.Run("captcha settings merge policy and endpoint overrides", func(t *testing.T) {
		settings := integrations.ResolveCaptchaSettings(
			`{"required":false,"action":"contact","theme":"dark"}`,
			`{"required":true,"action":"checkout","language":"it"}`,
		)

		assert.True(t, settings.Required)
		assert.Equal(t, "checkout", settings.Action)
		assert.Equal(t, "dark", settings.Theme)
		assert.Equal(t, "it", settings.Language)

		defaults := integrations.ResolveCaptchaSettings("", "")
		assert.True(t, defaults.Required)
		assert.Equal(t, "submit", defaults.Action)
		assert.Equal(t, "auto", defaults.Theme)
	})
}

func assertValidation(t *testing.T, err error, field string) {
	t.Helper()
	var validation *integrations.ValidationError
	require.True(t, errors.As(err, &validation), "error %v must be a ValidationError", err)
	assert.Equal(t, field, validation.Field)
}
