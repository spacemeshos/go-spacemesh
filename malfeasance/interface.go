package malfeasance

import (
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

//go:generate mockgen -package=malfeasance -destination=./mocks.go -source=./interface.go

type SigVerifier interface {
	Verify(signing.Domain, types.NodeID, []byte, types.EdSignature) bool
}

type tortoise interface {
	OnMalfeasance(types.NodeID)
}
