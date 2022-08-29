package fetch

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen

// LayerData is the data response for a given layer ID.
type LayerData struct {
	// Ballots are the ballots in layer
	Ballots []types.BallotID
	// Blocks are the blocks in a layer
	Blocks []types.BlockID
	// Hash is the hash of contextually valid blocks (sorted by block ID) in the given layer
	Hash types.Hash32
	// AggregatedHash is the aggregated hash of all layers up to the given layer
	AggregatedHash types.Hash32
}

// LayerOpinions is the response for opinions for a given layer.
type LayerOpinions struct {
	// Cert is the certificate for the layer.
	Cert *types.Certificate
}
