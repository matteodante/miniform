package internal

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

const rateLimitSweepInterval = time.Second

type rateLimitStorage struct {
	mu        sync.Mutex
	entries   map[string]rateLimitEntry
	nextSweep time.Time
}

type rateLimitEntry struct {
	value     []byte
	expiresAt time.Time
}

func newRateLimitStorage() *rateLimitStorage {
	return &rateLimitStorage{entries: make(map[string]rateLimitEntry)}
}

func (storage *rateLimitStorage) Get(key string) ([]byte, error) {
	if key == "" {
		return nil, nil
	}

	now := time.Now()
	storage.mu.Lock()
	entry, found := storage.entries[key]
	if found && !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
		delete(storage.entries, key)
		found = false
	}
	storage.mu.Unlock()
	if !found {
		return nil, nil
	}
	return bytes.Clone(entry.value), nil
}

func (storage *rateLimitStorage) Set(key string, value []byte, expiration time.Duration) error {
	if key == "" || len(value) == 0 {
		return nil
	}

	now := time.Now()
	entry := rateLimitEntry{value: bytes.Clone(value)}
	if expiration > 0 {
		entry.expiresAt = now.Add(expiration)
	}

	storage.mu.Lock()
	storage.sweepExpired(now)
	storage.entries[strings.Clone(key)] = entry
	storage.mu.Unlock()
	return nil
}

func (storage *rateLimitStorage) Delete(key string) error {
	storage.mu.Lock()
	delete(storage.entries, key)
	storage.mu.Unlock()
	return nil
}

func (storage *rateLimitStorage) Reset() error {
	storage.mu.Lock()
	storage.entries = make(map[string]rateLimitEntry)
	storage.nextSweep = time.Time{}
	storage.mu.Unlock()
	return nil
}

func (storage *rateLimitStorage) Close() error {
	return storage.Reset()
}

func (storage *rateLimitStorage) sweepExpired(now time.Time) {
	if now.Before(storage.nextSweep) {
		return
	}
	for key, entry := range storage.entries {
		if !entry.expiresAt.IsZero() && !now.Before(entry.expiresAt) {
			delete(storage.entries, key)
		}
	}
	storage.nextSweep = now.Add(rateLimitSweepInterval)
}
