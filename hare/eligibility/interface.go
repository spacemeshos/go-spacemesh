package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=eligibility -destination=./mocks.go -source=./interface.go

type cache interface {
	Add(key, value any) (evicted bool)
	Get(key any) (value any, ok bool)
}

type vrfVerifier interface {
	Verify(d signing.Domain, nodeID types.NodeID, msg []byte, sig types.VrfSignature) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}
