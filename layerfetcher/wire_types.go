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
	Blocks          []types.BlockID `ssz-max:"4096"`
	LatestBlocks    []types.BlockID `ssz-max:"4096"` // LatestBlocks are the blocks received in the last 30 seconds from gossip
	VerifyingVector []types.BlockID `ssz-max:"4096"` // VerifyingVector is the input vector for verifying tortoise
}

type atxContainer struct {
	List []types.ATXID `ssz-max:"4096"`
}

type blocksContainer struct {
	List []types.BlockID `ssz-max:"4096"`
}
