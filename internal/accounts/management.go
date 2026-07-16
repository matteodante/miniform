package accounts

import (
	"fmt"
	"log/slog"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

// GetAdmin returns the operator account managed by this Miniform instance.
func GetAdmin(db *gorm.DB) (*User, error) {
	var user User
	if err := db.Order("id ASC").First(&user).Error; err != nil {
		return nil, err
	}
	return &user, nil
}

// ResetPassword replaces an account password without requiring the old one.
// It is intended for local administrative recovery commands only.
func ResetPassword(logger *slog.Logger, db *gorm.DB, email, newPassword string) error {
	if len(newPassword) < 8 {
		return ErrWeakPassword
	}

	user, err := FindByEmail(db, email)
	if err != nil {
		return err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash replacement password: %w", err)
	}

	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		return tx.Model(&User{}).
			Where("id = ?", user.ID).
			Update("password_hash", string(hash)).Error
	}); err != nil {
		return fmt.Errorf("reset account password: %w", err)
	}

	return nil
}

// ListSettings returns application settings ordered by key.
func ListSettings(db *gorm.DB) ([]Settings, error) {
	var settings []Settings
	if err := db.Where("key <> ''").Order("key ASC").Find(&settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

// DeleteSetting removes a database-backed application setting.
func DeleteSetting(logger *slog.Logger, db *gorm.DB, key string) error {
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		result := tx.Where("key = ?", key).Delete(&Settings{})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return gorm.ErrRecordNotFound
		}
		return nil
	})
}
