package internal_test

import (
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/karloscodes/cartridge"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	cartridgetestsupport "github.com/karloscodes/cartridge/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/matteodante/miniform/internal"
	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/database"
	"github.com/matteodante/miniform/internal/forms"
)

const fetchMetadataRejection = "browser requests only"

func testServer(t *testing.T, environment string) *cartridgetestsupport.TestServer {
	t.Helper()
	appConfig := &config.Config{
		Config: &cartridgeconfig.Config{
			AppName: "miniform", Environment: environment,
			SessionSecret: "test-secret", SessionTimeout: 3600,
		},
		MaxInputFields: 200,
	}
	server := cartridgetestsupport.NewTestServer(t, cartridgetestsupport.TestServerOptions{
		Models: database.Models(),
		RouteMountFunc: func(server *cartridge.Server) {
			server.SetSession(cartridge.NewSessionManager(cartridge.SessionConfig{
				CookieName: "miniform_session", Secret: "test-secret",
				TTL: time.Hour, LoginPath: "/admin/login",
			}))
			internal.MountRoutes(server, appConfig)
		},
	})
	server.DB.GetConnection().Config.NowFunc = func() time.Time { return time.Now().UTC() }
	return server
}

func postForm(t *testing.T, server *cartridgetestsupport.TestServer, path, body string, headers map[string]string) (int, string) {
	t.Helper()
	request := httptest.NewRequestWithContext(t.Context(), "POST", path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for name, value := range headers {
		request.Header.Set(name, value)
	}
	response, err := server.App.Test(request, -1)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(content)
}

func getPage(t *testing.T, server *cartridgetestsupport.TestServer, path string) (int, string) {
	t.Helper()
	response := server.Get(path)
	defer func() { _ = response.Body.Close() }()
	content, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	return response.StatusCode, string(content)
}

func createAdmin(t *testing.T, server *cartridgetestsupport.TestServer) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("miniform"), bcrypt.DefaultCost)
	require.NoError(t, err)
	now := time.Now().UTC()
	require.NoError(t, server.DB.GetConnection().Create(&accounts.User{
		Email: "admin@miniform.local", PasswordHash: string(hash), LastLoginAt: &now,
	}).Error)
}

func TestRoutes(t *testing.T) {
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(func() { slog.SetDefault(previousLogger) })

	t.Run("allows public POST entry points without fetch metadata", func(t *testing.T) {
		cases := []struct{ name, path, body string }{
			{"login", "/admin/login", "email=admin@miniform.local&password=miniform"},
			{"form ingestion", "/forms/missing/submit?token=x", "field=value"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				server := testServer(t, cartridgeconfig.Test)
				createAdmin(t, server)
				status, body := postForm(t, server, tc.path, tc.body, nil)
				assert.NotEqual(t, 403, status)
				assert.NotContains(t, body, fetchMetadataRejection)
			})
		}
	})

	t.Run("protects state-changing admin routes", func(t *testing.T) {
		for _, path := range []string{"/admin/logout", "/admin/forms", "/admin/settings/password"} {
			t.Run(path, func(t *testing.T) {
				status, body := postForm(t, testServer(t, cartridgeconfig.Test), path, "", nil)
				assert.Equal(t, 403, status)
				assert.Contains(t, body, fetchMetadataRejection)
			})
		}
	})

	t.Run("accepts same-origin login", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		createAdmin(t, server)
		status, _ := postForm(t, server, "/admin/login",
			"email=admin@miniform.local&password=miniform",
			map[string]string{"Sec-Fetch-Site": "same-origin"})
		assert.Equal(t, 302, status)
	})

	t.Run("requires both form token and allowed origin", func(t *testing.T) {
		cases := []struct {
			name, path string
			headers    map[string]string
			status     int
			message    string
		}{
			{"missing token", "/forms/contact/submit", map[string]string{"Origin": "https://example.com"}, 401, ""},
			{"wrong token", "/forms/contact/submit?token=wrong", map[string]string{"Origin": "https://example.com"}, 401, ""},
			{"foreign origin", "/forms/contact/submit?token=secret-token", map[string]string{"Origin": "https://attacker.test"}, 403, "origin not allowed"},
			{"missing origin", "/forms/contact/submit?token=secret-token", nil, 403, "origin not allowed"},
			{"valid request", "/forms/contact/submit?token=secret-token", map[string]string{"Origin": "https://example.com"}, 200, ""},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				server := testServer(t, cartridgeconfig.Test)
				require.NoError(t, server.DB.GetConnection().Create(&forms.Form{
					Name: "Contact", Slug: "contact", Token: "secret-token", AllowedOrigins: "example.com",
				}).Error)
				status, body := postForm(t, server, tc.path, "field=value", tc.headers)
				assert.Equal(t, tc.status, status)
				if tc.message != "" {
					assert.Contains(t, body, tc.message)
				}
			})
		}
	})

	t.Run("exposes demo guidance only outside production", func(t *testing.T) {
		for _, tc := range []struct {
			name, environment string
			contains          bool
		}{
			{"test", cartridgeconfig.Test, true},
			{"production", cartridgeconfig.Production, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				status, body := getPage(t, testServer(t, tc.environment), "/_demo")
				assert.Equal(t, 404, status)
				assert.Equal(t, tc.contains, strings.Contains(body, "Run 'make demo'"))
			})
		}
	})
}
