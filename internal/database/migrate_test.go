package database

import (
	"testing"
	"time"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type legacyUser struct {
	ID           uint `gorm:"primaryKey"`
	Email        string
	PasswordHash string
	LastLoginAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

func (legacyUser) TableName() string { return "users" }

type legacyCaptchaProfile struct {
	ID           uint   `gorm:"primaryKey"`
	Name         string `gorm:"not null;uniqueIndex"`
	Provider     string `gorm:"not null;default:'turnstile'"`
	SecretKey    string
	SiteKeysJSON string
	PolicyJSON   string
}

func (legacyCaptchaProfile) TableName() string { return "captcha_profiles" }

type legacyMailerProfile struct {
	ID               uint   `gorm:"primaryKey"`
	Name             string `gorm:"not null;uniqueIndex"`
	Provider         string `gorm:"not null;default:'smtp'"`
	APIKey           string
	Domain           string
	DefaultsJSON     string
	DefaultFromName  string
	DefaultFromEmail string
	SMTPHost         string
	SMTPPort         int
	SMTPUsername     string
	SMTPPassword     string
	SMTPEncryption   string
}

func (legacyMailerProfile) TableName() string { return "mailer_profiles" }

type legacyForm struct {
	ID                   uint   `gorm:"primaryKey"`
	PublicID             string `gorm:"uniqueIndex"`
	Name                 string
	Slug                 string `gorm:"uniqueIndex"`
	Token                string `gorm:"uniqueIndex"`
	AllowedOrigins       string
	CaptchaProfileID     *uint
	CaptchaOverridesJSON string
}

func (legacyForm) TableName() string { return "forms" }

type legacyEmailDelivery struct {
	ID              uint `gorm:"primaryKey"`
	FormID          uint `gorm:"uniqueIndex;not null"`
	Enabled         bool
	MailerProfileID *uint
	OverridesJSON   string
}

func (legacyEmailDelivery) TableName() string { return "email_deliveries" }

type partiallySimplifiedCaptchaProfile struct {
	ID           uint   `gorm:"primaryKey"`
	Name         string `gorm:"not null;uniqueIndex"`
	Provider     string `gorm:"not null;default:'turnstile'"`
	SecretKey    string
	SiteKey      string
	SiteKeysJSON string
	PolicyJSON   string
}

func (partiallySimplifiedCaptchaProfile) TableName() string { return "captcha_profiles" }

type partiallySimplifiedEmailDelivery struct {
	ID              uint `gorm:"primaryKey"`
	FormID          uint `gorm:"uniqueIndex;not null"`
	Enabled         bool
	MailerProfileID *uint
	Recipient       *string
	OverridesJSON   string
}

func (partiallySimplifiedEmailDelivery) TableName() string { return "email_deliveries" }

type legacySetting struct {
	ID    uint `gorm:"primaryKey"`
	Key   string
	Value string
}

func (legacySetting) TableName() string { return "settings" }

func TestMigrate(t *testing.T) {
	t.Run("preserves the password change requirement for legacy first login accounts", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacyUser{}))
		now := time.Now().UTC()
		require.NoError(t, db.Create(&[]legacyUser{
			{Email: "pending@example.com", PasswordHash: "pending"},
			{Email: "active@example.com", PasswordHash: "active", LastLoginAt: &now},
		}).Error)
		require.False(t, db.Migrator().HasColumn(&accounts.User{}, "PasswordChangeRequired"))

		require.NoError(t, Migrate(db))

		pending, err := accounts.FindByEmail(db, "pending@example.com")
		require.NoError(t, err)
		require.True(t, pending.PasswordChangeRequired)
		active, err := accounts.FindByEmail(db, "active@example.com")
		require.NoError(t, err)
		require.False(t, active.PasswordChangeRequired)
	})

	t.Run("restores missing delivery records for legacy forms", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, Migrate(db))
		form := &forms.Form{Name: "Legacy", Slug: "legacy", AllowedOrigins: "*"}
		require.NoError(t, db.Create(form).Error)

		require.NoError(t, Migrate(db))
		require.NoError(t, Migrate(db))

		var emailCount, webhookCount int64
		require.NoError(t, db.Model(&forms.EmailDelivery{}).Where("form_id = ?", form.ID).Count(&emailCount).Error)
		require.NoError(t, db.Model(&forms.WebhookDelivery{}).Where("form_id = ?", form.ID).Count(&webhookCount).Error)
		require.Equal(t, int64(1), emailCount)
		require.Equal(t, int64(1), webhookCount)
	})

	t.Run("retains legacy settings for downgrade and recovery", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacySetting{}))
		require.NoError(t, db.Create(&legacySetting{Key: "operator-note", Value: "preserve me"}).Error)

		require.NoError(t, Migrate(db))
		require.NoError(t, Migrate(db))

		var setting legacySetting
		require.NoError(t, db.Where("key = ?", "operator-note").First(&setting).Error)
		require.Equal(t, "preserve me", setting.Value)
	})

	t.Run("preserves simplified captcha and email configuration", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacyCaptchaProfile{}, &legacyMailerProfile{}, &legacyEmailDelivery{}))
		require.NoError(t, db.Create(&[]legacyCaptchaProfile{
			{ID: 1, Name: "Policy key", SecretKey: "policy-secret", SiteKeysJSON: `[{"host_pattern":"*","site_key":"fallback-key"}]`, PolicyJSON: `{"site_key":"policy-key"}`},
			{ID: 2, Name: "Mapped key", SecretKey: "mapped-secret", SiteKeysJSON: `[{"host_pattern":"*","site_key":"mapped-key"}]`},
		}).Error)
		require.NoError(t, db.Create(&legacyMailerProfile{
			ID: 1, Name: "SMTP", Provider: "smtp", DefaultFromEmail: "sender@example.com",
			SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPEncryption: "starttls",
		}).Error)
		mailerID := uint(1)
		require.NoError(t, db.Create(&legacyEmailDelivery{
			ID: 1, FormID: 42, Enabled: true, MailerProfileID: &mailerID, OverridesJSON: `{"to":"owner@example.com"}`,
		}).Error)

		require.NoError(t, Migrate(db))
		require.NoError(t, Migrate(db))

		var captchas []integrations.CaptchaProfile
		require.NoError(t, db.Order("id").Find(&captchas).Error)
		require.Len(t, captchas, 2)
		require.Equal(t, "policy-key", captchas[0].SiteKey)
		require.Equal(t, "policy-secret", captchas[0].SecretKey)
		require.Equal(t, "mapped-key", captchas[1].SiteKey)
		var policyJSON, siteKeysJSON string
		require.NoError(t, db.Raw("SELECT policy_json, site_keys_json FROM captcha_profiles WHERE id = 1").Row().Scan(&policyJSON, &siteKeysJSON))
		require.JSONEq(t, `{"site_key":"policy-key"}`, policyJSON)
		require.JSONEq(t, `[{"host_pattern":"*","site_key":"fallback-key"}]`, siteKeysJSON)

		var delivery forms.EmailDelivery
		require.NoError(t, db.First(&delivery, 1).Error)
		require.True(t, delivery.Enabled)
		require.Equal(t, "owner@example.com", delivery.Recipient)
		var overridesJSON string
		require.NoError(t, db.Raw("SELECT overrides_json FROM email_deliveries WHERE id = 1").Row().Scan(&overridesJSON))
		require.JSONEq(t, `{"to":"owner@example.com"}`, overridesJSON)
	})

	t.Run("preserves effective legacy captcha behavior without blocking submissions", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacyCaptchaProfile{}, &legacyForm{}))
		require.NoError(t, db.Create(&[]legacyCaptchaProfile{
			{ID: 1, Name: "Optional override", Provider: "turnstile", SecretKey: "secret-1", PolicyJSON: `{"required":true,"site_key":"base-1"}`},
			{ID: 2, Name: "Optional policy", Provider: "turnstile", SecretKey: "secret-2", PolicyJSON: `{"required":false,"site_key":"base-2"}`},
			{ID: 3, Name: "Custom action", Provider: "turnstile", SecretKey: "secret-3", PolicyJSON: `{"action":"checkout","site_key":"base-3"}`},
			{ID: 4, Name: "Mapped", Provider: "turnstile", SecretKey: "secret-4", SiteKeysJSON: `[{"host_pattern":"example.com","site_key":"mapped-key"},{"host_pattern":"*","site_key":"fallback-key"}]`},
			{ID: 5, Name: "Per form", Provider: "turnstile", SecretKey: "secret-5", PolicyJSON: `{"site_key":"base-5"}`},
			{ID: 6, Name: "Leading dot", Provider: "turnstile", SecretKey: "secret-6", SiteKeysJSON: `[{"host_pattern":"other.test","site_key":"other-key"},{"host_pattern":".example.com","site_key":"dot-key"},{"host_pattern":"*","site_key":"fallback-key"}]`},
		}).Error)
		profile := func(id uint) *uint { return &id }
		require.NoError(t, db.Create(&[]legacyForm{
			{ID: 1, PublicID: "legacy-1", Name: "Optional override", Slug: "legacy-1", Token: "token-1", AllowedOrigins: "*", CaptchaProfileID: profile(1), CaptchaOverridesJSON: `{"required":false}`},
			{ID: 2, PublicID: "legacy-2", Name: "Optional policy", Slug: "legacy-2", Token: "token-2", AllowedOrigins: "*", CaptchaProfileID: profile(2)},
			{ID: 3, PublicID: "legacy-3", Name: "Custom action", Slug: "legacy-3", Token: "token-3", AllowedOrigins: "*", CaptchaProfileID: profile(3)},
			{ID: 4, PublicID: "legacy-4", Name: "Mapped", Slug: "legacy-4", Token: "token-4", AllowedOrigins: "https://example.com", CaptchaProfileID: profile(4)},
			{ID: 5, PublicID: "legacy-5", Name: "Per form", Slug: "legacy-5", Token: "token-5", AllowedOrigins: "*", CaptchaProfileID: profile(5), CaptchaOverridesJSON: `{"site_key":"override-key"}`},
			{ID: 6, PublicID: "legacy-6", Name: "Leading dot", Slug: "legacy-6", Token: "token-6", AllowedOrigins: "https://app.example.com", CaptchaProfileID: profile(6)},
		}).Error)

		require.NoError(t, Migrate(db))
		require.NoError(t, Migrate(db))

		for _, id := range []uint{1, 2, 3} {
			migrated, err := forms.GetByID(db, id)
			require.NoError(t, err)
			require.Nil(t, migrated.CaptchaProfileID)
		}
		mapped, err := forms.GetByID(db, 4)
		require.NoError(t, err)
		require.NotNil(t, mapped.CaptchaProfile)
		require.Equal(t, "mapped-key", mapped.CaptchaProfile.SiteKey)
		require.Equal(t, "secret-4", mapped.CaptchaProfile.SecretKey)
		overridden, err := forms.GetByID(db, 5)
		require.NoError(t, err)
		require.NotNil(t, overridden.CaptchaProfile)
		require.Equal(t, "override-key", overridden.CaptchaProfile.SiteKey)
		require.Equal(t, "secret-5", overridden.CaptchaProfile.SecretKey)
		leadingDot, err := forms.GetByID(db, 6)
		require.NoError(t, err)
		require.NotNil(t, leadingDot.CaptchaProfile)
		require.Equal(t, "dot-key", leadingDot.CaptchaProfile.SiteKey)

		var overridesJSON string
		require.NoError(t, db.Raw("SELECT captcha_overrides_json FROM forms WHERE id = 1").Row().Scan(&overridesJSON))
		require.JSONEq(t, `{"required":false}`, overridesJSON)
	})

	t.Run("keeps captcha credentials already configured by a simplified release", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&partiallySimplifiedCaptchaProfile{}, &legacyForm{}))
		require.NoError(t, db.Create(&partiallySimplifiedCaptchaProfile{
			ID: 1, Name: "Current", Provider: "turnstile", SecretKey: "current-secret", SiteKey: "current-site",
			SiteKeysJSON: `[{"host_pattern":"*","site_key":"old-site"}]`, PolicyJSON: `{"required":false,"site_key":"old-site"}`,
		}).Error)
		profileID := uint(1)
		require.NoError(t, db.Create(&legacyForm{
			ID: 1, PublicID: "current", Name: "Current", Slug: "current", Token: "current-token",
			AllowedOrigins: "*", CaptchaProfileID: &profileID,
		}).Error)

		require.NoError(t, Migrate(db))
		require.NoError(t, Migrate(db))

		form, err := forms.GetByID(db, 1)
		require.NoError(t, err)
		require.NotNil(t, form.CaptchaProfileID)
		require.Equal(t, profileID, *form.CaptchaProfileID)
		require.NotNil(t, form.CaptchaProfile)
		require.Equal(t, "current-site", form.CaptchaProfile.SiteKey)
		require.Equal(t, "current-secret", form.CaptchaProfile.SecretKey)
	})

	t.Run("detaches required captcha profiles that cannot produce a valid challenge", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacyCaptchaProfile{}, &legacyForm{}))
		require.NoError(t, db.Create(&legacyCaptchaProfile{
			ID: 1, Name: "Incomplete", Provider: "turnstile", PolicyJSON: `{"required":true,"action":"submit"}`,
		}).Error)
		profileID := uint(1)
		require.NoError(t, db.Create(&legacyForm{
			ID: 1, PublicID: "incomplete", Name: "Incomplete", Slug: "incomplete", Token: "incomplete-token",
			AllowedOrigins: "*", CaptchaProfileID: &profileID,
		}).Error)

		require.NoError(t, Migrate(db))

		form, err := forms.GetByID(db, 1)
		require.NoError(t, err)
		require.Nil(t, form.CaptchaProfileID)
	})

	t.Run("disables legacy non SMTP and incomplete email routes", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacyMailerProfile{}, &legacyForm{}, &legacyEmailDelivery{}))
		require.NoError(t, db.Create(&[]legacyMailerProfile{
			{ID: 1, Name: "Resend", Provider: "resend", APIKey: "re_secret", DefaultFromEmail: "sender@example.com"},
			{ID: 2, Name: "SMTP", Provider: "smtp", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPEncryption: "starttls"},
			{ID: 3, Name: "Incomplete SMTP", Provider: "smtp", DefaultFromEmail: "sender@example.com"},
			{ID: 4, Name: "Mailgun", Provider: "mailgun", APIKey: "mailgun-secret", Domain: "mg.example.com", DefaultsJSON: `{"tag":"legacy"}`},
		}).Error)
		require.NoError(t, db.Create(&[]legacyForm{
			{ID: 1, PublicID: "mail-1", Name: "Resend", Slug: "mail-1", Token: "mail-token-1", AllowedOrigins: "*"},
			{ID: 2, PublicID: "mail-2", Name: "SMTP", Slug: "mail-2", Token: "mail-token-2", AllowedOrigins: "*"},
			{ID: 3, PublicID: "mail-3", Name: "Incomplete", Slug: "mail-3", Token: "mail-token-3", AllowedOrigins: "*"},
		}).Error)
		mailer := func(id uint) *uint { return &id }
		require.NoError(t, db.Create(&[]legacyEmailDelivery{
			{ID: 1, FormID: 1, Enabled: true, MailerProfileID: mailer(1), OverridesJSON: `{"to":"owner@example.com"}`},
			{ID: 2, FormID: 2, Enabled: true, MailerProfileID: mailer(2), OverridesJSON: `{"to":"owner@example.com"}`},
			{ID: 3, FormID: 3, Enabled: true, MailerProfileID: mailer(3), OverridesJSON: `{"to":"owner@example.com"}`},
		}).Error)

		require.NoError(t, Migrate(db))

		for id, enabled := range map[uint]bool{1: false, 2: true, 3: false} {
			var delivery forms.EmailDelivery
			require.NoError(t, db.First(&delivery, id).Error)
			require.Equal(t, enabled, delivery.Enabled)
			require.Equal(t, "owner@example.com", delivery.Recipient)
		}
		var provider, apiKey string
		require.NoError(t, db.Raw("SELECT provider, api_key FROM mailer_profiles WHERE id = 1").Row().Scan(&provider, &apiKey))
		require.Equal(t, "resend", provider)
		require.Equal(t, "re_secret", apiKey)
		var domain, defaultsJSON string
		require.NoError(t, db.Raw("SELECT domain, defaults_json FROM mailer_profiles WHERE id = 4").Row().Scan(&domain, &defaultsJSON))
		require.Equal(t, "mg.example.com", domain)
		require.JSONEq(t, `{"tag":"legacy"}`, defaultsJSON)
	})

	t.Run("keeps a legacy provider row already converted to SMTP", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacyMailerProfile{}, &legacyForm{}, &partiallySimplifiedEmailDelivery{}))
		require.NoError(t, db.Create(&legacyMailerProfile{
			ID: 1, Name: "Converted", Provider: "resend", APIKey: "obsolete",
			DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com", SMTPPort: 587, SMTPEncryption: "starttls",
		}).Error)
		require.NoError(t, db.Create(&legacyForm{
			ID: 1, PublicID: "converted", Name: "Converted", Slug: "converted", Token: "converted-token", AllowedOrigins: "*",
		}).Error)
		mailerID := uint(1)
		recipient := "owner@example.com"
		require.NoError(t, db.Create(&partiallySimplifiedEmailDelivery{
			ID: 1, FormID: 1, Enabled: true, MailerProfileID: &mailerID, Recipient: &recipient,
			OverridesJSON: `{"to":"obsolete@example.com"}`,
		}).Error)

		require.NoError(t, Migrate(db))
		require.NoError(t, Migrate(db))

		var delivery forms.EmailDelivery
		require.NoError(t, db.First(&delivery, 1).Error)
		require.True(t, delivery.Enabled)
		require.Equal(t, recipient, delivery.Recipient)
	})

	t.Run("keeps only legacy email routes with an effective recipient", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
		require.NoError(t, err)
		require.NoError(t, db.AutoMigrate(&legacyMailerProfile{}, &legacyForm{}, &partiallySimplifiedEmailDelivery{}))
		require.NoError(t, db.Create(&legacyMailerProfile{
			ID: 1, Name: "SMTP", Provider: "smtp", DefaultFromEmail: "sender@example.com",
			SMTPHost: "smtp.example.com", SMTPPort: 587,
		}).Error)
		require.NoError(t, db.Create(&[]legacyForm{
			{ID: 1, PublicID: "missing-recipient", Name: "Missing recipient", Slug: "missing-recipient", Token: "missing-recipient-token", AllowedOrigins: "*"},
			{ID: 2, PublicID: "override-recipient", Name: "Override recipient", Slug: "override-recipient", Token: "override-recipient-token", AllowedOrigins: "*"},
		}).Error)
		mailerID := uint(1)
		emptyRecipient := ""
		require.NoError(t, db.Create(&[]partiallySimplifiedEmailDelivery{
			{ID: 1, FormID: 1, Enabled: true, MailerProfileID: &mailerID, Recipient: &emptyRecipient, OverridesJSON: `{}`},
			{ID: 2, FormID: 2, Enabled: true, MailerProfileID: &mailerID, Recipient: &emptyRecipient, OverridesJSON: `{"to":"owner@example.com"}`},
		}).Error)

		require.NoError(t, Migrate(db))

		var missing, migrated forms.EmailDelivery
		require.NoError(t, db.First(&missing, 1).Error)
		require.False(t, missing.Enabled)
		require.NoError(t, db.First(&migrated, 2).Error)
		require.True(t, migrated.Enabled)
		require.Equal(t, "owner@example.com", migrated.Recipient)
	})

	t.Run("guards legacy profile deletion rules", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(t.TempDir()+"/legacy.db?_foreign_keys=on"), &gorm.Config{})
		require.NoError(t, err)
		for _, statement := range []string{
			`CREATE TABLE captcha_profiles (
				id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, provider TEXT, secret_key TEXT,
				site_keys_json TEXT, policy_json TEXT
			)`,
			`CREATE TABLE mailer_profiles (
				id INTEGER PRIMARY KEY, name TEXT NOT NULL UNIQUE, provider TEXT, default_from_name TEXT,
				default_from_email TEXT, smtp_host TEXT, smtp_port INTEGER, smtp_username TEXT,
				smtp_password TEXT, smtp_encryption TEXT
			)`,
			`CREATE TABLE forms (
				id INTEGER PRIMARY KEY, public_id TEXT NOT NULL UNIQUE, name TEXT NOT NULL, slug TEXT NOT NULL UNIQUE,
				token TEXT NOT NULL UNIQUE, allowed_origins TEXT, captcha_profile_id INTEGER, captcha_overrides_json TEXT,
				CONSTRAINT fk_forms_captcha_profile FOREIGN KEY (captcha_profile_id)
					REFERENCES captcha_profiles(id) ON DELETE SET NULL
			)`,
			`CREATE TABLE email_deliveries (
				id INTEGER PRIMARY KEY, form_id INTEGER NOT NULL UNIQUE, enabled NUMERIC,
				mailer_profile_id INTEGER, overrides_json TEXT,
				CONSTRAINT fk_email_deliveries_mailer_profile FOREIGN KEY (mailer_profile_id)
					REFERENCES mailer_profiles(id) ON DELETE SET NULL
			)`,
		} {
			require.NoError(t, db.Exec(statement).Error)
		}

		require.NoError(t, Migrate(db))

		mailer, err := integrations.CreateMailerProfile(migrationLogger, db, integrations.MailerProfileParams{
			Name: "Protected", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)
		captcha, err := integrations.CreateCaptchaProfile(migrationLogger, db, integrations.CaptchaProfileParams{
			Name: "Protected", SiteKey: "site", SecretKey: "secret",
		})
		require.NoError(t, err)
		form, err := forms.Create(migrationLogger, db, forms.CreateParams{
			Name: "Protected", Slug: "protected", AllowedOrigins: "*",
			MailerProfileID: &mailer.ID, EmailEnabled: true, EmailRecipient: "owner@example.com",
			CaptchaProfileID: &captcha.ID,
		})
		require.NoError(t, err)

		require.ErrorIs(t, integrations.DeleteMailerProfile(migrationLogger, db, mailer.ID), integrations.ErrProfileInUse)
		require.ErrorIs(t, integrations.DeleteCaptchaProfile(migrationLogger, db, captcha.ID), integrations.ErrProfileInUse)
		require.NoError(t, forms.DeleteForm(migrationLogger, db, "", form.ID))
		require.NoError(t, integrations.DeleteMailerProfile(migrationLogger, db, mailer.ID))
		require.NoError(t, integrations.DeleteCaptchaProfile(migrationLogger, db, captcha.ID))
	})

	t.Run("creates indexes for chronological queries", func(t *testing.T) {
		db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
			NowFunc: func() time.Time { return time.Now().UTC() },
		})
		require.NoError(t, err)
		require.NoError(t, Migrate(db))

		indexes := []struct {
			model any
			name  string
		}{
			{&forms.Submission{}, "idx_submissions_created_at"},
			{&forms.Submission{}, "idx_submissions_form_created"},
			{&forms.WebhookEvent{}, "idx_webhook_events_submission_created"},
			{&forms.WebhookEvent{}, "idx_webhook_events_queue"},
			{&forms.WebhookEvent{}, "idx_webhook_events_status"},
			{&forms.EmailEvent{}, "idx_email_events_submission_created"},
			{&forms.EmailEvent{}, "idx_email_events_queue"},
			{&forms.EmailEvent{}, "idx_email_events_status"},
		}

		for _, index := range indexes {
			t.Run(index.name, func(t *testing.T) {
				require.True(t, db.Migrator().HasIndex(index.model, index.name))
			})
		}

		redundantIndexes := []struct {
			model any
			name  string
		}{
			{&forms.Submission{}, "idx_submissions_form_id"},
			{&forms.WebhookEvent{}, "idx_webhook_events_submission_id"},
			{&forms.EmailEvent{}, "idx_email_events_submission_id"},
		}

		for _, index := range redundantIndexes {
			t.Run("omits "+index.name, func(t *testing.T) {
				require.False(t, db.Migrator().HasIndex(index.model, index.name))
			})
		}

		t.Run("omits removed generic fields and tables", func(t *testing.T) {
			require.True(t, db.Migrator().HasColumn(&forms.EmailDelivery{}, "recipient"))
			require.False(t, db.Migrator().HasColumn(&forms.EmailDelivery{}, "overrides_json"))
			require.False(t, db.Migrator().HasColumn(&forms.Form{}, "captcha_overrides_json"))
			require.False(t, db.Migrator().HasColumn(&forms.Submission{}, "ip_hash"))
			require.False(t, db.Migrator().HasTable("settings"))
		})
	})
}
