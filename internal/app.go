package internal

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	htmlengine "github.com/gofiber/template/html/v2"
	"github.com/karloscodes/cartridge"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/database"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/jobs"
	"github.com/matteodante/miniform/internal/server"
	"github.com/matteodante/miniform/web"
)

type App struct {
	Config    *config.Config
	Logger    *slog.Logger
	DBManager *database.Manager
	Server    *cartridge.Server

	runJobs        func(context.Context)
	cancelRequests context.CancelFunc
	logCloser      io.Closer
	previousLogger *slog.Logger

	listenerMu sync.Mutex
	listener   net.Listener
}

func NewApp() (_ *App, resultErr error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, fmt.Errorf("load miniform configuration: %w", err)
	}
	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", net.JoinHostPort("", cfg.GetPort()))
	if err != nil {
		return nil, fmt.Errorf("listen on HTTP port %s: %w", cfg.GetPort(), err)
	}
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, listener.Close())
		}
	}()
	if err := cfg.EnsureDirectories(); err != nil {
		return nil, fmt.Errorf("prepare miniform storage: %w", err)
	}

	logger, logCloser := server.NewLogger(cfg)
	defer func() {
		if resultErr != nil && logCloser != nil {
			resultErr = errors.Join(resultErr, logCloser.Close())
		}
	}()
	manager := database.NewManager(cfg.DatabasePath, cfg.GetMaxOpenConns(), cfg.GetMaxIdleConns())
	defer func() {
		if resultErr != nil {
			resultErr = errors.Join(resultErr, manager.Close())
		}
	}()

	db, err := manager.Connect()
	if err != nil {
		return nil, fmt.Errorf("connect application database: %w", err)
	}

	serverConfig := cartridge.DefaultServerConfig()
	serverConfig.Config = cfg
	serverConfig.Logger = logger
	serverConfig.DBManager = manager
	serverConfig.ViewsEngine = newViewsEngine(cfg)
	serverConfig.ErrorHandler = server.ErrorHandler(logger, cfg)
	serverConfig.EnableStaticAssets = false
	httpServer, err := cartridge.NewServer(serverConfig)
	if err != nil {
		return nil, fmt.Errorf("create HTTP server: %w", err)
	}
	httpServer.App().Server().MaxRequestBodySize = forms.MaxTotalFiles*forms.MaxFileSize + 1024*1024
	requestRoot, cancelRequests := context.WithCancel(context.Background())
	defer func() {
		if resultErr != nil {
			cancelRequests()
		}
	}()
	httpServer.App().Use(func(ctx *fiber.Ctx) error {
		requestCtx, cancelRequest := context.WithCancel(requestRoot)
		ctx.SetUserContext(requestCtx)
		defer cancelRequest()
		return ctx.Next()
	})

	session := cartridge.NewSessionManager(cartridge.SessionConfig{
		CookieName: cfg.AppName + "_session",
		Secret:     cfg.GetSessionSecret(),
		TTL:        time.Duration(cfg.GetSessionTimeout()) * time.Second,
		Secure:     cfg.IsProduction(),
		LoginPath:  "/admin/login",
	})
	httpServer.SetSession(session)
	MountRoutes(httpServer, cfg)
	mountAssets(httpServer, cfg)

	runner := jobs.NewRunner(
		logger,
		db,
		time.Second,
		jobs.NewWebhookDispatcher(cfg),
		jobs.NewEmailDispatcher(cfg),
	)
	previousLogger := slog.Default()
	slog.SetDefault(logger)
	return &App{
		Config: cfg, Logger: logger, DBManager: manager, Server: httpServer,
		runJobs: runner.Run, cancelRequests: cancelRequests, logCloser: logCloser,
		previousLogger: previousLogger, listener: listener,
	}, nil
}

func (app *App) Run(ctx context.Context, timeout time.Duration) error {
	return app.run(ctx, timeout)
}

