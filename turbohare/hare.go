package turbohare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"sort"
)

type BlockProvider interface {
	GetUnverifiedLayerBlocks(layerId types.LayerID) ([]types.BlockID, error)
}

type SuperHare struct {
	blocks BlockProvider
}

func New(blocks BlockProvider) *SuperHare {
	return &SuperHare{blocks}
}

func (h *SuperHare) Start() error {
	return nil
}

func (h *SuperHare) Close() {

}

func (h *SuperHare) GetResult(lower types.LayerID, upper types.LayerID) ([]types.BlockID, error) {
	var output []types.BlockID
	for i := lower; i <= upper; i++ {
		blks, err := h.blocks.GetUnverifiedLayerBlocks(types.LayerID(i))
		if err != nil {
			log.Error("WTF SUPERHARE?? %v err: %v", i, err)
			return nil, err
		}
		sort.Slice(blks, func(i, j int) bool { return bytes.Compare(blks[i].ToBytes(), blks[j].ToBytes()) == -1 })
		output = append(output, blks...)
	}
	return output, nil
}
