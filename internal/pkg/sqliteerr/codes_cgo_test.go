//go:build cgo

package sqliteerr

import (
	"fmt"
	"testing"

	"github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
)

func TestDriverCodes(t *testing.T) {
	t.Run("classifies wrapped SQLite errors by code", func(t *testing.T) {
		assert.True(t, IsContention(fmt.Errorf("write: %w", sqlite3.Error{Code: sqlite3.ErrBusy})))
		assert.True(t, IsUniqueConstraint(sqlite3.Error{ExtendedCode: sqlite3.ErrConstraintUnique}))
		assert.True(t, IsUniqueOrPrimaryConstraint(sqlite3.Error{ExtendedCode: sqlite3.ErrConstraintPrimaryKey}))
		assert.True(t, IsForeignKeyConstraint(sqlite3.Error{ExtendedCode: sqlite3.ErrConstraintForeignKey}))
		assert.False(t, IsUniqueConstraint(sqlite3.Error{Code: sqlite3.ErrLocked}))
	})
}
