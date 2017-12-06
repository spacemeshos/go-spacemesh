package main

import (
	"github.com/UnrulyOS/go-unruly/app"
)

// set by build too
var (
	commit  = ""
	branch  = ""
	version = "0.0.1"
)

func main() {

	// run the app
	app.Main(commit, branch, version)

	// add any playground tests here....
}
