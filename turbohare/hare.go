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

func (h *SuperHare) GetResult(id types.LayerID) ([]types.BlockID, error) {
	blks, err := h.blocks.GetUnverifiedLayerBlocks(id)
	if err != nil {
		log.Error("WTF SUPERHARE?? %v err: %v", id, err)
		return nil, err
	}
	sort.Slice(blks, func(i, j int) bool { return bytes.Compare(blks[i].ToBytes(), blks[j].ToBytes()) == -1 })
	return blks, nil
}
