package integrations

import (
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

const TurnstileAction = "submit"

type CaptchaProfileParams struct {
	Name      string
	SiteKey   string
	SecretKey string
}

func CreateCaptchaProfile(logger *slog.Logger, db *gorm.DB, params CaptchaProfileParams) (*CaptchaProfile, error) {
	profile, err := params.captchaProfile()
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

func UpdateCaptchaProfile(logger *slog.Logger, db *gorm.DB, id uint, params CaptchaProfileParams) (*CaptchaProfile, error) {
	profile, err := params.captchaProfile()
	if err != nil {
		return nil, err
	}
	if _, err := GetCaptchaProfileByID(db, id); err != nil {
		return nil, err
	}
	if err := persistProfile(logger, db, "update", func(tx *gorm.DB) error {
		return tx.Model(&CaptchaProfile{ID: id}).Updates(map[string]any{
			"name": profile.Name, "site_key": profile.SiteKey, "secret_key": profile.SecretKey,
		}).Error
	}); err != nil {
		return nil, err
	}
	return GetCaptchaProfileByID(db, id)
}

func DeleteCaptchaProfile(logger *slog.Logger, db *gorm.DB, id uint) error {
	return deleteProfile[CaptchaProfile](logger, db, id)
}

func (params CaptchaProfileParams) captchaProfile() (*CaptchaProfile, error) {
	name, err := profileName(params.Name)
	if err != nil {
		return nil, err
	}
	siteKey := strings.TrimSpace(params.SiteKey)
	if siteKey == "" {
		return nil, &ValidationError{Field: "site_key", Message: "Site key is required"}
	}
	secretKey := strings.TrimSpace(params.SecretKey)
	if secretKey == "" {
		return nil, &ValidationError{Field: "secret_key", Message: "Secret key is required"}
	}
	return &CaptchaProfile{Name: name, SiteKey: siteKey, SecretKey: secretKey}, nil
}
