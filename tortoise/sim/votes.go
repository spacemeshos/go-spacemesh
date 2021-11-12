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
// TODO(dshulyak) what is the best way to encapsulate all configuration that is required to generate votes?
type VotesGenerator func(rng *rand.Rand, layers []*types.Layer, i int) Voting

// PerfectVoting select base block from previouos layer and supports all blocks from previous layer.
// used by default.
func PerfectVoting(rng *rand.Rand, layers []*types.Layer, _ int) Voting {
	baseLayer := layers[len(layers)-1]
	support := layers[len(layers)-1].BlocksIDs()
	base := baseLayer.Blocks()[rng.Intn(len(baseLayer.Blocks()))]
	return Voting{Base: base.ID(), Support: support}
}
