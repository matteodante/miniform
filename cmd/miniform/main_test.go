package main

import (
	"bytes"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/matteodante/miniform/internal/cli"
)

func TestCommandEntryPoint(t *testing.T) {
	t.Run("help and version describe this application", func(t *testing.T) {
		var output bytes.Buffer
		require.NoError(t, printUsage(&output))
		assert.Contains(t, output.String(), "Miniform — self-hosted form inbox")
		assert.Contains(t, output.String(), "submission | event | commands")
		assert.Contains(t, output.String(), "backup                      Create")

		output.Reset()
		require.NoError(t, printVersion(&output))
		assert.Contains(t, output.String(), "Miniform "+version)
		assert.NotContains(t, output.String(), "Build Time")
	})

	t.Run("deployment manager keeps the Miniform contract", func(t *testing.T) {
		t.Setenv("APP_IMAGE", "")
		configuration := newMatcha().GetConfig()
		assert.Equal(t, "miniform", configuration.Name)
		assert.Equal(t, "ghcr.io/matteodante/miniform:latest", configuration.AppImage)
		assert.Equal(t, "/_health", configuration.HealthPath)
		assert.True(t, configuration.Backups)
		assert.False(t, configuration.CronUpdates)
	})

	t.Run("deployment manager accepts a test image", func(t *testing.T) {
		t.Setenv("APP_IMAGE", "localhost:5000/miniform:test")
		assert.Equal(t, "localhost:5000/miniform:test", newMatcha().GetConfig().AppImage)
	})

	t.Run("server flags reject unexpected input before startup", func(t *testing.T) {
		require.ErrorContains(t, runServer([]string{"unexpected"}), "unexpected server argument")
		require.Error(t, runServer([]string{"--unknown"}))
	})

	t.Run("invalid data CLI flags do not initialize storage", func(t *testing.T) {
		workingDirectory := t.TempDir()
		dataDirectory := filepath.Join(workingDirectory, "storage")
		t.Chdir(workingDirectory)
		t.Setenv("MINIFORM_ENV", "test")
		t.Setenv("MINIFORM_DATA_DIR", dataDirectory)
		t.Setenv("MINIFORM_LOGS_DIR", filepath.Join(dataDirectory, "logs"))
		t.Setenv("MINIFORM_DATABASE_PATH", filepath.Join(dataDirectory, "miniform.db"))

		exitCode := runDataCLI([]string{"form", "list", "--bogus"})

		assert.Equal(t, cli.ExitUsage, exitCode)
		_, err := os.Stat(dataDirectory)
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("command help does not load production configuration or storage", func(t *testing.T) {
		for _, helpFlag := range []string{"--help", "-h"} {
			t.Run(helpFlag, func(t *testing.T) {
				dataDirectory := filepath.Join(t.TempDir(), "storage")
				t.Setenv("MINIFORM_ENV", "production")
				t.Setenv("MINIFORM_SESSION_SECRET", "")
				t.Setenv("MINIFORM_DATA_DIR", dataDirectory)
				t.Setenv("MINIFORM_LOGS_DIR", filepath.Join(dataDirectory, "logs"))
				t.Setenv("MINIFORM_DATABASE_PATH", filepath.Join(dataDirectory, "miniform.db"))

				exitCode := execute([]string{"form", "list", helpFlag})

				assert.Equal(t, cli.ExitSuccess, exitCode)
				_, err := os.Stat(dataDirectory)
				assert.ErrorIs(t, err, os.ErrNotExist)
			})
		}
	})

	t.Run("exits cleanly on SIGTERM", func(t *testing.T) {
		if os.Getenv("MINIFORM_SIGTERM_HELPER") == "1" {
			os.Exit(execute([]string{"serve"}))
		}
		if runtime.GOOS == "windows" {
			t.Skip("SIGTERM is unavailable on Windows")
		}

		probe, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", ":0")
		require.NoError(t, err)
		_, port, err := net.SplitHostPort(probe.Addr().String())
		require.NoError(t, err)
		require.NoError(t, probe.Close())
		storage := t.TempDir()

		command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=TestCommandEntryPoint/exits_cleanly_on_SIGTERM")
		command.Env = append(os.Environ(),
			"MINIFORM_SIGTERM_HELPER=1",
			"MINIFORM_ENV=test",
			"MINIFORM_PORT="+port,
			"MINIFORM_DATA_DIR="+storage,
			"MINIFORM_LOGS_DIR="+filepath.Join(storage, "logs"),
			"MINIFORM_DATABASE_PATH="+filepath.Join(storage, "miniform.db"),
			"MINIFORM_SESSION_SECRET=test-session-secret",
		)
		var output bytes.Buffer
		command.Stdout, command.Stderr = &output, &output
		require.NoError(t, command.Start())
		t.Cleanup(func() {
			if command.ProcessState == nil {
				_ = command.Process.Kill()
			}
		})
		done := make(chan error, 1)
		go func() { done <- command.Wait() }()

		healthURL := "http://127.0.0.1:" + port + "/_health"
		require.Eventually(t, func() bool {
			request, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, healthURL, nil)
			if requestErr != nil {
				return false
			}
			response, requestErr := http.DefaultClient.Do(request) // #nosec G107 -- the URL points to the helper process.
			if requestErr != nil {
				select {
				case processErr := <-done:
					t.Fatalf("server exited before becoming ready: %v\n%s", processErr, output.String())
				default:
				}
				return false
			}
			_ = response.Body.Close()
			return response.StatusCode == http.StatusOK
		}, 5*time.Second, 25*time.Millisecond)

		require.NoError(t, command.Process.Signal(syscall.SIGTERM))
		select {
		case err := <-done:
			require.NoError(t, err, output.String())
		case <-time.After(5 * time.Second):
			t.Fatal("server did not stop after SIGTERM")
		}
	})
}
