package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/karloscodes/matcha"
	_ "github.com/mattn/go-sqlite3"
)

const dockerCommandTimeout = 10 * time.Minute

//nolint:gocyclo // Deployment branches keep their compensation steps next to each mutating command.
func runDeploymentCommand(manager *matcha.Matcha, command string, input io.Reader, output io.Writer) error {
	lock, err := lockDeployment(manager.DataDir())
	if err != nil {
		return err
	}
	defer func() {
		_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
		_ = lock.Close()
	}()

	switch command {
	case "install":
		configured, err := deploymentConfigured(manager)
		if err != nil {
			return err
		}
		if !configured {
			residual, err := findResidualDatabase(filepath.Join(manager.DataDir(), "storage"))
			if err != nil {
				return fmt.Errorf("inspect residual storage: %w", err)
			}
			if residual != "" {
				return fmt.Errorf("existing Miniform database %q found without deployment configuration; restore the Matcha configuration or move and back up the database before install", residual)
			}
		}
		if err := requireDocker(); err != nil {
			return err
		}
		if configured {
			active, err := runningApplication(manager)
			if err != nil {
				return fmt.Errorf("inspect existing deployment: %w", err)
			}
			if active != "" {
				return fmt.Errorf("deployment is already running; use update or reload")
			}
			databasePath, err := deployedDatabasePath(manager)
			if err != nil {
				return err
			}
			if _, err := os.Stat(databasePath); err == nil {
				if _, err := backupDeploymentDatabase(manager); err != nil {
					return fmt.Errorf("pre-install backup: %w", err)
				}
			} else if !os.IsNotExist(err) {
				return fmt.Errorf("inspect database: %w", err)
			}
		}
		return manager.Install()
	case "backup":
		path, err := backupDeploymentDatabase(manager)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "Backup created: %s\n", path)
		return err
	case "update":
		if _, err := manager.SelfUpdate(); err != nil {
			return fmt.Errorf("update manager: %w", err)
		}
		app, err := loadDeploymentApp(manager)
		if err != nil {
			return err
		}
		if strings.TrimSpace(app.Image) == "" {
			return fmt.Errorf("deployment image is empty")
		}
		for _, image := range []string{app.Image, manager.GetConfig().ProxyImage} {
			if err := runDocker("pull", image); err != nil {
				return fmt.Errorf("pull image %q: %w", image, err)
			}
		}
		return restartDeployment(manager)
	case "reload":
		return restartDeployment(manager)
	case "restore-db":
		databasePath, err := deployedDatabasePath(manager)
		if err != nil {
			return err
		}
		selected, err := selectDatabaseBackup(filepath.Join(manager.DataDir(), "backups"), input, output)
		if err != nil {
			return err
		}
		staged, err := prepareDatabaseRestore(databasePath, selected)
		if err != nil {
			return err
		}
		defer func() { _ = os.Remove(staged) }()
		stopped, err := stopApplication(manager)
		if err != nil {
			return fmt.Errorf("stop application: %w", err)
		}
		safetyBackup, err := backupDatabaseForRestore(manager, databasePath)
		if err != nil {
			return errors.Join(fmt.Errorf("pre-restore backup: %w", err), restartStoppedApplication(manager, stopped))
		}
		if err := replaceDatabase(databasePath, staged); err != nil {
			return errors.Join(err, rollbackRestore(manager, stopped, databasePath, safetyBackup))
		}
		if err := startDeployment(manager); err != nil {
			return errors.Join(err, rollbackRestore(manager, stopped, databasePath, safetyBackup))
		}
		return nil
	default:
		return fmt.Errorf("unsupported deployment command %q", command)
	}
}

func requireDocker() error {
	var stderr bytes.Buffer
	if err := runDockerCommand(30*time.Second, io.Discard, &stderr, "version"); err != nil {
		message := "Docker Engine must be installed and running before Miniform installation; follow https://docs.docker.com/engine/install/"
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			message += ": " + detail
		}
		return fmt.Errorf("%s: %w", message, err)
	}
	return nil
}

func loadDeploymentApp(manager *matcha.Matcha) (matcha.AppConfig, error) {
	configuration := manager.GetConfig()
	if configuration.ConfigPath == "" {
		return matcha.LoadApp(configuration.Name)
	}
	return matcha.LoadAppFrom(configuration.ConfigPath, configuration.Name)
}

