package weakcoin

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// HandleSerializedMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
func (wc *WeakCoin) HandleSerializedMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	wc.logger.With().Debug("received weak coin message",
		log.String("from", data.Sender().String()),
		log.Binary("data", data.Bytes()),
	)

	var message Message
	if err := types.BytesToInterface(data.Bytes(), &message); err != nil {
		wc.logger.With().Debug("received invalid weak coin message",
			log.Binary("message", data.Bytes()),
			log.Err(err))
		return
	}

	if err := wc.receiveMessage(message); err != nil {
		wc.logger.With().Debug("received invalid proposal",
			log.Err(err),
			log.Uint32("epoch_id", uint32(message.Epoch)),
			log.Uint64("round_id", uint64(message.Round)))
		return
	}
	data.ReportValidation(ctx, GossipProtocol)
}

func (wc *WeakCoin) receiveMessage(message Message) error {
	if wc.exceedsThreshold(message.Signature) {
		return fmt.Errorf("proposal %x exceeds threshold", message.Signature)
	}

	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.epoch != message.Epoch || wc.round != message.Round {
		if wc.isNextRound(message.Epoch, message.Round) && len(wc.nextRoundBuffer) < cap(wc.nextRoundBuffer) {
			wc.nextRoundBuffer = append(wc.nextRoundBuffer, message)
			return nil
		}
		return fmt.Errorf("message for the wrong round %v/%v", message.Epoch, message.Round)
	}
	return wc.updateProposal(message)
}

func (wc *WeakCoin) isNextRound(epoch types.EpochID, round types.RoundID) bool {
	if wc.epoch == epoch && wc.round+1 == round && round <= wc.config.MaxRound {
		return true
	}
	// after completed epoch but haven't started the new one
	if wc.epoch+1 == epoch && wc.round == 0 && wc.allowances == nil {
		return true
	}
	// after started epoch but didn't start the round
	if wc.epoch == epoch && wc.round == 0 {
		return true
	}
	return false
}
