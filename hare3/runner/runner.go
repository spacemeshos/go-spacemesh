package runner

import (
	"context"
	"fmt"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare3"
)

type ProtocolRunner struct {
	clock    RoundClock
	protocol hare3.Protocol
	maxRound hare3.AbsRound
	gossiper NetworkGossiper
}

func NewProtocolRunner(
	clock RoundClock,
	protocol *hare3.Protocol,
	iterationLimit int8,
	gossiper NetworkGossiper,
) *ProtocolRunner {
	return &ProtocolRunner{
		clock:    clock,
		protocol: *protocol,
		maxRound: hare3.NewAbsRound(iterationLimit, 0),
		gossiper: gossiper,
	}
}

// Run waits for successive rounds from the clock and drives the protocol round by round.
func (r *ProtocolRunner) Run(ctx context.Context) ([]types.Hash20, error) {
	// ok want to actually use a lock here
	for {
		if r.protocol.Round() == r.maxRound {
			return nil, fmt.Errorf("hare protocol runner exceeded iteration limit of %d", r.maxRound.Iteration())
		}
		select {
		// We await the beginning of the round, which is achieved by calling AwaitEndOfRound with (round - 1).
		case <-r.clock.AwaitEndOfRound(uint32(r.protocol.Round() - 1)):
			// This will need to be set per round to determine if this parcicipant is active in this round.
			var active bool
			toSend, output := r.protocol.NextRound(active)
			if toSend != nil {
				msg, err := buildEncodedOutputMessgae(toSend)
				if err != nil {
					// This should never happen
					panic(err)
				}
				r.gossiper.Gossip(msg)
			}
			if output != nil {
				return output, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type NetworkGossiper interface {
	Gossip(msg []byte) error
}

func buildEncodedOutputMessgae(m *hare3.OutputMessage) ([]byte, error) {
	return nil, nil
}

// RoundClock is a timer interface.
type RoundClock interface {
	AwaitWakeup() <-chan struct{}
	// RoundEnd returns the time at which round ends, passing round-1 will
	// return the time at which round starts.
	RoundEnd(round uint32) time.Time
	AwaitEndOfRound(round uint32) <-chan struct{}
}

type LayerClock interface {
	LayerToTime(types.LayerID) time.Time
	AwaitLayer(types.LayerID) chan struct{}
	CurrentLayer() types.LayerID
}

type HareRunner struct {
	clock    RoundClock
	protocol hare3.Protocol
	maxRound hare3.AbsRound
	gossiper NetworkGossiper
}
