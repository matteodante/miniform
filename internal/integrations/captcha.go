package integrations

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

type CaptchaProfileParams struct {
	Name         string
	Provider     string
	SecretKey    string
	SiteKeysJSON string
	PolicyJSON   string
}

type CaptchaSettings struct {
	Required bool
	Action   string
	Theme    string
	Language string
	Widget   string
	Size     string
	SiteKey  string
}

type captchaSettingsJSON struct {
	Required *bool  `json:"required"`
	Action   string `json:"action"`
	Theme    string `json:"theme"`
	Language string `json:"language"`
	Widget   string `json:"widget"`
	Size     string `json:"size"`
	SiteKey  string `json:"site_key"`
}

type captchaSiteKey struct {
	HostPattern string `json:"host_pattern"`
	SiteKey     string `json:"site_key"`
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
			"name": profile.Name, "provider": profile.Provider, "secret_key": profile.SecretKey,
			"site_keys_json": profile.SiteKeysJSON, "policy_json": profile.PolicyJSON,
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
	provider := strings.ToLower(strings.TrimSpace(params.Provider))
	if provider == "" {
		provider = "turnstile"
	}
	if provider != "turnstile" {
		return nil, &ValidationError{Field: "provider", Message: "Provider must be turnstile"}
	}
	siteKeys, err := captchaSiteKeysJSON(params.SiteKeysJSON)
	if err != nil {
		return nil, err
	}
	policy := strings.TrimSpace(params.PolicyJSON)
	if err := ValidateCaptchaSettingsJSON(policy); err != nil {
		return nil, &ValidationError{Field: "policy_json", Message: err.Error()}
	}
	return &CaptchaProfile{
		Name: name, Provider: provider, SecretKey: strings.TrimSpace(params.SecretKey),
		SiteKeysJSON: siteKeys, PolicyJSON: policy,
	}, nil
}

func ResolveCaptchaSettings(policyJSON, overridesJSON string) CaptchaSettings {
	settings := CaptchaSettings{Required: true, Action: "submit", Theme: "auto"}
	for _, raw := range []string{policyJSON, overridesJSON} {
		var next captchaSettingsJSON
		if json.Unmarshal([]byte(raw), &next) != nil {
			continue
		}
		if next.Required != nil {
			settings.Required = *next.Required
		}
		copySetting(&settings.Action, next.Action)
		copySetting(&settings.Theme, next.Theme)
		copySetting(&settings.Language, next.Language)
		copySetting(&settings.Widget, next.Widget)
		copySetting(&settings.Size, next.Size)
		copySetting(&settings.SiteKey, next.SiteKey)
	}
	return settings
}

func ValidateCaptchaSettingsJSON(raw string) error {
	value := strings.TrimSpace(raw)
	if value == "" {
		return nil
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &object); err != nil || object == nil {
		return errors.New("captcha settings must be a JSON object")
	}
	var settings captchaSettingsJSON
	if err := json.Unmarshal([]byte(value), &settings); err != nil {
		return errors.New("captcha settings contain an invalid value type")
	}
	if action := strings.TrimSpace(settings.Action); action != "" && !validTurnstileAction(action) {
		return errors.New("captcha action must be at most 32 letters, numbers, underscores, or hyphens")
	}
	if theme := strings.TrimSpace(settings.Theme); theme != "" && theme != "auto" && theme != "light" && theme != "dark" {
		return errors.New("captcha theme must be auto, light, or dark")
	}
	if size := strings.TrimSpace(settings.Size); size != "" && size != "normal" && size != "flexible" && size != "compact" {
		return errors.New("captcha size must be normal, flexible, or compact")
	}
	return nil
}

func captchaSiteKeysJSON(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", nil
	}
	var keys []captchaSiteKey
	if err := json.Unmarshal([]byte(value), &keys); err != nil || keys == nil {
		return "", &ValidationError{Field: "site_keys_json", Message: "Site keys must be a JSON array"}
	}
	for _, key := range keys {
		if strings.TrimSpace(key.HostPattern) == "" || strings.TrimSpace(key.SiteKey) == "" {
			return "", &ValidationError{Field: "site_keys_json", Message: "Each site key needs host_pattern and site_key"}
		}
	}
	return value, nil
}

func copySetting(destination *string, value string) {
	if value = strings.TrimSpace(value); value != "" {
		*destination = value
	}
}

func validTurnstileAction(action string) bool {
	if len(action) == 0 || len(action) > 32 {
		return false
	}
	for _, character := range action {
		if character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' || character == '_' || character == '-' {
			continue
		}
		return false
	}
	return true
}
