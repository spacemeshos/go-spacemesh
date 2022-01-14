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

// OnEvent ...
func (b *beacon) OnEvent(event Event) []Event {
	switch ev := event.(type) {
	case EventLayerStart:
		if ev.LayerID.GetEpoch() == ev.LayerID.Sub(1).GetEpoch() {
			return nil
		}
		// first layer of the epoch
		beacon := types.Beacon{}
		b.rng.Read(beacon[:])
		return []Event{
			EventBeacon{
				EpochID: ev.LayerID.GetEpoch(),
				Beacon:  beacon,
			},
		}
	}
	return nil
}
