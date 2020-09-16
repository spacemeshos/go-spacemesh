// Package turbohare is a component returning block ids for layer as seen by this miner, without running any consensus process
package turbohare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"sort"
)

type blockProvider interface {
	LayerBlockIds(layerID types.LayerID) ([]types.BlockID, error)
	SaveLayerInputVector(lyrid types.LayerID, vector []types.BlockID) error
}

// SuperHare is a method to provide fast hare results without consensus based on received blocks from gossip
type SuperHare struct {
	blocks    blockProvider
	closeChan chan struct{}
}

// New creates a new instance of SuperHare
func New(blocksDb blockProvider) *SuperHare {
	return &SuperHare{blocksDb, make(chan struct{}, 1)}
}

// Start is a stub to support service API
func (h *SuperHare) Start() error {
	return nil
}

// Close is a stup to support service API
func (h *SuperHare) Close() {
	close(h.closeChan)
}

// GetResult is the implementation for receiving consensus process result
func (h *SuperHare) GetResult(id types.LayerID) ([]types.BlockID, error) {
	blks, err := h.blocks.LayerBlockIds(id)
	if err != nil {
		log.Error("WTF SUPERHARE?? %v err: %v", id, err)
		return nil, err
	}
	sort.Slice(blks, func(i, j int) bool { return bytes.Compare(blks[i].Bytes(), blks[j].Bytes()) == -1 })
	return blks, nil
}
