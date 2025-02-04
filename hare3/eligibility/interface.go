package eligibility

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -typed -package=eligibility -destination=./mocks.go -source=./interface.go

type activeSetCache interface {
	Add(key types.EpochID, value *cachedActiveSet) (evicted bool)
	Get(key types.EpochID) (value *cachedActiveSet, ok bool)
}

type vrfVerifier interface {
	Verify(nodeID types.NodeID, msg []byte, sig types.VrfSignature) bool
}

// Rolacle is the roles oracle provider.
type Rolacle interface {
	Validate(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature, uint16) (bool, error)
	CalcEligibility(context.Context, types.LayerID, uint32, int, types.NodeID, types.VrfSignature) (uint16, error)
}

type BeaconProvider interface {
	Beacon(context.Context, types.EpochID) (types.Beacon, error)
}
