package vm

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func athenaLibPath() string {
	var err error

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}

	switch runtime.GOOS {
	case "windows":
		return filepath.Join(cwd, "../build/libathenavmwrapper.dll")
	case "darwin":
		return filepath.Join(cwd, "../build/libathenavmwrapper.dylib")
	default:
		return filepath.Join(cwd, "../build/libathenavmwrapper.so")
	}
}

func TestNewHost(t *testing.T) {
	host, err := NewHost(athenaLibPath())
	require.NoError(t, err)
	defer host.Destroy()

	require.Equal(t, "Athena", host.vm.Name())
}
