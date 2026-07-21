package jobs

import (
	"context"
	"log/slog"
	"time"

	"github.com/karloscodes/cartridge"
	"gorm.io/gorm"
)

type Runner struct {
	logger     *slog.Logger
	db         *gorm.DB
	interval   time.Duration
	processors []cartridge.Processor
}

func NewRunner(logger *slog.Logger, db *gorm.DB, interval time.Duration, processors ...cartridge.Processor) *Runner {
	return &Runner{logger: logger, db: db, interval: interval, processors: processors}
}

func (runner *Runner) Run(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	ticker := time.NewTicker(runner.interval)
	defer ticker.Stop()

	for {
		runner.process(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (runner *Runner) process(ctx context.Context) {
	jobCtx := &cartridge.JobContext{Context: ctx, Logger: runner.logger, DB: runner.db}
	for _, processor := range runner.processors {
		if ctx.Err() != nil {
			return
		}
		if err := processor.ProcessBatch(jobCtx); err != nil {
			runner.logger.Error("job processor failed", slog.Any("error", err))
		}
	}
}
