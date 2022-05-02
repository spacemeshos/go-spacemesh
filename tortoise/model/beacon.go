package model

import (
	"math/rand"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

func newBeacon(rng *rand.Rand) *beacon {
	return &beacon{rng: rng}
}

// beacon outputs beacon at the last layer in epoch.
type beacon struct {
	rng *rand.Rand
}

// OnMessage ...
func (b *beacon) OnMessage(m Messenger, event Message) {
	switch ev := event.(type) {
	case MessageLayerStart:
		if ev.LayerID != ev.LayerID.GetEpoch().FirstLayer() {
			return
		}
		// first layer of the epoch
		beacon := types.Beacon{}
		b.rng.Read(beacon[:])
		m.Send(MessageBeacon{
			EpochID: ev.LayerID.GetEpoch(),
			Beacon:  beacon,
		})
	}
}
