package tests

import (
	"github.com/UnrulyOS/go-unruly/app"
	"github.com/UnrulyOS/go-unruly/assert"
	"testing"
	"time"
)

func TestApp(t *testing.T) {

	//you can add any flag for testing
	//os.Args = append(os.Args,"-jrpc")

	go app.Main("", "master", "")

	assert.NotNil(t, app.App)

	// let node warmup
	time.Sleep(3 * time.Second)

	assert.NotNil(t, app.App.Node)
	assert.NotNil(t, app.App)

	// app should exit based on this signal
	app.ExitApp <- true
}
