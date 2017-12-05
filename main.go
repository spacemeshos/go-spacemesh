package main

import (
	"github.com/UnrulyOS/go-unruly/app"
	"github.com/UnrulyOS/go-unruly/log"
)

// set by build too
var commit, branch, version string

func main() {

	log.Info("App Starting... %s %s %s", commit, branch, version)

	// run the app
	app.Main(commit, branch, version)

	// add any playground tests here....
}
