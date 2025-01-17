// go-spacemesh is a golang implementation of the Spacemesh node.
// See - https://spacemesh.io
package main

import (
	_ "net/http/pprof"
	"os"

	"github.com/spf13/cobra"

	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/node"
)

var (
	version   string
	commit    string
	branch    string
	noMainNet string
	rootCmd   = &cobra.Command{
		Use:   "service",
		Short: "Start spacemesh service",
	}
)

func main() { // run the app
	cmd.Version = version
	cmd.Commit = commit
	cmd.Branch = branch
	cmd.NoMainNet = noMainNet == "true"
	rootCmd.AddCommand(node.GetNodeServiceCommand())
	rootCmd.AddCommand(node.GetSmeshingServiceCommand())
	if err := rootCmd.Execute(); err != nil {
		// Do not print error as cmd.SilenceErrors is false
		// and the error was already printed
		os.Exit(1)
	}
}
