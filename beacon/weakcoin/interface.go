package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=weakcoin -destination=./mocks.go -source=./interface.go

type vrfSigner interface {
	Sign(msg []byte) types.VrfSignature
	NodeID() types.NodeID
	LittleEndian() bool
}

type vrfVerifier interface {
	Verify(types.NodeID, []byte, types.VrfSignature) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}

type allowance interface {
	MinerAllowance(types.EpochID, types.NodeID) uint32
}
