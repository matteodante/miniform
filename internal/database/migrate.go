package database

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

const legacyConfigurationMigration = "simplify-core-configuration-v1"

var migrationLogger = slog.New(slog.DiscardHandler)

type migrationRecord struct {
	Name      string `gorm:"primaryKey;size:191"`
	AppliedAt time.Time
}

func (migrationRecord) TableName() string { return "miniform_migrations" }

//nolint:gocyclo // Schema detection and ordered compatibility steps must remain explicit and atomic.
func Migrate(db *gorm.DB) error {
	legacyCaptchaConfiguration := db.Migrator().HasTable("captcha_profiles") &&
		db.Migrator().HasColumn("captcha_profiles", "site_keys_json")
	legacyCaptchaHasSiteKey := legacyCaptchaConfiguration &&
		db.Migrator().HasColumn("captcha_profiles", "site_key")
	legacyEmailConfiguration := db.Migrator().HasTable("email_deliveries") &&
		db.Migrator().HasColumn("email_deliveries", "overrides_json")
	legacyCaptchaOverrides := db.Migrator().HasTable("forms") &&
		db.Migrator().HasColumn("forms", "captcha_overrides_json")
	legacyMailerProviders := db.Migrator().HasTable("mailer_profiles") &&
		db.Migrator().HasColumn("mailer_profiles", "provider")
	legacyPasswordState := db.Migrator().HasTable(&accounts.User{}) &&
		!db.Migrator().HasColumn(&accounts.User{}, "PasswordChangeRequired")
	if legacyPasswordState {
		if err := dbtxn.WithRetry(migrationLogger, db, func(tx *gorm.DB) error {
			if err := tx.AutoMigrate(&accounts.User{}); err != nil {
				return err
			}
			return tx.Model(&accounts.User{}).
				Where("last_login_at IS NULL").
				Update("password_change_required", true).Error
		}); err != nil {
			return fmt.Errorf("migrate legacy account password state: %w", err)
		}
	}
	if err := dbtxn.WithRetry(migrationLogger, db, func(tx *gorm.DB) error {
		if err := dropUniqueEmailDeliveryFormIndex(tx); err != nil {
			return err
		}
		if err := tx.AutoMigrate(Models()...); err != nil {
			return err
		}
		if err := backfillEmailDeliveryConfiguration(tx); err != nil {
			return err
		}
		if tx.Migrator().HasColumn("forms", "use_sdk") {
			if err := tx.Exec(`ALTER TABLE forms DROP COLUMN use_sdk`).Error; err != nil {
				return fmt.Errorf("drop obsolete form SDK preference: %w", err)
			}
		}
		return ensureProfileDeleteGuards(tx)
	}); err != nil {
		return fmt.Errorf("migrate application schema: %w", err)
	}
	if legacyCaptchaConfiguration || legacyEmailConfiguration || legacyMailerProviders {
		if err := dbtxn.WithRetry(migrationLogger, db, func(tx *gorm.DB) error {
			explicitCaptchaProfiles := make(map[uint]struct{})
			if legacyCaptchaHasSiteKey {
				var ids []uint
				if err := tx.Table("captcha_profiles").
					Where("TRIM(COALESCE(site_key, '')) <> ''").
					Pluck("id", &ids).Error; err != nil {
					return fmt.Errorf("read simplified captcha profiles: %w", err)
				}
				for _, id := range ids {
					explicitCaptchaProfiles[id] = struct{}{}
				}
			}
			if legacyCaptchaConfiguration {
				if err := migrateCaptchaConfiguration(tx); err != nil {
					return err
				}
			}
			if legacyEmailConfiguration {
				if err := migrateEmailConfiguration(tx); err != nil {
					return err
				}
			}
			applied, err := migrationApplied(tx, legacyConfigurationMigration)
			if err != nil {
				return err
			}
			if !applied {
				if legacyCaptchaConfiguration {
					if err := migrateLegacyCaptchaForms(tx, legacyCaptchaOverrides, explicitCaptchaProfiles); err != nil {
						return err
					}
				}
				if legacyEmailConfiguration || legacyMailerProviders {
					if err := disableIncompleteLegacyEmailDeliveries(tx); err != nil {
						return err
					}
				}
				if err := tx.Create(&migrationRecord{Name: legacyConfigurationMigration, AppliedAt: time.Now().UTC()}).Error; err != nil {
					return fmt.Errorf("record legacy configuration migration: %w", err)
				}
			}
			return nil
		}); err != nil {
			return fmt.Errorf("preserve simplified configuration: %w", err)
		}
	}
	if err := forms.ReconcileDeliveryRecords(migrationLogger, db); err != nil {
		return fmt.Errorf("restore form delivery records: %w", err)
	}
	return nil
}

