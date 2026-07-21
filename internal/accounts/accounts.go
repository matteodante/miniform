package accounts

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/dbtxn"
	"github.com/matteodante/miniform/internal/pkg/sqliteerr"
)

const (
	DefaultAdminEmail     = "admin@miniform.local"
	minimumPasswordLength = 8
	timingEqualizerHash   = "$2y$10$GHFFo8zcQOUi53r/u8jJ9.vZ/yz0zvsGz89xg1lmNRW0cFrwvSY.e"
)

var (
	ErrInvalidCredentials = errors.New("invalid email or password")
	ErrUserNotFound       = errors.New("user not found")
	ErrWeakPassword       = errors.New("password must be at least 8 characters")
	ErrPasswordMismatch   = errors.New("current password is incorrect")
	ErrPasswordUnchanged  = errors.New("new password must be different")
	ErrMissingFields      = errors.New("required fields are missing")
	ErrDuplicateEmail     = errors.New("email is already in use")
	ErrInvalidEmail       = errors.New("email is not valid")
)

type User struct {
	ID                     uint       `gorm:"primaryKey"`
	Email                  string     `gorm:"size:255;uniqueIndex;not null"`
	PasswordHash           string     `gorm:"size:255;not null"`
	PasswordChangeRequired bool       `gorm:"not null;default:false"`
	LastLoginAt            *time.Time `gorm:"index"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ActiveSession struct {
	TokenHash string    `gorm:"primaryKey;size:64"`
	UserID    uint      `gorm:"index;not null"`
	ExpiresAt time.Time `gorm:"index;not null"`
	CreatedAt time.Time
}

func RequiresPasswordChange(db *gorm.DB) bool {
	admin, err := GetAdmin(db)
	return err == nil && admin.PasswordChangeRequired
}

func FindByEmail(db *gorm.DB, email string) (*User, error) {
	return loadUser(db.Where("email = ?", normalizeEmail(email)))
}

func FindByID(db *gorm.DB, id uint) (*User, error) {
	return loadUser(db.Where("id = ?", id))
}

func GetAdmin(db *gorm.DB) (*User, error) {
	return loadUser(db.Order("id ASC"))
}

func EnsureAdmin(logger *slog.Logger, db *gorm.DB, password string, markLoggedIn bool, announce func() error) (bool, error) {
	var users int64
	if err := db.Model(&User{}).Count(&users).Error; err != nil {
		return false, fmt.Errorf("count accounts: %w", err)
	}
	if users != 0 {
		return false, nil
	}
	if len(password) < minimumPasswordLength {
		return false, ErrWeakPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return false, fmt.Errorf("hash initial password: %w", err)
	}
	admin := &User{
		Email:                  DefaultAdminEmail,
		PasswordHash:           string(hash),
		PasswordChangeRequired: !markLoggedIn,
	}
	if markLoggedIn {
		now := time.Now().UTC()
		admin.LastLoginAt = &now
	}
	created := false
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		created = false
		if err := tx.Model(&User{}).Count(&users).Error; err != nil {
			return err
		}
		if users != 0 {
			return nil
		}
		if err := tx.Create(admin).Error; err != nil {
			return err
		}
		if announce != nil {
			if err := announce(); err != nil {
				return fmt.Errorf("announce initial credentials: %w", err)
			}
		}
		created = true
		return nil
	}); err != nil {
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, fmt.Errorf("create initial admin: %w", err)
	}
	return created, nil
}

func Authenticate(logger *slog.Logger, db *gorm.DB, email, password string) (*User, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return nil, ErrMissingFields
	}

	user, lookupErr := FindByEmail(db, email)
	hash := timingEqualizerHash
	if lookupErr == nil {
		hash = user.PasswordHash
	} else if !errors.Is(lookupErr, ErrUserNotFound) {
		return nil, fmt.Errorf("authenticate account: %w", lookupErr)
	}

	passwordOK := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
	if lookupErr != nil || !passwordOK {
		return nil, ErrInvalidCredentials
	}

	now := time.Now().UTC()
	if err := dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		return tx.Model(&User{}).Where("id = ?", user.ID).Update("last_login_at", now).Error
	}); err != nil {
		return nil, fmt.Errorf("record account login: %w", err)
	}
	user.LastLoginAt = &now

	return user, nil
}

func ChangeEmail(logger *slog.Logger, db *gorm.DB, currentEmail, newEmail, currentPassword string) error {
	currentEmail = normalizeEmail(currentEmail)
	newEmail = normalizeEmail(newEmail)
	if !isMailbox(newEmail) {
		return ErrInvalidEmail
	}

	user, err := FindByEmail(db, currentEmail)
	if err != nil {
		return err
	}
	if !passwordMatches(user.PasswordHash, currentPassword) {
		return ErrPasswordMismatch
	}
	if currentEmail == newEmail {
		return nil
	}

	err = dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", user.ID).Update("email", newEmail).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", user.ID).Delete(&ActiveSession{}).Error
	})
	if isUniqueViolation(err) {
		return ErrDuplicateEmail
	}
	if err != nil {
		return fmt.Errorf("change account email: %w", err)
	}
	return nil
}

func ChangePassword(logger *slog.Logger, db *gorm.DB, email, currentPassword, newPassword string) error {
	if len(newPassword) < minimumPasswordLength {
		return ErrWeakPassword
	}

	user, err := FindByEmail(db, email)
	if err != nil {
		return err
	}
	if !passwordMatches(user.PasswordHash, currentPassword) {
		return ErrPasswordMismatch
	}
	if passwordMatches(user.PasswordHash, newPassword) {
		return ErrPasswordUnchanged
	}
	if err := storePassword(logger, db, user.ID, newPassword); err != nil {
		return fmt.Errorf("change account password: %w", err)
	}
	return nil
}

func ResetPassword(logger *slog.Logger, db *gorm.DB, email, newPassword string) error {
	if len(newPassword) < minimumPasswordLength {
		return ErrWeakPassword
	}
	user, err := FindByEmail(db, email)
	if err != nil {
		return err
	}
	if err := storePassword(logger, db, user.ID, newPassword); err != nil {
		return fmt.Errorf("reset account password: %w", err)
	}
	return nil
}

func loadUser(query *gorm.DB) (*User, error) {
	var user User
	if err := query.First(&user).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrUserNotFound
		}
		return nil, fmt.Errorf("load account: %w", err)
	}
	return &user, nil
}

func storePassword(logger *slog.Logger, db *gorm.DB, userID uint, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", userID).Updates(map[string]any{
			"password_hash":            string(hash),
			"password_change_required": false,
		}).Error; err != nil {
			return err
		}
		return tx.Where("user_id = ?", userID).Delete(&ActiveSession{}).Error
	})
}

func RegisterSession(logger *slog.Logger, db *gorm.DB, userID uint, token string, expiresAt time.Time) error {
	token = strings.TrimSpace(token)
	if userID == 0 || token == "" || expiresAt.IsZero() {
		return errors.New("invalid session registration")
	}
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		now := time.Now().UTC()
		if err := tx.Where("expires_at <= ?", now).Delete(&ActiveSession{}).Error; err != nil {
			return err
		}
		return tx.Save(&ActiveSession{
			TokenHash: sessionTokenHash(token), UserID: userID,
			ExpiresAt: expiresAt.UTC(), CreatedAt: now,
		}).Error
	})
}

func IsSessionActive(db *gorm.DB, userID uint, token string, now time.Time) (bool, error) {
	if strings.TrimSpace(token) == "" {
		return false, nil
	}
	var count int64
	err := db.Model(&ActiveSession{}).
		Where("token_hash = ? AND user_id = ? AND expires_at > ?", sessionTokenHash(token), userID, now.UTC()).
		Count(&count).Error
	return count != 0, err
}

func RevokeSession(logger *slog.Logger, db *gorm.DB, token string) error {
	if strings.TrimSpace(token) == "" {
		return nil
	}
	return dbtxn.WithRetry(logger, db, func(tx *gorm.DB) error {
		return tx.Where("token_hash = ?", sessionTokenHash(token)).Delete(&ActiveSession{}).Error
	})
}

func SessionExpiresAt(token string) (time.Time, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return time.Time{}, errors.New("invalid session token")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return time.Time{}, errors.New("invalid session payload")
	}
	var data struct {
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(payload, &data); err != nil || data.ExpiresAt.IsZero() {
		return time.Time{}, errors.New("invalid session data")
	}
	return data.ExpiresAt, nil
}

func sessionTokenHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

func passwordMatches(hash, password string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func isMailbox(email string) bool {
	address, err := mail.ParseAddress(email)
	return err == nil && address.Address == email
}

func isUniqueViolation(err error) bool {
	return sqliteerr.IsUniqueConstraint(err)
}
