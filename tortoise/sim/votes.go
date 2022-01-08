package sim

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Voting contains blocks voting.
type Voting = types.Votes

// VotesGenerator allows to replace default votes generator.
// TODO(dshulyak) what is the best way to encapsulate all configuration that is required to generate votes?
type VotesGenerator func(rng *rand.Rand, layers []*types.Layer, i int) Voting

// PerfectVoting selects base ballot from previous layer and supports all blocks from previous layer.
// used by default.
func PerfectVoting(rng *rand.Rand, layers []*types.Layer, _ int) Voting {
	baseLayer := layers[len(layers)-1]
	support := layers[len(layers)-1].BlocksIDs()[0:1]
	against := layers[len(layers)-1].BlocksIDs()[1:]
	ballots := baseLayer.Ballots()
	base := ballots[rng.Intn(len(ballots))]
	return Voting{Base: base.ID(), Against: against, Support: support}
}
