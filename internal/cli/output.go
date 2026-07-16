package cli

import (
	"encoding/json"
	"fmt"
	"io"
)

type successEnvelope struct {
	OK      bool   `json:"ok"`
	Command string `json:"command"`
	Data    any    `json:"data"`
}

type errorEnvelope struct {
	OK      bool         `json:"ok"`
	Command string       `json:"command,omitempty"`
	Error   errorPayload `json:"error"`
}

type errorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(writer io.Writer, value any, compact bool) error {
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if !compact {
		encoder.SetIndent("", "  ")
	}
	return encoder.Encode(value)
}

func (r *Runner) writeSuccess(command string, data any) error {
	if r.JSON {
		return writeJSON(r.Stdout, successEnvelope{OK: true, Command: command, Data: data}, true)
	}
	return writeJSON(r.Stdout, data, false)
}

func (r *Runner) writeFailure(command string, commandErr *commandError) {
	if r.JSON {
		_ = writeJSON(r.Stderr, errorEnvelope{
			OK:      false,
			Command: command,
			Error: errorPayload{
				Code:    commandErr.Code,
				Message: commandErr.Message,
			},
		}, true)
		return
	}
	_, _ = fmt.Fprintf(r.Stderr, "error [%s]: %s\n", commandErr.Code, commandErr.Message)
}

// WriteStartupFailure emits a CLI-compatible error before a Runner can be created.
func WriteStartupFailure(args []string, writer io.Writer, operation string) int {
	jsonOutput := false
	for _, argument := range args {
		switch argument {
		case "--json":
			jsonOutput = true
		case "--json=false":
			jsonOutput = false
		}
	}
	message := operation + " failed"
	if jsonOutput {
		cleanArgs := stripKnownGlobalFlags(args)
		_ = writeJSON(writer, errorEnvelope{
			OK:      false,
			Command: commandName(cleanArgs),
			Error: errorPayload{
				Code:    "internal_error",
				Message: message,
			},
		}, true)
	} else {
		_, _ = fmt.Fprintf(writer, "error [internal_error]: %s\n", message)
	}
	return ExitInternal
}
