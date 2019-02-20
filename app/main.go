package app

import "github.com/spacemeshos/go-spacemesh/app/cmd"

// Main is the entry point for the Spacemesh console app - responsible for parsing and routing cli flags and commands.
// This is the root of all evil, called from Main.main().
func Main() {
	cmd.Execute()
}
