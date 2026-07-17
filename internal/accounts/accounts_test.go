package accounts_test

import (
	"errors"
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

	t.Run("keeps a temporary account restricted until its password changes", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created, err := accounts.EnsureAdmin(logger, db, "password123", false, nil)
		require.NoError(t, err)
		require.True(t, created)

		assert.True(t, accounts.RequiresPasswordChange(db))
		user, err := accounts.Authenticate(logger, db, "  ADMIN@MINIFORM.LOCAL ", "password123")
		require.NoError(t, err)
		assert.True(t, user.PasswordChangeRequired)
		assert.NotNil(t, user.LastLoginAt)
		assert.True(t, accounts.RequiresPasswordChange(db))

		assert.ErrorIs(t,
			accounts.ChangePassword(logger, db, user.Email, "password123", "password123"),
			accounts.ErrPasswordUnchanged,
		)
		require.NoError(t, accounts.ChangePassword(logger, db, user.Email, "password123", "replacement-password"))
		user, err = accounts.Authenticate(logger, db, accounts.DefaultAdminEmail, "replacement-password")
		require.NoError(t, err)
		assert.False(t, user.PasswordChangeRequired)
		assert.False(t, accounts.RequiresPasswordChange(db))
	})

	t.Run("authenticates and records the login", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		createUser(t, db, accounts.DefaultAdminEmail, "password123")

		user, err := accounts.Authenticate(logger, db, accounts.DefaultAdminEmail, "password123")
		require.NoError(t, err)
		assert.NotNil(t, user.LastLoginAt)
	})

	t.Run("tracks the password requirement after an email change", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		created, err := accounts.EnsureAdmin(logger, db, "password123", false, nil)
		require.NoError(t, err)
		require.True(t, created)

		require.NoError(t, accounts.ChangeEmail(logger, db, accounts.DefaultAdminEmail, "operator@example.com", "password123"))
		assert.True(t, accounts.RequiresPasswordChange(db))
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

	t.Run("does not persist initial credentials that could not be announced", func(t *testing.T) {
		db := testsupport.SetupTestDB(t)
		announcementErr := errors.New("output unavailable")

		created, err := accounts.EnsureAdmin(logger, db, "password123", false, func() error {
			return announcementErr
		})

		assert.False(t, created)
		assert.ErrorIs(t, err, announcementErr)
		_, err = accounts.GetAdmin(db)
		assert.ErrorIs(t, err, accounts.ErrUserNotFound)
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