func deploymentConfigured(manager *matcha.Matcha) (bool, error) {
	path := manager.GetConfig().ConfigPath
	if path == "" {
		path = matcha.ConfigPath()
	}
	configuration, err := matcha.LoadMatchaConfigFrom(path)
	if err != nil {
		return false, fmt.Errorf("load deployment configuration: %w", err)
	}
	_, configured := configuration.Apps[manager.AppContainerName()]
	return configured, nil
}

func findResidualDatabase(storageDirectory string) (string, error) {
	if _, err := os.Stat(storageDirectory); os.IsNotExist(err) {
		return "", nil
	} else if err != nil {
		return "", err
	}
	found := ""
	stop := errors.New("residual database found")
	err := filepath.Walk(storageDirectory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if path != storageDirectory && (info.Name() == "uploads" || strings.HasPrefix(info.Name(), ".upload-")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		extension := strings.ToLower(filepath.Ext(path))
		if extension == ".db" || extension == ".sqlite" || extension == ".sqlite3" || validateDatabase(path) == nil {
			found = path
			return stop
		}
		return nil
	})
	if errors.Is(err, stop) {
		return found, nil
	}
	return found, err
}

func backupDeploymentDatabase(manager *matcha.Matcha) (string, error) {
	path, err := deployedDatabasePath(manager)
	if err != nil {
		return "", err
	}
	return createDatabaseBackup(path, filepath.Join(manager.DataDir(), "backups"), time.Now())
}

func backupDatabaseForRestore(manager *matcha.Matcha, databasePath string) (string, error) {
	info, err := os.Stat(databasePath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", nil
	}
	if validationErr := validateDatabase(databasePath); validationErr != nil {
		return "", nil //nolint:nilerr // An invalid previous database cannot be used as rollback state.
	}
	return createDatabaseBackup(databasePath, filepath.Join(manager.DataDir(), "backups"), time.Now())
}

func restartDeployment(manager *matcha.Matcha) error {
	databasePath, err := deployedDatabasePath(manager)
	if err != nil {
		return err
	}
	stopped, err := stopApplication(manager)
	if err != nil {
		return fmt.Errorf("stop application: %w", err)
	}
	safetyBackup, err := backupDeploymentDatabase(manager)
	if err != nil {
		return errors.Join(fmt.Errorf("pre-deploy backup: %w", err), restartStoppedApplication(manager, stopped))
	}
	if err := startDeployment(manager); err != nil {
		return errors.Join(err, rollbackRestore(manager, stopped, databasePath, safetyBackup))
	}
	return nil
}

func startDeployment(manager *matcha.Matcha) error {
	configuration := manager.GetConfig()
	configuration.Backups = false
	if err := matcha.New(configuration).Reload(); err != nil {
		return fmt.Errorf("start application: %w", err)
	}
	return nil
}

func stopApplication(manager *matcha.Matcha) (string, error) {
	original, err := runningApplication(manager)
	if err != nil || original == "" {
		return original, err
	}
	base := manager.AppContainerName()
	next := base + "-next"
	if err := runDocker("stop", "--timeout", "15", original); err != nil {
		return "", err
	}
	if original == next {
		stale, err := dockerOutput("ps", "-aq", "--filter", "name=^"+base+"$")
		if err != nil {
			return "", errors.Join(err, restartPreviousApplication(original))
		}
		if strings.TrimSpace(stale) != "" {
			if err := runDocker("rm", "-f", base); err != nil {
				return "", errors.Join(err, restartPreviousApplication(original))
			}
		}
		if err := runDocker("rename", next, base); err != nil {
			return "", errors.Join(err, restartPreviousApplication(original))
		}
	}
	return original, nil
}

func runningApplication(manager *matcha.Matcha) (string, error) {
	base := manager.AppContainerName()
	next := base + "-next"
	var running []string
	for _, name := range []string{base, next} {
		output, err := dockerOutput("ps", "-q", "--filter", "name=^"+name+"$")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(output) != "" {
			running = append(running, name)
		}
	}
	if len(running) > 1 {
		return "", fmt.Errorf("both application containers are running")
	}
	if len(running) == 0 {
		return "", nil
	}
	return running[0], nil
}

