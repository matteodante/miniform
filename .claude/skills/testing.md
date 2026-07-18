# Testing Pattern

## Overview

Miniform uses testify assertions with a shared test database helper. Tests are organized using subtests (contexts) for clarity. Table-driven tests are used only when testing many similar input/output variations.

## Test Database Setup

Location: `internal/pkg/testsupport/db.go`

```go
import "github.com/matteodante/miniform/internal/pkg/testsupport"

func TestCreateForm(t *testing.T) {
    t.Run("creates a valid form", func(t *testing.T) {
        db := testsupport.SetupTestDB(t)
        logger := slog.New(slog.NewTextHandler(io.Discard, nil))
        // Exercise the owning domain API.
    })
}
```

## Subtests (Primary Pattern)

Use `t.Run()` to organize tests into contexts:

```go
func TestFormOperations(t *testing.T) {
    t.Run("creates a form", func(t *testing.T) {
        db := testsupport.SetupTestDB(t)
        logger := slog.New(slog.NewTextHandler(io.Discard, nil))

        form, err := forms.Create(logger, db, forms.CreateParams{
            Name: "Test Form", Slug: "test-form", AllowedOrigins: "*",
        })
        require.NoError(t, err)
        assert.NotZero(t, form.ID)
        assert.NotEmpty(t, form.PublicID)
    })

    t.Run("rejects a missing slug", func(t *testing.T) {
        db := testsupport.SetupTestDB(t)
        logger := slog.New(slog.NewTextHandler(io.Discard, nil))

        _, err := forms.Create(logger, db, forms.CreateParams{Name: "Test Form", AllowedOrigins: "*"})
        var validation *forms.ValidationError
        require.ErrorAs(t, err, &validation)
        assert.Equal(t, "slug", validation.Field)
    })
}
```

## Assertions

Use testify for assertions:

```go
import (
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestExample(t *testing.T) {
    // Use require for fatal checks (test stops if fails)
    require.NoError(t, err)
    require.NotNil(t, result)

    // Use assert for non-fatal checks (test continues)
    assert.Equal(t, expected, actual)
    assert.Contains(t, str, "substring")
    assert.True(t, condition)
    assert.ErrorIs(t, err, expectedErr)
}
```

## Test helpers

Reuse `internal/pkg/testsupport` and neighboring helpers. Prefer domain APIs for behavior under test. Direct database setup is acceptable only when the test explicitly needs a legacy or invalid state that public APIs cannot create.

## Table-Driven Tests (When Appropriate)

Use table-driven tests only for testing many similar variations of input/output:

```go
func TestIsBusyError(t *testing.T) {
    tests := []struct {
        name string
        err  error
        want bool
    }{
        {"nil error", nil, false},
        {"database is locked", errors.New("database is locked"), true},
        {"database is busy", errors.New("database is busy"), true},
        {"unrelated error", errors.New("connection refused"), false},
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got := isBusyError(tt.err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

**When to use table-driven:**
- Testing a function with many input variations
- Validation functions with multiple edge cases
- Pure functions without side effects

**When NOT to use table-driven:**
- Tests with complex setup/teardown
- Tests requiring database state
- Integration tests

## Silent Logging in Tests

```go
logger := slog.New(slog.NewTextHandler(io.Discard, nil))
```

## Browser tests

- Keep Playwright tests sequential.
- Use semantic roles, labels, stable actions, or explicit stable attributes.
- Assert the final URL and visible outcome, including error recovery.
- Let global setup create and mark a unique temporary data directory.
- Never remove an explicitly supplied `MINIFORM_E2E_DATA_DIR` during teardown.

## Running Tests

```bash
# Run all tests
go test ./...

# Run with verbose output
go test -v ./...

# Run specific package
go test ./internal/forms/...

# Run specific test
go test -v -run TestFormOperations ./internal/forms/...

# Run lifecycle and concurrency coverage
make test-race

# Run Node teardown checks and Playwright
make test-e2e
```
