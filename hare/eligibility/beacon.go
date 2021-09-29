package eligibility

import (
	"context"
	"encoding/binary"

	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Beacon provides the value that is under consensus as defined by the hare.
type Beacon struct {
	beaconGetter blocks.BeaconGetter
	log.Log
}

// NewBeacon returns a new beacon
func NewBeacon(beaconGetter blocks.BeaconGetter, logger log.Log) *Beacon {
	return &Beacon{
		beaconGetter: beaconGetter,
		Log:          logger,
	}
}

// Value returns the beacon value for an epoch
func (b *Beacon) Value(ctx context.Context, epochID types.EpochID) (uint32, error) {
	v, err := b.beaconGetter.GetBeacon(epochID)
	if err != nil {
		return 0, err
	}

	value := binary.LittleEndian.Uint32(v)
	b.WithContext(ctx).With().Info("hare eligibility beacon value for epoch",
		epochID,
		log.String("beacon", types.BytesToHash(v).ShortString()),
		log.Uint32("beacon_dec", value))

	return value, nil
}
