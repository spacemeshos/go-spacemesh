package proposals

import (
	"fmt"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/proposals/util"
)

var (
	CalcEligibleLayer   = util.CalcEligibleLayer
	GetNumEligibleSlots = util.GetNumEligibleSlots
	// ComputeWeightPerEligibility computes the ballot weight per eligibility w.r.t the active set recorded in its reference ballot.
	ComputeWeightPerEligibility = util.ComputeWeightPerEligibility
)

//go:generate scalegen -types VrfMessage

// VrfMessage is a verification message. It is the payload for the signature in `VotingEligibility`.
type VrfMessage struct {
	Type    types.EligibilityType // always types.EligibilityVoting
	Beacon  types.Beacon
	Epoch   types.EpochID
	Nonce   types.VRFPostIndex
	Counter uint32
}

// SerializeVRFMessage serializes a message for generating/verifying a VRF signature.
func SerializeVRFMessage(beacon types.Beacon, epoch types.EpochID, nonce types.VRFPostIndex, counter uint32) ([]byte, error) {
	m := VrfMessage{
		Type:    types.EligibilityVoting,
		Beacon:  beacon,
		Epoch:   epoch,
		Nonce:   nonce,
		Counter: counter,
	}
	serialized, err := codec.Encode(&m)
	if err != nil {
		return nil, fmt.Errorf("serialize vrf message: %w", err)
	}
	return serialized, nil
}
