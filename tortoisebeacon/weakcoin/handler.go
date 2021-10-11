package weakcoin

import (
	"context"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// HandleSerializedMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
func (wc *WeakCoin) HandleSerializedMessage(ctx context.Context, data service.GossipMessage, _ service.Fetcher) {
	logger := wc.logger.WithContext(ctx)
	logger.With().Debug("received weak coin message",
		log.String("from", data.Sender().String()),
		log.Binary("data", data.Bytes()),
	)

	var message Message
	if err := types.BytesToInterface(data.Bytes(), &message); err != nil {
		logger.With().Warning("received invalid weak coin message",
			log.Binary("message", data.Bytes()),
			log.Err(err))
		return
	}

	if err := wc.receiveMessage(ctx, message); err != nil {
		logger.With().Debug("received invalid proposal", message.Epoch, message.Round, log.Err(err))
		return
	}
	data.ReportValidation(ctx, GossipProtocol)
}

func (wc *WeakCoin) receiveMessage(ctx context.Context, message Message) error {
	if wc.aboveThreshold(message.Signature) {
		return fmt.Errorf("proposal %x is above threshold", message.Signature)
	}

	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.epoch != message.Epoch || wc.round != message.Round || !wc.epochStarted || !wc.roundStarted {
		if wc.isNextRound(message.Epoch, message.Round) && len(wc.nextRoundBuffer) < cap(wc.nextRoundBuffer) {
			wc.nextRoundBuffer = append(wc.nextRoundBuffer, message)
			return nil
		}
		return fmt.Errorf("message for the wrong round %v/%v", message.Epoch, message.Round)
	}
	return wc.updateProposal(ctx, message)
}

func (wc *WeakCoin) isNextRound(epoch types.EpochID, round types.RoundID) bool {
	if wc.epoch == epoch && wc.round+1 == round && round <= wc.config.MaxRound {
		return true
	}
	if wc.epoch+1 == epoch && wc.round == wc.config.MaxRound {
		return true
	}
	// after completed epoch but haven't started the new one
	if wc.epoch+1 == epoch && !wc.roundStarted && !wc.epochStarted {
		return true
	}
	// after started epoch but didn't start the round
	if wc.epoch == epoch && !wc.roundStarted && wc.epochStarted {
		return true
	}
	return false
}
