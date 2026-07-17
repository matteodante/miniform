package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandEntryPoint(t *testing.T) {
	t.Run("help and version describe this application", func(t *testing.T) {
		var output bytes.Buffer
		require.NoError(t, printUsage(&output))
		assert.Contains(t, output.String(), "Miniform — self-hosted form inbox")
		assert.Contains(t, output.String(), "submission | event | commands")

		output.Reset()
		require.NoError(t, printVersion(&output))
		assert.Contains(t, output.String(), "Miniform "+version)
	})

	t.Run("deployment manager keeps the Miniform contract", func(t *testing.T) {
		t.Setenv("APP_IMAGE", "")
		configuration := newMatcha().GetConfig()
		assert.Equal(t, "miniform", configuration.Name)
		assert.Equal(t, "ghcr.io/matteodante/miniform:latest", configuration.AppImage)
		assert.Equal(t, "/_health", configuration.HealthPath)
		assert.True(t, configuration.Backups)
	})

	t.Run("deployment manager accepts a test image", func(t *testing.T) {
		t.Setenv("APP_IMAGE", "localhost:5000/miniform:test")
		assert.Equal(t, "localhost:5000/miniform:test", newMatcha().GetConfig().AppImage)
	})

	t.Run("server flags reject unexpected input before startup", func(t *testing.T) {
		require.ErrorContains(t, runServer([]string{"unexpected"}), "unexpected server argument")
		require.Error(t, runServer([]string{"--unknown"}))
	})
}
