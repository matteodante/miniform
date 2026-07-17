package accounts

import (
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
)

type Settings struct {
	ID        uint      `gorm:"primaryKey"`
	Key       string    `gorm:"uniqueIndex;not null"`
	Value     string    `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null;autoCreateTime:milli"`
	UpdatedAt time.Time `gorm:"not null;autoUpdateTime:milli"`
}

func GetSetting(db *gorm.DB, key string) (string, error) {
	var setting Settings
	if err := db.Where("key = ?", key).Take(&setting).Error; err != nil {
		return "", fmt.Errorf("get setting %q: %w", key, err)
	}
	return setting.Value, nil
}

func ListSettings(db *gorm.DB) ([]Settings, error) {
	var settings []Settings
	if err := db.Where("key <> ''").Order("key ASC").Find(&settings).Error; err != nil {
		return nil, fmt.Errorf("list settings: %w", err)
	}
	return settings, nil
}

func SetSetting(db *gorm.DB, logger *slog.Logger, key, value string) error {
	setting := Settings{Key: key, Value: value}
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		return tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "key"}},
			DoUpdates: clause.AssignmentColumns([]string{"value", "updated_at"}),
		}).Create(&setting).Error
	}); err != nil {
		return fmt.Errorf("set setting %q: %w", key, err)
	}
	return nil
}

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