func prepareApplicationRollback(manager *matcha.Matcha, original string) error {
	base := manager.AppContainerName()
	next := base + "-next"
	replacement, err := dockerOutput("ps", "-aq", "--filter", "name=^"+next+"$")
	if err != nil {
		return fmt.Errorf("inspect failed replacement: %w", err)
	}
	if strings.TrimSpace(replacement) != "" {
		if err := runDocker("rm", "-f", next); err != nil {
			return fmt.Errorf("remove failed replacement: %w", err)
		}
	}
	if original == next {
		if err := runDocker("rename", base, next); err != nil {
			return fmt.Errorf("restore previous container name: %w", err)
		}
	}
	return nil
}

func restartPreviousApplication(original string) error {
	if original == "" {
		return nil
	}
	if err := runDocker("start", original); err != nil {
		return fmt.Errorf("restart previous container: %w", err)
	}
	return nil
}

func restartStoppedApplication(manager *matcha.Matcha, original string) error {
	if err := prepareApplicationRollback(manager, original); err != nil {
		return err
	}
	return restartPreviousApplication(original)
}

func rollbackRestore(manager *matcha.Matcha, original, databasePath, safetyBackup string) error {
	if err := prepareApplicationRollback(manager, original); err != nil {
		return err
	}
	if safetyBackup == "" {
		return fmt.Errorf("previous database was not valid; application remains stopped")
	}
	if err := restoreDatabase(databasePath, safetyBackup); err != nil {
		return err
	}
	return restartPreviousApplication(original)
}

func runDocker(arguments ...string) error {
	return runDockerCommand(dockerCommandTimeout, os.Stdout, os.Stderr, arguments...)
}

func runDockerCommand(timeout time.Duration, stdout, stderr io.Writer, arguments ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// #nosec G204 -- Docker receives an argument vector without a shell; callers only pass lifecycle arguments and root-owned image configuration.
	command := exec.CommandContext(ctx, "docker", arguments...)
	command.Stdout = stdout
	command.Stderr = stderr
	if err := command.Run(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("docker command timed out after %s: %w", timeout, ctxErr)
		}
		return err
	}
	return nil
}

