// go-spacemesh is a golang implementation of the Spacemesh node.
// See - https://spacemesh.io
package main

import (
	_ "net/http/pprof"
	"os"
	"slices"

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
		Use:   "go-spacemesh",
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
	// TODO: Move version, relay subcommands from node service and smeshing
	// service subcommands to root command.

	if len(os.Args) > 1 && !slices.Contains(os.Args, "--help") && !slices.Contains(os.Args, "-h") {
		firstArg := os.Args[1]
		foundMatch := false
		for _, command := range rootCmd.Commands() {
			if command.Name() == firstArg {
				foundMatch = true
				break
			}
		}
		if !foundMatch {
			// TODO: replace starting in node service/legacy mode
			// with starting in combined mode, when it will be implemented
			// https://github.com/spacemeshos/go-spacemesh/issues/6638
			args := append(
				[]string{node.GetNodeServiceCommand().Name()},
				os.Args[1:]...,
			)
			rootCmd.SetArgs(args)
		}
	}

	if err := rootCmd.Execute(); err != nil {
		// Do not print error as cmd.SilenceErrors is false
		// and the error was already printed
		os.Exit(1)
	}
}
