//go-spacemesh is a golang implementation of the Spacemesh node.
//See - https://spacemesh.io
package main

import (
	"fmt"
	"os"

	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/cmd/node"
)

var (
	version string
	commit  string
	branch  string
)

func main() { // run the app
	cmd.Version = version
	cmd.Commit = commit
	cmd.Branch = branch
	if err := node.Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
