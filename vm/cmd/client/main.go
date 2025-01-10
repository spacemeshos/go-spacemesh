package main

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/vm/cmd/client/cmd"
)

func main() {
	types.SetNetworkHRP("atest")
	cmd.Execute()
}