func dropUniqueEmailDeliveryFormIndex(tx *gorm.DB) error {
	if !tx.Migrator().HasTable("email_deliveries") {
		return nil
	}
	var indexes []struct {
		Name   string `gorm:"column:name"`
		Unique int    `gorm:"column:unique"`
	}
	if err := tx.Raw(`PRAGMA index_list('email_deliveries')`).Scan(&indexes).Error; err != nil {
		return fmt.Errorf("inspect email delivery indexes: %w", err)
	}
	for _, index := range indexes {
		if index.Unique != 0 && index.Name == "idx_email_deliveries_form_id" {
			if err := tx.Migrator().DropIndex(&forms.EmailDelivery{}, index.Name); err != nil {
				return fmt.Errorf("allow multiple email deliveries per form: %w", err)
			}
		}
	}
	return nil
}

func backfillEmailDeliveryConfiguration(tx *gorm.DB) error {
	if err := tx.Model(&forms.EmailDelivery{}).
		Where("TRIM(COALESCE(name, '')) = ''").
		Update("name", forms.DefaultEmailDeliveryName).Error; err != nil {
		return fmt.Errorf("name existing email deliveries: %w", err)
	}
	if err := tx.Model(&forms.EmailDelivery{}).
		Where("TRIM(COALESCE(recipient_source, '')) = ''").
		Update("recipient_source", forms.EmailRecipientStatic).Error; err != nil {
		return fmt.Errorf("set existing email recipient sources: %w", err)
	}
	if err := tx.Model(&forms.EmailDelivery{}).
		Where("TRIM(COALESCE(reply_to_source, '')) = ''").
		Update("reply_to_source", forms.EmailReplyToNone).Error; err != nil {
		return fmt.Errorf("set existing email reply-to sources: %w", err)
	}
	if err := tx.Exec(`UPDATE email_events
		SET email_delivery_id = (
			SELECT email_deliveries.id
			FROM email_deliveries
			JOIN submissions ON submissions.form_id = email_deliveries.form_id
			WHERE submissions.id = email_events.submission_id
			ORDER BY email_deliveries.id
			LIMIT 1
		)
		WHERE email_delivery_id IS NULL`).Error; err != nil {
		return fmt.Errorf("link existing email events to deliveries: %w", err)
	}
	return nil
}

func ensureProfileDeleteGuards(tx *gorm.DB) error {
	if err := ensureProfileDeleteGuard(
		tx, "forms", "captcha_profiles",
		`DROP TRIGGER IF EXISTS miniform_restrict_captcha_profile_delete`,
		`CREATE TRIGGER IF NOT EXISTS miniform_restrict_captcha_profile_delete
		BEFORE DELETE ON captcha_profiles
		FOR EACH ROW WHEN EXISTS (SELECT 1 FROM forms WHERE captcha_profile_id = OLD.id)
		BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed'); END`,
	); err != nil {
		return fmt.Errorf("protect captcha profile references: %w", err)
	}
	if err := ensureProfileDeleteGuard(
		tx, "email_deliveries", "mailer_profiles",
		`DROP TRIGGER IF EXISTS miniform_restrict_mailer_profile_delete`,
		`CREATE TRIGGER IF NOT EXISTS miniform_restrict_mailer_profile_delete
		BEFORE DELETE ON mailer_profiles
		FOR EACH ROW WHEN EXISTS (SELECT 1 FROM email_deliveries WHERE mailer_profile_id = OLD.id)
		BEGIN SELECT RAISE(ABORT, 'foreign key constraint failed'); END`,
	); err != nil {
		return fmt.Errorf("protect mailer profile references: %w", err)
	}
	return nil
}

