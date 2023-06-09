package weakcoin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/metrics"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// HandleProposal defines method to handle Beacon Weak Coin Messages from gossip.
func (wc *WeakCoin) HandleProposal(ctx context.Context, peer p2p.Peer, msg []byte) error {
	receivedTime := time.Now()
	logger := wc.logger.WithContext(ctx)

	var message Message
	if err := codec.Decode(msg, &message); err != nil {
		logger.With().Warning("malformed weak coin message", log.Err(err))
		return pubsub.ErrValidationReject
	}

	latency := receivedTime.Sub(wc.msgTime.WeakCoinProposalSendTime(message.Epoch, message.Round))
	metrics.ReportMessageLatency(pubsub.BeaconProtocol, pubsub.BeaconWeakCoinProtocol, latency)

	if err := wc.receiveMessage(ctx, message); err != nil {
		if !errors.Is(err, errNotSmallest) {
			logger.With().Debug("invalid proposal",
				message.Epoch,
				message.Round,
				log.Stringer("peer", peer),
				log.Err(err),
			)
		}
		return err
	}
	return nil
}

func (wc *WeakCoin) receiveMessage(ctx context.Context, message Message) error {
	if wc.aboveThreshold(message.VRFSignature) {
		return fmt.Errorf("proposal %s is above threshold", message.VRFSignature)
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
