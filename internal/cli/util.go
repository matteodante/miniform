package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

type stringSliceValue []string

func (v *stringSliceValue) String() string {
	return strings.Join(*v, ",")
}

func (v *stringSliceValue) Set(value string) error {
	*v = append(*v, value)
	return nil
}

var errHelpShown = errors.New("help shown")

func newFlagSet(name string) *flag.FlagSet {
	set := flag.NewFlagSet(name, flag.ContinueOnError)
	set.SetOutput(io.Discard)
	return set
}

func (r *Runner) parseFlags(set *flag.FlagSet, command string, args []string) error {
	if err := set.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.writeCommandHelp(command)
			return errHelpShown
		}
		return usageError(err.Error())
	}
	if set.NArg() > 0 {
		return usageError("unexpected positional arguments: " + strings.Join(set.Args(), " "))
	}
	return nil
}

func flagWasSet(set *flag.FlagSet, name string) bool {
	found := false
	set.Visit(func(item *flag.Flag) {
		if item.Name == name {
			found = true
		}
	})
	return found
}

func requireUint(value uint, name string) error {
	if value == 0 {
		return usageError("--" + name + " is required and must be greater than zero")
	}
	return nil
}

func requireString(value, name string) error {
	if strings.TrimSpace(value) == "" {
		return usageError("--" + name + " is required")
	}
	return nil
}

func optionalUint(value uint) *uint {
	if value == 0 {
		return nil
	}
	result := value
	return &result
}

func readFileValue(path string, stdin io.Reader) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	var content []byte
	var err error
	if path == "-" {
		content, err = io.ReadAll(stdin)
	} else {
		content, err = os.ReadFile(path)
	}
	if err != nil {
		return "", fmt.Errorf("read value file: %w", err)
	}

	value := string(content)
	value = strings.TrimSuffix(value, "\n")
	value = strings.TrimSuffix(value, "\r")
	return value, nil
}

func readContentFile(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read content file: %w", err)
	}
	return string(content), nil
}

func parseOptionalBool(value string) (*bool, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return nil, validationError("boolean value must be true or false")
	}
	return &parsed, nil
}

func redact(value string, show bool) string {
	if value == "" {
		return ""
	}
	if show {
		return value
	}
	return "[REDACTED]"
}
