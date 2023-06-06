package eligibility

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=eligibility -destination=./mocks.go -source=./interface.go

type activeSetCache interface {
	Add(key types.EpochID, value *cachedActiveSet) (evicted bool)
	Get(key types.EpochID) (value *cachedActiveSet, ok bool)
}

type vrfVerifier interface {
	Verify(d signing.Domain, nodeID types.NodeID, msg []byte, sig types.VrfSignature) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}
