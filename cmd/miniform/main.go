package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/matteodante/miniform/internal"
	"github.com/matteodante/miniform/internal/cli"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/database"

	"github.com/karloscodes/matcha"
	"gorm.io/gorm"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	os.Exit(execute(os.Args[1:]))
}

func execute(args []string) int {
	if cli.IsInvocation(args) {
		return runDataCLI(args)
	}
	if len(args) == 0 {
		return report(runServer(nil))
	}

	switch args[0] {
	case "version", "--version", "-v":
		return report(printVersion(os.Stdout))
	case "--help", "-h":
		return report(printUsage(os.Stdout))
	case "serve", "server", "run":
		return report(runServer(args[1:]))
	}
	if strings.HasPrefix(args[0], "-") {
		return report(runServer(args))
	}

	manager := newMatcha()
	commands := map[string]func() error{
		"install": func() error {
			return runDeploymentCommand(manager, "install", os.Stdin, os.Stdout)
		},
		"update": func() error {
			return runDeploymentCommand(manager, "update", os.Stdin, os.Stdout)
		},
		"reload": func() error {
			return runDeploymentCommand(manager, "reload", os.Stdin, os.Stdout)
		},
		"backup": func() error {
			return runDeploymentCommand(manager, "backup", os.Stdin, os.Stdout)
		},
		"restore-db": func() error {
			return runDeploymentCommand(manager, "restore-db", os.Stdin, os.Stdout)
		},
		"check": matcha.Check,
	}
	command, found := commands[args[0]]
	if !found {
		if _, err := fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0]); err != nil {
			return 1
		}
		if err := printUsage(os.Stderr); err != nil {
			return 1
		}
		return 1
	}
	if len(args) > 1 {
		if _, err := fmt.Fprintf(os.Stderr, "%s does not accept positional arguments\n", args[0]); err != nil {
			return 1
		}
		return 1
	}
	return report(command())
}

func report(err error) int {
	if err == nil || errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if _, writeErr := fmt.Fprintf(os.Stderr, "miniform: %v\n", err); writeErr != nil {
		return 1
	}
	return 1
}

func runDataCLI(args []string) int {
	var cfg *config.Config
	if cli.RequiresConfig(args) {
		loaded, err := config.Load()
		if err != nil {
			return cli.WriteStartupFailure(args, os.Stderr, "load configuration", err)
		}
		cfg = loaded
	}
	dependencies := cli.Dependencies{Config: cfg}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	var manager *database.Manager
	var runner *cli.Runner
	defer func() {
		if manager != nil {
			if err := manager.Close(); err != nil {
				log.Printf("close database: %v", err)
			}
		}
	}()
	dependencies.Logger = logger
	dependencies.ConnectDatabase = func() (*gorm.DB, error) {
		if cfg == nil {
			loaded, err := config.Load()
			if err != nil {
				return nil, fmt.Errorf("load configuration: %w", err)
			}
			cfg = loaded
			runner.Config = loaded
		}
		if manager == nil {
			manager = database.NewManager(cfg.DatabasePath, cfg.GetMaxOpenConns(), cfg.GetMaxIdleConns())
		}
		if err := cfg.EnsureDirectories(); err != nil {
			return nil, fmt.Errorf("prepare storage: %w", err)
		}
		db, err := manager.Connect()
		if err != nil {
			return nil, err
		}
		if err := database.Migrate(db); err != nil {
			return nil, fmt.Errorf("prepare database: %w", err)
		}
		return db, nil
	}

	runner = cli.NewRunner(dependencies)
	return runner.Run(args)
}

func newMatcha() *matcha.Matcha {
	appImage := strings.TrimSpace(os.Getenv("APP_IMAGE"))
	if appImage == "" {
		appImage = "ghcr.io/matteodante/miniform:latest"
	}
	return matcha.New(matcha.Config{
		Name: "miniform", AppImage: appImage, HealthPath: "/_health",
		Volumes: []string{"/app/storage"}, Backups: true,
		ManagerRepo: "matteodante/miniform", ManagerVersion: version,
	})
}

func runServer(args []string) error {
	flags := flag.NewFlagSet("miniform serve", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	seed := flags.Bool("seed", false, "seed the database with sample data")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected server argument %q", flags.Arg(0))
	}
	if *showVersion {
		return printVersion(os.Stdout)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	app, err := internal.NewApp()
	if err != nil {
		return err
	}
	defer func() {
		if err := app.Close(); err != nil {
			log.Printf("close application: %v", err)
		}
	}()
	if err := ctx.Err(); err != nil {
		return nil
	}

	log.Print("Preparing database")
	if err := internal.RunMigrations(ctx, app); err != nil {
		if ctx.Err() != nil {
			return nil
		}
		return fmt.Errorf("prepare database: %w", err)
	}
	if *seed {
		if err := seedDatabase(ctx, app); err != nil && ctx.Err() == nil {
			return err
		}
		return nil
	}

	shutdownTimeout := 2 * time.Second
	if app.Config.IsProduction() {
		shutdownTimeout = 10 * time.Second
	}
	return app.Run(ctx, shutdownTimeout)
}

func seedDatabase(ctx context.Context, app *internal.App) error {
	db, err := app.DBManager.Connect()
	if err != nil {
		return fmt.Errorf("connect database for seed: %w", err)
	}
	if err := database.Seed(db.WithContext(ctx)); err != nil {
		return fmt.Errorf("seed database: %w", err)
	}
	log.Print("Sample data created")
	return nil
}

func printVersion(writer io.Writer) error {
	_, err := fmt.Fprintf(writer, "Miniform %s\n  Commit: %s\n", version, commit)
	return err
}

func printUsage(writer io.Writer) error {
	_, err := fmt.Fprint(writer, `Miniform — self-hosted form inbox

Usage: miniform [command] [options]

Server:
  serve [--seed]              Start Miniform (default)

Deployment:
  install                     Install via Docker
  update                      Back up and update with one app process
  reload                      Back up and reload with one app process
  backup                      Create a verified database backup
  restore-db                  Restore a database backup
  check                       Check server security

Data:
  account | config | form | mailer | captcha
  submission | event | commands
  Run "miniform help <resource>" for actions and flags.

Other:
  version                     Show build information
  --help                      Show this help
`)
	return err
}
