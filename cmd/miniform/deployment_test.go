package main

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/karloscodes/matcha"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentLifecycle(t *testing.T) {
	t.Run("requires a working Docker Engine without installing remote scripts", func(t *testing.T) {
		emptyPath := t.TempDir()
		t.Setenv("PATH", emptyPath)
		manager := matcha.New(matcha.Config{
			Name: "miniform", ConfigPath: filepath.Join(t.TempDir(), "matcha.yml"), DataDirBase: t.TempDir(),
		})

		err := runDeploymentCommand(manager, "install", strings.NewReader(""), &bytes.Buffer{})

		require.ErrorContains(t, err, "Docker Engine must be installed and running")
		assert.ErrorContains(t, err, "docs.docker.com/engine/install")
	})

	t.Run("times out a stuck direct Docker command", func(t *testing.T) {
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_SLEEP", "5")

		started := time.Now()
		err := runDockerCommand(25*time.Millisecond, io.Discard, io.Discard, "ps")

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Less(t, time.Since(started), 2*time.Second)
	})

	t.Run("refuses to install over a residual database without deployment configuration", func(t *testing.T) {
		root := t.TempDir()
		configPath := filepath.Join(root, "matcha.yml")
		manager := matcha.New(matcha.Config{Name: "miniform", ConfigPath: configPath, DataDirBase: root})
		residual := filepath.Join(manager.DataDir(), "storage", "archive", "inbox.data")
		writeDatabaseValue(t, residual, "existing")

		err := runDeploymentCommand(manager, "install", strings.NewReader(""), &bytes.Buffer{})
		require.ErrorContains(t, err, "existing Miniform database")
		assert.ErrorContains(t, err, residual)
	})

	t.Run("refuses to install over a running deployment", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "current")
		installFakeDocker(t)

		err := runDeploymentCommand(manager, "install", strings.NewReader(""), &bytes.Buffer{})
		require.ErrorContains(t, err, "already running")
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.NotContains(t, string(dockerLog), "stop")
	})

	t.Run("takes update and reload backups after the application stops", func(t *testing.T) {
		for _, command := range []string{"update", "reload"} {
			t.Run(command, func(t *testing.T) {
				manager, databasePath := testDeploymentManager(t)
				writeDatabaseValue(t, databasePath, "before-stop")
				afterStop := filepath.Join(manager.DataDir(), "after-stop.db")
				writeDatabaseValue(t, afterStop, "after-stop")
				installFakeDocker(t)
				t.Setenv("FAKE_DOCKER_DATABASE_PATH", databasePath)
				t.Setenv("FAKE_DOCKER_DATABASE_ON_STOP", afterStop)

				require.NoError(t, runDeploymentCommand(manager, command, strings.NewReader(""), &bytes.Buffer{}))
				dockerLog, err := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
				require.NoError(t, err)
				stopIndex := strings.Index(string(dockerLog), "stop --timeout 15 miniform")
				startIndex := strings.Index(string(dockerLog), "run ")
				require.NotEqual(t, -1, stopIndex)
				require.NotEqual(t, -1, startIndex)
				assert.Less(t, stopIndex, startIndex)
				backups, err := filepath.Glob(filepath.Join(manager.DataDir(), "backups", "backup_*.db"))
				require.NoError(t, err)
				require.Len(t, backups, 1)
				assert.Equal(t, "after-stop", readDatabaseValue(t, backups[0]))
			})
		}
	})

	t.Run("restarts the old container when the post-stop backup fails", func(t *testing.T) {
		manager, _ := testDeploymentManager(t)
		installFakeDocker(t)

		err := runDeploymentCommand(manager, "update", strings.NewReader(""), &bytes.Buffer{})
		require.ErrorContains(t, err, "inspect database")
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.Contains(t, string(dockerLog), "stop --timeout 15 miniform")
		assert.Contains(t, string(dockerLog), "start miniform")
	})

	t.Run("restarts the old container when the replacement cannot start", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "current")
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_FAIL_RUN", "1")

		err := runDeploymentCommand(manager, "update", strings.NewReader(""), &bytes.Buffer{})
		require.ErrorContains(t, err, "start application")
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.GreaterOrEqual(t, strings.Count(string(dockerLog), "rm -f miniform-next"), 2)
		assert.Contains(t, string(dockerLog), "start miniform")
	})

	t.Run("restores the database only after removing the replacement and before starting the old container", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "before")
		safetyBackup, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Now())
		require.NoError(t, err)
		writeDatabaseValue(t, databasePath, "failed")
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_FAIL_RUN", "1")
		t.Setenv("FAKE_DOCKER_DATABASE_DIR", filepath.Dir(databasePath))
		require.NoError(t, os.Chmod(filepath.Dir(databasePath), 0o500))
		t.Cleanup(func() { _ = os.Chmod(filepath.Dir(databasePath), 0o700) })

		require.NoError(t, rollbackRestore(manager, "miniform", databasePath, safetyBackup))
		require.NoError(t, os.Chmod(filepath.Dir(databasePath), 0o700))
		assert.Equal(t, "before", readDatabaseValue(t, databasePath))
	})

	t.Run("does not restore or restart when the failed replacement cannot be removed", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "before")
		safetyBackup, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Now())
		require.NoError(t, err)
		writeDatabaseValue(t, databasePath, "failed")
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_FAIL_RUN", "1")
		t.Setenv("FAKE_DOCKER_FAIL_RM", "1")

		err = rollbackRestore(manager, "miniform", databasePath, safetyBackup)
		require.ErrorContains(t, err, "remove failed replacement")
		assert.Equal(t, "failed", readDatabaseValue(t, databasePath))
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.NotContains(t, string(dockerLog), "start miniform")
	})

	t.Run("does not restart the old container when restoring its database fails", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "failed")
		invalidBackup := filepath.Join(manager.DataDir(), "backups", "invalid.db")
		require.NoError(t, os.MkdirAll(filepath.Dir(invalidBackup), 0o700))
		require.NoError(t, os.WriteFile(invalidBackup, []byte("not sqlite"), 0o600))
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_FAIL_RUN", "1")

		err := rollbackRestore(manager, "miniform", databasePath, invalidBackup)
		require.Error(t, err)
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.Contains(t, string(dockerLog), "rm -f miniform-next")
		assert.NotContains(t, string(dockerLog), "start miniform")
	})

	t.Run("propagates a failed compensating restart while stopping", func(t *testing.T) {
		manager, _ := testDeploymentManager(t)
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_ACTIVE", "miniform-next")
		t.Setenv("FAKE_DOCKER_FAIL_RENAME", "1")
		t.Setenv("FAKE_DOCKER_FAIL_START", "1")

		_, err := stopApplication(manager)
		require.ErrorContains(t, err, "restart previous container")
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.Contains(t, string(dockerLog), "rename miniform-next miniform")
		assert.Contains(t, string(dockerLog), "start miniform-next")
	})

	t.Run("preserves the exact next container when its replacement fails", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "current")
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_ACTIVE", "miniform-next")
		t.Setenv("FAKE_DOCKER_FAIL_RUN", "1")

		err := runDeploymentCommand(manager, "update", strings.NewReader(""), &bytes.Buffer{})
		require.ErrorContains(t, err, "start application")
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.Contains(t, string(dockerLog), "stop --timeout 15 miniform-next")
		assert.Contains(t, string(dockerLog), "rename miniform-next miniform")
		assert.Contains(t, string(dockerLog), "rename miniform miniform-next")
		assert.Contains(t, string(dockerLog), "start miniform-next")
	})

	t.Run("replaces a next container without running two application processes", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "current")
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_ACTIVE", "miniform-next")

		require.NoError(t, runDeploymentCommand(manager, "update", strings.NewReader(""), &bytes.Buffer{}))
		dockerLog, err := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, err)
		assert.Contains(t, string(dockerLog), "stop --timeout 15 miniform-next")
		assert.Contains(t, string(dockerLog), "rename miniform-next miniform")
		assert.NotContains(t, string(dockerLog), "start miniform-next")
	})

	t.Run("restores a selected backup only while the application is stopped", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "restored")
		selected, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(manager.DataDir(), "backups", "backup_notes.db"), []byte("not a snapshot"), 0o600))
		writeDatabaseValue(t, databasePath, "current")
		installFakeDocker(t)
		var output bytes.Buffer

		require.NoError(t, runDeploymentCommand(manager, "restore-db", strings.NewReader("1\n"), &output))
		assert.Contains(t, output.String(), filepath.Base(selected))
		assert.NotContains(t, output.String(), "backup_notes.db")
		assert.Equal(t, "restored", readDatabaseValue(t, databasePath))
		_, err = os.Stat(os.Getenv("FAKE_DOCKER_STOPPED"))
		require.NoError(t, err)
	})

	t.Run("rejects an invalid restore selection before stopping the application", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "current")
		_, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		installFakeDocker(t)

		err = runDeploymentCommand(manager, "restore-db", strings.NewReader("9\n"), &bytes.Buffer{})
		require.ErrorContains(t, err, "selection out of range")
		_, statErr := os.Stat(os.Getenv("FAKE_DOCKER_STOPPED"))
		assert.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("restores when the current database disappears after stop", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "restored")
		_, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		writeDatabaseValue(t, databasePath, "current")
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_DATABASE_PATH", databasePath)
		t.Setenv("FAKE_DOCKER_REMOVE_DATABASE_ON_STOP", "1")

		require.NoError(t, runDeploymentCommand(manager, "restore-db", strings.NewReader("1\n"), &bytes.Buffer{}))
		assert.Equal(t, "restored", readDatabaseValue(t, databasePath))
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.Contains(t, string(dockerLog), "stop --timeout 15 miniform")
		assert.NotContains(t, string(dockerLog), "start miniform")
	})

	t.Run("restores when the current database is already missing", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "restored")
		_, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, os.Remove(databasePath))
		installFakeDocker(t)

		require.NoError(t, runDeploymentCommand(manager, "restore-db", strings.NewReader("1\n"), &bytes.Buffer{}))
		assert.Equal(t, "restored", readDatabaseValue(t, databasePath))
	})

	t.Run("restores over a corrupt current database", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "restored")
		_, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(databasePath, []byte("not sqlite"), 0o600))
		installFakeDocker(t)

		require.NoError(t, runDeploymentCommand(manager, "restore-db", strings.NewReader("1\n"), &bytes.Buffer{}))
		assert.Equal(t, "restored", readDatabaseValue(t, databasePath))
	})

	t.Run("does not restart a corrupt database when the restored application cannot start", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "restored")
		_, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(databasePath, []byte("not sqlite"), 0o600))
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_FAIL_RUN", "1")

		err = runDeploymentCommand(manager, "restore-db", strings.NewReader("1\n"), &bytes.Buffer{})
		require.ErrorContains(t, err, "start application")
		assert.ErrorContains(t, err, "previous database was not valid")
		assert.Equal(t, "restored", readDatabaseValue(t, databasePath))
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.Contains(t, string(dockerLog), "rm -f miniform-next")
		assert.NotContains(t, string(dockerLog), "start miniform")
	})

	t.Run("restores the final stopped database state when the restored app cannot start", func(t *testing.T) {
		manager, databasePath := testDeploymentManager(t)
		writeDatabaseValue(t, databasePath, "restored")
		_, err := createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Date(2026, 7, 16, 10, 0, 0, 0, time.UTC))
		require.NoError(t, err)
		writeDatabaseValue(t, databasePath, "before-stop")
		afterStop := filepath.Join(manager.DataDir(), "after-stop.db")
		writeDatabaseValue(t, afterStop, "after-stop")
		installFakeDocker(t)
		t.Setenv("FAKE_DOCKER_FAIL_RUN", "1")
		t.Setenv("FAKE_DOCKER_DATABASE_PATH", databasePath)
		t.Setenv("FAKE_DOCKER_DATABASE_ON_STOP", afterStop)

		err = runDeploymentCommand(manager, "restore-db", strings.NewReader("1\n"), &bytes.Buffer{})
		require.ErrorContains(t, err, "start application")
		assert.Equal(t, "after-stop", readDatabaseValue(t, databasePath))
		dockerLog, readErr := os.ReadFile(os.Getenv("FAKE_DOCKER_LOG"))
		require.NoError(t, readErr)
		assert.Contains(t, string(dockerLog), "start miniform")
	})
}

