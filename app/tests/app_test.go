package tests

import (
	"github.com/UnrulyOS/go-unruly/app"
	"github.com/UnrulyOS/go-unruly/assert"
	"os"
	"testing"
	"time"
)

func TestApp(t *testing.T) {

	// remove all inkected test flags for now
	os.Args = []string{"go-unruly", "-jrpc"}

	go app.Main("", "master", "")

	assert.NotNil(t, app.App)

	// let node warmup
	time.Sleep(3 * time.Second)

	assert.NotNil(t, app.App.Node)
	assert.NotNil(t, app.App)

	// app should exit based on this signal
	app.ExitApp <- true
}
