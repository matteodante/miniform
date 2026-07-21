package forms

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
)

const (
	MaxFileSize        = 5 * 1024 * 1024
	MaxFilesPerField   = 1
	MaxTotalFiles      = 1
	MaxRequestBodySize = MaxFileSize + 1024*1024
	MaxFilenameBytes   = 200
)

var allowedExtensions = []string{
	".jpg", ".jpeg", ".png", ".gif", ".webp", ".pdf", ".txt", ".csv",
}

var allowedContentTypes = map[string][]string{
	".jpg":  {"image/jpeg"},
	".jpeg": {"image/jpeg"},
	".png":  {"image/png"},
	".gif":  {"image/gif"},
	".webp": {"image/webp"},
	".pdf":  {"application/pdf"},
	".txt":  {"text/plain; charset=utf-8"},
	".csv":  {"text/plain; charset=utf-8"},
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
		return fmt.Errorf("file %q exceeds maximum size of 5MB", header.Filename)
	}
	if len(header.Filename) == 0 || len(header.Filename) > MaxFilenameBytes {
		return fmt.Errorf("file name must contain between 1 and %d bytes", MaxFilenameBytes)
	}
	if strings.IndexFunc(header.Filename, unicode.IsControl) >= 0 {
		return fmt.Errorf("file name contains control characters")
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
			contentType, err := DetectFileContentType(reader, header.Filename)
			if err != nil {
				_ = reader.Close()
				CloseFiles(files)
				return nil, fmt.Errorf("file %q: %w", header.Filename, err)
			}
			files = append(files, &UploadedFile{
				FieldName: field, Filename: safeFilename(header.Filename),
				ContentType: contentType, Size: header.Size, Data: reader,
			})
		}
	}
	return files, nil
}

func DetectFileContentType(file io.ReadSeeker, filename string) (string, error) {
	buffer := make([]byte, 512)
	read, err := file.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		return "", fmt.Errorf("inspect content: %w", err)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("rewind content: %w", err)
	}
	extension := strings.ToLower(filepath.Ext(filename))
	contentType := http.DetectContentType(buffer[:read])
	if !slices.Contains(allowedContentTypes[extension], contentType) {
		return "", fmt.Errorf("content type %q does not match extension %q", contentType, extension)
	}
	return contentType, nil
}

func stageUnassignedFiles(dataDir string, files []*UploadedFile) ([]*SubmissionFile, *stagedUploadDeletion, error) {
	token, err := randomHex(rand.Reader, 16)
	if err != nil {
		CloseFiles(files)
		return nil, nil, fmt.Errorf("generate upload directory: %w", err)
	}
	storageDirectory := filepath.Join("uploads", token)
	operation := filepath.Join(uploadStagingRoot, token)
	stagingDirectory := filepath.Join(operation, storageDirectory)
	records, err := saveFilesAt(dataDir, stagingDirectory, storageDirectory, 0, files)
	if err != nil {
		_ = deleteUploadPath(dataDir, operation)
		return nil, nil, err
	}
	staged := &stagedUploadDeletion{dataDir: dataDir, operation: operation}
	for _, record := range records {
		staged.files = append(staged.files, stagedUploadFile{
			original: record.StoragePath,
			staged:   filepath.Join(operation, record.StoragePath),
		})
	}
	return records, staged, nil
}

func saveFilesAt(dataDir, writeDir, storageDir string, submissionID uint, files []*UploadedFile) ([]*SubmissionFile, error) {
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

	if err := root.MkdirAll(writeDir, 0o700); err != nil {
		return nil, fmt.Errorf("create upload directory: %w", err)
	}
	complete := false
	defer func() {
		if !complete {
			_ = root.RemoveAll(writeDir)
		}
	}()

	records := make([]*SubmissionFile, 0, len(files))
	for _, file := range files {
		filename, err := storedFilename(file.Filename)
		if err != nil {
			return nil, fmt.Errorf("generate storage name for %q: %w", file.Filename, err)
		}
		writePath := filepath.Join(writeDir, filename)
		storagePath := filepath.Join(storageDir, filename)
		destination, err := root.OpenFile(writePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
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
	suffix, err := randomHex(rand.Reader, 16)
	if err != nil {
		return "", err
	}
	return suffix + strings.ToLower(extension), nil
}

func CloseFiles(files []*UploadedFile) {
	for _, file := range files {
		if closer, ok := file.Data.(io.Closer); ok {
			_ = closer.Close()
		}
	}
}
