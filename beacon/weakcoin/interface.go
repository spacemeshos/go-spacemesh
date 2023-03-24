package weakcoin

import (
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=weakcoin -destination=./mocks.go -source=./interface.go

type vrfSigner interface {
	Sign(msg []byte) []byte
	PublicKey() *signing.PublicKey
	LittleEndian() bool
}

type vrfVerifier interface {
	Verify(nodeID types.NodeID, msg, sig []byte) bool
}

type nonceFetcher interface {
	VRFNonce(types.NodeID, types.EpochID) (types.VRFPostIndex, error)
}

type allowance interface {
	MinerAllowance(types.EpochID, []byte) uint32
}

// weakCoinClock interface exists so that we can pass an object from the beacon
// package to the weakCoinPackage (as does allowance), this is indicatave of a
// circular dependency, probably the weak coin should be merged with the beacon
// package.
// Issue: https://github.com/spacemeshos/go-spacemesh/issues/4199
type weakCoinClock interface {
	WeakCoinProposalSendTime(epoch types.EpochID, round types.RoundID) time.Time
}
