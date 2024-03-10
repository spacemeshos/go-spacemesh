// Package cmd is the base package for various sets of builds and executables created from go-spacemesh
package cmd

var (
	// Version is the app's semantic version. Designed to be overwritten by make.
	Version string

	// Branch is the git branch used to build the App. Designed to be overwritten by make.
	Branch string

	// Commit is the git commit used to build the app. Designed to be overwritten by make.
	Commit string

	// Prohibit this build from running on the mainnet.
	NoMainNet bool
)
