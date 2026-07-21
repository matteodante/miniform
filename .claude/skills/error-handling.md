# Error Handling Pattern

## Overview

Miniform uses domain-specific error types and a global error handler for consistent error responses.

## Domain Error Types

Each domain defines its own sentinel errors:

```go
// internal/accounts/accounts.go
var (
    ErrInvalidCredentials = errors.New("invalid email or password")
    ErrUserNotFound       = errors.New("user not found")
    ErrWeakPassword       = errors.New("password must be at least 8 characters")
    ErrPasswordMismatch   = errors.New("current password is incorrect")
)

// internal/forms/forms.go
type ValidationError struct {
    Field   string
    Message string
}

func (e *ValidationError) Error() string {
    return fmt.Sprintf("%s: %s", e.Field, e.Message)
}
```

## Error Checking

Use `errors.Is()` for sentinel errors:

```go
if errors.Is(err, accounts.ErrInvalidCredentials) {
    return ctx.Status(401).JSON(fiber.Map{"error": "Invalid credentials"})
}

if errors.Is(err, gorm.ErrRecordNotFound) {
    return fiber.ErrNotFound
}
```

## Global Error Handler

Location: `internal/server/server.go`

```go
func ErrorHandler(log *slog.Logger, cfg *config.Config) fiber.ErrorHandler {
    return func(c *fiber.Ctx, err error) error {
        code, message := publicError(err, cfg.IsDevelopment())

        log.Error("HTTP request failed", slog.Int("status", code),
            slog.String("method", c.Method()), slog.String("path", c.Path()), slog.Any("error", err))

        if c.Accepts(fiber.MIMEApplicationJSON) == fiber.MIMEApplicationJSON {
            return c.Status(code).JSON(fiber.Map{"error": http.StatusText(code), "message": message})
        }
        if code == fiber.StatusInternalServerError {
            return c.Status(code).Render("layouts/base", fiber.Map{
                "Title": "500 - Internal Server Error", "ContentView": "errors/500/content",
                "DevMode": cfg.IsDevelopment(), "ErrorMessage": message,
            }, "")
        }
        if code >= fiber.StatusBadRequest && code < fiber.StatusInternalServerError {
            return c.Status(code).Render("layouts/base", fiber.Map{
                "Title": fmt.Sprintf("%d - %s", code, http.StatusText(code)),
                "ContentView": "errors/4xx/content", "RecoveryURL": "/admin/submissions",
            }, "")
        }
        return c.Status(code).SendString(fmt.Sprintf("Error: %d - %s", code, message))
    }
}
```

`publicError` exposes unexpected error details only in development. Production clients receive stable public messages while structured logs retain diagnostic context.

## HTTP Error Responses

Use Fiber's built-in errors for common cases:

```go
return fiber.ErrNotFound           // 404
return fiber.ErrBadRequest         // 400
return fiber.ErrUnauthorized       // 401
return fiber.ErrForbidden          // 403
return fiber.ErrInternalServerError // 500
```

## Logging Errors

Always log with context:

```go
logger.Error("failed to create form",
    slog.Any("error", err),
    slog.String("form_name", form.Name),
    slog.Uint64("user_id", uint64(userID)),
)
```

Actionable admin validation and reference conflicts should be re-rendered with the submitted values. Do not turn them into plaintext global errors. HTMX failures must redirect or render a complete recovery page so no branch leaves the browser at a dead end.
