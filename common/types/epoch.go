package types

import (
	"strconv"

	"github.com/spacemeshos/go-scale"
	"github.com/spacemeshos/go-spacemesh/log"
)

// EpochID is the running epoch number. It's zero-based, so the genesis epoch has EpochID == 0.
type EpochID uint32

// EncodeScale implements scale codec interface.
func (l EpochID) EncodeScale(e *scale.Encoder) (int, error) {
	n, err := scale.EncodeCompact32(e, uint32(l))
	if err != nil {
		return 0, err
	}
	return n, nil
}

// DecodeScale implements scale codec interface.
func (l *EpochID) DecodeScale(d *scale.Decoder) (int, error) {
	value, n, err := scale.DecodeCompact32(d)
	if err != nil {
		return 0, err
	}
	*l = EpochID(value)
	return n, nil
}

// IsGenesis returns true if this epoch is in genesis. The first two epochs are considered genesis epochs.
func (l EpochID) IsGenesis() bool {
	return l < 2
}

// FirstLayer returns the layer ID of the first layer in the epoch.
func (l EpochID) FirstLayer() LayerID {
	return NewLayerID(uint32(l)).Mul(GetLayersPerEpoch())
}

// Add Epochs to the EpochID. Panics on wraparound.
func (l EpochID) Add(epochs uint32) EpochID {
	nl := uint32(l) + epochs
	if nl < uint32(l) {
		panic("epoch_id wraparound")
	}
	l = EpochID(nl)
	return l
}

// Field returns a log field. Implements the LoggableField interface.
func (l EpochID) Field() log.Field { return log.Uint32("epoch_id", uint32(l)) }

// String returns string representation of the epoch id numeric value.
func (l EpochID) String() string {
	return strconv.FormatUint(uint64(l), 10)
}
