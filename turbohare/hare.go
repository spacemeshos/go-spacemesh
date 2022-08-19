// Package turbohare is a component returning block ids for layer as seen by this miner, without running any consensus process
package turbohare

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
)

// SuperHare is a method to provide fast hare results without consensus based on received blocks from gossip.
type SuperHare struct {
	db           *sql.Database
	conf         config.Config
	ch           chan hare.LayerOutput
	beginLayer   chan types.LayerID
	closeChannel chan struct{}
	logger       log.Log
}

// New creates a new instance of SuperHare.
func New(db *sql.Database, conf config.Config, ch chan hare.LayerOutput, beginLayer chan types.LayerID, logger log.Log) *SuperHare {
	return &SuperHare{db, conf, ch, beginLayer, make(chan struct{}), logger}
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
					if err := layers.SetWeakCoin(h.db, layerID, coinflip); err != nil {
						logger.With().Error("error getting weakcoin", layerID, log.Err(err))
						return
					}
					logger.With().Info("superhare recorded coinflip", layerID, log.Bool("coinflip", coinflip))

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
					h.ch <- hare.LayerOutput{
						Ctx:       ctx,
						Layer:     layerID,
						Proposals: types.ToProposalIDs(props),
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
