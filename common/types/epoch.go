package types

import (
	"strconv"

	"github.com/spacemeshos/go-scale"

	"github.com/spacemeshos/go-spacemesh/log"
)

// EpochID is the running epoch number. It's zero-based, so the genesis epoch has EpochID == 0.
type EpochID uint32

func (e EpochID) Uint32() uint32 {
	return uint32(e)
}

// EncodeScale implements scale codec interface.
func (e EpochID) EncodeScale(enc *scale.Encoder) (int, error) {
	return scale.EncodeCompact32(enc, e.Uint32())
}

// DecodeScale implements scale codec interface.
func (e *EpochID) DecodeScale(dec *scale.Decoder) (int, error) {
	value, n, err := scale.DecodeCompact32(dec)
	if err != nil {
		return n, err
	}
	*e = EpochID(value)
	return n, nil
}

// FirstLayer returns the layer ID of the first layer in the epoch.
func (e EpochID) FirstLayer() LayerID {
	return LayerID(e).Mul(GetLayersPerEpoch())
}

// Add Epochs to the EpochID. Panics on wraparound.
func (e EpochID) Add(epochs uint32) EpochID {
	nl := uint32(e) + epochs
	if nl < uint32(e) {
		panic("epoch_id wraparound")
	}
	e = EpochID(nl)
	return e
}

// Field returns a log field. Implements the LoggableField interface.
func (e EpochID) Field() log.Field { return log.Uint32("epoch_id", uint32(e)) }

// String returns string representation of the epoch id numeric value.
func (e EpochID) String() string {
	return strconv.FormatUint(uint64(e), 10)
}
