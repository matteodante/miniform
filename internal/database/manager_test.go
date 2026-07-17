package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/forms"
)

func TestManager(t *testing.T) {
	t.Run("does not reopen after an explicit close", func(t *testing.T) {
		manager := NewManager(filepath.Join(t.TempDir(), "miniform.db"), 1, 1)
		t.Cleanup(func() { require.NoError(t, manager.Close()) })

		first, err := manager.Connect()
		require.NoError(t, err)
		require.NoError(t, Migrate(first))
		require.NoError(t, manager.Close())

		reopened, err := manager.Connect()
		assert.Nil(t, reopened)
		assert.ErrorContains(t, err, "closed")
		require.NoError(t, manager.Close())
	})

	t.Run("enforces foreign keys on every pooled connection", func(t *testing.T) {
		manager := NewManager(filepath.Join(t.TempDir(), "miniform.db"), 2, 2)
		t.Cleanup(func() { require.NoError(t, manager.Close()) })

		db, err := manager.Connect()
		require.NoError(t, err)
		require.NoError(t, Migrate(db))

		sqlDB, err := db.DB()
		require.NoError(t, err)
		first, err := sqlDB.Conn(context.Background())
		require.NoError(t, err)
		defer first.Close()
		second, err := sqlDB.Conn(context.Background())
		require.NoError(t, err)
		defer second.Close()

		for _, connection := range []*sql.Conn{first, second} {
			var enabled int
			require.NoError(t, connection.QueryRowContext(context.Background(), "PRAGMA foreign_keys").Scan(&enabled))
			assert.Equal(t, 1, enabled)
		}
		require.NoError(t, first.Close())
		require.NoError(t, second.Close())

		err = db.Create(&forms.Submission{FormID: 999, DataJSON: `{}`}).Error
		assert.Error(t, err)
	})
}
