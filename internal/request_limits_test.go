package internal_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/karloscodes/cartridge"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	cartridgetestsupport "github.com/karloscodes/cartridge/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/database"
	"github.com/matteodante/miniform/internal/forms"
)

func TestRequestBodyLimits(t *testing.T) {
	t.Run("rejects compressed bodies before parsing public submissions or login", func(t *testing.T) {
		for _, test := range []struct {
			name, format, path string
		}{
			{"JSON submission", "json", "submit"},
			{"multipart submission", "multipart", "submit"},
			{"multipart login", "multipart", "/admin/login"},
		} {
			t.Run(test.name, func(t *testing.T) {
				server, form := limitedSubmissionServer(t, 64)
				body, contentType := submissionRequestBody(t, test.format, "attachment", "empty.txt", nil)
				var compressed bytes.Buffer
				writer := gzip.NewWriter(&compressed)
				_, err := writer.Write(body)
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				path := test.path
				if path == "submit" {
					path = "/forms/" + form.Slug + "/submit?token=" + form.Token
				}
				request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, &compressed)
				request.Header.Set("Content-Type", contentType)
				request.Header.Set("Content-Encoding", "gzip")
				request.Header.Set("Origin", "https://example.com")
				response, err := server.App.Test(request, -1)
				require.NoError(t, err)
				defer response.Body.Close()
				assert.Equal(t, http.StatusUnsupportedMediaType, response.StatusCode)
				assert.Empty(t, response.Cookies())
				assertSubmissionCounts(t, server, 0, 0)
			})
		}
	})

	t.Run("preserves plain and identity encoded submissions", func(t *testing.T) {
		for _, format := range []string{"json", "form", "multipart"} {
			for _, encoding := range []string{"", "Identity"} {
				t.Run(format+"/"+encoding, func(t *testing.T) {
					server, form := limitedSubmissionServer(t, 64)
					body, contentType := submissionRequestBody(t, format, "attachment", "empty.txt", nil)
					request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
						"/forms/"+form.Slug+"/submit?token="+form.Token, bytes.NewReader(body))
					request.Header.Set("Content-Type", contentType)
					request.Header.Set("Origin", "https://example.com")
					if encoding != "" {
						request.Header.Set("Content-Encoding", encoding)
					}
					response, err := server.App.Test(request, -1)
					require.NoError(t, err)
					defer response.Body.Close()
					assert.Equal(t, http.StatusOK, response.StatusCode)
					fileCount := int64(0)
					if format == "multipart" {
						fileCount = 1
					}
					assertSubmissionCounts(t, server, 1, fileCount)
				})
			}
		}
	})

	t.Run("includes file metadata in the payload budget", func(t *testing.T) {
		for _, test := range []struct {
			name, field, filename string
			values                map[string]string
			budget, status        int
		}{
			{"field name too long", strings.Repeat("a", forms.MaxFileFieldBytes+1), "empty.txt", nil, 1024, http.StatusBadRequest},
			{"filename too long", "attachment", strings.Repeat("a", forms.MaxFilenameBytes) + ".txt", nil, 1024, http.StatusBadRequest},
			{"metadata alone exceeds budget", "attachment", "empty.txt", nil, 18, http.StatusRequestEntityTooLarge},
			{"text and metadata share budget", "attachment", "empty.txt", map[string]string{"name": "Ada"}, 25, http.StatusRequestEntityTooLarge},
			{"metadata at budget", "attachment", "empty.txt", nil, 19, http.StatusOK},
			{"text and metadata at budget", "attachment", "empty.txt", map[string]string{"name": "Ada"}, 26, http.StatusOK},
		} {
			t.Run(test.name, func(t *testing.T) {
				server, form := limitedSubmissionServer(t, test.budget)
				body, contentType := submissionRequestBody(t, "multipart", test.field, test.filename, test.values)
				request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
					"/forms/"+form.Slug+"/submit?token="+form.Token, bytes.NewReader(body))
				request.Header.Set("Content-Type", contentType)
				request.Header.Set("Origin", "https://example.com")
				response, err := server.App.Test(request, -1)
				require.NoError(t, err)
				defer response.Body.Close()
				responseBody, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				assert.Equal(t, test.status, response.StatusCode, string(responseBody))
				if test.status == http.StatusOK {
					assertSubmissionCounts(t, server, 1, 1)
				} else {
					assert.Less(t, len(responseBody), 200)
					assertSubmissionCounts(t, server, 0, 0)
				}
			})
		}
	})
}

func limitedSubmissionServer(t *testing.T, maxPayloadBytes int) (*cartridgetestsupport.TestServer, *forms.Form) {
	t.Helper()
	cfg := &config.Config{
		Config: &cartridgeconfig.Config{
			AppName: "miniform", Environment: cartridgeconfig.Test,
			SessionSecret: "test-secret", SessionTimeout: 3600, DataDirectory: t.TempDir(),
		},
		MaxInputFields: 200, MaxPayloadBytes: maxPayloadBytes,
		MaxUploadStorageBytes: 1, PublicGlobalRateLimit: 120,
	}
	server := cartridgetestsupport.NewTestServer(t, cartridgetestsupport.TestServerOptions{
		Models: database.Models(), ServerConfig: testServerConfig(t),
		RouteMountFunc: func(server *cartridge.Server) { internal.MountRoutes(server, cfg) },
	})
	form, err := forms.Create(slog.New(slog.NewTextHandler(io.Discard, nil)), server.DB.GetConnection(), forms.CreateParams{
		Name: "Contact", Slug: "contact", AllowedOrigins: "example.com", UploadsEnabled: true,
	})
	require.NoError(t, err)
	return server, form
}

func submissionRequestBody(t *testing.T, format, field, filename string, values map[string]string) ([]byte, string) {
	t.Helper()
	switch format {
	case "json":
		return []byte(`{"name":"Ada"}`), "application/json"
	case "form":
		return []byte("name=Ada"), "application/x-www-form-urlencoded"
	default:
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		for name, value := range values {
			require.NoError(t, writer.WriteField(name, value))
		}
		_, err := writer.CreateFormFile(field, filename)
		require.NoError(t, err)
		require.NoError(t, writer.Close())
		return body.Bytes(), writer.FormDataContentType()
	}
}

func assertSubmissionCounts(t *testing.T, server *cartridgetestsupport.TestServer, submissions, files int64) {
	t.Helper()
	var count int64
	require.NoError(t, server.DB.GetConnection().Model(&forms.Submission{}).Count(&count).Error)
	assert.Equal(t, submissions, count)
	require.NoError(t, server.DB.GetConnection().Model(&forms.SubmissionFile{}).Count(&count).Error)
	assert.Equal(t, files, count)
}
