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
	ballots := baseLayer.Ballots()
	base := ballots[rng.Intn(len(ballots))]
	votes := Voting{Base: base.ID()}
	if len(layers[len(layers)-1].BlocksIDs()) > 0 {
		votes.Support = layers[len(layers)-1].BlocksIDs()[0:1]
		votes.Against = layers[len(layers)-1].BlocksIDs()[1:]
	}
	return votes
}

// ConsistentVoting selects same base ballot for ballot at a specific index.
func ConsistentVoting(rng *rand.Rand, layers []*types.Layer, i int) Voting {
	baseLayer := layers[len(layers)-1]
	ballots := baseLayer.Ballots()
	base := ballots[i%len(ballots)]
	votes := Voting{Base: base.ID()}
	if len(layers[len(layers)-1].BlocksIDs()) > 0 {
		votes.Support = layers[len(layers)-1].BlocksIDs()[0:1]
		votes.Against = layers[len(layers)-1].BlocksIDs()[1:]
	}
	return votes
}

// VaryingVoting votes using first generator for ballots before mid, and with second generator after mid.
func VaryingVoting(mid int, first, second VotesGenerator) VotesGenerator {
	return func(rng *rand.Rand, layers []*types.Layer, i int) Voting {
		if i < mid {
			return first(rng, layers, i)
		}
		return second(rng, layers, i)
	}
}
