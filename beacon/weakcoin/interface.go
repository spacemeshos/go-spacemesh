package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=weakcoin -destination=./mocks.go -source=./interface.go

type vrfSigner interface {
	Sign(d signing.Domain, msg []byte) types.VrfSignature
	NodeID() types.NodeID
	LittleEndian() bool
}

type vrfVerifier interface {
	Verify(domain signing.Domain, nodeID types.NodeID, msg []byte, sig types.VrfSignature) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}

type allowance interface {
	MinerAllowance(types.EpochID, types.NodeID) uint32
}
