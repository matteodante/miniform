package forms

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/textproto"
	"os"
	"path/filepath"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFiles(t *testing.T) {
	t.Run("validates size and extension", func(t *testing.T) {
		cases := []struct {
			name, filename string
			size           int64
			wantError      string
		}{
			{"image", "photo.JPG", 1 << 20, ""},
			{"document", "report.pdf", 5 << 20, ""},
			{"office file", "sheet.xlsx", 1024, ""},
			{"too large", "archive.pdf", MaxFileSize + 1, "maximum size"},
			{"unsupported", "program.exe", 1024, "not allowed"},
			{"missing extension", "README", 1024, "no extension"},
		}

		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := ValidateFile(&multipart.FileHeader{Filename: tc.filename, Size: tc.size})
				if tc.wantError == "" {
					assert.NoError(t, err)
				} else {
					assert.ErrorContains(t, err, tc.wantError)
				}
			})
		}
	})

	t.Run("normalizes client filenames", func(t *testing.T) {
		cases := map[string]string{
			"../../../etc/passwd":    "passwd",
			`..\..\windows\system32`: "system32",
			"my-photo.jpg":           "my-photo.jpg",
			"":                       "unnamed",
		}
		for input, want := range cases {
			assert.Equal(t, want, safeFilename(input))
		}
	})

	t.Run("extracts only bounded multipart input", func(t *testing.T) {
		files, err := ExtractFiles(nil)
		assert.NoError(t, err)
		assert.Nil(t, files)

		headers := make([]*multipart.FileHeader, MaxFilesPerField+1)
		for i := range headers {
			headers[i] = &multipart.FileHeader{
				Filename: "file.pdf",
				Size:     1024,
				Header:   make(textproto.MIMEHeader),
			}
		}
		_, err = ExtractFiles(&multipart.Form{File: map[string][]*multipart.FileHeader{"attachment": headers}})
		assert.ErrorContains(t, err, "too many files for field")
	})

	t.Run("stores private uploads and returns their metadata", func(t *testing.T) {
		root := t.TempDir()
		records, err := SaveFiles(root, 12, 34, []*UploadedFile{{
			FieldName: "attachment", Filename: "report.pdf", ContentType: "application/pdf",
			Data: bytes.NewBufferString("test data"),
		}})

		require.NoError(t, err)
		require.Len(t, records, 1)
		assert.Equal(t, filepath.Join("uploads", "12", "34"), filepath.Dir(records[0].StoragePath))
		assert.Equal(t, int64(9), records[0].Size)
		source, err := OpenSubmissionFile(root, records[0])
		require.NoError(t, err)
		defer func() { _ = source.Close() }()
		content, err := io.ReadAll(source)
		require.NoError(t, err)
		assert.Equal(t, "test data", string(content))
		info, err := source.Stat()
		require.NoError(t, err)
		assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	})

	t.Run("rejects upload paths outside storage", func(t *testing.T) {
		baseDirectory := t.TempDir()
		storageDirectory := filepath.Join(baseDirectory, "storage")
		require.NoError(t, os.Mkdir(storageDirectory, 0o700))
		require.NoError(t, os.WriteFile(filepath.Join(baseDirectory, "secret.txt"), []byte("secret"), 0o600))

		source, err := OpenSubmissionFile(storageDirectory, &SubmissionFile{StoragePath: "../secret.txt"})

		assert.Error(t, err)
		assert.Nil(t, source)
	})

	t.Run("does not follow upload symlinks outside storage", func(t *testing.T) {
		baseDirectory := t.TempDir()
		storageDirectory := filepath.Join(baseDirectory, "storage")
		require.NoError(t, os.Mkdir(storageDirectory, 0o700))
		secretPath := filepath.Join(baseDirectory, "secret.txt")
		require.NoError(t, os.WriteFile(secretPath, []byte("secret"), 0o600))
		require.NoError(t, os.Symlink(secretPath, filepath.Join(storageDirectory, "secret.txt")))

		source, err := OpenSubmissionFile(storageDirectory, &SubmissionFile{StoragePath: "secret.txt"})

		assert.Error(t, err)
		assert.Nil(t, source)
	})

	t.Run("does not delete relative uploads when the data directory is empty", func(t *testing.T) {
		workingDirectory := t.TempDir()
		t.Chdir(workingDirectory)
		filePath := filepath.Join("uploads", "12", "34", "keep.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(filePath), 0o700))
		require.NoError(t, os.WriteFile(filePath, []byte("keep"), 0o600))

		require.NoError(t, DeleteSubmissionFiles("", 12, 34))
		assert.FileExists(t, filePath)
	})

	t.Run("does not follow directory symlinks while deleting uploads", func(t *testing.T) {
		baseDirectory := t.TempDir()
		storageDirectory := filepath.Join(baseDirectory, "storage")
		outsideDirectory := filepath.Join(baseDirectory, "outside")
		require.NoError(t, os.MkdirAll(filepath.Join(storageDirectory, "uploads"), 0o700))
		require.NoError(t, os.MkdirAll(filepath.Join(outsideDirectory, "34"), 0o700))
		outsideFile := filepath.Join(outsideDirectory, "34", "keep.txt")
		require.NoError(t, os.WriteFile(outsideFile, []byte("keep"), 0o600))
		require.NoError(t, os.Symlink(outsideDirectory, filepath.Join(storageDirectory, "uploads", "12")))

		err := DeleteSubmissionFiles(storageDirectory, 12, 34)

		assert.Error(t, err)
		assert.FileExists(t, outsideFile)
	})

	t.Run("removes a partial batch after an IO failure", func(t *testing.T) {
		root := t.TempDir()
		_, err := SaveFiles(root, 12, 34, []*UploadedFile{
			{Filename: "first.pdf", Data: bytes.NewBufferString("saved")},
			{Filename: "broken.pdf", Data: iotest.ErrReader(errors.New("read failed"))},
		})

		assert.Error(t, err)
		_, statErr := os.Stat(filepath.Join(root, "uploads", "12", "34"))
		assert.True(t, os.IsNotExist(statErr))
	})
}
