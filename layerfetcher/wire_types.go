package layerfetcher

import "github.com/spacemeshos/go-spacemesh/common/types"

type layerHash struct {
	// ProcessedLayer is the latest processed layer from peer
	ProcessedLayer types.LayerID
	// Hash is the hash of contextually valid blocks (sorted by block ID) in the given layer
	Hash types.Hash32
	// AggregatedHash is the aggregated hash of all layers up to the given layer
	AggregatedHash types.Hash32
}

// layerBlocks is the response for a given layer hash
type layerBlocks struct {
	Blocks          []types.BlockID
	LatestBlocks    []types.BlockID // LatestBlocks are the blocks received in the last 30 seconds from gossip
	VerifyingVector []types.BlockID // VerifyingVector is the input vector for verifying tortoise
}
