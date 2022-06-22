// Package turbohare is a component returning block ids for layer as seen by this miner, without running any consensus process
package turbohare

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
)

type meshProvider interface {
	AddBlockWithTXs(context.Context, *types.Block) error
	SetZeroBlockLayer(types.LayerID) error
	ProcessLayerPerHareOutput(context.Context, types.LayerID, types.BlockID) error
}

type blockGenerator interface {
	GenerateBlock(context.Context, types.LayerID, []*types.Proposal) (*types.Block, error)
}

// SuperHare is a method to provide fast hare results without consensus based on received blocks from gossip.
type SuperHare struct {
	db           *sql.Database
	mesh         meshProvider
	blockGen     blockGenerator
	conf         config.Config
	beginLayer   chan types.LayerID
	closeChannel chan struct{}
	logger       log.Log
}

// New creates a new instance of SuperHare.
func New(db *sql.Database, conf config.Config, mesh meshProvider, blockGen blockGenerator, beginLayer chan types.LayerID, logger log.Log) *SuperHare {
	return &SuperHare{db, mesh, blockGen, conf, beginLayer, make(chan struct{}), logger}
}

// Start is a stub to support service API.
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
					layers.SetWeakCoin(h.db, layerID, coinflip)
					logger.With().Info("superhare recorded coinflip", layerID, log.Bool("coinflip", coinflip))

					// pass all blocks in the layer to the mesh
					if layerID.GetEpoch().IsGenesis() {
						logger.With().Info("not sending blocks to mesh for genesis layer")
						return
					}
					props, err := proposals.GetByLayer(h.db, layerID)
					if err != nil {
						logger.With().Warning("error getting proposals for layer, using empty set",
							layerID,
							log.Err(err))
					}
					hareOutput := types.EmptyBlockID
					if len(props) == 0 {
						logger.With().Warning("hare output empty set")
						if err := h.mesh.SetZeroBlockLayer(layerID); err != nil {
							logger.With().Error("failed to set layer as a zero block", log.Err(err))
						}
					} else if block, err := h.blockGen.GenerateBlock(ctx, layerID, props); err != nil {
						logger.With().Error("failed to generate block", log.Err(err))
						return
					} else if err = h.mesh.AddBlockWithTXs(ctx, block); err != nil {
						logger.With().Error("failed to save block", log.Err(err))
						return
					} else {
						hareOutput = block.ID()
					}

					if err := h.mesh.ProcessLayerPerHareOutput(ctx, layerID, hareOutput); err != nil {
						logger.With().Error("mesh failed to process layer", layerID, log.Err(err))
					}
				}()
			}
		}
	}()
	return nil
}

// Close is a stup to support service API.
func (h *SuperHare) Close() {
	close(h.closeChannel)
}

// GetHareMsgHandler returns the gossip handler for hare protocol message.
func (h *SuperHare) GetHareMsgHandler() pubsub.GossipHandler {
	return func(context.Context, p2p.Peer, []byte) pubsub.ValidationResult {
		return pubsub.ValidationAccept
	}
}
