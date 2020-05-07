//go-spacemesh is a golang implementation of the Spacemesh node.
//See - https://spacemesh.io
package main

import (
	"cloud.google.com/go/profiler"
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
	if err := profiler.Start(profiler.Config{
		Service:        "go-spacemesh",
		ServiceVersion: fmt.Sprintf("%s+%s+%s", version, commit, branch),
		MutexProfiling: true,
	}); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "failed to start profiler:", err)
	}

	cmd.Version = version
	cmd.Commit = commit
	cmd.Branch = branch
	if err := node.Cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
