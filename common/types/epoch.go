package types

import (
	"strconv"

	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
)

// EpochID is the running epoch number. It's zero-based, so the genesis epoch has EpochID == 0.
type EpochID uint32

// ToBytes returns a byte-slice representation of the EpochID, using little endian encoding.
func (l EpochID) ToBytes() []byte { return util.Uint32ToBytes(uint32(l)) }

// IsGenesis returns true if this epoch is in genesis. The first two epochs are considered genesis epochs.
func (l EpochID) IsGenesis() bool {
	return l < 2
}

// NeedsGoldenPositioningATX returns true if ATXs in this epoch require positioning ATX to be equal to the Golden ATX.
// All ATXs in epoch 1 must have the Golden ATX as positioning ATX.
func (l EpochID) NeedsGoldenPositioningATX() bool {
	return l == 1
}

// FirstLayer returns the layer ID of the first layer in the epoch.
func (l EpochID) FirstLayer() LayerID {
	return NewLayerID(uint32(l)).Mul(GetLayersPerEpoch())
}

// Field returns a log field. Implements the LoggableField interface.
func (l EpochID) Field() log.Field { return log.Uint32("epoch_id", uint32(l)) }

// String returns string representation of the epoch id numeric value.
func (l EpochID) String() string {
	return strconv.FormatUint(uint64(l), 10)
}

type EpochStatus uint32

// ToBytes returns a byte-slice representation of the EpochStatus, using little endian encoding.
func (l EpochStatus) ToBytes() []byte { return util.Uint32ToBytes(uint32(l)) }

const (
	EpochStatusNotSmeshing EpochStatus = iota + 1
	EpochStatusGeneratingPoSData
	EpochStatusWaitingForPoETChallenge
	EpochStatusGeneratingPoS
	EpochStatusWaitingNextEpoch
	EpochStatusSmeshing
)
