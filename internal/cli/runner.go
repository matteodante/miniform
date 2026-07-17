package cli

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/config"
)

// Runner executes local administrative commands against one Miniform instance.
type Runner struct {
	DB          *gorm.DB
	Config      *config.Config
	Logger      *slog.Logger
	Stdin       io.Reader
	Stdout      io.Writer
	Stderr      io.Writer
	JSON        bool
	ShowSecrets bool
	connectDB   func() (*gorm.DB, error)
}

// Dependencies contains the runtime services used by a Runner.
type Dependencies struct {
	DB              *gorm.DB
	ConnectDatabase func() (*gorm.DB, error)
	Config          *config.Config
	Logger          *slog.Logger
	Stdin           io.Reader
	Stdout          io.Writer
	Stderr          io.Writer
}

// NewRunner creates a command runner with safe process I/O defaults.
func NewRunner(deps Dependencies) *Runner {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	stdin := deps.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	stdout := deps.Stdout
	if stdout == nil {
		stdout = os.Stdout
	}
	stderr := deps.Stderr
	if stderr == nil {
		stderr = os.Stderr
	}

	return &Runner{
		DB:        deps.DB,
		Config:    deps.Config,
		Logger:    logger,
		Stdin:     stdin,
		Stdout:    stdout,
		Stderr:    stderr,
		connectDB: deps.ConnectDatabase,
	}
}

// IsCommand reports whether a top-level argument belongs to the data CLI.
func IsCommand(name string) bool {
	switch name {
	case "account", "captcha", "commands", "config", "event", "form", "help", "mailer", "submission":
		return true
	default:
		return false
	}
}

// IsInvocation recognizes a data CLI command even when global flags come first.
func IsInvocation(args []string) bool {
	clean := stripKnownGlobalFlags(args)
	return len(clean) > 0 && IsCommand(clean[0])
}

// RequiresDatabase reports whether command execution needs SQLite.
func RequiresDatabase(args []string) bool {
	args = stripKnownGlobalFlags(args)
	if helpRequested(args) {
		return false
	}
	if len(args) < 2 {
		return false
	}
	name := args[0] + " " + args[1]
	for _, command := range commandManifest() {
		if command.Name == name {
			return command.RequiresDatabase
		}
	}
	return false
}

// RequiresConfig reports whether command execution needs the effective runtime config.
func RequiresConfig(args []string) bool {
	args = stripKnownGlobalFlags(args)
	if helpRequested(args) {
		return false
	}
	if RequiresDatabase(args) {
		return true
	}
	if len(args) < 2 {
		return false
	}
	return args[0] == "config" && args[1] == "show"
}

func helpRequested(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "-h", "-help", "--help":
			return true
		}
	}
	return false
}

func stripKnownGlobalFlags(args []string) []string {
	clean := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--json", "--json=false", "--show-secrets", "--show-secrets=false":
			continue
		default:
			clean = append(clean, arg)
		}
	}
	return clean
}

// Run executes a command and returns a stable process exit code.
func (r *Runner) Run(args []string) int {
	cleanArgs, err := r.extractGlobalFlags(args)
	if err != nil {
		commandErr := classifyError(err)
		r.writeFailure("", commandErr)
		return commandErr.ExitCode
	}
	if len(cleanArgs) == 0 {
		r.writeRootHelp()
		return ExitSuccess
	}

	command := commandName(cleanArgs)
	data, err := r.dispatch(cleanArgs)
	if errors.Is(err, errHelpShown) {
		return ExitSuccess
	}
	if err != nil {
		commandErr := classifyError(err)
		if commandErr.ExitCode == ExitInternal && commandErr.Cause != nil {
			r.Logger.Error("CLI command failed", slog.String("command", command), slog.Any("error", commandErr.Cause))
		}
		r.writeFailure(command, commandErr)
		return commandErr.ExitCode
	}
	if data == nil {
		return ExitSuccess
	}
	if err := r.writeSuccess(command, data); err != nil {
		commandErr := classifyError(internalError("write command output", err))
		r.writeFailure(command, commandErr)
		return commandErr.ExitCode
	}
	return ExitSuccess
}

func (r *Runner) dispatch(args []string) (any, error) {
	switch args[0] {
	case "commands":
		if len(args) > 1 {
			return nil, usageError("commands does not accept positional arguments")
		}
		return commandManifest(), nil
	case "help":
		return r.runHelp(args[1:])
	case "account":
		return r.runAccount(args[1:])
	case "config":
		return r.runConfig(args[1:])
	case "form":
		return r.runForm(args[1:])
	case "mailer":
		return r.runMailer(args[1:])
	case "captcha":
		return r.runCaptcha(args[1:])
	case "submission":
		return r.runSubmission(args[1:])
	case "event":
		return r.runEvent(args[1:])
	default:
		return nil, usageError("unknown command: " + args[0])
	}
}

func (r *Runner) extractGlobalFlags(args []string) ([]string, error) {
	clean := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--json":
			r.JSON = true
		case "--show-secrets":
			r.ShowSecrets = true
		case "--json=false":
			r.JSON = false
		case "--show-secrets=false":
			r.ShowSecrets = false
		default:
			if strings.HasPrefix(arg, "--json=") || strings.HasPrefix(arg, "--show-secrets=") {
				return nil, usageError("global boolean flags accept only true or false")
			}
			clean = append(clean, arg)
		}
	}
	return clean, nil
}

func commandName(args []string) string {
	if len(args) == 0 {
		return ""
	}
	if len(args) == 1 {
		return args[0]
	}
	return args[0] + "." + args[1]
}

func requireAction(resource string, args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", nil, usageError("missing action; run `miniform help " + resource + "`")
	}
	return args[0], args[1:], nil
}

func (r *Runner) requireDatabase() error {
	if r.DB != nil {
		return nil
	}
	if r.connectDB == nil {
		return internalError("connect database", errors.New("database dependency is unavailable"))
	}
	db, err := r.connectDB()
	if err != nil {
		return &commandError{
			Code:     "internal_error",
			Message:  "connect database: " + err.Error(),
			ExitCode: ExitInternal,
			Cause:    err,
		}
	}
	if db == nil {
		return internalError("connect database", errors.New("database connector returned no connection"))
	}
	r.DB = db
	return nil
}
