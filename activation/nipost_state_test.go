package activation

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/natefinch/atomic"
	"github.com/stretchr/testify/require"
)

func TestWriteRecover(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test")
	data := []byte("secret")
	require.NoError(t, write(path, data))
	got, err := read(path)
	require.NoError(t, err)
	require.True(t, bytes.Equal(data, got))
}

func TestCorrupted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "test")
	data := []byte("secret")
	require.NoError(t, write(path, data))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	data = append([]byte("new "), data...)
	err = atomic.WriteFile(path, bytes.NewReader(data))
	require.NoError(t, err)
	got, err := read(path)
	require.ErrorContains(t, err, "wrong checksum")
	require.Nil(t, got)
}
