package blockcerts

import "github.com/spacemeshos/go-spacemesh/common/types"

type blockSignature struct {
	// Eligibility Params
	blockID          types.BlockID
	layerID          types.LayerID
	senderNodeID     types.NodeID
	eligibilityProof []byte
	eligibilityCount uint16
	// Content
	blockSignature []byte
}

func blockSignEligibility() {

}
