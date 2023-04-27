package result

import "github.com/spacemeshos/go-spacemesh/common/types"

type Layer struct {
	Layer   types.LayerID
	Opinion types.Hash32
	Blocks  []Block
}

type Block struct {
	Header types.Vote
	Valid  bool
	Hare   bool
	Data   bool
}
