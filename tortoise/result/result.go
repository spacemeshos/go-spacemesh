package result

import "github.com/spacemeshos/go-spacemesh/common/types"

type Layer struct {
	Layer   types.LayerID
	Opinion types.Hash32
	Blocks  []Block
}

func (l *Layer) Empty() bool {
	return len(l.Blocks) == 0
}

type Block struct {
	Header types.Vote
	Valid  bool // valid based on votes counting
	Hare   bool // have certificate or observed hare termination
	Data   bool // storing data locally
}
