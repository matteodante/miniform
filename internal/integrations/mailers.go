package integrations

import (
	"log/slog"
	"net/mail"
	"strings"

	"gorm.io/gorm"
)

type MailerProfileParams struct {
	Name             string
	DefaultFromName  string
	DefaultFromEmail string
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
	host := strings.TrimSpace(params.SMTPHost)
	if host == "" {
		return nil, &ValidationError{Field: "smtp_host", Message: "SMTP host is required"}
	}
	fromEmail := strings.TrimSpace(params.DefaultFromEmail)
	address, err := mail.ParseAddress(fromEmail)
	if err != nil || address.Address != fromEmail {
		return nil, &ValidationError{Field: "default_from_email", Message: "From email must be a valid address"}
	}
	return &MailerProfile{
		Name:             name,
		DefaultFromName:  strings.TrimSpace(params.DefaultFromName),
		DefaultFromEmail: fromEmail,
		SMTPHost:         host,
		SMTPPort:         port,
		SMTPUsername:     params.SMTPUsername,
		SMTPPassword:     params.SMTPPassword,
		SMTPEncryption:   encryption,
	}, nil
}

func (profile *MailerProfile) mailerValues() map[string]any {
	return map[string]any{
		"name":              profile.Name,
		"default_from_name": profile.DefaultFromName, "default_from_email": profile.DefaultFromEmail,
		"smtp_host": profile.SMTPHost,
		"smtp_port": profile.SMTPPort, "smtp_username": profile.SMTPUsername,
		"smtp_password": profile.SMTPPassword, "smtp_encryption": profile.SMTPEncryption,
	}
}
