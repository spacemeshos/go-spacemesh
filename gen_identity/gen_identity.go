package main

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"os"
)

func main() {

	if len(os.Args) != 2 {
		fmt.Printf("Usage: %v <path/to/output_dir>\n", os.Args[0])
		return
	}

	key, err := p2p.EnsureIdentity(os.Args[1])
	if err != nil {
		fmt.Printf("failed generating identity: %v\n", err)
	}

	fmt.Printf("Private Key: %x\n", key)
}
