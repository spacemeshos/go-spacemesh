package app

import (
	"github.com/spacemeshos/go-spacemesh/assert"
	"os"
	"testing"
	"time"
)

func TestApp(t *testing.T) {

	// remove all injected test flags for now
	os.Args = []string{"/go-spacemesh", "-jrpc"}

	go Main("", "master", "")

	assert.NotNil(t, App)

	// let node warmup
	time.Sleep(3 * time.Second)

	assert.NotNil(t, App.Node)
	assert.NotNil(t, App)

	// app should exit based on this signal
	ExitApp <- true
}
