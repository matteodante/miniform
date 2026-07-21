package internal_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/forms"
)

func TestPublicSubmissionRoutes(t *testing.T) {
	t.Run("allows only the public submission content type header", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		request := httptest.NewRequestWithContext(t.Context(), http.MethodOptions, "/forms/contact/submit", nil)
		request.Header.Set("Origin", "https://example.com")
		request.Header.Set("Access-Control-Request-Method", http.MethodPost)
		request.Header.Set("Access-Control-Request-Headers", "Content-Type")

		response, err := server.App.Test(request, -1)
		require.NoError(t, err)
		defer response.Body.Close()

		assert.Equal(t, http.StatusNoContent, response.StatusCode)
		assert.Equal(t, "Content-Type", response.Header.Get("Access-Control-Allow-Headers"))
		assert.NotContains(t, response.Header.Get("Access-Control-Allow-Headers"), "Authorization")
		assert.NotContains(t, response.Header.Get("Access-Control-Allow-Headers"), "User-Agent")
	})

	t.Run("resolves a relative success redirect on the verified form origin", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		require.NoError(t, server.DB.GetConnection().Create(&forms.Form{
			Name: "Contact", Slug: "contact", Token: "secret-token", AllowedOrigins: "example.com",
		}).Error)

		body := url.Values{"field": {"value"}, "_success_url": {"thanks"}}.Encode()
		request := httptest.NewRequestWithContext(
			t.Context(), "POST", "/forms/contact/submit?token=secret-token", strings.NewReader(body),
		)
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Origin", "https://example.com")
		response, err := server.App.Test(request, -1)
		require.NoError(t, err)
		defer response.Body.Close()

		assert.Equal(t, 302, response.StatusCode)
		assert.Equal(t, "https://example.com/thanks", response.Header.Get("Location"))
	})

	t.Run("rejects submissions made only of internal controls", func(t *testing.T) {
		tests := []struct {
			name, body, location string
			status               int
		}{
			{"empty honeypot", "__mf_hp=", "", http.StatusBadRequest},
			{"empty legacy honeypot", "__fl_hp=", "", http.StatusBadRequest},
			{"success redirect", "_success_url=thanks", "", http.StatusBadRequest},
			{"error redirect", "_error_url=failed", "https://example.com/failed", http.StatusFound},
			{"obsolete captcha token", "cf-turnstile-response=obsolete", "", http.StatusBadRequest},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				server := testServer(t, cartridgeconfig.Test)
				require.NoError(t, server.DB.GetConnection().Create(&forms.Form{
					Name: "Contact", Slug: "contact", Token: "secret-token", AllowedOrigins: "example.com",
				}).Error)

				request := httptest.NewRequestWithContext(
					t.Context(), http.MethodPost, "/forms/contact/submit?token=secret-token", strings.NewReader(test.body),
				)
				request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				request.Header.Set("Origin", "https://example.com")
				response, err := server.App.Test(request, -1)
				require.NoError(t, err)
				defer response.Body.Close()

				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				assert.Equal(t, test.status, response.StatusCode)
				assert.Equal(t, test.location, response.Header.Get("Location"))
				if test.status == http.StatusBadRequest {
					assert.Contains(t, string(body), "submission payload empty")
				}
				var count int64
				require.NoError(t, server.DB.GetConnection().Model(&forms.Submission{}).Count(&count).Error)
				assert.Zero(t, count)
			})
		}
	})
}
