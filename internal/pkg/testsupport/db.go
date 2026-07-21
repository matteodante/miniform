package testsupport

import (
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/matteodante/miniform/internal/database"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

var databaseSequence atomic.Uint64

func SetupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:miniform-test-%d?mode=memory&cache=shared&_foreign_keys=on&_txlock=immediate", databaseSequence.Add(1))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	require.NoError(t, err)
	require.NoError(t, database.Migrate(db))
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })

	return db
}
