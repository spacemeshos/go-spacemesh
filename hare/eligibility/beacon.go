package eligibility

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
)

// Beacon provides the value that is under consensus as defined by the hare.
type Beacon struct {
	beaconGetter system.BeaconGetter
	log.Log
}

// NewBeacon returns a new beacon.
func NewBeacon(beaconGetter system.BeaconGetter, logger log.Log) *Beacon {
	return &Beacon{
		beaconGetter: beaconGetter,
		Log:          logger,
	}
}

// Value returns the beacon value for an epoch.
func (b *Beacon) Value(ctx context.Context, epochID types.EpochID) (uint32, error) {
	v, err := b.beaconGetter.GetBeacon(epochID)
	if err != nil {
		return 0, fmt.Errorf("get beacon: %w", err)
	}

	value := binary.LittleEndian.Uint32(v)
	b.WithContext(ctx).With().Debug("hare eligibility beacon value for epoch",
		epochID,
		log.String("beacon", types.BytesToHash(v).ShortString()),
		log.Uint32("beacon_dec", value))

	return value, nil
}
