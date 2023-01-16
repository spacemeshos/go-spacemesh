package weakcoin

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	signing "github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=weakcoin -destination=./mocks.go -source=./interface.go

type vrfSigner interface {
	Sign(msg []byte, epoch types.EpochID) ([]byte, error)
	PublicKey() *signing.PublicKey
	LittleEndian() bool
}

type vrfVerifier interface {
	Verify(nodeID types.NodeID, epoch types.EpochID, msg, sig []byte) bool
}
