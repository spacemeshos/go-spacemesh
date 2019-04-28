//go-spacemesh is a golang implementation of the Spacemesh node.
//See - https://spacemesh.io
package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/cmd/node"
	"os"
)

func main() { // run the app
	if err := node.Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
