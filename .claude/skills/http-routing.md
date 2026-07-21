# HTTP Routing Pattern

## Overview

Miniform uses the cartridge framework (Fiber wrapper) with config-based route definitions.

## Route Configuration

Location: `internal/routes.go`

```go
func MountRoutes(server *cartridge.Server, cfg *config.Config) {
    requireSession := handlers.RequireSession(server.Session())
    requirePasswordChanged := handlers.RequirePasswordChanged(server.Session(), server.GetDBManager())
    authenticated := &cartridge.RouteConfig{
        CustomMiddleware: []fiber.Handler{requireSession, requirePasswordChanged},
    }

    public := &cartridge.RouteConfig{
        EnableSecFetchSite: cartridge.Bool(false),
        EnableCORS:         true,
        CORSConfig: &cors.Config{
            AllowOrigins: "*",
            AllowMethods: "POST,OPTIONS",
            AllowHeaders: "Content-Type",
        },
        CustomMiddleware: []fiber.Handler{
            limiter.New(limiter.Config{
                Max: 30, Expiration: time.Minute,
                Storage: newRateLimitStorage(),
                KeyGenerator: func(ctx *fiber.Ctx) string {
                    return miniformserver.ClientIP(ctx, cfg.IsMatchaManaged())
                },
                Next: func(*fiber.Ctx) bool { return cfg.IsDevelopment() || cfg.IsTest() },
            }),
        },
    }

    server.Get("/admin/forms", handlers.AdminFormsIndex, authenticated)
    server.Post("/forms/:slug/submit", func(ctx *cartridge.Context) error {
        return handlers.PublicFormSubmission(ctx, cfg)
    }, public)
}
```

## Handler Signature

All handlers use `cartridge.Context`:

```go
func AdminFormsIndex(ctx *cartridge.Context) error {
    db := ctx.DB()

    forms, err := forms.List(db)
    if err != nil {
        return err
    }

    return ctx.Render("layouts/base", fiber.Map{
        "Title":       "Forms",
        "Forms":       forms,
        "ContentView": "admin/forms/index/content",
    }, "")
}
```

## Route Config Options

```go
&cartridge.RouteConfig{
    // Middleware
    CustomMiddleware: []fiber.Handler{...},

    // CORS
    EnableCORS: true,
    CORSConfig: &cors.Config{...},

    // Security policy
    EnableSecFetchSite: cartridge.Bool(false),
}
```

## Response Types

```go
// JSON response
return ctx.JSON(fiber.Map{
    "success": true,
    "data":    result,
})

// HTML template
return ctx.Render("layouts/base", fiber.Map{
    "Title":       "Page Title",
    "ContentView": "path/to/content",
}, "")

// Redirect
return ctx.Redirect("/admin/forms")

// Status codes
return ctx.Status(201).JSON(fiber.Map{...})
return ctx.SendStatus(204)  // No content
```

## URL Parameters

```go
// Path params: /forms/:id
id := ctx.Params("id")

// Query params: /forms?page=1
page := ctx.Query("page", "1")  // with default

// Form data
name := ctx.FormValue("name")
```

## Rate Limiting

Applied per-route via middleware:

```go
limiter.New(limiter.Config{
    Max: 30, Expiration: time.Minute,
    Storage: newRateLimitStorage(),
    KeyGenerator: func(ctx *fiber.Ctx) string {
        return miniformserver.ClientIP(ctx, cfg.IsMatchaManaged())
    },
    Next: func(*fiber.Ctx) bool { return cfg.IsDevelopment() || cfg.IsTest() },
})
```

## SQLite writes and cancellation

Route middleware does not serialize SQLite mutations. The owning domain wraps each write with `dbtxn.WithRetry`, while handlers pass `ctx.UserContext()` into cancellable database or network work. The application cancels all request contexts during shutdown.

Production rate limits use in-process storage. Direct deployments ignore `X-Forwarded-For`; only a Matcha-managed deployment trusts the last address appended by its proxy.
