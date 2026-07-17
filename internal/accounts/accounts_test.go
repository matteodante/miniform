package accounts_test

import (
	"io"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

func TestAccounts(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	t.Run("authenticates and records the login", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		createUser(t, db, accounts.DefaultAdminEmail, "password123")

		assert.True(t, accounts.IsFirstLoginPending(db))
		result, err := accounts.Authenticate(logger, db, "  ADMIN@MINIFORM.LOCAL ", "password123")
		require.NoError(t, err)
		assert.True(t, result.IsFirstLogin)
		assert.NotNil(t, result.User.LastLoginAt)
		assert.False(t, accounts.IsFirstLoginPending(db))

		result, err = accounts.Authenticate(logger, db, accounts.DefaultAdminEmail, "password123")
		require.NoError(t, err)
		assert.False(t, result.IsFirstLogin)
	})

	t.Run("does not disclose invalid accounts", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		createUser(t, db, "user@example.com", "correct-password")

		_, wrongPassword := accounts.Authenticate(logger, db, "user@example.com", "wrong-password")
		_, missingUser := accounts.Authenticate(logger, db, "missing@example.com", "wrong-password")
		_, missingField := accounts.Authenticate(logger, db, "", "wrong-password")

		assert.ErrorIs(t, wrongPassword, accounts.ErrInvalidCredentials)
		assert.ErrorIs(t, missingUser, accounts.ErrInvalidCredentials)
		assert.ErrorIs(t, missingField, accounts.ErrMissingFields)
	})

	t.Run("changes credentials", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		createUser(t, db, "old@example.com", "old-password")
		createUser(t, db, "taken@example.com", "other-password")

		require.NoError(t, accounts.ChangeEmail(logger, db, "old@example.com", " NEW@Example.com ", "old-password"))
		updated, err := accounts.FindByEmail(db, "new@example.com")
		require.NoError(t, err)
		assert.Equal(t, "new@example.com", updated.Email)

		assert.ErrorIs(t, accounts.ChangeEmail(logger, db, "new@example.com", "taken@example.com", "old-password"), accounts.ErrDuplicateEmail)
		assert.ErrorIs(t, accounts.ChangeEmail(logger, db, "new@example.com", "not-an-email", "old-password"), accounts.ErrInvalidEmail)
		assert.ErrorIs(t, accounts.ChangeEmail(logger, db, "new@example.com", "next@example.com", "wrong"), accounts.ErrPasswordMismatch)

		assert.ErrorIs(t, accounts.ChangePassword(logger, db, "new@example.com", "old-password", "short"), accounts.ErrWeakPassword)
		assert.ErrorIs(t, accounts.ChangePassword(logger, db, "new@example.com", "wrong", "new-password"), accounts.ErrPasswordMismatch)
		require.NoError(t, accounts.ChangePassword(logger, db, "NEW@example.com", "old-password", "new-password"))
		_, err = accounts.Authenticate(logger, db, "new@example.com", "new-password")
		assert.NoError(t, err)
	})

	t.Run("supports local password recovery", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created := createUser(t, db, "admin@example.com", "old-password")

		byID, err := accounts.FindByID(db, created.ID)
		require.NoError(t, err)
		assert.Equal(t, created.Email, byID.Email)
		admin, err := accounts.GetAdmin(db)
		require.NoError(t, err)
		assert.Equal(t, created.ID, admin.ID)

		require.NoError(t, accounts.ResetPassword(logger, db, created.Email, "replacement-password"))
		_, err = accounts.Authenticate(logger, db, created.Email, "replacement-password")
		assert.NoError(t, err)
	})

	t.Run("upserts and removes settings", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)

		require.NoError(t, accounts.SetSetting(db, logger, "theme", "dark"))
		require.NoError(t, accounts.SetSetting(db, logger, "theme", "light"))
		value, err := accounts.GetSetting(db, "theme")
		require.NoError(t, err)
		assert.Equal(t, "light", value)

		settings, err := accounts.ListSettings(db)
		require.NoError(t, err)
		assert.Len(t, settings, 1)
		require.NoError(t, accounts.DeleteSetting(logger, db, "theme"))
		assert.ErrorIs(t, accounts.DeleteSetting(logger, db, "theme"), gorm.ErrRecordNotFound)
	})
}

func createUser(t *testing.T, db *gorm.DB, email, password string) *accounts.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	require.NoError(t, err)

	user := &accounts.User{Email: email, PasswordHash: string(hash)}
	require.NoError(t, db.Create(user).Error)
	return user
}
