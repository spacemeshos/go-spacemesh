// Package turbohare is a component returning block ids for layer as seen by this miner, without running any consensus process
package turbohare

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
)

type meshProvider interface {
	LayerBlockIds(types.LayerID) ([]types.BlockID, error)
	RecordCoinflip(context.Context, types.LayerID, bool)
	HandleValidatedLayer(context.Context, types.LayerID, []types.BlockID)
}

// SuperHare is a method to provide fast hare results without consensus based on received blocks from gossip
type SuperHare struct {
	mesh         meshProvider
	conf         config.Config
	beginLayer   chan types.LayerID
	closeChannel chan struct{}
	logger       log.Log
}

// New creates a new instance of SuperHare
func New(_ context.Context, conf config.Config, mesh meshProvider, beginLayer chan types.LayerID, logger log.Log) *SuperHare {
	return &SuperHare{mesh, conf, beginLayer, make(chan struct{}), logger}
}

// Start is a stub to support service API
func (h *SuperHare) Start(ctx context.Context) error {
	logger := h.logger.WithContext(ctx)

	// simulate publishing weak coin values to the mesh
	go func() {
		logger.Info("superhare mocker running")
		for {
			select {
			case <-h.closeChannel:
				logger.Info("superhare shutting down")
				return
			case layerID := <-h.beginLayer:
				go func() {
					logger.With().Info("superhare got layer tick, simulating consensus process run", layerID)

					// don't block here if Close was called
					select {
					case <-h.closeChannel:
						logger.Info("superhare closing lingering goroutine")
						return
					case <-time.After(time.Second * time.Duration(h.conf.WakeupDelta+5*h.conf.RoundDuration)):
					}

					// use lowest-order bit
					coinflip := layerID.Bytes()[0]&byte(1) == byte(1)
					h.mesh.RecordCoinflip(ctx, layerID, coinflip)
					logger.With().Info("superhare recorded coinflip", layerID, log.Bool("coinflip", coinflip))

					// pass all blocks in the layer to the mesh
					if layerID.GetEpoch().IsGenesis() {
						logger.With().Info("not sending blocks to mesh for genesis layer")
						return
					} else if layerBlocks, err := h.mesh.LayerBlockIds(layerID); err != nil {
						logger.With().Error("error reading block ids for layer, not sending to mesh",
							layerID,
							log.Err(err))
					} else {
						h.mesh.HandleValidatedLayer(ctx, layerID, layerBlocks)
					}
				}()
			}
		}
	}()
	return nil
}

// Close is a stup to support service API
func (h *SuperHare) Close() {
	close(h.closeChannel)
}

// GetResult is the implementation for receiving consensus process result
func (h *SuperHare) GetResult(id types.LayerID) ([]types.BlockID, error) {
	blks, err := h.mesh.LayerBlockIds(id)
	if err != nil {
		h.logger.With().Error("superhare failed to read block ids for layer", id, log.Err(err))
		return nil, fmt.Errorf("read layer block IDs: %w", err)
	}

	sort.Slice(blks, func(i, j int) bool { return bytes.Compare(blks[i].Bytes(), blks[j].Bytes()) == -1 })
	return blks, nil
}
