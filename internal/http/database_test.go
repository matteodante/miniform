package http

import (
	"context"
	"errors"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

type requestDBManager struct {
	db  *gorm.DB
	err error
}

func (manager requestDBManager) GetConnection() *gorm.DB { return manager.db }
func (manager requestDBManager) Connect() (*gorm.DB, error) {
	return manager.db, manager.err
}

func TestRequestDB(t *testing.T) {
	t.Run("attaches the Fiber user context", func(t *testing.T) {
		app := fiber.New()
		fiberCtx := app.AcquireCtx(&fasthttp.RequestCtx{})
		t.Cleanup(func() { app.ReleaseCtx(fiberCtx) })

		requestCtx, cancel := context.WithCancel(context.Background())
		t.Cleanup(cancel)
		fiberCtx.SetUserContext(requestCtx)
		ctx := &cartridge.Context{Ctx: fiberCtx, DBManager: requestDBManager{db: testsupport.SetupTestDB(t)}}

		db, err := requestDB(ctx)
		require.NoError(t, err)
		assert.Same(t, requestCtx, db.Statement.Context)
	})

	t.Run("preserves connection errors", func(t *testing.T) {
		connectionErr := errors.New("database offline")
		ctx := &cartridge.Context{DBManager: requestDBManager{err: connectionErr}}

		_, err := requestDB(ctx)
		assert.ErrorIs(t, err, connectionErr)
	})

	t.Run("rejects a missing manager", func(t *testing.T) {
		_, err := requestDB(&cartridge.Context{})
		assert.EqualError(t, err, "request database manager is unavailable")
	})

	t.Run("rejects an unavailable connection", func(t *testing.T) {
		ctx := &cartridge.Context{DBManager: requestDBManager{}}

		_, err := requestDB(ctx)
		assert.EqualError(t, err, "connect request database: connection is unavailable")
	})
}
