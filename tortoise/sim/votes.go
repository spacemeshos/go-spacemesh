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

// GapVote will skip one layer in voting.
func GapVote(rng *rand.Rand, layers []*types.Layer) Voting {
	if len(layers) == 1 {
		return perfectVoting(rng, layers)
	}
	baseLayer := layers[len(layers)-1]
	support := layers[len(layers)-2].BlocksIDs()
	base := baseLayer.Blocks()[rng.Intn(len(baseLayer.Blocks()))]
	return Voting{Base: base.ID(), Support: support}
}

// OlderExceptions will vote for block older then base block.
func OlderExceptions(rng *rand.Rand, layers []*types.Layer) Voting {
	if len(layers) == 1 {
		return perfectVoting(rng, layers)
	}
	baseLayer := layers[len(layers)-1]
	base := baseLayer.Blocks()[rng.Intn(len(baseLayer.Blocks()))]
	voting := Voting{Base: base.ID()}
	for _, layer := range layers[len(layers)-2:] {
		for _, bid := range layer.BlocksIDs() {
			voting.Support = append(voting.Support, bid)
		}
	}
	return voting
}
