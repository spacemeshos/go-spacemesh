package layerfetcher

import "github.com/spacemeshos/go-spacemesh/common/types"

// layerBlocks is the response for layer hash
type layerBlocks struct {
	Blocks []types.BlockID
	LatestBlocks []types.BlockID // LatestBlocks are the blocks received in the last 30 seconds from gossip
	VerifyingVector []types.BlockID // VerifyingVector is the input vector for verifying tortoise
}
