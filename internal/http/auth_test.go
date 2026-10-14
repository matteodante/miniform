package http

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/karloscodes/cartridge"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/pkg/testsupport"
)

type loginTestViews struct{}

func (loginTestViews) Load() error { return nil }

func (loginTestViews) Render(writer io.Writer, _ string, data any, _ ...string) error {
	_, err := fmt.Fprint(writer, data.(fiber.Map)["Error"])
	return err
}

func TestAdminLoginSubmit(t *testing.T) {
	for _, test := range []struct {
		name             string
		resetDuringLogin bool
		wantStatus       int
		wantSessionCount int64
	}{
		{"registers a session after authentication", false, http.StatusFound, 1},
		{"rejects credentials revoked during authentication", true, http.StatusOK, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			db := testsupport.SetupTestDB(t)
			created, err := accounts.EnsureAdmin(logger, db, "old-password", true, nil)
			require.NoError(t, err)
			require.True(t, created)

			reset := false
			if test.resetDuringLogin {
				// Pause the login after its credential snapshot is read, then revoke it.
				// This forces the race's ordering without relying on scheduler timing.
				require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:reset_during_login", func(tx *gorm.DB) {
					if reset || tx.Statement.Table != "users" {
						return
					}
					reset = true
					tx.AddError(accounts.ResetPassword(logger, db, accounts.DefaultAdminEmail, "replacement-password"))
				}))
			}

			cfg := &config.Config{Config: &cartridgeconfig.Config{AppName: "miniform", Environment: cartridgeconfig.Test}}
			session := cartridge.NewSessionManager(cartridge.SessionConfig{
				CookieName: cfg.SessionCookieName(), Secret: "test-secret", TTL: time.Hour,
			})
			app := fiber.New(fiber.Config{Views: loginTestViews{}})
			app.Post("/admin/login", func(ctx *fiber.Ctx) error {
				return AdminLoginSubmit(&cartridge.Context{
					Ctx: ctx, Logger: logger, DBManager: requestDBManager{db: db}, Session: session,
				}, cfg)
			})

			request := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/admin/login",
				strings.NewReader("email=admin%40miniform.local&password=old-password"))
			request.Header.Set(fiber.HeaderContentType, fiber.MIMEApplicationForm)
			response, err := app.Test(request, -1)
			require.NoError(t, err)
			defer func() { _ = response.Body.Close() }()
			assert.Equal(t, test.wantStatus, response.StatusCode)
			assert.Equal(t, test.resetDuringLogin, reset)
			var sessions int64
			require.NoError(t, db.Model(&accounts.ActiveSession{}).Count(&sessions).Error)
			assert.Equal(t, test.wantSessionCount, sessions)

			cookies := response.Cookies()
			require.Len(t, cookies, 1)
			assert.Equal(t, cfg.SessionCookieName(), cookies[0].Name)
			if test.resetDuringLogin {
				body, err := io.ReadAll(response.Body)
				require.NoError(t, err)
				assert.Contains(t, string(body), "Invalid credentials")
				assert.Empty(t, response.Header.Get(fiber.HeaderLocation))
				assert.Empty(t, cookies[0].Value)
				assert.True(t, cookies[0].Expires.Before(time.Now()))
			} else {
				assert.Equal(t, "/admin/submissions", response.Header.Get(fiber.HeaderLocation))
				assert.NotEmpty(t, cookies[0].Value)
			}
		})
	}
}
