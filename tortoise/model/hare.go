package model

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func newHare(rng *rand.Rand) *hare {
	return &hare{rng: rng}
}

// hare is an instance of the hare consensus.
// At the end of each layer it outputs input vector and coinflip events.
type hare struct {
	rng *rand.Rand
}

// OnEvent produces blocks.
func (h *hare) OnEvent(event Event) []Event {
	switch ev := event.(type) {
	case EventLayerStart:
		id := types.BlockID{}
		h.rng.Read(id[:])
		block := types.NewExistingBlock(
			id,
			types.InnerBlock{
				LayerIndex: ev.LayerID,
			},
		)
		// head and tails are at equal probability.
		return []Event{
			EventCoinflip{LayerID: ev.LayerID, Coinflip: h.rng.Int()%2 == 0},
			EventBlock{Block: block},
		}
	}
	return nil
}
