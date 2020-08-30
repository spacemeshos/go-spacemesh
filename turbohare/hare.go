// Package turbohare is a component returning block ids for layer as seen by this miner, without running any consensus process
package turbohare

import (
	"bytes"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"sort"
	"time"
)

type blockProvider interface {
	LayerBlockIds(layerID types.LayerID) ([]types.BlockID, error)
	SaveLayerInputVector(lyrid types.LayerID, vector []types.BlockID) error
}

// SuperHare is a method to provide fast hare results without consensus based on received blocks from gossip
type SuperHare struct {
	ticker    timesync.LayerTimer
	blocks    blockProvider
	closeChan chan struct{}
}

// New creates a new instance of SuperHare
func New(blocks blockProvider, ticker timesync.LayerTimer) *SuperHare {
	return &SuperHare{ticker, blocks, make(chan struct{}, 1)}
}

// Start is a stub to support service API
func (h *SuperHare) Start() error {
	go h.dbSaveRoutine()
	return nil
}

// Close is a stup to support service API
func (h *SuperHare) Close() {
	close(h.closeChan)
}

func (h *SuperHare) dbSaveRoutine() {
	for {
		select {
		case l := <-h.ticker:
			if l < types.GetEffectiveGenesis() {
				continue
			}

			time.Sleep(time.Second * 3)
			// this cover ups the fact that hare doesn't run in tests
			//  but tortoise relays on hare finishing
			blks, err := h.blocks.LayerBlockIds(l)
			if err != nil {
				log.Error("WTF SUPERHARE?? %v err: %v", l, err)
				continue
			}
			if err := h.blocks.SaveLayerInputVector(l, blks); err != nil {
				log.Error("WTF SUPERHARE?? %v err: %v", l, err)
				continue
			}
		case <-h.closeChan:
			return
		}
	}
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
