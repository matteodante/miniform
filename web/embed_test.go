package web

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedAssets(t *testing.T) {
	t.Run("include production assets", func(t *testing.T) {
		for _, path := range []string{"app.built.css", "mark.svg", "vendor/htmx.min.js"} {
			file, err := Static.Open(path)
			require.NoError(t, err, path)
			require.NoError(t, file.Close(), path)
		}
	})

	t.Run("exclude source and removed vendor assets", func(t *testing.T) {
		for _, path := range []string{"app.css", "vendor/highlight.min.js", "vendor/highlight-github-dark.min.css"} {
			file, err := Static.Open(path)
			if file != nil {
				require.NoError(t, file.Close(), path)
			}
			assert.ErrorIs(t, err, fs.ErrNotExist, path)
		}
	})

	t.Run("exclude Fiber development compression sidecars", func(t *testing.T) {
		err := fs.WalkDir(Static, ".", func(path string, entry fs.DirEntry, err error) error {
			require.NoError(t, err)
			require.False(t, strings.HasSuffix(path, ".fiber.gz"), path)
			return nil
		})
		require.NoError(t, err)
	})
}
