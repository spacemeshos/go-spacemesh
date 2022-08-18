package fetch

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate scalegen -types LayerData

// LayerData is the response for a given layer ID.
type LayerData struct {
	// Ballots are the ballots in layer
	Ballots []types.BallotID
	// Blocks are the blocks in a layer
	Blocks []types.BlockID
	// HareOutput is the output of hare consensus and input for verifying tortoise
	HareOutput types.BlockID
	// ProcessedLayer is the latest processed layer from peer
	ProcessedLayer types.LayerID
	// Hash is the hash of contextually valid blocks (sorted by block ID) in the given layer
	Hash types.Hash32
	// AggregatedHash is the aggregated hash of all layers up to the given layer
	AggregatedHash types.Hash32
}
