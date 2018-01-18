package tests

import (
	"github.com/spacemeshos/go-spacemesh/app"
	"github.com/spacemeshos/go-spacemesh/assert"
	"github.com/spacemeshos/go-spacemesh/filesystem"
	"os"
	"testing"
	"time"
)

func TestApp(t *testing.T) {

	filesystem.DeleteSpaceMeshDataFolders(t)

	// remove all injected test flags for now
	os.Args = []string{"/go-spacemesh", "-jrpc"}

	go app.Main("", "master", "")

	assert.NotNil(t, app.App)

	// let node warmup
	time.Sleep(3 * time.Second)

	assert.NotNil(t, app.App.Node)
	assert.NotNil(t, app.App)

	// app should exit based on this signal
	app.ExitApp <- true

	filesystem.DeleteSpaceMeshDataFolders(t)

}
