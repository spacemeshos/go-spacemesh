package weakcoin

import (
	"context"
	"errors"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
)

// HandleSerializedMessage defines method to handle Tortoise Beacon Weak Coin Messages from gossip.
// TODO(nkryuchkov): use context
func (wc *weakCoin) HandleSerializedMessage(ctx context.Context, data service.GossipMessage, sync service.Fetcher) {
	var m Message

	// TODO(nkryuchkov): check VRF. if invalid, ignore the message

	if err := types.BytesToInterface(data.Bytes(), &m); err != nil {
		wc.Log.With().Error("Received invalid weak coin message",
			log.String("message", string(data.Bytes())),
			log.Err(err))

		return
	}

	if err := wc.handleWeakCoinMessage(m); err != nil {
		wc.Log.With().Error("Failed to handle weak coin message",
			log.String("message", m.String()),
			log.Err(err))

		return
	}

	data.ReportValidation(ctx, GossipProtocol)
}

// ErrMalformedMessage is returned when weak coin message is malformed.
var ErrMalformedMessage = errors.New("malformed weak coin message")

func (wc *weakCoin) handleWeakCoinMessage(m Message) error {
	pair := epochRoundPair{
		EpochID: m.Epoch,
		Round:   m.Round,
	}

	wc.proposalsMu.Lock()
	defer wc.proposalsMu.Unlock()

	wc.activeRoundsMu.RLock()
	defer wc.activeRoundsMu.RUnlock()

	if _, ok := wc.activeRounds[pair]; !ok {
		wc.Log.Warning("Received malformed weak coin message",
			log.Uint64("epoch_id", uint64(m.Epoch)),
			log.Uint64("round", uint64(m.Round)))

		return ErrMalformedMessage
	}

	wc.proposals[pair] = append(wc.proposals[pair], m.Proposal)

	return nil
}
