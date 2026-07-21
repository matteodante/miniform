package database

import (
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Manager owns Miniform's SQLite connection pool.
type Manager struct {
	path         string
	maxOpenConns int
	maxIdleConns int

	mu     sync.Mutex
	db     *gorm.DB
	closed bool
}

func NewManager(path string, maxOpenConns, maxIdleConns int) *Manager {
	if maxOpenConns <= 0 {
		maxOpenConns = 1
	}
	if maxIdleConns <= 0 {
		maxIdleConns = 1
	}
	return &Manager{path: path, maxOpenConns: maxOpenConns, maxIdleConns: maxIdleConns}
}

func (manager *Manager) Connect() (*gorm.DB, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.closed {
		return nil, errors.New("SQLite database manager is closed")
	}
	if manager.db != nil {
		return manager.db.Session(&gorm.Session{}), nil
	}

	db, err := gorm.Open(sqlite.Open(manager.dsn()), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
		NowFunc:                func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("open SQLite database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("access SQLite connection pool: %w", err)
	}
	sqlDB.SetMaxOpenConns(manager.maxOpenConns)
	sqlDB.SetMaxIdleConns(manager.maxIdleConns)
	manager.db = db
	return db.Session(&gorm.Session{}), nil
}

func (manager *Manager) GetConnection() *gorm.DB {
	db, err := manager.Connect()
	if err != nil {
		return nil
	}
	return db
}

func (manager *Manager) CheckpointWAL(mode string) error {
	mode = strings.ToUpper(mode)
	switch mode {
	case "PASSIVE", "FULL", "RESTART", "TRUNCATE":
	default:
		return fmt.Errorf("invalid WAL checkpoint mode %q", mode)
	}
	db, err := manager.Connect()
	if err != nil {
		return err
	}
	if err := db.Exec("PRAGMA wal_checkpoint(" + mode + ")").Error; err != nil {
		return fmt.Errorf("checkpoint SQLite WAL: %w", err)
	}
	return nil
}

func (manager *Manager) Close() error {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if manager.closed {
		return nil
	}
	manager.closed = true
	if manager.db == nil {
		return nil
	}
	sqlDB, err := manager.db.DB()
	if err != nil {
		return fmt.Errorf("access SQLite connection pool: %w", err)
	}
	manager.db = nil
	if err := sqlDB.Close(); err != nil {
		return fmt.Errorf("close SQLite database: %w", err)
	}
	return nil
}

func (manager *Manager) dsn() string {
	query := url.Values{
		"_busy_timeout": []string{"5000"},
		"_foreign_keys": []string{"on"},
		"_journal_mode": []string{"WAL"},
		"_synchronous":  []string{"NORMAL"},
		"_txlock":       []string{"immediate"},
	}
	path := url.PathEscape(filepath.ToSlash(manager.path))
	path = strings.ReplaceAll(path, "%2F", "/")
	return "file:" + path + "?" + query.Encode()
}