func ensureProfileDeleteGuard(tx *gorm.DB, childTable, parentTable, dropSQL, createSQL string) error {
	var onDelete string
	if err := tx.Raw(
		`SELECT on_delete FROM pragma_foreign_key_list(?) WHERE "table" = ? LIMIT 1`,
		childTable, parentTable,
	).Scan(&onDelete).Error; err != nil {
		return err
	}
	if onDelete == "RESTRICT" || onDelete == "NO ACTION" {
		return tx.Exec(dropSQL).Error
	}
	return tx.Exec(createSQL).Error
}

func migrationApplied(tx *gorm.DB, name string) (bool, error) {
	var count int64
	if err := tx.Model(&migrationRecord{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return false, fmt.Errorf("read migration marker: %w", err)
	}
	return count != 0, nil
}

func migrateCaptchaConfiguration(tx *gorm.DB) error {
	var records []struct {
		ID           uint
		SiteKey      string
		SiteKeysJSON string
		PolicyJSON   string
	}
	if err := tx.Table("captcha_profiles").
		Select("id, site_key, site_keys_json, policy_json").
		Find(&records).Error; err != nil {
		return fmt.Errorf("read legacy captcha configuration: %w", err)
	}
	for _, record := range records {
		if strings.TrimSpace(record.SiteKey) != "" {
			continue
		}
		siteKey := legacyCaptchaSiteKey(record.PolicyJSON, record.SiteKeysJSON)
		if siteKey == "" {
			continue
		}
		if err := tx.Table("captcha_profiles").Where("id = ?", record.ID).Update("site_key", siteKey).Error; err != nil {
			return fmt.Errorf("migrate captcha profile %d: %w", record.ID, err)
		}
	}
	return nil
}

func legacyCaptchaSiteKey(policyJSON, siteKeysJSON string) string {
	var policy struct {
		SiteKey string `json:"site_key"`
	}
	if json.Unmarshal([]byte(policyJSON), &policy) == nil {
		if siteKey := strings.TrimSpace(policy.SiteKey); siteKey != "" {
			return siteKey
		}
	}
	var siteKeys []struct {
		SiteKey string `json:"site_key"`
	}
	if json.Unmarshal([]byte(siteKeysJSON), &siteKeys) == nil {
		for _, candidate := range siteKeys {
			if siteKey := strings.TrimSpace(candidate.SiteKey); siteKey != "" {
				return siteKey
			}
		}
	}
	return ""
}

type legacyCaptchaSettings struct {
	Required bool
	Action   string
	SiteKey  string
}

type legacyCaptchaSettingsJSON struct {
	Required *bool  `json:"required"`
	Action   string `json:"action"`
	SiteKey  string `json:"site_key"`
}

type legacyCaptchaSiteKeyRecord struct {
	HostPattern string `json:"host_pattern"`
	SiteKey     string `json:"site_key"`
}

func migrateLegacyCaptchaForms(tx *gorm.DB, hasOverrides bool, explicitProfiles map[uint]struct{}) error {
	overridesColumn := "'' AS captcha_overrides_json"
	if hasOverrides {
		overridesColumn = "forms.captcha_overrides_json"
	}
	var records []struct {
		FormID               uint
		AllowedOrigins       string
		CaptchaOverridesJSON string
		ProfileID            uint
		ProfileName          string
		Provider             string
		SecretKey            string
		SiteKey              string
		SiteKeysJSON         string
		PolicyJSON           string
	}
	selection := strings.Join([]string{
		"forms.id AS form_id", "forms.allowed_origins", overridesColumn,
		"captcha_profiles.id AS profile_id", "captcha_profiles.name AS profile_name",
		"captcha_profiles.provider", "captcha_profiles.secret_key", "captcha_profiles.site_key",
		"captcha_profiles.site_keys_json", "captcha_profiles.policy_json",
	}, ", ")
	if err := tx.Table("forms").
		Select(selection).
		Joins("JOIN captcha_profiles ON captcha_profiles.id = forms.captcha_profile_id").
		Find(&records).Error; err != nil {
		return fmt.Errorf("read legacy form captcha configuration: %w", err)
	}
	for _, record := range records {
		if _, explicit := explicitProfiles[record.ProfileID]; explicit && strings.TrimSpace(record.SecretKey) != "" {
			continue
		}
		settings := resolveLegacyCaptchaSettings(record.PolicyJSON, record.CaptchaOverridesJSON)
		if !strings.EqualFold(strings.TrimSpace(record.Provider), "turnstile") ||
			!settings.Required || !strings.EqualFold(settings.Action, integrations.TurnstileAction) {
			if err := tx.Table("forms").Where("id = ?", record.FormID).Update("captcha_profile_id", nil).Error; err != nil {
				return fmt.Errorf("disable incompatible legacy captcha on form %d: %w", record.FormID, err)
			}
			continue
		}
		effectiveSiteKey := settings.SiteKey
		if effectiveSiteKey == "" {
			effectiveSiteKey = chooseLegacyCaptchaSiteKey(record.AllowedOrigins, record.SiteKeysJSON)
		}
		if effectiveSiteKey == "" {
			effectiveSiteKey = strings.TrimSpace(record.SiteKey)
		}
		if effectiveSiteKey == "" || strings.TrimSpace(record.SecretKey) == "" {
			if err := tx.Table("forms").Where("id = ?", record.FormID).Update("captcha_profile_id", nil).Error; err != nil {
				return fmt.Errorf("disable incomplete legacy captcha on form %d: %w", record.FormID, err)
			}
			continue
		}
		if effectiveSiteKey == strings.TrimSpace(record.SiteKey) {
			continue
		}
		cloneID, err := legacyCaptchaClone(tx, record.ProfileID, record.FormID, record.ProfileName, record.SecretKey, effectiveSiteKey)
		if err != nil {
			return err
		}
		if err := tx.Table("forms").Where("id = ?", record.FormID).Update("captcha_profile_id", cloneID).Error; err != nil {
			return fmt.Errorf("assign migrated captcha profile to form %d: %w", record.FormID, err)
		}
	}
	return nil
}

func resolveLegacyCaptchaSettings(policyJSON, overridesJSON string) legacyCaptchaSettings {
	settings := legacyCaptchaSettings{Required: true, Action: integrations.TurnstileAction}
	for _, raw := range []string{policyJSON, overridesJSON} {
		var next legacyCaptchaSettingsJSON
		if json.Unmarshal([]byte(raw), &next) != nil {
			continue
		}
		if next.Required != nil {
			settings.Required = *next.Required
		}
		if value := strings.TrimSpace(next.Action); value != "" {
			settings.Action = value
		}
		if value := strings.TrimSpace(next.SiteKey); value != "" {
			settings.SiteKey = value
		}
	}
	return settings
}

func chooseLegacyCaptchaSiteKey(allowedOrigins, rawSiteKeys string) string {
	var keys []legacyCaptchaSiteKeyRecord
	if json.Unmarshal([]byte(rawSiteKeys), &keys) != nil {
		return ""
	}
	if value := strings.TrimSpace(allowedOrigins); value != "" && value != "*" {
		for _, origin := range strings.Split(value, ",") {
			host := legacyOriginHost(origin)
			if host == "" {
				continue
			}
			for _, key := range keys {
				if strings.TrimSpace(key.SiteKey) != "" && (&forms.Form{AllowedOrigins: key.HostPattern}).IsOriginAllowed(host) {
					return strings.TrimSpace(key.SiteKey)
				}
			}
		}
	}
	for _, key := range keys {
		if strings.TrimSpace(key.HostPattern) == "*" && strings.TrimSpace(key.SiteKey) != "" {
			return strings.TrimSpace(key.SiteKey)
		}
	}
	for _, key := range keys {
		if strings.TrimSpace(key.SiteKey) != "" {
			return strings.TrimSpace(key.SiteKey)
		}
	}
	return ""
}

func legacyOriginHost(value string) string {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "*."))
	if value == "" {
		return ""
	}
	if !strings.Contains(value, "://") {
		value = "//" + value
	}
	parsed, err := url.Parse(value)
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
}