func dockerOutput(arguments ...string) (string, error) {
	var stdout, stderr bytes.Buffer
	if err := runDockerCommand(dockerCommandTimeout, &stdout, &stderr, arguments...); err != nil {
		return "", fmt.Errorf("docker %s: %w: %s", strings.Join(arguments, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.String(), nil
}

func lockDeployment(directory string) (*os.File, error) {
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create deployment directory: %w", err)
	}
	// #nosec G304 -- Matcha supplies the fixed, root-owned application data directory.
	file, err := os.OpenFile(filepath.Join(directory, "deployment.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open deployment lock: %w", err)
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("another deployment command is already running")
	}
	return file, nil
}

func deployedDatabasePath(manager *matcha.Matcha) (string, error) {
	configuration := manager.GetConfig()
	var (
		app matcha.AppConfig
		err error
	)
	if configuration.ConfigPath == "" {
		app, err = matcha.LoadApp(configuration.Name)
	} else {
		app, err = matcha.LoadAppFrom(configuration.ConfigPath, configuration.Name)
	}
	if err != nil {
		return "", fmt.Errorf("load deployment configuration: %w", err)
	}

	containerPath := strings.TrimSpace(app.Env["MINIFORM_DATABASE_PATH"])
	if containerPath == "" {
		dataDirectory := strings.TrimSpace(app.Env["MINIFORM_DATA_DIR"])
		if dataDirectory == "" {
			dataDirectory = "/app/storage"
		} else if !filepath.IsAbs(dataDirectory) {
			dataDirectory = filepath.Join("/app", dataDirectory)
		}
		filename := strings.TrimSpace(app.Env["MINIFORM_DATABASE_FILENAME"])
		if filename == "" {
			filename = "miniform.db"
		} else if filename == "." || filename == ".." || filepath.IsAbs(filename) || filepath.Base(filename) != filename {
			return "", fmt.Errorf("MINIFORM_DATABASE_FILENAME must be a filename without path components; use MINIFORM_DATABASE_PATH for database paths")
		}
		extension := filepath.Ext(filename)
		base := strings.TrimSuffix(filename, extension)
		if extension == "" {
			extension = ".db"
		}
		environment := strings.TrimSpace(app.Env["MINIFORM_ENV"])
		if environment == "" {
			environment = "production"
		}
		containerPath = filepath.Join(dataDirectory, base+"."+environment+extension)
	} else if !filepath.IsAbs(containerPath) {
		containerPath = filepath.Join("/app", containerPath)
	}

	relative, err := filepath.Rel("/app/storage", filepath.Clean(containerPath))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("database path %q is outside /app/storage", containerPath)
	}
	return filepath.Join(manager.DataDir(), "storage", relative), nil
}

func createDatabaseBackup(databasePath, backupDirectory string, now time.Time) (string, error) {
	info, err := os.Stat(databasePath)
	if err != nil {
		return "", fmt.Errorf("inspect database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("database is not a regular file: %s", databasePath)
	}
	if err := os.MkdirAll(backupDirectory, 0o700); err != nil {
		return "", fmt.Errorf("create backup directory: %w", err)
	}
	backupPath, err := availableBackupPath(backupDirectory, now)
	if err != nil {
		return "", err
	}

	if err := snapshotDatabase(databasePath, backupPath); err != nil {
		_ = os.Remove(backupPath)
		return "", err
	}
	if err := os.Chmod(backupPath, 0o600); err != nil {
		_ = os.Remove(backupPath)
		return "", fmt.Errorf("secure backup: %w", err)
	}
	if err := removeOldBackups(backupDirectory); err != nil {
		return "", err
	}
	return backupPath, nil
}

func snapshotDatabase(source, destination string) (resultErr error) {
	db, err := sql.Open("sqlite3", sqliteFileDSN(source))
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(1)
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	if _, err := db.ExecContext(context.Background(), `VACUUM INTO ?`, destination); err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", err)
	}
	if err := validateDatabase(destination); err != nil {
		return fmt.Errorf("validate snapshot: %w", err)
	}
	return nil
}

func restoreDatabase(databasePath, backupPath string) error {
	temporaryPath, err := prepareDatabaseRestore(databasePath, backupPath)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(temporaryPath) }()
	return replaceDatabase(databasePath, temporaryPath)
}

func prepareDatabaseRestore(databasePath, backupPath string) (string, error) {
	if err := validateDatabase(backupPath); err != nil {
		return "", fmt.Errorf("validate selected backup: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(databasePath), 0o700); err != nil {
		return "", fmt.Errorf("create database directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(databasePath), ".miniform-restore-*.db")
	if err != nil {
		return "", fmt.Errorf("create restore file: %w", err)
	}
	temporaryPath := temporary.Name()
	prepared := false
	defer func() {
		if !prepared {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close restore file: %w", err)
	}
	if err := os.Remove(temporaryPath); err != nil {
		return "", fmt.Errorf("prepare restore file: %w", err)
	}
	if err := snapshotDatabase(backupPath, temporaryPath); err != nil {
		return "", err
	}
	if err := os.Chmod(temporaryPath, 0o600); err != nil {
		return "", fmt.Errorf("secure restored database: %w", err)
	}
	prepared = true
	return temporaryPath, nil
}

func replaceDatabase(databasePath, temporaryPath string) (resultErr error) {
	info, err := os.Stat(databasePath)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("inspect current database: %w", err)
	}
	if err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("current database is not a regular file: %s", databasePath)
		}
		if err := os.Chmod(temporaryPath, info.Mode().Perm()); err != nil {
			return fmt.Errorf("preserve database permissions: %w", err)
		}
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			if err := os.Chown(temporaryPath, int(stat.Uid), int(stat.Gid)); err != nil {
				return fmt.Errorf("preserve database ownership: %w", err)
			}
		}
		if validateDatabase(databasePath) == nil {
			if err := checkpointDatabase(databasePath); err != nil {
				return err
			}
		}
	}
	for _, sidecar := range []string{databasePath + "-wal", databasePath + "-shm"} {
		if err := os.Remove(sidecar); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("remove stale SQLite sidecar: %w", err)
		}
	}
	if err := os.Rename(temporaryPath, databasePath); err != nil {
		return fmt.Errorf("replace database: %w", err)
	}
	directory, err := os.Open(filepath.Dir(databasePath))
	if err != nil {
		return fmt.Errorf("open database directory: %w", err)
	}
	defer func() { resultErr = errors.Join(resultErr, directory.Close()) }()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync restored database: %w", err)
	}
	return nil
}

