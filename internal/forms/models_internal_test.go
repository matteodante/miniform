package forms

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingRandomReader struct{}

func (failingRandomReader) Read([]byte) (int, error) {
	return 0, errors.New("random source unavailable")
}

func TestRandomHex(t *testing.T) {
	t.Run("encodes every requested byte", func(t *testing.T) {
		value, err := randomHex(bytes.NewReader([]byte{0xab, 0xcd}), 2)
		require.NoError(t, err)
		assert.Equal(t, "abcd", value)
	})

	t.Run("propagates source errors", func(t *testing.T) {
		_, err := randomHex(failingRandomReader{}, 10)
		require.Error(t, err)
		assert.ErrorContains(t, err, "random source unavailable")
	})
}
