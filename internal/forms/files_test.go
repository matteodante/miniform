package forms

import (
	"bytes"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateFile(t *testing.T) {
	t.Run("accepts valid image", func(t *testing.T) {
		header := &multipart.FileHeader{
			Filename: "photo.jpg",
			Size:     1024 * 1024, // 1MB
		}

		err := ValidateFile(header)

		assert.NoError(t, err)
	})

	t.Run("accepts valid PDF", func(t *testing.T) {
		header := &multipart.FileHeader{
			Filename: "document.pdf",
			Size:     5 * 1024 * 1024, // 5MB
		}

		err := ValidateFile(header)

		assert.NoError(t, err)
	})

	t.Run("accepts valid doc files", func(t *testing.T) {
		extensions := []string{".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".txt", ".csv"}
		for _, ext := range extensions {
			header := &multipart.FileHeader{
				Filename: "file" + ext,
				Size:     1024,
			}

			err := ValidateFile(header)

			assert.NoError(t, err, "should accept %s", ext)
		}
	})

	t.Run("rejects file exceeding size limit", func(t *testing.T) {
		header := &multipart.FileHeader{
			Filename: "huge.pdf",
			Size:     15 * 1024 * 1024, // 15MB > 10MB limit
		}

		err := ValidateFile(header)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "exceeds maximum size")
	})

	t.Run("rejects disallowed extension", func(t *testing.T) {
		header := &multipart.FileHeader{
			Filename: "script.exe",
			Size:     1024,
		}

		err := ValidateFile(header)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not allowed")
	})

	t.Run("rejects file without extension", func(t *testing.T) {
		header := &multipart.FileHeader{
			Filename: "noextension",
			Size:     1024,
		}

		err := ValidateFile(header)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "no extension")
	})
}

func TestSanitizeFilename(t *testing.T) {
	t.Run("removes path traversal", func(t *testing.T) {
		result := sanitizeFilename("../../../etc/passwd")
		assert.NotContains(t, result, "..")
		assert.NotContains(t, result, "/")
	})

	t.Run("removes backslashes", func(t *testing.T) {
		result := sanitizeFilename("..\\..\\windows\\system32")
		assert.NotContains(t, result, "\\")
	})

	t.Run("preserves valid filename", func(t *testing.T) {
		result := sanitizeFilename("my-photo.jpg")
		assert.Equal(t, "my-photo.jpg", result)
	})

	t.Run("handles empty string", func(t *testing.T) {
		result := sanitizeFilename("")
		assert.Equal(t, "unnamed", result)
	})
}

func TestExtractFiles(t *testing.T) {
	t.Run("returns nil for nil form", func(t *testing.T) {
		files, err := ExtractFiles(nil)

		assert.NoError(t, err)
		assert.Nil(t, files)
	})

	t.Run("returns nil for form without files", func(t *testing.T) {
		form := &multipart.Form{
			Value: map[string][]string{"name": {"John"}},
			File:  nil,
		}

		files, err := ExtractFiles(form)

		assert.NoError(t, err)
		assert.Nil(t, files)
	})

	t.Run("rejects too many files per field", func(t *testing.T) {
		headers := make([]*multipart.FileHeader, MaxFilesPerField+1)
		for i := range headers {
			headers[i] = &multipart.FileHeader{
				Filename: "file.pdf",
				Size:     1024,
				Header:   make(textproto.MIMEHeader),
			}
		}

		form := &multipart.Form{
			File: map[string][]*multipart.FileHeader{
				"attachments": headers,
			},
		}

		_, err := ExtractFiles(form)

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "too many files for field")
	})
}

func TestSaveFiles(t *testing.T) {
	t.Run("stores uploads inside the data directory with private permissions", func(t *testing.T) {
		dataDir := t.TempDir()
		files := []*UploadedFile{{
			FieldName:   "attachment",
			Filename:    "report.pdf",
			ContentType: "application/pdf",
			Data:        bytes.NewBufferString("test data"),
		}}

		records, err := SaveFiles(dataDir, 12, 34, files)

		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, filepath.Join("uploads", "12", "34"), filepath.Dir(records[0].StoragePath))

		info, err := os.Stat(filepath.Join(dataDir, records[0].StoragePath))
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
	})
}
