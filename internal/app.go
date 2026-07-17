package internal

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/database"
	"github.com/matteodante/miniform/internal/jobs"
	"github.com/matteodante/miniform/internal/server"
	"github.com/matteodante/miniform/web"
)

type App struct {
	*cartridge.App
	Config *config.Config
}

func NewApp() (*App, error) {
	cfg, err := config.Get()
	if err != nil {
		return nil, fmt.Errorf("load miniform configuration: %w", err)
	}
	application, err := cartridge.NewSSRApp("miniform",
		cartridge.WithConfig(cfg.Config),
		cartridge.WithAssets(web.Templates, web.Static),
		cartridge.WithTemplateFuncs(server.TemplateFuncs()),
		cartridge.WithErrorHandler(server.ErrorHandler(slog.Default(), cfg)),
		cartridge.WithSession("/admin/login"),
		cartridge.WithJobs(2*time.Minute, jobs.NewWebhookDispatcher(cfg), jobs.NewEmailDispatcher(cfg)),
		cartridge.WithRoutes(func(router *cartridge.Server) { MountRoutes(router, cfg) }),
	)
	if err != nil {
		return nil, fmt.Errorf("create miniform application: %w", err)
	}
	return &App{App: application, Config: cfg}, nil
}

func RunMigrations(app *App) error {
	db, err := app.DBManager.Connect()
	if err != nil {
		return fmt.Errorf("connect application database: %w", err)
	}
	if err := database.Migrate(db); err != nil {
		return err
	}

	password := rand.Text()
	loggedIn := false
	if app.Config.IsTest() {
		password = "miniform"
		loggedIn = true
	}
	created, err := accounts.EnsureAdmin(app.Logger, db, password, loggedIn)
	if err != nil {
		return fmt.Errorf("initialize admin account: %w", err)
	}
	if created && !app.Config.IsTest() {
		if err := printInitialCredentials(password); err != nil {
			return err
		}
	}
	if err := app.DBManager.CheckpointWAL("FULL"); err != nil {
		app.Logger.Warn("checkpoint database after migration", slog.Any("error", err))
	}
	return nil
}

func printInitialCredentials(password string) error {
	_, err := fmt.Fprintf(os.Stdout,
		"\nInitial admin credentials\n  Email: %s\n  Temporary password: %s\nChange the password after signing in.\n\n",
		accounts.DefaultAdminEmail, password,
	)
	return err
}
