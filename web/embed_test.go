package web

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEmbeddedAssets(t *testing.T) {
	t.Run("exclude Fiber development compression sidecars", func(t *testing.T) {
		err := fs.WalkDir(Static, ".", func(path string, entry fs.DirEntry, err error) error {
			require.NoError(t, err)
			require.False(t, strings.HasSuffix(path, ".fiber.gz"), path)
			return nil
		})
		require.NoError(t, err)
	})
}
