package server

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequireUncompressedBody(t *testing.T) {
	for _, test := range []struct {
		name     string
		headers  []string
		accepted bool
	}{
		{"no encoding", nil, true},
		{"identity", []string{"identity"}, true},
		{"identity case and whitespace", []string{" Identity "}, true},
		{"multiple identity headers", []string{"identity", "IDENTITY"}, true},
		{"gzip", []string{"gzip"}, false},
		{"gzip case", []string{"GZip"}, false},
		{"brotli", []string{"br"}, false},
		{"deflate", []string{"deflate"}, false},
		{"zstd", []string{"zstd"}, false},
		{"stacked encoding", []string{"identity, gzip"}, false},
		{"gzip before identity header", []string{"gzip", "identity"}, false},
		{"gzip after identity header", []string{"identity", "gzip"}, false},
		{"oversized unknown encoding", []string{strings.Repeat("a", 1000)}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(RequireUncompressedBody)
			called := false
			app.Post("/", func(ctx *fiber.Ctx) error {
				called = true
				assert.Empty(t, ctx.Get(fiber.HeaderContentEncoding))
				return ctx.SendStatus(http.StatusNoContent)
			})
			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", strings.NewReader("body"))
			for _, encoding := range test.headers {
				request.Header.Add("Content-Encoding", encoding)
			}
			response, err := app.Test(request)
			require.NoError(t, err)
			defer response.Body.Close()
			assert.Equal(t, test.accepted, called)
			if test.accepted {
				assert.Equal(t, http.StatusNoContent, response.StatusCode)
			} else {
				assert.Equal(t, http.StatusUnsupportedMediaType, response.StatusCode)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				assert.Equal(t, "compressed request bodies are not supported", string(body))
			}
		})
	}
}
