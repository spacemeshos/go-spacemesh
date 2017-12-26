package main

import (
	"github.com/spacemeshos/go-spacemesh/app"
)

// vars set by make from outside
var (
	commit  = ""
	branch  = ""
	version = "0.0.1"
)

func main() {
	// run the app
	app.Main(commit, branch, version)
}
