package forms

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

const (
	MaxFileSize      = 10 * 1024 * 1024
	MaxFilesPerField = 5
	MaxTotalFiles    = 10
)

var allowedExtensions = []string{
	".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg",
	".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx",
	".txt", ".csv", ".rtf", ".odt", ".ods", ".odp",
}

type UploadedFile struct {
	FieldName   string
	Filename    string
	ContentType string
	Size        int64
	Data        io.Reader
}

func ValidateFile(header *multipart.FileHeader) error {
	if header.Size < 0 || header.Size > MaxFileSize {
		return fmt.Errorf("file %q exceeds maximum size of 10MB", header.Filename)
	}
	extension := strings.ToLower(filepath.Ext(header.Filename))
	if extension == "" {
		return fmt.Errorf("file %q has no extension", header.Filename)
	}
	if !slices.Contains(allowedExtensions, extension) {
		return fmt.Errorf("file type %q not allowed", extension)
	}
	return nil
}

func ExtractFiles(form *multipart.Form) ([]*UploadedFile, error) {
	if form == nil || len(form.File) == 0 {
		return nil, nil
	}

	total := 0
	for field, headers := range form.File {
		if len(headers) > MaxFilesPerField {
			return nil, fmt.Errorf("too many files for field %q (max %d)", field, MaxFilesPerField)
		}
		total += len(headers)
	}
	if total > MaxTotalFiles {
		return nil, fmt.Errorf("too many files (max %d)", MaxTotalFiles)
	}

	files := make([]*UploadedFile, 0, total)
	for field, headers := range form.File {
		for _, header := range headers {
			if err := ValidateFile(header); err != nil {
				CloseFiles(files)
				return nil, err
			}
			reader, err := header.Open()
			if err != nil {
				CloseFiles(files)
				return nil, fmt.Errorf("open upload %q: %w", header.Filename, err)
			}
			files = append(files, &UploadedFile{
				FieldName: field, Filename: safeFilename(header.Filename),
				ContentType: header.Header.Get("Content-Type"), Size: header.Size, Data: reader,
			})
		}
	}
	return files, nil
}

func SaveFiles(dataDir string, formID, submissionID uint, files []*UploadedFile) ([]*SubmissionFile, error) {
	if len(files) == 0 {
		return nil, nil
	}
	defer CloseFiles(files)

	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return nil, fmt.Errorf("open data directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	uploadDir := filepath.Join("uploads", strconv.FormatUint(uint64(formID), 10), strconv.FormatUint(uint64(submissionID), 10))
	if err := root.MkdirAll(uploadDir, 0o700); err != nil {
		return nil, fmt.Errorf("create upload directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = root.RemoveAll(uploadDir)
		}
	}()

	records := make([]*SubmissionFile, 0, len(files))
	for _, file := range files {
		filename, err := storedFilename(file.Filename)
		if err != nil {
			return nil, fmt.Errorf("generate storage name for %q: %w", file.Filename, err)
		}
		storagePath := filepath.Join(uploadDir, filename)
		destination, err := root.OpenFile(storagePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return nil, fmt.Errorf("create upload %q: %w", file.Filename, err)
		}

		written, copyErr := io.Copy(destination, file.Data)
		if err := errors.Join(copyErr, destination.Close()); err != nil {
			return nil, fmt.Errorf("write upload %q: %w", file.Filename, err)
		}
		records = append(records, &SubmissionFile{
			SubmissionID: submissionID,
			FieldName:    file.FieldName,
			Filename:     file.Filename,
			ContentType:  file.ContentType,
			Size:         written,
			StoragePath:  storagePath,
		})
	}

	complete = true
	return records, nil
}

func OpenSubmissionFile(dataDir string, file *SubmissionFile) (*os.File, error) {
	source, err := os.OpenInRoot(dataDir, file.StoragePath)
	if err != nil {
		return nil, fmt.Errorf("open stored submission file %q: %w", file.StoragePath, err)
	}
	return source, nil
}

func DeleteSubmissionFiles(dataDir string, formID, submissionID uint) error {
	return deleteUploadPath(dataDir, filepath.Join(
		"uploads",
		strconv.FormatUint(uint64(formID), 10),
		strconv.FormatUint(uint64(submissionID), 10),
	))
}

func DeleteFormFiles(dataDir string, formID uint) error {
	return deleteUploadPath(dataDir, filepath.Join("uploads", strconv.FormatUint(uint64(formID), 10)))
}

func deleteUploadPath(dataDir, path string) error {
	if strings.TrimSpace(dataDir) == "" {
		return nil
	}
	root, err := os.OpenRoot(dataDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("open data directory: %w", err)
	}
	defer func() { _ = root.Close() }()

	if err := root.RemoveAll(path); err != nil {
		return fmt.Errorf("remove upload path %q: %w", path, err)
	}
	return nil
}

func safeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.ReplaceAll(name, "\x00", "_")
	if name == "" || name == "." {
		return "unnamed"
	}
	return name
}

func storedFilename(name string) (string, error) {
	extension := filepath.Ext(name)
	suffix, err := randomHex(rand.Reader, 4)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(name, extension) + "-" + suffix + extension, nil
}

func CloseFiles(files []*UploadedFile) {
	for _, file := range files {
		if closer, ok := file.Data.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}
