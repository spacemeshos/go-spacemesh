package proposals

import (
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/proposals/util"
)

var (
	CalcEligibleLayer   = util.CalcEligibleLayer
	GetNumEligibleSlots = util.GetNumEligibleSlots
)

func MustGetNumEligibleSlots(
	weight, minWeight, totalWeight uint64,
	committeeSize, layersPerEpoch uint32,
) uint32 {
	slots, err := GetNumEligibleSlots(weight, minWeight, totalWeight, committeeSize, layersPerEpoch)
	if err != nil {
		panic(err)
	}
	return slots
}

//go:generate scalegen -types VrfMessage

// VrfMessage is a verification message. It is the payload for the signature in `VotingEligibility`.
type VrfMessage struct {
	Type    types.EligibilityType // always types.EligibilityVoting
	Beacon  types.Beacon
	Epoch   types.EpochID
	Nonce   types.VRFPostIndex
	Counter uint32
}

// MustSerializeVRFMessage serializes a message for generating/verifying a VRF signature.
func MustSerializeVRFMessage(
	beacon types.Beacon,
	epoch types.EpochID,
	nonce types.VRFPostIndex,
	counter uint32,
) []byte {
	m := VrfMessage{
		Type:    types.EligibilityVoting,
		Beacon:  beacon,
		Epoch:   epoch,
		Nonce:   nonce,
		Counter: counter,
	}
	return codec.MustEncode(&m)
}
