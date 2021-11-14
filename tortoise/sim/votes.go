package sim

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Voting contains blocks voting.
type Voting struct {
	Base                      types.BlockID
	Support, Against, Abstain []types.BlockID
}

// VotesGenerator allows to replace default votes generator.
type VotesGenerator func(rng *rand.Rand, layers []*types.Layer) Voting

// perfectVoting select base block from previouos layer and supports all blocks from previous layer.
// used by default.
func perfectVoting(rng *rand.Rand, layers []*types.Layer) Voting {
	baseLayer := layers[len(layers)-1]
	support := layers[len(layers)-1].BlocksIDs()
	base := baseLayer.Blocks()[rng.Intn(len(baseLayer.Blocks()))]
	return Voting{Base: base.ID(), Support: support}
}
