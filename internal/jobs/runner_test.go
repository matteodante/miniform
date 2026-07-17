package jobs

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/karloscodes/cartridge"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

type recordingProcessor struct {
	processed chan struct{}
}

func (processor *recordingProcessor) ProcessBatch(*cartridge.JobContext) error {
	processor.processed <- struct{}{}
	return nil
}

func TestRunner(t *testing.T) {
	t.Run("runs immediately and stops when canceled", func(t *testing.T) {
		processor := &recordingProcessor{processed: make(chan struct{}, 1)}
		runner := NewRunner(
			slog.New(slog.NewTextHandler(io.Discard, nil)),
			testsupport.SetupTestDB(t),
			time.Hour,
			processor,
		)
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			runner.Run(ctx)
			close(done)
		}()

		select {
		case <-processor.processed:
		case <-time.After(time.Second):
			t.Fatal("runner did not process immediately")
		}
		cancel()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("runner did not stop after cancellation")
		}
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	})
}
