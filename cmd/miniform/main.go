package main

import (
	"bufio"
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
	"golang.org/x/term"
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
		"install":               manager.Install,
		"update":                manager.Update,
		"reload":                manager.Reload,
		"restore-db":            manager.RestoreDB,
		"change-admin-password": func() error { return changeAdminPassword(manager) },
		"check":                 matcha.Check,
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

func changeAdminPassword(manager *matcha.Matcha) error {
	reader := bufio.NewReader(os.Stdin)
	email, err := readLine(reader, os.Stdout, "Admin email: ")
	if err != nil {
		return fmt.Errorf("read email: %w", err)
	}
	if email == "" {
		return errors.New("email cannot be empty")
	}
	password, err := readConfirmedPassword(os.Stdout)
	if err != nil {
		return err
	}

	if err := writeLine(os.Stdout, "Changing password..."); err != nil {
		return err
	}
	if err := manager.Exec("/app/fnctl", "change-admin-password", email, password); err != nil {
		return fmt.Errorf("change password: %w", err)
	}
	return writeLine(os.Stdout, "Password changed successfully.")
}

func readLine(reader *bufio.Reader, writer io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(writer, prompt); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	value, err := reader.ReadString('\n')
	return strings.TrimSpace(value), err
}

func readConfirmedPassword(writer io.Writer) (string, error) {
	for {
		password, err := readHidden(writer, "New password (minimum 8 characters): ")
		if err != nil {
			return "", err
		}
		if len(password) < 8 {
			if err := writeLine(writer, "Password must be at least 8 characters."); err != nil {
				return "", err
			}
			continue
		}
		confirmation, err := readHidden(writer, "Confirm password: ")
		if err != nil {
			return "", err
		}
		if password == confirmation {
			return password, nil
		}
		if err := writeLine(writer, "Passwords do not match; try again."); err != nil {
			return "", err
		}
	}
}

func readHidden(writer io.Writer, prompt string) (string, error) {
	if _, err := fmt.Fprint(writer, prompt); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}
	value, readErr := term.ReadPassword(int(os.Stdin.Fd()))
	if err := writeLine(writer, ""); err != nil {
		return "", err
	}
	if readErr != nil {
		return "", fmt.Errorf("read password: %w", readErr)
	}
	return strings.TrimSpace(string(value)), nil
}

func writeLine(writer io.Writer, text string) error {
	if _, err := fmt.Fprintln(writer, text); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
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
  change-admin-password       Change the remote admin password
  check                       Check server security

Data:
  account | config | setting | form | mailer | captcha
  submission | event | commands
  Run "miniform help <resource>" for actions and flags.

Other:
  version                     Show build information
  --help                      Show this help
`)
	return err
}
