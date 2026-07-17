package integrations

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
	"github.com/matteodante/miniform/internal/pkg/sqliteerr"
)

type MailerProfile struct {
	ID               uint   `gorm:"primaryKey"`
	Name             string `gorm:"size:255;not null;uniqueIndex"`
	DefaultFromName  string `gorm:"size:255"`
	DefaultFromEmail string `gorm:"size:255"`
	SMTPHost         string `gorm:"size:255"`
	SMTPPort         int    `gorm:"default:587"`
	SMTPUsername     string `gorm:"size:255"`
	SMTPPassword     string `gorm:"type:text"`
	SMTPEncryption   string `gorm:"size:20;default:'starttls'"`
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type CaptchaProfile struct {
	ID        uint   `gorm:"primaryKey"`
	Name      string `gorm:"size:255;not null;uniqueIndex"`
	SiteKey   string `gorm:"type:text;not null"`
	SecretKey string `gorm:"type:text;not null"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

type ValidationError struct {
	Field   string
	Message string
}

func (err *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", err.Field, err.Message)
}

func ListMailerProfiles(db *gorm.DB) ([]MailerProfile, error) {
	return listProfiles[MailerProfile](db)
}

func ListCaptchaProfiles(db *gorm.DB) ([]CaptchaProfile, error) {
	return listProfiles[CaptchaProfile](db)
}

func GetMailerProfileByID(db *gorm.DB, id uint) (*MailerProfile, error) {
	return getProfile[MailerProfile](db, id)
}

func GetCaptchaProfileByID(db *gorm.DB, id uint) (*CaptchaProfile, error) {
	return getProfile[CaptchaProfile](db, id)
}

func listProfiles[T any](db *gorm.DB) ([]T, error) {
	var profiles []T
	err := db.Order("name").Find(&profiles).Error
	return profiles, err
}

func getProfile[T any](db *gorm.DB, id uint) (*T, error) {
	var profile T
	if err := db.First(&profile, id).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func profileName(raw string) (string, error) {
	name := strings.TrimSpace(raw)
	if name == "" {
		return "", &ValidationError{Field: "name", Message: "Name is required"}
	}
	return name, nil
}

func persistProfile(logger *slog.Logger, db *gorm.DB, action string, write func(*gorm.DB) error) error {
	err := dbtxn.WithRetry(logger, db, write)
	if err == nil {
		return nil
	}
	if uniqueViolation(err) {
		return &ValidationError{Field: "name", Message: "A profile with this name already exists"}
	}
	if logger != nil {
		logger.Error("profile write failed", slog.String("action", action), slog.Any("error", err))
	}
	return fmt.Errorf("failed to %s profile: %w", action, err)
}

func deleteProfile[T any](logger *slog.Logger, db *gorm.DB, id uint) error {
	return persistProfile(logger, db, "delete", func(tx *gorm.DB) error {
		result := tx.Delete(new(T), id)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}

func uniqueViolation(err error) bool {
	return sqliteerr.IsUniqueConstraint(err)
}
