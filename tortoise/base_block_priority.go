package tortoise

import (
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// prioritizeBlocks will sort blocks inplace according to internal prioritization.
func prioritizeBlocks(
	blocks []types.BlockID,
	goodBlocks map[types.BlockID]bool, // existence of the block means that the block is good, ignore the value here
	disagreements map[types.BlockID]types.LayerID,
	blockLayer map[types.BlockID]types.LayerID,
) {
	sort.Slice(blocks, func(i, j int) bool {
		ibid := blocks[i]
		jbid := blocks[j]
		// prioritize good blocks
		_, iexist := goodBlocks[ibid]
		_, jexist := goodBlocks[jbid]
		if iexist != jexist {
			return iexist
		}
		// prioritize blocks with less disagreements to a local opinion
		if disagreements[ibid] != disagreements[jbid] {
			return disagreements[ibid].After(disagreements[jbid])
		}
		// priortize blocks from higher layers
		if blockLayer[ibid] != blockLayer[jbid] {
			return blockLayer[ibid].After(blockLayer[jbid])
		}
		// otherwise just sort determistically
		return ibid.Compare(jbid)
	})
}
