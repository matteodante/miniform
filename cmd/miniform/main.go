package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"log"
	"log/slog"
	"os"
	"strings"
	"syscall"
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
	if len(os.Args) < 2 {
		runServer()
		return
	}
	if cli.IsInvocation(os.Args[1:]) {
		os.Exit(runDataCLI(os.Args[1:]))
	}
	if strings.HasPrefix(os.Args[1], "-") {
		runServer()
		return
	}

	m := newMatcha()

	switch os.Args[1] {
	case "serve", "server", "run":
		os.Args = append(os.Args[:1], os.Args[2:]...)
		runServer()
	case "install":
		if err := m.Install(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "update":
		if err := m.Update(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "reload":
		if err := m.Reload(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "restore-db":
		if err := m.RestoreDB(); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "change-admin-password":
		if err := runAdminPasswordChange(m); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
	case "check":
		if err := matcha.Check(); err != nil {
			os.Exit(1)
		}
	case "version", "--version", "-v":
		printVersion()
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Printf("Unknown command: %s\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
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
			Path:         cfg.DatabasePath,
			MaxOpenConns: cfg.GetMaxOpenConns(),
			MaxIdleConns: cfg.GetMaxIdleConns(),
			Logger:       logger,
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

		dependencies.DB = db
		dependencies.Logger = logger
	}

	runner := cli.NewRunner(dependencies)
	return runner.Run(args)
}

func newMatcha() *matcha.Matcha {
	return matcha.New(matcha.Config{
		Name:           "miniform",
		AppImage:       "ghcr.io/matteodante/miniform:latest",
		HealthPath:     "/_health",
		Volumes:        []string{"/app/storage", "/app/logs"},
		CronUpdates:    true,
		Backups:        true,
		ManagerRepo:    "matteodante/miniform",
		ManagerVersion: version,
	})
}

func runServer() {
	seedFlag := flag.Bool("seed", false, "Seed the database with sample data")
	versionFlag := flag.Bool("version", false, "Print version and exit")
	flag.Parse()

	if *versionFlag {
		printVersion()
		return
	}

	app, err := internal.NewApp()
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Running database migrations...")
	if err := internal.RunMigrations(app); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database migrations completed")

	if *seedFlag {
		db := app.GetDB()
		log.Println("Seeding database...")
		if err := database.Seed(db); err != nil {
			log.Fatalf("Failed to seed database: %v", err)
		}
		log.Println("Database seeded successfully!")
		return
	}

	shutdownTimeout := 2 * time.Second
	if app.Config.IsProduction() {
		shutdownTimeout = 10 * time.Second
	}
	if err := app.RunWithTimeout(shutdownTimeout); err != nil {
		log.Fatal(err)
	}
}

func runAdminPasswordChange(m *matcha.Matcha) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("Enter admin email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read email: %w", err)
	}
	email = strings.TrimSpace(email)
	if email == "" {
		return fmt.Errorf("email cannot be empty")
	}

	var password string
	for {
		fmt.Print("Enter new admin password (minimum 8 characters): ")
		passBytes, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		fmt.Println()

		password = strings.TrimSpace(string(passBytes))
		if len(password) < 8 {
			fmt.Println("Error: password must be at least 8 characters")
			continue
		}

		fmt.Print("Confirm new admin password: ")
		confirmBytes, err := term.ReadPassword(syscall.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read password: %w", err)
		}
		fmt.Println()

		if password != strings.TrimSpace(string(confirmBytes)) {
			fmt.Println("Error: Passwords do not match. Please try again.")
			continue
		}
		break
	}

	fmt.Println("Changing password...")
	if err := m.Exec("/app/fnctl", "change-admin-password", email, password); err != nil {
		return fmt.Errorf("failed to change password: %w", err)
	}

	fmt.Println("Password changed successfully.")
	return nil
}

func printVersion() {
	fmt.Printf("Miniform %s\n", version)
	fmt.Printf("  Commit:     %s\n", commit)
	fmt.Printf("  Build Time: %s\n", buildTime)
}

func printUsage() {
	fmt.Println("Miniform - Your self-hosted form inbox")
	fmt.Println("")
	fmt.Println("Usage: miniform [command] [options]")
	fmt.Println("")
	fmt.Println("Server Commands:")
	fmt.Println("  serve                       Start the web server (default)")
	fmt.Println("  --seed                      Seed database with sample data")
	fmt.Println("")
	fmt.Println("Installer Commands:")
	fmt.Println("  install                     Install Miniform via Docker")
	fmt.Println("  update                      Update existing installation")
	fmt.Println("  reload                      Reload containers")
	fmt.Println("  restore-db                  Restore database from backup")
	fmt.Println("  change-admin-password       Change admin password")
	fmt.Println("  check                       Check server security")
	fmt.Println("")
	fmt.Println("Data Commands:")
	fmt.Println("  account <action>            Manage operator credentials")
	fmt.Println("  config <action>             Inspect or persist runtime configuration")
	fmt.Println("  setting <action>            Manage database-backed settings")
	fmt.Println("  form <action>               Manage endpoints and delivery policies")
	fmt.Println("  mailer <action>             Manage SMTP and Mailgun profiles")
	fmt.Println("  captcha <action>            Manage captcha profiles")
	fmt.Println("  submission <action>         Manage inbox entries and files")
	fmt.Println("  event <action>              Inspect or retry delivery events")
	fmt.Println("  commands --json             Print the machine-readable command catalog")
	fmt.Println("")
	fmt.Println("Other Commands:")
	fmt.Println("  version                     Show version info")
	fmt.Println("  help                        Show this help")
}
