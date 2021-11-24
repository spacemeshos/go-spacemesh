package proposals

import (
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
)

// ErrZeroTotalWeight is returned when zero total epoch weight is used when calculating eligible slots.
var ErrZeroTotalWeight = errors.New("zero total weight not allowed")

// CalcEligibleLayer calculates the eligible layer from the VRF signature.
func CalcEligibleLayer(epochNumber types.EpochID, layersPerEpoch uint32, vrfSig []byte) types.LayerID {
	vrfInteger := util.BytesToUint64(vrfSig)
	eligibleLayerOffset := vrfInteger % uint64(layersPerEpoch)
	return epochNumber.FirstLayer().Add(uint32(eligibleLayerOffset))
}

// GetNumEligibleSlots calculates the number of eligible slots for a smesher in an epoch.
func GetNumEligibleSlots(weight, totalWeight uint64, committeeSize uint32, layersPerEpoch uint32) (uint32, error) {
	if totalWeight == 0 {
		return 0, ErrZeroTotalWeight
	}
	numberOfEligibleBlocks := weight * uint64(committeeSize) * uint64(layersPerEpoch) / totalWeight // TODO: ensure no overflow
	if numberOfEligibleBlocks == 0 {
		numberOfEligibleBlocks = 1
	}
	return uint32(numberOfEligibleBlocks), nil
}

type vrfMessage struct {
	Beacon  []byte
	Epoch   types.EpochID
	Counter uint32
}

// SerializeVRFMessage serializes a message for generating/verifying a VRF signature.
func SerializeVRFMessage(beacon []byte, epoch types.EpochID, counter uint32) ([]byte, error) {
	m := vrfMessage{
		Beacon:  beacon,
		Epoch:   epoch,
		Counter: counter,
	}
	serialized, err := types.InterfaceToBytes(&m)
	if err != nil {
		return nil, fmt.Errorf("serialize vrf message: %w", err)
	}
	return serialized, nil
}
