package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/matteodante/miniform/internal"
	"github.com/matteodante/miniform/internal/cli"
	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/database"

	cartridgesqlite "github.com/karloscodes/cartridge/sqlite"
	"github.com/karloscodes/matcha"
)

var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
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
		"install":    manager.Install,
		"update":     manager.Update,
		"reload":     manager.Reload,
		"restore-db": manager.RestoreDB,
		"check":      matcha.Check,
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
			return cli.WriteStartupFailure(args, os.Stderr, "load configuration")
		}
		cfg = loaded
	}
	dependencies := cli.Dependencies{Config: cfg}

	if cli.RequiresDatabase(args) {
		if err := cfg.EnsureDirectories(); err != nil {
			return cli.WriteStartupFailure(args, os.Stderr, "prepare storage")
		}
		logger := slog.New(slog.NewTextHandler(io.Discard, nil))
		manager := cartridgesqlite.NewManager(cartridgesqlite.Config{
			Path: cfg.DatabasePath, MaxOpenConns: cfg.GetMaxOpenConns(), MaxIdleConns: cfg.GetMaxIdleConns(), Logger: logger,
		})
		db, err := manager.Connect()
		if err != nil {
			return cli.WriteStartupFailure(args, os.Stderr, "connect database")
		}
		if err := database.Migrate(db); err != nil {
			return cli.WriteStartupFailure(args, os.Stderr, "prepare database")
		}
		defer func() {
			if err := manager.Close(); err != nil {
				log.Printf("close database: %v", err)
			}
		}()
		dependencies.DB, dependencies.Logger = db, logger
	}

	return cli.NewRunner(dependencies).Run(args)
}

func newMatcha() *matcha.Matcha {
	appImage := strings.TrimSpace(os.Getenv("APP_IMAGE"))
	if appImage == "" {
		appImage = "ghcr.io/matteodante/miniform:latest"
	}
	return matcha.New(matcha.Config{
		Name: "miniform", AppImage: appImage, HealthPath: "/_health",
		Volumes: []string{"/app/storage"}, CronUpdates: true, Backups: true,
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

	app, err := internal.NewApp()
	if err != nil {
		return err
	}
	log.Print("Preparing database")
	if err := internal.RunMigrations(app); err != nil {
		return fmt.Errorf("prepare database: %w", err)
	}
	if *seed {
		return seedDatabase(app)
	}

	shutdownTimeout := 2 * time.Second
	if app.Config.IsProduction() {
		shutdownTimeout = 10 * time.Second
	}
	return app.RunWithTimeout(shutdownTimeout)
}

func seedDatabase(app *internal.App) error {
	db, err := app.DBManager.Connect()
	if err != nil {
		return fmt.Errorf("connect database for seed: %w", err)
	}
	if err := database.Seed(db); err != nil {
		return fmt.Errorf("seed database: %w", err)
	}
	log.Print("Sample data created")
	return nil
}

func printVersion(writer io.Writer) error {
	_, err := fmt.Fprintf(writer, "Miniform %s\n  Commit:     %s\n  Build Time: %s\n", version, commit, buildTime)
	return err
}

func printUsage(writer io.Writer) error {
	_, err := fmt.Fprint(writer, `Miniform — self-hosted form inbox

Usage: miniform [command] [options]

Server:
  serve [--seed]              Start Miniform (default)

Deployment:
  install                     Install via Docker
  update                      Update the installation
  reload                      Reload containers
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
