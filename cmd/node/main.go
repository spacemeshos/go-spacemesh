// go-spacemesh is a golang implementation of the Spacemesh node.
// See - https://spacemesh.io
package main

import (
	_ "net/http/pprof"
	"os"

	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/node"
)

var (
	version   string
	commit    string
	branch    string
	noMainNet string
)

func main() { // run the app
	cmd.Version = version
	cmd.Commit = commit
	cmd.Branch = branch
	cmd.NoMainNet = noMainNet == "true"
	if err := node.GetCommand().Execute(); err != nil {
		// Do not print error as cmd.SilenceErrors is false
		// and the error was already printed
		os.Exit(1)
	}
}
