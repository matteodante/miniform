package tests

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/karloscodes/matcha/testrunner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstallation(t *testing.T) {
	t.Run("installs Miniform and exposes its health endpoint", func(t *testing.T) {
		if os.Getenv("MINIFORM_RUN_INSTALLATION_TEST") != "1" {
			t.Skip("set MINIFORM_RUN_INSTALLATION_TEST=1 to run the VM installer test")
		}
		t.Setenv("ENV", "test")

		binary, err := installationBinary(t)
		require.NoError(t, err)
		configuration := installerConfig(binary)
		keepEnvironmentForVerification(t, configuration.VMName)

		runner := testrunner.NewTestRunner(configuration)
		err = runner.Run()
		require.NoErrorf(t, err, "installer failed\nstdout:\n%s\nstderr:\n%s", runner.Stdout(), runner.Stderr())

		for _, text := range []string{"Domain", "Summary", "Proceed?", "Done.", "Visit"} {
			assert.Contains(t, runner.Stdout(), text)
		}
		assert.True(t, installationHealthy(t, runner), "GET /_health did not become ready")
		verifyInstalledState(t, runner)
	})
}

func installationBinary(t *testing.T) (string, error) {
	t.Helper()
	if configured := strings.TrimSpace(os.Getenv("BINARY_PATH")); configured != "" {
		if info, err := os.Stat(configured); err != nil || info.IsDir() {
			return "", fmt.Errorf("BINARY_PATH does not point to a file: %s", configured)
		}
		if !isLinuxExecutable(configured) {
			return "", fmt.Errorf("BINARY_PATH is not a Linux executable: %s", configured)
		}
		return configured, nil
	}
	if runtime.GOOS != "linux" {
		return "", errors.New("set BINARY_PATH to a CGO-enabled Linux binary; make test-integration builds one automatically")
	}

	root, err := filepath.Abs("../..")
	if err != nil {
		return "", fmt.Errorf("resolve project root: %w", err)
	}
	output := filepath.Join(t.TempDir(), "miniform-linux")
	command := exec.CommandContext(t.Context(), "go", "build", "-trimpath", "-o", output, "./cmd/miniform")
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=1")
	if buildOutput, err := command.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build Linux installer binary: %w\n%s", err, buildOutput)
	}
	return output, nil
}

func isLinuxExecutable(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	magic := make([]byte, 4)
	_, err = file.Read(magic)
	return err == nil && string(magic) == "\x7fELF"
}

func installerConfig(binary string) testrunner.Config {
	appImage := strings.TrimSpace(os.Getenv("MINIFORM_INSTALLER_TEST_IMAGE"))
	if appImage == "" {
		appImage = "ghcr.io/matteodante/miniform:latest"
	}
	configuration := testrunner.DefaultConfig()
	configuration.BinaryPath = binary
	configuration.BinaryName = "miniform"
	configuration.Args = []string{"install"}
	configuration.StdinInput = "localhost\ny\n"
	configuration.Timeout = 10 * time.Minute
	configuration.VMName = "miniform-install-test"
	configuration.Debug = os.Getenv("DEBUG") == "1"
	configuration.SkipCleanup = true
	configuration.EnvVars = map[string]string{
		"ENV":                "test",
		"SKIP_PORT_CHECKING": "1",
		"APP_IMAGE":          appImage,
	}
	return configuration
}

func keepEnvironmentForVerification(t *testing.T, vmName string) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") == "true" || os.Getenv("KEEP_VM") == "1" {
		return
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if output, err := exec.CommandContext(ctx, "orb", "delete", vmName, "-f").CombinedOutput(); err != nil {
			t.Logf("delete test VM: %v (%s)", err, strings.TrimSpace(string(output)))
		}
	})
}

func installationHealthy(t *testing.T, runner *testrunner.TestRunner) bool {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") != "true" {
		return runner.CheckHealth("http://localhost/_health", 10)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	for attempt := 0; attempt < 10; attempt++ {
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://localhost/_health", nil)
		if err != nil {
			return false
		}
		response, err := client.Do(request)
		if err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusOK {
				return true
			}
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

func verifyInstalledState(t *testing.T, runner *testrunner.TestRunner) {
	t.Helper()
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		output, err := exec.CommandContext(t.Context(), "docker", "ps", "--format", "{{.Names}}\t{{.Image}}", "--filter", "name=^miniform").CombinedOutput()
		require.NoError(t, err, string(output))
		assert.Contains(t, string(output), strings.TrimSpace(os.Getenv("MINIFORM_INSTALLER_TEST_IMAGE")))

		registry, err := os.ReadFile("/etc/matcha/config.yml")
		require.NoError(t, err)
		assert.Contains(t, string(registry), "domain: localhost")
		assert.Contains(t, string(registry), strings.TrimSpace(os.Getenv("MINIFORM_INSTALLER_TEST_IMAGE")))
		return
	}
	containers, err := runner.RunCommand("docker ps --format '{{.Names}}'", true)
	require.NoError(t, err)
	assert.Contains(t, containers, "miniform")
	assert.Contains(t, containers, "matcha-proxy")

	registry, err := runner.RunCommand("cat /etc/matcha/config.yml", true)
	require.NoError(t, err)
	assert.Contains(t, registry, "miniform:")
	assert.Contains(t, registry, "domain: localhost")
	assert.Contains(t, registry, "PRIVATE_KEY")
}
