package internal_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/template/html/v2"
	"github.com/karloscodes/cartridge"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	cartridgetestsupport "github.com/karloscodes/cartridge/testsupport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal"
	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/database"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
	miniformserver "github.com/matteodante/miniform/internal/server"
	"github.com/matteodante/miniform/web"
)

const fetchMetadataRejection = "browser requests only"

type unavailableDatabase struct{}

func (unavailableDatabase) Connect() (*gorm.DB, error) {
	return nil, errors.New("database unavailable")
}
func (unavailableDatabase) GetConnection() *gorm.DB { return nil }

func testServer(t *testing.T, environment string) *cartridgetestsupport.TestServer {
	t.Helper()
	appConfig := &config.Config{
		Config: &cartridgeconfig.Config{
			AppName: "miniform", Environment: environment,
			SessionSecret: "test-secret", SessionTimeout: 3600, DataDirectory: t.TempDir(),
		},
		MaxInputFields: 200,
	}
	server := cartridgetestsupport.NewTestServer(t, cartridgetestsupport.TestServerOptions{
		Models:       database.Models(),
		ServerConfig: testServerConfig(t),
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

func testServerConfig(t *testing.T) *cartridge.ServerConfig {
	t.Helper()
	engine := html.NewFileSystem(http.FS(web.Templates), ".html")
	engine.AddFunc("render", func(name string, data any) (template.HTML, error) {
		if !engine.Loaded {
			if err := engine.Load(); err != nil {
				return "", err
			}
		}
		view := engine.Templates.Lookup(name)
		if view == nil {
			return "", fmt.Errorf("template %q not found", name)
		}
		var output bytes.Buffer
		if err := view.Execute(&output, data); err != nil {
			return "", err
		}
		return template.HTML(output.String()), nil
	})
	for name, function := range miniformserver.TemplateFuncs() {
		engine.AddFunc(name, function)
	}
	config := cartridge.DefaultServerConfig()
	config.ViewsEngine = engine
	return config
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

	t.Run("serves immutable same-origin assets and omits the removed SDK", func(t *testing.T) {
		defaultLogger := slog.Default()
		t.Cleanup(func() { slog.SetDefault(defaultLogger) })
		dataDirectory := t.TempDir()
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)
		port := fmt.Sprint(listener.Addr().(*net.TCPAddr).Port)
		require.NoError(t, listener.Close())
		t.Setenv("MINIFORM_ENV", cartridgeconfig.Test)
		t.Setenv("MINIFORM_PORT", port)
		t.Setenv("MINIFORM_DATA_DIR", dataDirectory)
		t.Setenv("MINIFORM_LOGS_DIR", dataDirectory)
		t.Setenv("MINIFORM_DATABASE_PATH", dataDirectory+"/miniform.db")
		t.Setenv("MINIFORM_SESSION_SECRET", "test-secret")

		app, err := internal.NewApp()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, app.Close()) })

		request := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/assets/mark.svg", nil)
		response, err := app.Server.App().Test(request, -1)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, response.StatusCode)
		assert.Equal(t, "same-origin", response.Header.Get(fiber.HeaderCrossOriginResourcePolicy))
		assert.Equal(t, "public, max-age=31536000", response.Header.Get(fiber.HeaderCacheControl))
		require.NoError(t, response.Body.Close())

		request = httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/assets/miniform.js", nil)
		response, err = app.Server.App().Test(request, -1)
		require.NoError(t, err)
		assert.Equal(t, http.StatusNotFound, response.StatusCode)
		require.NoError(t, response.Body.Close())
	})

	t.Run("returns the public JSON error contract when rate limited", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Production)
		for range 30 {
			status, _ := postForm(t, server, "/forms/missing/submit?token=invalid", "name=Ada", nil)
			require.Equal(t, http.StatusNotFound, status)
		}

		status, body := postForm(t, server, "/forms/missing/submit?token=invalid", "name=Ada", nil)

		assert.Equal(t, http.StatusTooManyRequests, status)
		assert.JSONEq(t, `{"ok":false,"error":"rate limit exceeded"}`, body)
	})

	t.Run("reports an unavailable database as not ready", func(t *testing.T) {
		appConfig := &config.Config{Config: &cartridgeconfig.Config{
			AppName: "miniform", Environment: cartridgeconfig.Test, SessionSecret: "test-secret",
		}}
		serverConfig := testServerConfig(t)
		serverConfig.Config = appConfig
		serverConfig.Logger = slog.Default()
		serverConfig.DBManager = unavailableDatabase{}
		server, err := cartridge.NewServer(serverConfig)
		require.NoError(t, err)
		server.SetSession(cartridge.NewSessionManager(cartridge.SessionConfig{Secret: "test-secret"}))
		internal.MountRoutes(server, appConfig)

		response, err := server.App().Test(httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_health", nil), -1)
		require.NoError(t, err)
		defer response.Body.Close()
		assert.Equal(t, http.StatusServiceUnavailable, response.StatusCode)
	})

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

	t.Run("accepts a submission containing only an upload", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		form, err := forms.Create(slog.Default(), server.DB.GetConnection(), forms.CreateParams{
			Name: "Upload", Slug: "upload", AllowedOrigins: "example.com",
		})
		require.NoError(t, err)
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("attachment", "note.txt")
		require.NoError(t, err)
		_, err = io.WriteString(part, "file only")
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			"/forms/upload/submit?token="+form.Token, &body)
		request.Header.Set("Content-Type", writer.FormDataContentType())
		request.Header.Set("Origin", "https://example.com")
		response, err := server.App.Test(request, -1)
		require.NoError(t, err)
		defer response.Body.Close()

		assert.Equal(t, http.StatusOK, response.StatusCode)
		var fileCount int64
		require.NoError(t, server.DB.GetConnection().Model(&forms.SubmissionFile{}).Count(&fileCount).Error)
		assert.Equal(t, int64(1), fileCount)
	})

	t.Run("protects state-changing admin routes", func(t *testing.T) {
		for _, path := range []string{"/admin/logout", "/admin/forms", "/admin/forms/1/emails/preview", "/admin/settings/password"} {
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

	t.Run("configures multiple email recipients and HTML from the admin editor", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		createAdmin(t, server)
		mailer, err := integrations.CreateMailerProfile(slog.Default(), server.DB.GetConnection(), integrations.MailerProfileParams{
			Name: "SMTP", DefaultFromEmail: "forms@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)

		login := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/login",
			strings.NewReader("email=admin@miniform.local&password=miniform"))
		login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		login.Header.Set("Sec-Fetch-Site", "same-origin")
		loginResponse, err := server.App.Test(login, -1)
		require.NoError(t, err)
		require.NoError(t, loginResponse.Body.Close())
		require.NotEmpty(t, loginResponse.Cookies())

		values := url.Values{
			"name": {"Email"}, "slug": {"email"}, "allowed_origins": {"*"},
			"email_enabled": {"on"}, "mailer_profile_id": {fmt.Sprint(mailer.ID)},
			"email_recipient": {"owner@example.com\narchive@example.com"}, "email_format": {"html"},
		}
		request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/forms", strings.NewReader(values.Encode()))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		request.AddCookie(loginResponse.Cookies()[0])
		response, err := server.App.Test(request, -1)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusFound, response.StatusCode)

		created, err := forms.GetBySlug(server.DB.GetConnection(), "email")
		require.NoError(t, err)
		delivery := forms.PrimaryEmailDelivery(created)
		require.NotNil(t, delivery)
		assert.Equal(t, "owner@example.com, archive@example.com", delivery.Recipient)
		assert.Equal(t, forms.EmailFormatHTML, delivery.Format)

		edit := httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/admin/forms/%d/edit", created.ID), nil)
		edit.AddCookie(loginResponse.Cookies()[0])
		editResponse, err := server.App.Test(edit, -1)
		require.NoError(t, err)
		body, err := io.ReadAll(editResponse.Body)
		require.NoError(t, err)
		require.NoError(t, editResponse.Body.Close())
		assert.Contains(t, string(body), "owner@example.com, archive@example.com")
		assert.Contains(t, string(body), `option value="html" selected`)
	})

	t.Run("manages multiple email notifications and Reply-To from the admin UI", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		createAdmin(t, server)
		mailer, err := integrations.CreateMailerProfile(slog.Default(), server.DB.GetConnection(), integrations.MailerProfileParams{
			Name: "SMTP", DefaultFromName: "PiùUDITO", DefaultFromEmail: "forms@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)
		form, err := forms.Create(slog.Default(), server.DB.GetConnection(), forms.CreateParams{
			Name: "Booking", Slug: "booking-notifications", AllowedOrigins: "*",
		})
		require.NoError(t, err)

		login := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/login",
			strings.NewReader("email=admin@miniform.local&password=miniform"))
		login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		login.Header.Set("Sec-Fetch-Site", "same-origin")
		loginResponse, err := server.App.Test(login, -1)
		require.NoError(t, err)
		require.NoError(t, loginResponse.Body.Close())
		require.NotEmpty(t, loginResponse.Cookies())
		cookie := loginResponse.Cookies()[0]

		values := url.Values{
			"name": {"Customer confirmation"}, "enabled": {"on"},
			"mailer_profile_id": {fmt.Sprint(mailer.ID)},
			"recipient_source":  {"field"}, "recipient": {"email"},
			"reply_to_source": {"static"}, "reply_to": {"Support <support@example.com>"},
			"subject_template": {"Thanks {{.Fields.name}}"}, "format": {"html"},
			"text_template": {"Hello {{.Fields.name}}"}, "html_template": {"<p>Hello {{.Fields.name}}</p>"},
		}
		create := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			fmt.Sprintf("/admin/forms/%d/emails", form.ID), strings.NewReader(values.Encode()))
		create.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		create.Header.Set("Sec-Fetch-Site", "same-origin")
		create.AddCookie(cookie)
		response, err := server.App.Test(create, -1)
		require.NoError(t, err)
		require.NoError(t, response.Body.Close())
		assert.Equal(t, http.StatusFound, response.StatusCode)

		deliveries, err := forms.ListEmailDeliveries(server.DB.GetConnection(), form.ID)
		require.NoError(t, err)
		require.Len(t, deliveries, 1)
		created := deliveries[0]
		assert.Equal(t, forms.EmailRecipientField, created.RecipientSource)
		assert.Equal(t, "email", created.Recipient)
		assert.Equal(t, forms.EmailReplyToStatic, created.ReplyToSource)
		assert.Equal(t, `"Support" <support@example.com>`, created.ReplyTo)

		form, err = forms.GetByID(server.DB.GetConnection(), form.ID)
		require.NoError(t, err)
		submission, err := forms.CreateSubmissionWithFiles(slog.Default(), server.DB.GetConnection(), form, map[string]any{
			"name": "<Ada>", "email": "ada@example.com",
		}, "preview route test", "", nil)
		require.NoError(t, err)
		var eventCountBefore int64
		require.NoError(t, server.DB.GetConnection().Model(&forms.EmailEvent{}).Count(&eventCountBefore).Error)

		edit := httptest.NewRequestWithContext(t.Context(), http.MethodGet,
			fmt.Sprintf("/admin/forms/%d/emails/%d/edit", form.ID, created.ID), nil)
		edit.AddCookie(cookie)
		editResponse, err := server.App.Test(edit, -1)
		require.NoError(t, err)
		body, err := io.ReadAll(editResponse.Body)
		require.NoError(t, err)
		require.NoError(t, editResponse.Body.Close())
		assert.Equal(t, http.StatusOK, editResponse.StatusCode)
		assert.Contains(t, string(body), "Customer confirmation")
		assert.Contains(t, string(body), "Thanks {{.Fields.name}}")
		assert.Contains(t, string(body), "Preview email")
		assert.Contains(t, string(body), fmt.Sprintf("Entry #%d", submission.ID))

		previewValues := url.Values{
			"mailer_profile_id": {fmt.Sprint(mailer.ID)},
			"recipient_source":  {"field"}, "recipient": {"email"},
			"reply_to_source": {"static"}, "reply_to": {"Support <support@example.com>"},
			"subject_template": {"Preview {{.Fields.name}}"}, "format": {"html"},
			"text_template": {"Hello {{.Fields.name}}"}, "html_template": {"<p>Hello {{.Fields.name}}</p>"},
			"preview_submission_id": {fmt.Sprint(submission.ID)},
		}
		preview := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			fmt.Sprintf("/admin/forms/%d/emails/preview", form.ID), strings.NewReader(previewValues.Encode()))
		preview.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		preview.Header.Set("Sec-Fetch-Site", "same-origin")
		preview.AddCookie(cookie)
		previewResponse, err := server.App.Test(preview, -1)
		require.NoError(t, err)
		var previewBody struct {
			OK      bool     `json:"ok"`
			From    string   `json:"from"`
			To      []string `json:"to"`
			ReplyTo string   `json:"reply_to"`
			Subject string   `json:"subject"`
			Text    string   `json:"text"`
			HTML    string   `json:"html"`
		}
		require.NoError(t, json.NewDecoder(previewResponse.Body).Decode(&previewBody))
		require.NoError(t, previewResponse.Body.Close())
		assert.Equal(t, http.StatusOK, previewResponse.StatusCode)
		assert.Equal(t, "no-store", previewResponse.Header.Get("Cache-Control"))
		assert.True(t, previewBody.OK)
		assert.Equal(t, "PiùUDITO <forms@example.com>", previewBody.From)
		assert.Equal(t, []string{"ada@example.com"}, previewBody.To)
		assert.Equal(t, `"Support" <support@example.com>`, previewBody.ReplyTo)
		assert.Equal(t, "Preview <Ada>", previewBody.Subject)
		assert.Equal(t, "Hello <Ada>", previewBody.Text)
		assert.Equal(t, "<p>Hello &lt;Ada&gt;</p>", previewBody.HTML)
		var eventCountAfter int64
		require.NoError(t, server.DB.GetConnection().Model(&forms.EmailEvent{}).Count(&eventCountAfter).Error)
		assert.Equal(t, eventCountBefore, eventCountAfter)

		remove := httptest.NewRequestWithContext(t.Context(), http.MethodPost,
			fmt.Sprintf("/admin/forms/%d/emails/%d/delete", form.ID, created.ID), nil)
		remove.Header.Set("Sec-Fetch-Site", "same-origin")
		remove.AddCookie(cookie)
		removeResponse, err := server.App.Test(remove, -1)
		require.NoError(t, err)
		require.NoError(t, removeResponse.Body.Close())
		assert.Equal(t, http.StatusFound, removeResponse.StatusCode)
		deliveries, err = forms.ListEmailDeliveries(server.DB.GetConnection(), form.ID)
		require.NoError(t, err)
		assert.Empty(t, deliveries)
	})

	t.Run("sends temporary accounts to password settings", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		created, err := accounts.EnsureAdmin(slog.Default(), server.DB.GetConnection(), "temporary-password", false, nil)
		require.NoError(t, err)
		require.True(t, created)

		request := httptest.NewRequestWithContext(t.Context(), "POST", "/admin/login",
			strings.NewReader("email=admin@miniform.local&password=temporary-password"))
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Sec-Fetch-Site", "same-origin")
		response, err := server.App.Test(request, -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()

		assert.Equal(t, 302, response.StatusCode)
		assert.Equal(t, "/admin/settings", response.Header.Get("Location"))
		require.NotEmpty(t, response.Cookies())

		protected := httptest.NewRequestWithContext(t.Context(), "GET", "/admin/submissions", nil)
		protected.AddCookie(response.Cookies()[0])
		protectedResponse, err := server.App.Test(protected, -1)
		require.NoError(t, err)
		defer func() { _ = protectedResponse.Body.Close() }()
		assert.Equal(t, 302, protectedResponse.StatusCode)
		assert.Equal(t, "/admin/settings", protectedResponse.Header.Get("Location"))

		settings := httptest.NewRequestWithContext(t.Context(), "GET", "/admin/settings", nil)
		settings.AddCookie(response.Cookies()[0])
		settingsResponse, err := server.App.Test(settings, -1)
		require.NoError(t, err)
		defer func() { _ = settingsResponse.Body.Close() }()
		settingsBody, err := io.ReadAll(settingsResponse.Body)
		require.NoError(t, err)
		assert.Equal(t, 200, settingsResponse.StatusCode)
		assert.Contains(t, string(settingsBody), "Change your temporary password")
		assert.NotContains(t, string(settingsBody), `href="/admin/submissions"`)
		assert.NotContains(t, string(settingsBody), `href="/admin/forms"`)

		unchanged := httptest.NewRequestWithContext(t.Context(), "POST", "/admin/settings/password",
			strings.NewReader("current_password=temporary-password&new_password=temporary-password&confirm_password=temporary-password"))
		unchanged.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		unchanged.Header.Set("Sec-Fetch-Site", "same-origin")
		unchanged.AddCookie(response.Cookies()[0])
		unchangedResponse, err := server.App.Test(unchanged, -1)
		require.NoError(t, err)
		defer func() { _ = unchangedResponse.Body.Close() }()
		unchangedBody, err := io.ReadAll(unchangedResponse.Body)
		require.NoError(t, err)
		assert.Equal(t, 200, unchangedResponse.StatusCode)
		assert.Contains(t, string(unchangedBody), "different from the current password")

		changed := httptest.NewRequestWithContext(t.Context(), "POST", "/admin/settings/password",
			strings.NewReader("current_password=temporary-password&new_password=replacement-password&confirm_password=replacement-password"))
		changed.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		changed.Header.Set("Sec-Fetch-Site", "same-origin")
		changed.AddCookie(response.Cookies()[0])
		changedResponse, err := server.App.Test(changed, -1)
		require.NoError(t, err)
		defer func() { _ = changedResponse.Body.Close() }()
		changedBody, err := io.ReadAll(changedResponse.Body)
		require.NoError(t, err)
		assert.Equal(t, 200, changedResponse.StatusCode)
		assert.Contains(t, string(changedBody), "Password updated successfully")
		assert.Contains(t, string(changedBody), `href="/admin/submissions"`)

		inbox := httptest.NewRequestWithContext(t.Context(), "GET", "/admin/submissions", nil)
		inbox.AddCookie(response.Cookies()[0])
		inboxResponse, err := server.App.Test(inbox, -1)
		require.NoError(t, err)
		defer func() { _ = inboxResponse.Body.Close() }()
		assert.Equal(t, 200, inboxResponse.StatusCode)
	})

	t.Run("redirects expired htmx sessions with a response header", func(t *testing.T) {
		request := httptest.NewRequestWithContext(t.Context(), "GET", "/admin/submissions", nil)
		request.Header.Set("HX-Request", "true")
		response, err := testServer(t, cartridgeconfig.Test).App.Test(request, -1)
		require.NoError(t, err)
		defer func() { _ = response.Body.Close() }()

		assert.Equal(t, 200, response.StatusCode)
		assert.Equal(t, "/admin/login", response.Header.Get("HX-Redirect"))
	})

	t.Run("does not render stored delivery secrets", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		createAdmin(t, server)
		mailer, err := integrations.CreateMailerProfile(slog.Default(), server.DB.GetConnection(), integrations.MailerProfileParams{
			Name: "SMTP", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com", SMTPPassword: "smtp-top-secret",
		})
		require.NoError(t, err)
		form, err := forms.Create(slog.Default(), server.DB.GetConnection(), forms.CreateParams{
			Name: "Webhook", Slug: "webhook", AllowedOrigins: "example.com",
			WebhookEnabled: true, WebhookURL: "https://example.com/hook",
			WebhookSecret: "webhook-top-secret", WebhookHeadersJSON: `{"Authorization":"Bearer top-secret"}`,
		})
		require.NoError(t, err)

		login := httptest.NewRequestWithContext(t.Context(), "POST", "/admin/login",
			strings.NewReader("email=admin@miniform.local&password=miniform"))
		login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		login.Header.Set("Sec-Fetch-Site", "same-origin")
		loginResponse, err := server.App.Test(login, -1)
		require.NoError(t, err)
		defer func() { _ = loginResponse.Body.Close() }()
		require.NotEmpty(t, loginResponse.Cookies())

		for _, page := range []string{
			fmt.Sprintf("/admin/settings/mailers/%d/edit", mailer.ID),
			fmt.Sprintf("/admin/forms/%d/edit", form.ID),
		} {
			request := httptest.NewRequestWithContext(t.Context(), "GET", page, nil)
			request.AddCookie(loginResponse.Cookies()[0])
			response, err := server.App.Test(request, -1)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.NoError(t, response.Body.Close())
			assert.Equal(t, 200, response.StatusCode)
			assert.NotContains(t, string(body), "smtp-top-secret")
			assert.NotContains(t, string(body), "webhook-top-secret")
			assert.NotContains(t, string(body), "Bearer top-secret")
		}

		updateMailer := httptest.NewRequestWithContext(t.Context(), "POST",
			fmt.Sprintf("/admin/settings/mailers/%d", mailer.ID),
			strings.NewReader("name=SMTP&default_from_email=sender%40example.com&smtp_host=smtp.example.com&smtp_port=587&smtp_encryption=starttls"))
		updateMailer.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		updateMailer.Header.Set("Sec-Fetch-Site", "same-origin")
		updateMailer.AddCookie(loginResponse.Cookies()[0])
		updateMailerResponse, err := server.App.Test(updateMailer, -1)
		require.NoError(t, err)
		require.NoError(t, updateMailerResponse.Body.Close())
		assert.Equal(t, 302, updateMailerResponse.StatusCode)
		updatedMailer, err := integrations.GetMailerProfileByID(server.DB.GetConnection(), mailer.ID)
		require.NoError(t, err)
		assert.Equal(t, "smtp-top-secret", updatedMailer.SMTPPassword)

		updateForm := httptest.NewRequestWithContext(t.Context(), "POST", fmt.Sprintf("/admin/forms/%d", form.ID),
			strings.NewReader("name=Webhook&allowed_origins=example.com&webhook_enabled=on&webhook_url=https%3A%2F%2Fexample.com%2Fhook"))
		updateForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		updateForm.Header.Set("Sec-Fetch-Site", "same-origin")
		updateForm.AddCookie(loginResponse.Cookies()[0])
		updateFormResponse, err := server.App.Test(updateForm, -1)
		require.NoError(t, err)
		require.NoError(t, updateFormResponse.Body.Close())
		assert.Equal(t, 302, updateFormResponse.StatusCode)
		updatedForm, err := forms.GetByID(server.DB.GetConnection(), form.ID)
		require.NoError(t, err)
		assert.Equal(t, "webhook-top-secret", updatedForm.WebhookDelivery.Secret)
		assert.JSONEq(t, `{"Authorization":"Bearer top-secret"}`, updatedForm.WebhookDelivery.HeadersJSON)

		invalidMailer := httptest.NewRequestWithContext(t.Context(), "POST",
			fmt.Sprintf("/admin/settings/mailers/%d", mailer.ID),
			strings.NewReader("name=&default_from_email=sender%40example.com&smtp_host=smtp.example.com&smtp_port=587&smtp_encryption=starttls&smtp_password=should-not-survive&clear_smtp_password=on"))
		invalidMailer.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		invalidMailer.Header.Set("Sec-Fetch-Site", "same-origin")
		invalidMailer.AddCookie(loginResponse.Cookies()[0])
		invalidMailerResponse, err := server.App.Test(invalidMailer, -1)
		require.NoError(t, err)
		invalidMailerBody, err := io.ReadAll(invalidMailerResponse.Body)
		require.NoError(t, err)
		require.NoError(t, invalidMailerResponse.Body.Close())
		assert.Equal(t, http.StatusOK, invalidMailerResponse.StatusCode)
		assert.Contains(t, string(invalidMailerBody), `name="clear_smtp_password" type="checkbox" checked`)
		assert.NotContains(t, string(invalidMailerBody), "smtp-top-secret")
		assert.NotContains(t, string(invalidMailerBody), "should-not-survive")
		unchangedMailer, err := integrations.GetMailerProfileByID(server.DB.GetConnection(), mailer.ID)
		require.NoError(t, err)
		assert.Equal(t, "smtp-top-secret", unchangedMailer.SMTPPassword)

		invalidForm := httptest.NewRequestWithContext(t.Context(), "POST", fmt.Sprintf("/admin/forms/%d", form.ID),
			strings.NewReader("name=&allowed_origins=example.com&webhook_enabled=on&webhook_url=https%3A%2F%2Fexample.com%2Fhook&webhook_secret=should-not-survive&webhook_headers=%7B%22X-Secret%22%3A%22should-not-survive%22%7D&clear_webhook_secret=on&clear_webhook_headers=on"))
		invalidForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		invalidForm.Header.Set("Sec-Fetch-Site", "same-origin")
		invalidForm.AddCookie(loginResponse.Cookies()[0])
		invalidFormResponse, err := server.App.Test(invalidForm, -1)
		require.NoError(t, err)
		invalidFormBody, err := io.ReadAll(invalidFormResponse.Body)
		require.NoError(t, err)
		require.NoError(t, invalidFormResponse.Body.Close())
		assert.Equal(t, http.StatusOK, invalidFormResponse.StatusCode)
		assert.Contains(t, string(invalidFormBody), `name="clear_webhook_secret" type="checkbox" checked`)
		assert.Contains(t, string(invalidFormBody), `name="clear_webhook_headers" type="checkbox" checked`)
		assert.NotContains(t, string(invalidFormBody), "webhook-top-secret")
		assert.NotContains(t, string(invalidFormBody), "Bearer top-secret")
		assert.NotContains(t, string(invalidFormBody), "should-not-survive")
		unchangedForm, err := forms.GetByID(server.DB.GetConnection(), form.ID)
		require.NoError(t, err)
		assert.Equal(t, "webhook-top-secret", unchangedForm.WebhookDelivery.Secret)
		assert.JSONEq(t, `{"Authorization":"Bearer top-secret"}`, unchangedForm.WebhookDelivery.HeadersJSON)

		clearMailer := httptest.NewRequestWithContext(t.Context(), "POST",
			fmt.Sprintf("/admin/settings/mailers/%d", mailer.ID),
			strings.NewReader("name=SMTP&default_from_email=sender%40example.com&smtp_host=smtp.example.com&smtp_port=587&smtp_encryption=starttls&smtp_password=should-not-survive&clear_smtp_password=on"))
		clearMailer.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		clearMailer.Header.Set("Sec-Fetch-Site", "same-origin")
		clearMailer.AddCookie(loginResponse.Cookies()[0])
		clearMailerResponse, err := server.App.Test(clearMailer, -1)
		require.NoError(t, err)
		require.NoError(t, clearMailerResponse.Body.Close())
		clearedMailer, err := integrations.GetMailerProfileByID(server.DB.GetConnection(), mailer.ID)
		require.NoError(t, err)
		assert.Empty(t, clearedMailer.SMTPPassword)

		clearForm := httptest.NewRequestWithContext(t.Context(), "POST", fmt.Sprintf("/admin/forms/%d", form.ID),
			strings.NewReader("name=Webhook&allowed_origins=example.com&webhook_enabled=on&webhook_url=https%3A%2F%2Fexample.com%2Fhook&webhook_secret=should-not-survive&webhook_headers=%7B%22X-Secret%22%3A%22should-not-survive%22%7D&clear_webhook_secret=on&clear_webhook_headers=on"))
		clearForm.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		clearForm.Header.Set("Sec-Fetch-Site", "same-origin")
		clearForm.AddCookie(loginResponse.Cookies()[0])
		clearFormResponse, err := server.App.Test(clearForm, -1)
		require.NoError(t, err)
		require.NoError(t, clearFormResponse.Body.Close())
		clearedForm, err := forms.GetByID(server.DB.GetConnection(), form.ID)
		require.NoError(t, err)
		assert.Empty(t, clearedForm.WebhookDelivery.Secret)
		assert.Empty(t, clearedForm.WebhookDelivery.HeadersJSON)
	})

	t.Run("keeps profile deletion conflicts inside the admin UI", func(t *testing.T) {
		server := testServer(t, cartridgeconfig.Test)
		createAdmin(t, server)
		mailer, err := integrations.CreateMailerProfile(slog.Default(), server.DB.GetConnection(), integrations.MailerProfileParams{
			Name: "Referenced SMTP", DefaultFromEmail: "sender@example.com", SMTPHost: "smtp.example.com",
		})
		require.NoError(t, err)
		captcha, err := integrations.CreateCaptchaProfile(slog.Default(), server.DB.GetConnection(), integrations.CaptchaProfileParams{
			Name: "Referenced safeguard", SiteKey: "site-key", SecretKey: "secret-key",
		})
		require.NoError(t, err)
		form := forms.Form{
			Name: "Contact", Slug: "contact", Token: "secret-token", AllowedOrigins: "example.com",
			CaptchaProfileID: &captcha.ID,
		}
		require.NoError(t, server.DB.GetConnection().Create(&form).Error)
		require.NoError(t, server.DB.GetConnection().Create(&forms.EmailDelivery{
			FormID: form.ID, Enabled: false, MailerProfileID: &mailer.ID, Recipient: "owner@example.com",
		}).Error)

		login := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/login",
			strings.NewReader("email=admin@miniform.local&password=miniform"))
		login.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		login.Header.Set("Sec-Fetch-Site", "same-origin")
		loginResponse, err := server.App.Test(login, -1)
		require.NoError(t, err)
		require.NoError(t, loginResponse.Body.Close())
		require.NotEmpty(t, loginResponse.Cookies())

		for _, test := range []struct {
			name, path, profileName, usageLabel string
		}{
			{"mailer", fmt.Sprintf("/admin/settings/mailers/%d/delete", mailer.ID), mailer.Name, "referenced delivery"},
			{"captcha", fmt.Sprintf("/admin/settings/captcha/%d/delete", captcha.ID), captcha.Name, "form uses this safeguard"},
		} {
			t.Run(test.name, func(t *testing.T) {
				request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, test.path, nil)
				request.Header.Set("Sec-Fetch-Site", "same-origin")
				request.AddCookie(loginResponse.Cookies()[0])
				response, err := server.App.Test(request, -1)
				require.NoError(t, err)
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())

				assert.Equal(t, http.StatusBadRequest, response.StatusCode)
				assert.Contains(t, string(body), test.profileName)
				assert.Contains(t, string(body), "Cannot delete profile")
				assert.Contains(t, string(body), test.usageLabel)
				assert.Contains(t, string(body), "Back to")
			})
		}
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