func legacyCaptchaClone(tx *gorm.DB, profileID, formID uint, profileName, secretKey, siteKey string) (uint, error) {
	name := fmt.Sprintf("Legacy captcha %d/%d · %.180s", profileID, formID, strings.TrimSpace(profileName))
	var existing integrations.CaptchaProfile
	result := tx.Where("name = ?", name).Limit(1).Find(&existing)
	if result.Error != nil {
		return 0, fmt.Errorf("inspect legacy captcha clone: %w", result.Error)
	}
	if result.RowsAffected == 1 {
		if existing.SiteKey == siteKey && existing.SecretKey == secretKey {
			return existing.ID, nil
		}
		return 0, fmt.Errorf("legacy captcha clone %q already exists with different credentials", name)
	}
	clone := &integrations.CaptchaProfile{Name: name, SiteKey: siteKey, SecretKey: secretKey}
	if err := tx.Create(clone).Error; err != nil {
		return 0, fmt.Errorf("create legacy captcha clone for form %d: %w", formID, err)
	}
	return clone.ID, nil
}

func disableIncompleteLegacyEmailDeliveries(tx *gorm.DB) error {
	result := tx.Exec(`UPDATE email_deliveries
		SET enabled = FALSE
		WHERE TRIM(COALESCE(recipient, '')) = ''
		   OR mailer_profile_id IS NULL
		   OR mailer_profile_id IN (
			SELECT id FROM mailer_profiles
			WHERE TRIM(COALESCE(smtp_host, '')) = ''
			   OR TRIM(COALESCE(default_from_email, '')) = ''
			   OR COALESCE(smtp_port, 0) NOT BETWEEN 1 AND 65535
			   OR LOWER(COALESCE(NULLIF(TRIM(smtp_encryption), ''), 'starttls')) NOT IN ('starttls', 'tls', 'none')
		)`)
	if result.Error != nil {
		return fmt.Errorf("disable incomplete legacy email deliveries: %w", result.Error)
	}
	return nil
}

