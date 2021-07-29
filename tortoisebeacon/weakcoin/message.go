package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Message defines weak coin message format.
type Message struct {
	Epoch        types.EpochID
	Round        types.RoundID
	MinerID      types.NodeID
	VRFSignature []byte
}
