package sqliteerr

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestContentionText(t *testing.T) {
	t.Run("recognizes driver messages without accepting unrelated errors", func(t *testing.T) {
		assert.True(t, IsContention(errors.New("DATABASE TABLE IS LOCKED")))
		assert.True(t, IsContention(errors.New("database is busy")))
		assert.False(t, IsContention(errors.New("permission denied")))
		assert.False(t, IsContention(nil))
	})
}
