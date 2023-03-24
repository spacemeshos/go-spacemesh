package eligibility

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=eligibility -destination=./mocks.go -source=./interface.go

type cache interface {
	Add(key, value any) (evicted bool)
	Get(key any) (value any, ok bool)
}

type vrfVerifier interface {
	Verify(types.NodeID, []byte, types.VrfSignature) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}
