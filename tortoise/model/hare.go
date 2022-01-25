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

// OnMessage produces blocks.
func (h *hare) OnMessage(m Messenger, event Message) {
	switch ev := event.(type) {
	case MessageLayerStart:
		id := types.BlockID{}
		h.rng.Read(id[:])
		block := types.NewExistingBlock(
			id,
			types.InnerBlock{
				LayerIndex: ev.LayerID,
			},
		)
		// head and tails are at equal probability.
		m.Send(
			MessageCoinflip{LayerID: ev.LayerID, Coinflip: h.rng.Int()%2 == 0},
			MessageBlock{Block: block},
		)
	}
}