func TestDeploymentDatabase(t *testing.T) {
	t.Run("backs up the selected database as a consistent snapshot", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "miniform.production.db")
		writeDatabaseValue(t, databasePath, "before")

		backupPath, err := createDatabaseBackup(databasePath, filepath.Join(root, "backups"), time.Date(2026, 7, 17, 10, 11, 12, 0, time.UTC))
		require.NoError(t, err)
		assert.Equal(t, filepath.Join(root, "backups", "backup_20260717_101112.db"), backupPath)

		writeDatabaseValue(t, databasePath, "after")
		assert.Equal(t, "before", readDatabaseValue(t, backupPath))
	})

	t.Run("opens database paths containing DSN punctuation literally", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "mini?form.db")
		writeDatabaseValue(t, databasePath, "current")

		backupPath, err := createDatabaseBackup(databasePath, filepath.Join(root, "backups"), time.Now())

		require.NoError(t, err)
		assert.FileExists(t, databasePath)
		assert.Equal(t, "current", readDatabaseValue(t, backupPath))
	})

	t.Run("keeps the three newest verified snapshots", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "miniform.production.db")
		writeDatabaseValue(t, databasePath, "current")
		start := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)
		for offset := range 4 {
			_, err := createDatabaseBackup(databasePath, filepath.Join(root, "backups"), start.Add(time.Duration(offset)*time.Second))
			require.NoError(t, err)
		}

		backups, err := filepath.Glob(filepath.Join(root, "backups", "backup_*.db"))
		require.NoError(t, err)
		assert.Len(t, backups, 3)
		assert.NotContains(t, backups, filepath.Join(root, "backups", "backup_20260717_100000.db"))
	})

	t.Run("does not collide with another backup from the same second", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "miniform.production.db")
		writeDatabaseValue(t, databasePath, "current")
		now := time.Date(2026, 7, 17, 10, 0, 0, 0, time.UTC)

		first, err := createDatabaseBackup(databasePath, filepath.Join(root, "backups"), now)
		require.NoError(t, err)
		second, err := createDatabaseBackup(databasePath, filepath.Join(root, "backups"), now)
		require.NoError(t, err)
		assert.NotEqual(t, first, second)
	})

	t.Run("resolves only the database persisted by the Miniform volume", func(t *testing.T) {
		tests := []struct {
			name        string
			environment map[string]string
			want        string
			wantError   string
		}{
			{name: "container defaults", want: "storage/miniform.production.db"},
			{name: "custom filename", environment: map[string]string{"MINIFORM_DATABASE_FILENAME": "inbox.sqlite"}, want: "storage/inbox.production.sqlite"},
			{name: "absolute filename", environment: map[string]string{"MINIFORM_DATABASE_FILENAME": "/tmp/inbox.db"}, wantError: "use MINIFORM_DATABASE_PATH"},
			{name: "parent filename", environment: map[string]string{"MINIFORM_DATABASE_FILENAME": "../inbox.db"}, wantError: "use MINIFORM_DATABASE_PATH"},
			{name: "nested filename", environment: map[string]string{"MINIFORM_DATABASE_FILENAME": "archive/inbox.db"}, wantError: "use MINIFORM_DATABASE_PATH"},
			{name: "dot filename", environment: map[string]string{"MINIFORM_DATABASE_FILENAME": "."}, wantError: "use MINIFORM_DATABASE_PATH"},
			{name: "parent directory filename", environment: map[string]string{"MINIFORM_DATABASE_FILENAME": ".."}, wantError: "use MINIFORM_DATABASE_PATH"},
			{name: "explicit persisted path", environment: map[string]string{"MINIFORM_DATABASE_PATH": "/app/storage/archive/inbox.db"}, want: "storage/archive/inbox.db"},
			{name: "path outside volume", environment: map[string]string{"MINIFORM_DATABASE_PATH": "/tmp/inbox.db"}, wantError: "outside /app/storage"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				root := t.TempDir()
				configPath := filepath.Join(root, "matcha.yml")
				require.NoError(t, matcha.SaveAppTo(configPath, "miniform", matcha.AppConfig{Env: tt.environment}))
				manager := matcha.New(matcha.Config{Name: "miniform", ConfigPath: configPath, DataDirBase: root})

				path, err := deployedDatabasePath(manager)
				if tt.wantError != "" {
					require.ErrorContains(t, err, tt.wantError)
					return
				}
				require.NoError(t, err)
				assert.Equal(t, filepath.Join(root, "miniform", filepath.FromSlash(tt.want)), path)
			})
		}
	})

	t.Run("restores a verified snapshot without exposing a partial database", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "miniform.production.db")
		backupPath := filepath.Join(root, "backups", "backup_20260717_100000.db")
		writeDatabaseValue(t, databasePath, "current")
		writeDatabaseValue(t, backupPath, "restored")
		require.NoError(t, os.Chmod(databasePath, 0o660))

		require.NoError(t, restoreDatabase(databasePath, backupPath))
		assert.Equal(t, "restored", readDatabaseValue(t, databasePath))
		assert.Equal(t, "restored", readDatabaseValue(t, backupPath))
		info, err := os.Stat(databasePath)
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o660), info.Mode().Perm())
	})

	t.Run("leaves the current database intact when the snapshot is invalid", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "miniform.production.db")
		backupPath := filepath.Join(root, "backups", "backup_20260717_100000.db")
		writeDatabaseValue(t, databasePath, "current")
		require.NoError(t, os.MkdirAll(filepath.Dir(backupPath), 0o700))
		require.NoError(t, os.WriteFile(backupPath, []byte("not sqlite"), 0o600))

		require.Error(t, restoreDatabase(databasePath, backupPath))
		assert.Equal(t, "current", readDatabaseValue(t, databasePath))
	})

	t.Run("removes the temporary restore after a snapshot failure", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "miniform.production.db")
		backupPath := filepath.Join(root, "backups", "backup_20260717_100000.db")
		writeDatabaseValue(t, backupPath, "restored")
		db, err := sql.Open("sqlite3", sqliteFileDSN(backupPath))
		require.NoError(t, err)
		_, err = db.ExecContext(t.Context(), `
			PRAGMA writable_schema = ON;
			UPDATE sqlite_schema
			SET sql = 'CREATE TABLE users (id INTEGER PRIMARY KEY, email TEXT COLLATE missing)'
			WHERE name = 'users';
			PRAGMA writable_schema = OFF;`)
		require.NoError(t, err)
		require.NoError(t, db.Close())
		require.NoError(t, validateDatabase(backupPath))

		_, err = prepareDatabaseRestore(databasePath, backupPath)
		require.ErrorContains(t, err, "create SQLite snapshot")
		temporaryFiles, globErr := filepath.Glob(filepath.Join(filepath.Dir(databasePath), ".miniform-restore-*.db"))
		require.NoError(t, globErr)
		assert.Empty(t, temporaryFiles)
	})

	t.Run("rejects an empty SQLite database without the Miniform schema", func(t *testing.T) {
		root := t.TempDir()
		databasePath := filepath.Join(root, "storage", "miniform.production.db")
		backupPath := filepath.Join(root, "backups", "backup_20260717_100000.db")
		writeDatabaseValue(t, databasePath, "current")
		require.NoError(t, os.MkdirAll(filepath.Dir(backupPath), 0o700))
		require.NoError(t, os.WriteFile(backupPath, nil, 0o600))

		require.ErrorContains(t, restoreDatabase(databasePath, backupPath), "Miniform schema")
		assert.Equal(t, "current", readDatabaseValue(t, databasePath))
	})
}

