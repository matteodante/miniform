package forms

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"gorm.io/gorm"
)

const (
	uploadDeletionRoot = ".upload-deletions"
	uploadStagingRoot  = ".upload-staging"
)

type stagedUploadFile struct {
	original string
	staged   string
}

type stagedUploadDeletion struct {
	dataDir   string
	operation string
	files     []stagedUploadFile
}

func stageStoredFiles(dataDir string, files []*SubmissionFile) (*stagedUploadDeletion, error) {
	paths, err := storedUploadPaths(files)
	if err != nil {
		return nil, err
	}
	deletion := &stagedUploadDeletion{dataDir: dataDir}
	if len(paths) == 0 {
		return deletion, nil
	}
	if strings.TrimSpace(dataDir) == "" {
		return nil, errors.New("data directory is required for stored uploads")
	}
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("open upload storage: %w", err)
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open upload storage: %w", err)
	}
	defer func() { _ = root.Close() }()

	token, err := randomHex(rand.Reader, 16)
	if err != nil {
		return nil, fmt.Errorf("create upload deletion token: %w", err)
	}
	deletion.operation = filepath.Join(uploadDeletionRoot, token)
	if err := root.MkdirAll(deletion.operation, 0o700); err != nil {
		return nil, fmt.Errorf("create upload deletion quarantine: %w", err)
	}

	for _, original := range paths {
		info, err := root.Lstat(original)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, errors.Join(fmt.Errorf("inspect stored upload %q: %w", original, err), deletion.restoreWithRoot(root))
		}
		if info.IsDir() {
			return nil, errors.Join(fmt.Errorf("stored upload %q is a directory", original), deletion.restoreWithRoot(root))
		}

		staged := filepath.Join(deletion.operation, original)
		if err := root.MkdirAll(filepath.Dir(staged), 0o700); err != nil {
			return nil, errors.Join(fmt.Errorf("prepare upload quarantine for %q: %w", original, err), deletion.restoreWithRoot(root))
		}
		if err := root.Rename(original, staged); err != nil {
			return nil, errors.Join(fmt.Errorf("quarantine stored upload %q: %w", original, err), deletion.restoreWithRoot(root))
		}
		deletion.files = append(deletion.files, stagedUploadFile{original: original, staged: staged})
		pruneEmptyUploadParents(root, original)
	}
	return deletion, nil
}

func storedUploadPaths(files []*SubmissionFile) ([]string, error) {
	seen := make(map[string]struct{}, len(files))
	paths := make([]string, 0, len(files))
	for _, file := range files {
		if file == nil {
			continue
		}
		path, err := cleanStoredUploadPath(file.StoragePath)
		if err != nil {
			return nil, err
		}
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		paths = append(paths, path)
	}
	return paths, nil
}

func cleanStoredUploadPath(value string) (string, error) {
	cleaned := filepath.Clean(value)
	if filepath.IsAbs(cleaned) || !strings.HasPrefix(cleaned, "uploads"+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid stored upload path %q", value)
	}
	return cleaned, nil
}

func (deletion *stagedUploadDeletion) restoreWithRoot(root *os.Root) error {
	var result error
	for index := len(deletion.files) - 1; index >= 0; index-- {
		result = errors.Join(result, restoreStagedUpload(root, deletion.files[index]))
	}
	if result == nil {
		if err := root.RemoveAll(deletion.operation); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove upload deletion quarantine: %w", err)
		}
	}
	return result
}

func restoreStagedUpload(root *os.Root, file stagedUploadFile) error {
	if _, err := root.Lstat(file.staged); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return fmt.Errorf("inspect quarantined upload %q: %w", file.staged, err)
	}
	if _, err := root.Lstat(file.original); err == nil {
		return fmt.Errorf("restore upload %q: destination already exists", file.original)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect upload restore destination %q: %w", file.original, err)
	}
	if err := root.MkdirAll(filepath.Dir(file.original), 0o700); err != nil {
		return fmt.Errorf("prepare upload restore destination %q: %w", file.original, err)
	}
	if err := root.Rename(file.staged, file.original); err != nil {
		return fmt.Errorf("restore upload %q: %w", file.original, err)
	}
	return nil
}

func (deletion *stagedUploadDeletion) finish(db *gorm.DB) error {
	if deletion == nil || deletion.operation == "" {
		return nil
	}
	root, err := os.OpenRoot(deletion.dataDir)
	if err != nil {
		return fmt.Errorf("open upload storage for cleanup: %w", err)
	}
	defer func() { _ = root.Close() }()

	for _, file := range deletion.files {
		var references int64
		if err := db.Model(&SubmissionFile{}).Where("storage_path = ?", file.original).Count(&references).Error; err != nil {
			return fmt.Errorf("check quarantined upload reference %q: %w", file.original, err)
		}
		if references > 0 {
			if err := restoreStagedUpload(root, file); err != nil {
				return err
			}
			continue
		}
		if err := root.RemoveAll(file.staged); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("remove quarantined upload %q: %w", file.original, err)
		}
	}
	if err := root.RemoveAll(deletion.operation); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove upload deletion quarantine: %w", err)
	}
	return nil
}

// RecoverUploadDeletions resolves interrupted upload staging from committed database state.
func RecoverUploadDeletions(db *gorm.DB, dataDir string) error {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	submissionFilesMutex.Lock()
	defer submissionFilesMutex.Unlock()

	root, err := os.OpenRoot(dataDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open upload storage for recovery: %w", err)
	}
	defer func() { _ = root.Close() }()

	for _, stagingRoot := range []string{uploadStagingRoot, uploadDeletionRoot} {
		if err := recoverStagedUploadRoot(root, db, dataDir, stagingRoot); err != nil {
			return err
		}
	}
	return nil
}

func recoverStagedUploadRoot(root *os.Root, db *gorm.DB, dataDir, stagingRoot string) error {
	operations, err := fs.ReadDir(root.FS(), stagingRoot)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("list interrupted upload operations in %q: %w", stagingRoot, err)
	}
	for _, entry := range operations {
		if !entry.IsDir() {
			return fmt.Errorf("unexpected file in upload staging root %q", filepath.Join(stagingRoot, entry.Name()))
		}
		operation := filepath.Join(stagingRoot, entry.Name())
		staged := &stagedUploadDeletion{dataDir: dataDir, operation: operation}
		walkRoot := filepath.ToSlash(operation)
		if err := fs.WalkDir(root.FS(), walkRoot, func(path string, item fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if item.IsDir() {
				return nil
			}
			original, err := cleanStoredUploadPath(filepath.FromSlash(strings.TrimPrefix(path, walkRoot+"/")))
			if err != nil {
				return err
			}
			staged.files = append(staged.files, stagedUploadFile{
				original: original,
				staged:   filepath.FromSlash(path),
			})
			return nil
		}); err != nil {
			return fmt.Errorf("inspect interrupted upload operation %q: %w", entry.Name(), err)
		}
		if err := staged.finish(db); err != nil {
			return fmt.Errorf("recover interrupted upload operation %q: %w", entry.Name(), err)
		}
	}
	if err := root.Remove(stagingRoot); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove empty upload staging root %q: %w", stagingRoot, err)
	}
	return nil
}

func pruneEmptyUploadParents(root *os.Root, storedPath string) {
	for directory := filepath.Dir(storedPath); directory != "uploads"; directory = filepath.Dir(directory) {
		if err := root.Remove(directory); err != nil {
			return
		}
	}
}
