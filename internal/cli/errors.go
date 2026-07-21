package cli

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/matteodante/miniform/internal/accounts"
	"github.com/matteodante/miniform/internal/forms"
	"github.com/matteodante/miniform/internal/integrations"
)

const (
	ExitSuccess    = 0
	ExitUsage      = 2
	ExitValidation = 3
	ExitNotFound   = 4
	ExitConflict   = 5
	ExitInternal   = 10
)

type commandError struct {
	Code     string
	Message  string
	ExitCode int
	Cause    error
}

func (e *commandError) Error() string {
	return e.Message
}

func (e *commandError) Unwrap() error {
	return e.Cause
}

func usageError(message string) error {
	return &commandError{Code: "usage_error", Message: message, ExitCode: ExitUsage}
}

func validationError(message string) error {
	return &commandError{Code: "validation_error", Message: message, ExitCode: ExitValidation}
}

func conflictError(message string) error {
	return &commandError{Code: "conflict", Message: message, ExitCode: ExitConflict}
}

func internalError(operation string, cause error) error {
	return &commandError{
		Code:     "internal_error",
		Message:  operation + " failed",
		ExitCode: ExitInternal,
		Cause:    cause,
	}
}

func classifyError(err error) *commandError {
	if err == nil {
		return nil
	}

	var known *commandError
	if errors.As(err, &known) {
		return known
	}

	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, accounts.ErrUserNotFound) {
		return &commandError{Code: "not_found", Message: "resource not found", ExitCode: ExitNotFound, Cause: err}
	}

	var formValidation *forms.ValidationError
	if errors.As(err, &formValidation) {
		return &commandError{
			Code:     "validation_error",
			Message:  fmt.Sprintf("%s: %s", formValidation.Field, formValidation.Message),
			ExitCode: ExitValidation,
			Cause:    err,
		}
	}

	var integrationValidation *integrations.ValidationError
	if errors.As(err, &integrationValidation) {
		return &commandError{
			Code:     "validation_error",
			Message:  fmt.Sprintf("%s: %s", integrationValidation.Field, integrationValidation.Message),
			ExitCode: ExitValidation,
			Cause:    err,
		}
	}

	switch {
	case errors.Is(err, accounts.ErrWeakPassword),
		errors.Is(err, accounts.ErrInvalidEmail),
		errors.Is(err, accounts.ErrMissingFields),
		errors.Is(err, accounts.ErrPasswordUnchanged):
		return &commandError{Code: "validation_error", Message: err.Error(), ExitCode: ExitValidation, Cause: err}
	case errors.Is(err, accounts.ErrPasswordMismatch), errors.Is(err, accounts.ErrInvalidCredentials):
		return &commandError{Code: "authentication_failed", Message: err.Error(), ExitCode: ExitValidation, Cause: err}
	case errors.Is(err, accounts.ErrDuplicateEmail):
		return &commandError{Code: "conflict", Message: err.Error(), ExitCode: ExitConflict, Cause: err}
	case errors.Is(err, integrations.ErrProfileInUse):
		return &commandError{Code: "conflict", Message: err.Error(), ExitCode: ExitConflict, Cause: err}
	}

	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "unique constraint") || strings.Contains(lower, "already exists") {
		return &commandError{Code: "conflict", Message: "resource already exists", ExitCode: ExitConflict, Cause: err}
	}

	return &commandError{Code: "internal_error", Message: "operation failed", ExitCode: ExitInternal, Cause: err}
}
