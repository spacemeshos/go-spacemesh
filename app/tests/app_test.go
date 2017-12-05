package tests

import (
	"github.com/UnrulyOS/go-unruly/app"
	"github.com/UnrulyOS/go-unruly/assert"
	"testing"
	"time"
)

func TestApp(t *testing.T) {
	go app.Main("", "master","")

	assert.NotNil(t,app.App)

	// let node warmup
	time.Sleep(3 * time.Second)

	assert.NotNil(t,app.App.Node)

	app.ExitApp <- true
}