func (app *App) run(ctx context.Context, timeout time.Duration) (resultErr error) {
	defer func() { resultErr = errors.Join(resultErr, app.Close()) }()

	listener, err := app.takeListener()
	if err != nil {
		return fmt.Errorf("listen on HTTP port %s: %w", app.Config.GetPort(), err)
	}
	defer func() {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			resultErr = errors.Join(resultErr, fmt.Errorf("close HTTP listener: %w", err))
		}
	}()

	serverDone := make(chan error, 1)
	go func() { serverDone <- app.Server.App().Listener(listener) }()

	jobsCtx, cancelJobs := context.WithCancel(ctx)
	var jobsDone chan struct{}
	if app.runJobs != nil {
		jobsDone = make(chan struct{})
		go func() {
			defer close(jobsDone)
			app.runJobs(jobsCtx)
		}()
	}

	app.Logger.Info("Server started and ready to accept requests", slog.String("port", app.Config.GetPort()))
	select {
	case <-ctx.Done():
	case err := <-serverDone:
		if err != nil {
			resultErr = fmt.Errorf("serve HTTP requests: %w", err)
		} else {
			resultErr = errors.New("HTTP server stopped unexpectedly")
		}
	}

	cancelJobs()
	if app.cancelRequests != nil {
		app.cancelRequests()
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), timeout)
	defer cancelShutdown()
	if err := app.Server.App().ShutdownWithContext(shutdownCtx); err != nil {
		resultErr = errors.Join(resultErr, fmt.Errorf("shut down HTTP server: %w", err))
	}
	if jobsDone != nil {
		select {
		case <-jobsDone:
		case <-shutdownCtx.Done():
			resultErr = errors.Join(resultErr, fmt.Errorf("stop background jobs: %w", shutdownCtx.Err()))
		}
	}
	if app.DBManager != nil {
		if err := app.DBManager.CheckpointWAL("TRUNCATE"); err != nil {
			resultErr = errors.Join(resultErr, err)
		}
	}
	return resultErr
}

func (app *App) Close() error {
	if app.cancelRequests != nil {
		app.cancelRequests()
	}
	app.listenerMu.Lock()
	listener := app.listener
	app.listener = nil
	logCloser := app.logCloser
	app.logCloser = nil
	previousLogger := app.previousLogger
	app.previousLogger = nil
	app.listenerMu.Unlock()
	if previousLogger != nil && slog.Default() == app.Logger {
		slog.SetDefault(previousLogger)
	}

	var result error
	if listener != nil {
		if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			result = errors.Join(result, fmt.Errorf("close HTTP listener: %w", err))
		}
	}
	if app.DBManager != nil {
		result = errors.Join(result, app.DBManager.Close())
	}
	if logCloser != nil {
		if err := logCloser.Close(); err != nil {
			result = errors.Join(result, fmt.Errorf("close application logger: %w", err))
		}
	}
	return result
}

func (app *App) takeListener() (net.Listener, error) {
	app.listenerMu.Lock()
	listener := app.listener
	app.listener = nil
	app.listenerMu.Unlock()
	if listener != nil {
		return listener, nil
	}
	return nil, errors.New("HTTP listener is not available")
}

func newViewsEngine(cfg *config.Config) *htmlengine.Engine {
	var engine *htmlengine.Engine
	if cfg.IsDevelopment() {
		engine = htmlengine.New("web/templates", ".html")
	} else {
		engine = htmlengine.NewFileSystem(http.FS(web.Templates), ".html")
	}
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
		return template.HTML(output.String()), nil // #nosec G203 -- this is already rendered by html/template.
	})
	for name, function := range server.TemplateFuncs() {
		engine.AddFunc(name, function)
	}
	engine.Debug(cfg.IsDevelopment())
	if cfg.IsDevelopment() {
		engine.Reload(true)
	}
	return engine
}

func RunMigrations(ctx context.Context, app *App) error {
	db, err := app.DBManager.Connect()
	if err != nil {
		return fmt.Errorf("connect application database: %w", err)
	}
	db = db.WithContext(ctx)
	if err := database.Migrate(db); err != nil {
		return err
	}
	if err := forms.RecoverUploadDeletions(db, app.Config.DataDirectory); err != nil {
		return fmt.Errorf("recover interrupted upload deletions: %w", err)
	}

	password := rand.Text()
	loggedIn := false
	if app.Config.IsTest() {
		password = "miniform"
		loggedIn = true
	}
	var announceCredentials func() error
	if !app.Config.IsTest() {
		announceCredentials = func() error { return printInitialCredentials(password) }
	}
	_, err = accounts.EnsureAdmin(app.Logger, db, password, loggedIn, announceCredentials)
	if err != nil {
		return fmt.Errorf("initialize admin account: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
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