func selectDatabaseBackup(directory string, input io.Reader, output io.Writer) (string, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return "", fmt.Errorf("list backups: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && isDatabaseBackup(entry.Name()) {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	if len(paths) == 0 {
		return "", fmt.Errorf("no backups found")
	}
	sort.Sort(sort.Reverse(sort.StringSlice(paths)))
	if _, err := fmt.Fprintln(output, "Available backups:"); err != nil {
		return "", err
	}
	for index, path := range paths {
		if _, err := fmt.Fprintf(output, "  %d. %s\n", index+1, filepath.Base(path)); err != nil {
			return "", err
		}
	}
	if _, err := fmt.Fprint(output, "Select backup number: "); err != nil {
		return "", err
	}
	var selection int
	if _, err := fmt.Fscan(input, &selection); err != nil {
		return "", fmt.Errorf("read backup selection: %w", err)
	}
	if selection < 1 || selection > len(paths) {
		return "", fmt.Errorf("selection out of range")
	}
	return paths[selection-1], nil
}

func isDatabaseBackup(name string) bool {
	if !strings.HasPrefix(name, "backup_") || !strings.HasSuffix(name, ".db") {
		return false
	}
	stem := strings.TrimSuffix(strings.TrimPrefix(name, "backup_"), ".db")
	parts := strings.Split(stem, "_")
	if len(parts) != 2 && len(parts) != 3 {
		return false
	}
	if _, err := time.Parse("20060102_150405", parts[0]+"_"+parts[1]); err != nil {
		return false
	}
	if len(parts) == 3 {
		if len(parts[2]) != 6 {
			return false
		}
		_, err := strconv.Atoi(parts[2])
		return err == nil
	}
	return true
}

func availableBackupPath(directory string, now time.Time) (string, error) {
	base := "backup_" + now.UTC().Format("20060102_150405")
	for index := 0; index < 1_000_000; index++ {
		name := base + ".db"
		if index > 0 {
			name = fmt.Sprintf("%s_%06d.db", base, index)
		}
		path := filepath.Join(directory, name)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return path, nil
		} else if err != nil {
			return "", fmt.Errorf("inspect backup path: %w", err)
		}
	}
	return "", fmt.Errorf("too many backups created in one second")
}

func checkpointDatabase(path string) error {
	db, err := sql.Open("sqlite3", sqliteFileDSN(path))
	if err != nil {
		return fmt.Errorf("open current database: %w", err)
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(context.Background(), `PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
		_ = db.Close()
		return fmt.Errorf("checkpoint current database: %w", err)
	}
	if err := db.Close(); err != nil {
		return fmt.Errorf("close current database: %w", err)
	}
	return nil
}

func validateDatabase(path string) (resultErr error) {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return fmt.Errorf("database does not contain the Miniform schema")
	}
	db, err := sql.Open("sqlite3", sqliteFileDSN(path))
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, db.Close()) }()
	var result string
	if err := db.QueryRowContext(context.Background(), `PRAGMA integrity_check`).Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("integrity check returned %q", result)
	}
	var schemaTables int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM sqlite_schema WHERE type = 'table' AND name IN ('users', 'forms', 'submissions')`).Scan(&schemaTables); err != nil {
		return err
	}
	if schemaTables != 3 {
		return fmt.Errorf("database does not contain the Miniform schema")
	}
	return nil
}

func removeOldBackups(directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("list backups: %w", err)
	}
	var paths []string
	for _, entry := range entries {
		if !entry.IsDir() && isDatabaseBackup(entry.Name()) {
			paths = append(paths, filepath.Join(directory, entry.Name()))
		}
	}
	for len(paths) > 3 {
		if err := os.Remove(paths[0]); err != nil {
			return fmt.Errorf("remove old backup: %w", err)
		}
		paths = paths[1:]
	}
	return nil
}

func sqliteFileDSN(path string) string {
	escaped := url.PathEscape(filepath.ToSlash(path))
	return "file:" + strings.ReplaceAll(escaped, "%2F", "/")
}
