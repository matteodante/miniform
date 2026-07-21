package dbtxn

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/pkg/sqliteerr"
)

const (
	maxAttempts = 10
	basePause   = 100 * time.Millisecond
	maxPause    = 5 * time.Second
)

// WithRetry runs one SQLite transaction, retrying only lock contention.
func WithRetry(logger *slog.Logger, db *gorm.DB, fn func(tx *gorm.DB) error) error {
	transactionDB := db.Session(&gorm.Session{SkipDefaultTransaction: true})

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastErr = transactionDB.Transaction(fn)
		if lastErr == nil {
			return nil
		}
		if !sqliteerr.IsContention(lastErr) {
			return lastErr
		}
		if attempt == maxAttempts {
			break
		}

		pause := retryPause(attempt)
		logger.Info("sqlite write waiting for lock",
			slog.Int("next_attempt", attempt+1),
			slog.Duration("pause", pause),
			slog.Any("error", lastErr),
		)
		ctx := transactionDB.Statement.Context
		if ctx == nil {
			ctx = context.Background()
		}
		if err := waitForRetry(ctx, pause); err != nil {
			return err
		}
	}

	return fmt.Errorf("sqlite write still locked after %d attempts: %w", maxAttempts, lastErr)
}

func waitForRetry(ctx context.Context, pause time.Duration) error {
	timer := time.NewTimer(pause)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func retryPause(attempt int) time.Duration {
	pause := basePause << (attempt - 1)
	if pause > maxPause {
		pause = maxPause
	}
	jitter, err := rand.Int(rand.Reader, big.NewInt(int64(pause/5)+1))
	if err != nil {
		return pause
	}
	return pause + time.Duration(jitter.Int64())
}
