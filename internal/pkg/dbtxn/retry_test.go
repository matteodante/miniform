package dbtxn

import (
	"context"
	"testing"
	"time"

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

	t.Run("stops waiting when the database context is cancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		started := time.Now()
		err := waitForRetry(ctx, time.Minute)

		assert.ErrorIs(t, err, context.Canceled)
		assert.Less(t, time.Since(started), 100*time.Millisecond)
	})
}
