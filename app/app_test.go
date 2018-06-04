package app

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/spacemeshos/go-spacemesh/config"
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

	assert.NotNil(t, App.Node)
	assert.NotNil(t, App)

	// app should exit based on this signal
	ExitApp <- true

	filesystem.DeleteSpacemeshDataFolders(t)

}

func TestParseConfig(t *testing.T) {
	err := config.LoadConfig("./config.toml")
	assert.Nil(t, err)
	_, err = ParseConfig()
	assert.Nil(t, err)
}
