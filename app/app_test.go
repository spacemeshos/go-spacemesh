package app

import (
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"os"
	"testing"
	"time"
)

func TestApp(t *testing.T) {
	filesystem.DeleteSpacemeshDataFolders(t)

	// remove all injected test flags for now
	os.Args = []string{"/go-spacemesh", "-json-server"}

	go Main("", "master", "")

	assert.NotNil(t, App)

	// let node warmup
	time.Sleep(3 * time.Second)

	assert.NotNil(t, App.Node)
	assert.NotNil(t, App)

	// app should exit based on this signal
	ExitApp <- true

	filesystem.DeleteSpacemeshDataFolders(t)
}
