package app

import (
	"os"
	"testing"

	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
)

func TestApp(t *testing.T) {

	filesystem.SetupTestSpacemeshDataFolders(t, "app_test")

	// remove all injected test flags for now
	os.Args = []string{"/go-spacemesh", "-json-server"}

	go Main("", "master", "")

	<-EntryPointCreated

	assert.NotNil(t, App)

	<-App.NodeInitCallback

	assert.NotNil(t, App.Node)
	assert.NotNil(t, App)

	// app should exit based on this signal
	ExitApp <- true

	filesystem.DeleteSpacemeshDataFolders(t)

}