func migrateEmailConfiguration(tx *gorm.DB) error {
	var records []struct {
		ID            uint
		Recipient     string
		OverridesJSON string
	}
	if err := tx.Table("email_deliveries").
		Select("id, recipient, overrides_json").
		Where("TRIM(COALESCE(recipient, '')) = ''").
		Find(&records).Error; err != nil {
		return fmt.Errorf("read legacy email configuration: %w", err)
	}
	for _, record := range records {
		var overrides struct {
			To string `json:"to"`
		}
		if json.Unmarshal([]byte(record.OverridesJSON), &overrides) != nil {
			continue
		}
		recipient := strings.TrimSpace(overrides.To)
		if recipient == "" {
			continue
		}
		if err := tx.Table("email_deliveries").Where("id = ?", record.ID).Update("recipient", recipient).Error; err != nil {
			return fmt.Errorf("migrate email delivery %d: %w", record.ID, err)
		}
	}
	return nil
}

func Models() []any {
	return []any{
		&migrationRecord{},
		&accounts.User{},
		&integrations.MailerProfile{}, &integrations.CaptchaProfile{},
		&forms.Form{}, &forms.EmailDelivery{}, &forms.WebhookDelivery{},
		&forms.Submission{}, &forms.SubmissionFile{},
		&forms.EmailEvent{}, &forms.WebhookEvent{},
	}
}
