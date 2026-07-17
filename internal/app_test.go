package internal

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	cartridgeconfig "github.com/karloscodes/cartridge/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/config"
	"github.com/matteodante/miniform/internal/forms"
)

func TestAppLifecycle(t *testing.T) {
	t.Run("closes its logger exactly once", func(t *testing.T) {
		previousLogger := slog.Default()
		ownedLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
		slog.SetDefault(ownedLogger)
		t.Cleanup(func() { slog.SetDefault(previousLogger) })
		closer := &countingCloser{}
		app := &App{Logger: ownedLogger, logCloser: closer, previousLogger: previousLogger}

		require.NoError(t, app.Close())
		require.NoError(t, app.Close())
		assert.Equal(t, 1, closer.calls)
		assert.Same(t, previousLogger, slog.Default())
	})

	t.Run("does not replace a newer default logger", func(t *testing.T) {
		previousLogger := slog.Default()
		ownedLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
		newerLogger := slog.New(slog.NewTextHandler(io.Discard, nil))
		slog.SetDefault(newerLogger)
		t.Cleanup(func() { slog.SetDefault(previousLogger) })
		app := &App{Logger: ownedLogger, previousLogger: previousLogger}

		require.NoError(t, app.Close())
		assert.Same(t, newerLogger, slog.Default())
	})

	t.Run("releases its listener when started with an already cancelled context", func(t *testing.T) {
		probe, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ":0")
		require.NoError(t, err)
		_, port, err := net.SplitHostPort(probe.Addr().String())
		require.NoError(t, err)
		require.NoError(t, probe.Close())

		storage := t.TempDir()
		t.Setenv("MINIFORM_ENV", "test")
		t.Setenv("MINIFORM_PORT", port)
		t.Setenv("MINIFORM_DATA_DIR", storage)
		t.Setenv("MINIFORM_LOGS_DIR", filepath.Join(storage, "logs"))
		t.Setenv("MINIFORM_DATABASE_PATH", filepath.Join(storage, "miniform.db"))
		t.Setenv("MINIFORM_SESSION_SECRET", "test-session-secret")
		app, err := NewApp()
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		require.NoError(t, app.run(ctx, time.Second))
		rebound, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", net.JoinHostPort("", port))
		require.NoError(t, err)
		require.NoError(t, rebound.Close())
	})

	t.Run("claims the HTTP port before initializing storage", func(t *testing.T) {
		occupied, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ":0")
		require.NoError(t, err)
		defer func() { _ = occupied.Close() }()
		_, port, err := net.SplitHostPort(occupied.Addr().String())
		require.NoError(t, err)
		root := t.TempDir()
		storage := filepath.Join(root, "not-created")
		t.Setenv("MINIFORM_ENV", "test")
		t.Setenv("MINIFORM_PORT", port)
		t.Setenv("MINIFORM_DATA_DIR", storage)
		t.Setenv("MINIFORM_LOGS_DIR", filepath.Join(storage, "logs"))
		t.Setenv("MINIFORM_DATABASE_PATH", filepath.Join(storage, "miniform.db"))
		t.Setenv("MINIFORM_SESSION_SECRET", "test-session-secret")

		app, err := NewApp()
		if app != nil {
			t.Cleanup(func() { require.NoError(t, app.Close()) })
		}

		require.ErrorContains(t, err, "listen")
		assert.NoDirExists(t, storage)
	})

	t.Run("does not start jobs when the HTTP port cannot be bound", func(t *testing.T) {
		listener, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ":0")
		require.NoError(t, err)
		defer func() { _ = listener.Close() }()
		_, port, err := net.SplitHostPort(listener.Addr().String())
		require.NoError(t, err)

		jobsStarted := false
		app := &App{
			Config: &config.Config{Config: &cartridgeconfig.Config{Port: port}},
			runJobs: func(context.Context) {
				jobsStarted = true
			},
		}

		err = app.run(context.Background(), time.Second)
		require.ErrorContains(t, err, "listen")
		assert.False(t, jobsStarted)
	})

	t.Run("stops the server and jobs when its context is cancelled", func(t *testing.T) {
		probe, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ":0")
		require.NoError(t, err)
		_, port, err := net.SplitHostPort(probe.Addr().String())
		require.NoError(t, err)
		require.NoError(t, probe.Close())

		storage := t.TempDir()
		t.Setenv("MINIFORM_ENV", "test")
		t.Setenv("MINIFORM_PORT", port)
		t.Setenv("MINIFORM_DATA_DIR", storage)
		t.Setenv("MINIFORM_LOGS_DIR", filepath.Join(storage, "logs"))
		t.Setenv("MINIFORM_DATABASE_PATH", filepath.Join(storage, "miniform.db"))
		t.Setenv("MINIFORM_SESSION_SECRET", "test-session-secret")
		app, err := NewApp()
		require.NoError(t, err)
		requestStarted := make(chan struct{})
		requestCanceled := make(chan struct{})
		app.Server.App().Get("/_test/wait", func(ctx *fiber.Ctx) error {
			close(requestStarted)
			<-ctx.UserContext().Done()
			close(requestCanceled)
			return ctx.SendStatus(fiber.StatusNoContent)
		})
		untrackedUpload := filepath.Join(storage, "uploads", "restored-later", "attachment.txt")
		require.NoError(t, os.MkdirAll(filepath.Dir(untrackedUpload), 0o700))
		require.NoError(t, os.WriteFile(untrackedUpload, []byte("attachment"), 0o600))
		require.NoError(t, RunMigrations(context.Background(), app))
		assert.FileExists(t, untrackedUpload)
		assert.GreaterOrEqual(t, app.Server.App().Server().MaxRequestBodySize, forms.MaxTotalFiles*forms.MaxFileSize)

		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- app.run(ctx, time.Second) }()

		url := "http://127.0.0.1:" + port + "/_health"
		require.Eventually(t, func() bool {
			request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
			if requestErr != nil {
				return false
			}
			response, requestErr := http.DefaultClient.Do(request) // #nosec G107 -- the URL points to this test server.
			if requestErr != nil {
				return false
			}
			_ = response.Body.Close()
			return response.StatusCode == http.StatusOK
		}, 3*time.Second, 20*time.Millisecond)
		request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://127.0.0.1:"+port+"/forms/missing/submit?token=x", bytes.NewReader(make([]byte, 5*1024*1024)))
		require.NoError(t, err)
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Origin", "https://example.com")
		response, err := http.DefaultClient.Do(request)
		require.NoError(t, err)
		defer response.Body.Close()
		assert.Equal(t, http.StatusNotFound, response.StatusCode)

		requestDone := make(chan struct{})
		waitingRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://127.0.0.1:"+port+"/_test/wait", nil)
		require.NoError(t, err)
		go func() {
			defer close(requestDone)
			response, requestErr := http.DefaultClient.Do(waitingRequest) // #nosec G107 -- the URL points to this test server.
			if requestErr == nil {
				_ = response.Body.Close()
			}
		}()
		select {
		case <-requestStarted:
		case <-time.After(time.Second):
			t.Fatal("request did not start")
		}
		cancel()
		select {
		case <-requestCanceled:
		case <-time.After(time.Second):
			t.Fatal("active request context was not cancelled")
		}
		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(3 * time.Second):
			t.Fatal("application did not stop")
		}
		select {
		case <-requestDone:
		case <-time.After(time.Second):
			t.Fatal("cancelled request did not finish")
		}
	})
}

type countingCloser struct {
	calls int
}

func (closer *countingCloser) Close() error {
	closer.calls++
	return nil
}
