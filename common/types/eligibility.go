package types

import (
	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen

type EligibilityType uint16

const (
	EligibilityBeacon EligibilityType = iota + 1
	EligibilityHare
	EligibilityVoting
	EligibilityBeaconWC
)

type HareEligibilityGossip struct {
	Layer       LayerID
	Round       uint32
	NodeID      NodeID
	Eligibility HareEligibility
}

func (hg *HareEligibilityGossip) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("layer", hg.Layer.Uint32())
	encoder.AddUint32("round", hg.Round)
	encoder.AddString("smesher", hg.NodeID.String())
	encoder.AddUint16("count", hg.Eligibility.Count)
	encoder.AddString("proof", hg.Eligibility.Proof.String())
	return nil
}

// HareEligibility includes the required values that, along with the smesher's VRF public key,
// allow non-interactive eligibility validation for hare round participation.
type HareEligibility struct {
	// VRF signature of EligibilityType, beacon, layer, round
	Proof VrfSignature
	// the eligibility count for this layer, round
	Count uint16
}

// MarshalLogObject implements logging interface.
func (e *HareEligibility) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint16("count", e.Count)
	encoder.AddString("proof", e.Proof.String())
	return nil
}

// VotingEligibility includes the required values that, along with the smeshers VRF public key,
// allow non-interactive voting eligibility validation. this proof provides eligibility for both voting and
// making proposals.
type VotingEligibility struct {
	// the counter value used to generate this eligibility proof. if the value of J is 3, this is the smeshers
	// eligibility proof of the 3rd ballot/proposal in the epoch.
	J uint32
	// the VRF signature of some epoch specific data and J. one can derive a Ballot's layerID from this signature.
	Sig VrfSignature
}

// MarshalLogObject implements logging interface.
func (v *VotingEligibility) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("j", v.J)
	encoder.AddString("sig", v.Sig.String())
	return nil
}
