package main

import (
	"fmt"
	"os"

	"github.com/spacemeshos/go-spacemesh/p2p"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Printf("Usage: %v <path/to/output_dir>\n", os.Args[0])
		os.Exit(1)
	}

	dir := os.Args[1]
	_, err := p2p.EnsureIdentity(dir)
	if err != nil {
		fmt.Printf("failed generating identity: %v\n", err)
		os.Exit(1)
	}

	identityStr, err := p2p.PrettyIdentityInfoFromDir(dir)
	if err != nil {
		fmt.Printf("failed fetching identity from file: %v\n", err)
		os.Exit(1)
	}

	// Print ID
	fmt.Printf("%s\n", identityStr)
}
