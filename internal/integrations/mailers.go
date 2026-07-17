package integrations

import (
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

type MailerProfileParams struct {
	Name             string
	Provider         string
	APIKey           string
	Domain           string
	DefaultFromName  string
	DefaultFromEmail string
	DefaultsJSON     string
	SMTPHost         string
	SMTPPort         int
	SMTPUsername     string
	SMTPPassword     string
	SMTPEncryption   string
}

func CreateMailerProfile(logger *slog.Logger, db *gorm.DB, params MailerProfileParams) (*MailerProfile, error) {
	profile, err := params.mailerProfile()
	if err != nil {
		return nil, err
	}
	if err := persistProfile(logger, db, "create", func(tx *gorm.DB) error {
		return tx.Create(profile).Error
	}); err != nil {
		return nil, err
	}
	return profile, nil
}

func UpdateMailerProfile(logger *slog.Logger, db *gorm.DB, id uint, params MailerProfileParams) (*MailerProfile, error) {
	profile, err := params.mailerProfile()
	if err != nil {
		return nil, err
	}
	if _, err := GetMailerProfileByID(db, id); err != nil {
		return nil, err
	}
	if err := persistProfile(logger, db, "update", func(tx *gorm.DB) error {
		return tx.Model(&MailerProfile{ID: id}).Updates(profile.mailerValues()).Error
	}); err != nil {
		return nil, err
	}
	return GetMailerProfileByID(db, id)
}

func DeleteMailerProfile(logger *slog.Logger, db *gorm.DB, id uint) error {
	return deleteProfile[MailerProfile](logger, db, id)
}

func (params MailerProfileParams) mailerProfile() (*MailerProfile, error) {
	name, err := profileName(params.Name)
	if err != nil {
		return nil, err
	}
	provider := strings.ToLower(strings.TrimSpace(params.Provider))
	if provider == "" {
		provider = "smtp"
	}
	if provider != "smtp" && provider != "mailgun" {
		return nil, &ValidationError{Field: "provider", Message: "Provider must be smtp or mailgun"}
	}
	port := params.SMTPPort
	if port == 0 {
		port = 587
	}
	if port < 1 || port > 65535 {
		return nil, &ValidationError{Field: "smtp_port", Message: "SMTP port must be between 1 and 65535"}
	}
	encryption := strings.ToLower(strings.TrimSpace(params.SMTPEncryption))
	if encryption == "" {
		encryption = "starttls"
	}
	if encryption != "starttls" && encryption != "tls" && encryption != "none" {
		return nil, &ValidationError{Field: "smtp_encryption", Message: "SMTP encryption must be starttls, tls, or none"}
	}
	defaults, err := jsonObjectField(params.DefaultsJSON, "defaults_json", "Defaults must be a JSON object")
	if err != nil {
		return nil, err
	}
	return &MailerProfile{
		Name:             name,
		Provider:         provider,
		APIKey:           strings.TrimSpace(params.APIKey),
		Domain:           strings.TrimSpace(params.Domain),
		DefaultFromName:  strings.TrimSpace(params.DefaultFromName),
		DefaultFromEmail: strings.TrimSpace(params.DefaultFromEmail),
		DefaultsJSON:     defaults,
		SMTPHost:         strings.TrimSpace(params.SMTPHost),
		SMTPPort:         port,
		SMTPUsername:     strings.TrimSpace(params.SMTPUsername),
		SMTPPassword:     strings.TrimSpace(params.SMTPPassword),
		SMTPEncryption:   encryption,
	}, nil
}

func (profile *MailerProfile) mailerValues() map[string]any {
	return map[string]any{
		"name": profile.Name, "provider": profile.Provider,
		"api_key": profile.APIKey, "domain": profile.Domain,
		"default_from_name": profile.DefaultFromName, "default_from_email": profile.DefaultFromEmail,
		"defaults_json": profile.DefaultsJSON, "smtp_host": profile.SMTPHost,
		"smtp_port": profile.SMTPPort, "smtp_username": profile.SMTPUsername,
		"smtp_password": profile.SMTPPassword, "smtp_encryption": profile.SMTPEncryption,
	}
}