func writeDatabaseValue(t *testing.T, path, value string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	db, err := sql.Open("sqlite3", sqliteFileDSN(path))
	require.NoError(t, err)
	_, err = db.ExecContext(t.Context(), `
		CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS forms (id INTEGER PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS submissions (id INTEGER PRIMARY KEY);
		CREATE TABLE IF NOT EXISTS lifecycle (value TEXT NOT NULL);
		DELETE FROM lifecycle;
		INSERT INTO lifecycle(value) VALUES (?)`, value)
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

func readDatabaseValue(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite3", sqliteFileDSN(path))
	require.NoError(t, err)
	defer db.Close()
	var value string
	require.NoError(t, db.QueryRowContext(t.Context(), `SELECT value FROM lifecycle`).Scan(&value))
	return value
}

func testDeploymentManager(t *testing.T) (*matcha.Matcha, string) {
	t.Helper()
	root := t.TempDir()
	configPath := filepath.Join(root, "matcha.yml")
	require.NoError(t, matcha.SaveAppTo(configPath, "miniform", matcha.AppConfig{
		Image: "registry.example/miniform:test", Domain: "localhost", Port: 8080,
		HealthPath: "/_health", Volumes: []string{"/app/storage"}, Env: map[string]string{},
	}))
	manager := matcha.New(matcha.Config{
		Name: "miniform", AppImage: "registry.example/miniform:test", HealthPath: "/_health",
		Volumes: []string{"/app/storage"}, Backups: true, ConfigPath: configPath, DataDirBase: root,
	})
	return manager, filepath.Join(manager.DataDir(), "storage", "miniform.production.db")
}

func installFakeDocker(t *testing.T) {
	t.Helper()
	directory := t.TempDir()
	marker := filepath.Join(directory, "stopped")
	logPath := filepath.Join(directory, "docker.log")
	t.Setenv("FAKE_DOCKER_STOPPED", marker)
	t.Setenv("FAKE_DOCKER_LOG", logPath)
	t.Setenv("PATH", directory+string(os.PathListSeparator)+os.Getenv("PATH"))
	script := `#!/bin/sh
set -eu
printf '%s\n' "$*" >> "$FAKE_DOCKER_LOG"
if test -n "${FAKE_DOCKER_SLEEP:-}"; then
	exec sleep "$FAKE_DOCKER_SLEEP"
fi
case "$1" in
  ps)
    case "$*" in
      *'name=^matcha-proxy$'*) echo proxy ;;
      *'name=^miniform-next$'*)
		if test "${FAKE_DOCKER_FAIL_RUN:-}" = 1 && test "$2" = -aq; then
		  echo failed
		else
		  test "${FAKE_DOCKER_ACTIVE:-miniform}" != miniform-next || test -f "$FAKE_DOCKER_STOPPED" || echo app
		fi
        ;;
      *'name=^miniform$'*)
        test "${FAKE_DOCKER_ACTIVE:-miniform}" != miniform || test -f "$FAKE_DOCKER_STOPPED" || echo app
        ;;
    esac
    ;;
	stop)
		if test ! -f "$FAKE_DOCKER_STOPPED"; then
			test "${FAKE_DOCKER_REMOVE_DATABASE_ON_STOP:-}" != 1 || rm -f "$FAKE_DOCKER_DATABASE_PATH"
			test -z "${FAKE_DOCKER_DATABASE_ON_STOP:-}" || cp "$FAKE_DOCKER_DATABASE_ON_STOP" "$FAKE_DOCKER_DATABASE_PATH"
		fi
		touch "$FAKE_DOCKER_STOPPED"
		;;
	rm)
		test "${FAKE_DOCKER_FAIL_RM:-}" != 1
		test -z "${FAKE_DOCKER_DATABASE_DIR:-}" || chmod 700 "$FAKE_DOCKER_DATABASE_DIR"
		;;
	rename) test "${FAKE_DOCKER_FAIL_RENAME:-}" != 1 ;;
	start)
		test "${FAKE_DOCKER_FAIL_START:-}" != 1
		test -z "${FAKE_DOCKER_DATABASE_DIR:-}" || chmod 500 "$FAKE_DOCKER_DATABASE_DIR"
		;;
  run)
    test -f "$FAKE_DOCKER_STOPPED"
    test "${FAKE_DOCKER_FAIL_RUN:-}" != 1
    echo new
    ;;
esac
`
	require.NoError(t, os.WriteFile(filepath.Join(directory, "docker"), []byte(script), 0o700))
}
