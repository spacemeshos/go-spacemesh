package weakcoin

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

var (
	// ErrMalformedMessage is returned when weak coin message is malformed.
	ErrMalformedMessage = errors.New("malformed weak coin message")
	// ErrMalformedProposal is returned if proposal message is malformed.
	ErrMalformedProposal = errors.New("malformed proposal message")
)

// HandleSerializedMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
func (wc *weakCoin) HandleSerializedMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	wc.Log.With().Info("New weak coin proposal message",
		log.String("from", data.Sender().String()))

	var message Message

	if err := types.BytesToInterface(data.Bytes(), &message); err != nil {
		wc.Log.With().Error("Received invalid weak coin message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := wc.handleWeakCoinMessage(message); err != nil {
		if errors.Is(err, ErrMalformedProposal) {
			wc.Log.With().Warning("Received malformed proposal",
				log.String("sender", message.MinerID.Key))
		} else {
			wc.Log.With().Error("Failed to handle weak coin message",
				log.String("sender", message.MinerID.Key),
				log.Err(err))
		}

		return
	}

	data.ReportValidation(ctx, GossipProtocol)
}

func (wc *weakCoin) handleWeakCoinMessage(message Message) error {
	if err := wc.verifyProposal(message); err != nil {
		return fmt.Errorf("verify proposal: %w", err)
	}

	pair := epochRoundPair{
		EpochID: message.Epoch,
		Round:   message.Round,
	}

	wc.activeRoundsMu.RLock()
	defer wc.activeRoundsMu.RUnlock()

	if _, ok := wc.activeRounds[pair]; !ok {
		wc.Log.Warning("Received malformed weak coin message",
			log.Uint64("epoch_id", uint64(message.Epoch)),
			log.Uint64("round", uint64(message.Round)))

		return ErrMalformedMessage
	}

	wc.proposalsMu.Lock()
	defer wc.proposalsMu.Unlock()

	wc.Log.Warning("Saving new proposal",
		log.Uint64("epoch_id", uint64(message.Epoch)),
		log.Uint64("round", uint64(message.Round)),
		log.String("proposal", types.BytesToHash(message.VRFSignature).ShortString()))

	wc.proposals[pair] = append(wc.proposals[pair], message.VRFSignature)

	return nil
}

func (wc *weakCoin) verifyProposal(message Message) error {
	expectedProposal, err := wc.generateProposal(message.Epoch, message.Round)
	if err != nil {
		return fmt.Errorf("calculate proposal: %w", err)
	}

	if !wc.verifier(message.MinerID.VRFPublicKey, expectedProposal, message.VRFSignature) {
		return ErrMalformedProposal
	}

	return nil
}
