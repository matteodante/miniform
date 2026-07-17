package dbtxn

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRetrySupport(t *testing.T) {
	t.Run("caps exponential pause", func(t *testing.T) {
		first := retryPause(1)
		late := retryPause(maxAttempts - 1)

		assert.GreaterOrEqual(t, first, basePause)
		assert.LessOrEqual(t, first, basePause+basePause/5)
		assert.GreaterOrEqual(t, late, maxPause)
		assert.LessOrEqual(t, late, maxPause+maxPause/5)
	})
}
