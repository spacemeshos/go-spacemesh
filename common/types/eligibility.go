package types

import (
	"encoding/hex"

	"github.com/spacemeshos/go-spacemesh/log"
)

//go:generate scalegen

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
