package testsupport

import (
	"testing"
	"time"

	"github.com/matteodante/miniform/internal/database"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func SetupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))

	return db
}
