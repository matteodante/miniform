package server

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"gopkg.in/natefinch/lumberjack.v2"

	"github.com/matteodante/miniform/internal/config"
)

func NewLogger(cfg *config.Config) (*slog.Logger, io.Closer) {
	return newLogger(cfg, os.Stdout)
}

func newLogger(cfg *config.Config, stdout io.Writer) (*slog.Logger, io.Closer) {
	level := parseLogLevel(cfg.LogLevel)
	options := &slog.HandlerOptions{Level: level, AddSource: level == slog.LevelDebug}
	if !cfg.IsProduction() {
		return slog.New(slog.NewTextHandler(stdout, options)), nil
	}

	appName := cfg.AppName
	if appName == "" {
		appName = "app"
	}
	logsDirectory := cfg.LogsDirectory
	if logsDirectory == "" {
		logsDirectory = "logs"
	}
	maxSize := cfg.LogsMaxSizeMB
	if maxSize <= 0 {
		maxSize = 100
	}
	maxBackups := cfg.LogsMaxBackups
	if maxBackups <= 0 {
		maxBackups = 3
	}
	maxAge := cfg.LogsMaxAgeDays
	if maxAge <= 0 {
		maxAge = 28
	}

	output := &rotatingLogOutput{logger: &lumberjack.Logger{
		Filename:   filepath.Join(logsDirectory, appName+".log"),
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   true,
	}}
	return slog.New(slog.NewJSONHandler(io.MultiWriter(stdout, output), options)), output
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

type rotatingLogOutput struct {
	mu     sync.Mutex
	logger *lumberjack.Logger
	closed bool
}

func (output *rotatingLogOutput) Write(data []byte) (int, error) {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed {
		return 0, os.ErrClosed
	}
	return output.logger.Write(data)
}

func (output *rotatingLogOutput) Close() error {
	output.mu.Lock()
	defer output.mu.Unlock()
	if output.closed {
		return nil
	}
	output.closed = true
	return output.logger.Close()
}
