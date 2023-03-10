package types

import (
	"encoding/hex"

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
	PubKey      []byte
	Eligibility HareEligibility
}

func (hg *HareEligibilityGossip) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("layer", hg.Layer.Value)
	encoder.AddUint32("round", hg.Round)
	encoder.AddString("smesher", BytesToNodeID(hg.PubKey).String())
	encoder.AddUint16("count", hg.Eligibility.Count)
	encoder.AddString("proof", hex.EncodeToString(hg.Eligibility.Proof))
	return nil
}

// HareEligibility includes the required values that, along with the smesher's VRF public key,
// allow non-interactive eligibility validation for hare round participation.
type HareEligibility struct {
	// VRF signature of EligibilityType, beacon, layer, round
	Proof []byte
	// the eligibility count for this layer, round
	Count uint16
}

// MarshalLogObject implements logging interface.
func (e *HareEligibility) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint16("count", e.Count)
	encoder.AddString("proof", hex.EncodeToString(e.Proof))
	return nil
}

// VotingEligibility includes the required values that, along with the smesher's VRF public key,
// allow non-interactive voting eligibility validation. this proof provides eligibility for both voting and
// making proposals.
type VotingEligibility struct {
	// the counter value used to generate this eligibility proof. if the value of J is 3, this is the smesher's
	// eligibility proof of the 3rd ballot/proposal in the epoch.
	J uint32
	// the VRF signature of some epoch specific data and J. one can derive a Ballot's layerID from this signature.
	Sig []byte
}

// MarshalLogObject implements logging interface.
func (v *VotingEligibility) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("j", v.J)
	encoder.AddString("sig", hex.EncodeToString(v.Sig))
	return nil
}
