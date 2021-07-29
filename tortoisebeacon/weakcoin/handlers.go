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
	wc.logger.With().Debug("weak coin proposal message",
		log.String("from", data.Sender().String()))

	var message Message
	if err := types.BytesToInterface(data.Bytes(), &message); err != nil {
		wc.logger.With().Error("received invalid weak coin message",
			log.Binary("message", data.Bytes()),
			log.Err(err))
		return
	}

	if err := wc.handleWeakCoinMessage(message); err != nil {
		wc.logger.With().Warning("received bad proposal",
			log.Err(err), log.Uint32("epoch", uint32(message.Epoch)),
			log.Uint64("round_id", uint64(message.Round)))
		return
	}
	data.ReportValidation(ctx, GossipProtocol)
}

func (wc *WeakCoin) handleWeakCoinMessage(message Message) error {
	if wc.exceedsThreshold(message.Signature) {
		return fmt.Errorf("proposal %x exceeds threshold", message.Signature)
	}

	msg := wc.encodeProposal(message.Epoch, message.Round, message.Unit)
	miner, err := wc.verifier.Extract(msg, message.Signature)
	if err != nil {
		return fmt.Errorf("can't recover miner id from signature: %w", err)
	}

	wc.mu.Lock()
	defer wc.mu.Unlock()
	if wc.epoch != message.Epoch || wc.round != message.Round {
		return fmt.Errorf("message for the wrong round %v/%v", message.Epoch, message.Round)
	}
	if wc.allowances[string(miner.Bytes())] < uint64(message.Unit) {
		return fmt.Errorf("miner %x is not allowed to submit proposal for unit %d", miner, message.Unit)
	}

	wc.logger.Debug("saving new proposal",
		log.Uint64("epoch_id", uint64(message.Epoch)),
		log.Uint64("round_id", uint64(message.Round)),
		log.String("proposal", types.BytesToHash(message.Signature).ShortString()))
	wc.updateVrf(message.Signature)
	return nil
}
