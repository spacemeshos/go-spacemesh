package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/filesystem"
)

func TestApp(t *testing.T) {
	filesystem.SetupTestSpacemeshDataFolders(t, "app_test")

	// remove all injected test flags for now
	os.Args = []string{"/go-spacemesh", "--json-server=true"}

	go Main()

	<-EntryPointCreated

	assert.NotNil(t, App)

	<-App.NodeInitCallback

	assert.NotNil(t, App.P2P)
	assert.NotNil(t, App)
	assert.Equal(t, App.Config.API.StartJSONServer, true)

	// app should exit based on this signal
	Cancel()

	filesystem.DeleteSpacemeshDataFolders(t)

}
