package vm

import (
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// get path to build/libathenavmwrapper.so
func athenaLibPath() string {
	var err error

	cwd, err := os.Getwd()
	if err != nil {
		log.Fatalf("Failed to get current working directory: %v", err)
	}
	return filepath.Join(cwd, "../build/libathenavmwrapper.so")
}

func TestNewHost(t *testing.T) {
	host, err := NewHost(athenaLibPath())
	require.NoError(t, err)
	defer host.Destroy()

	require.Equal(t, "Athena", host.vm.Name())
}
