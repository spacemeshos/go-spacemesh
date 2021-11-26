package miner

import "github.com/spacemeshos/go-spacemesh/common/types"

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type blockOracle interface {
	BlockEligible(types.LayerID) (types.ATXID, []types.BlockEligibilityProof, []types.ATXID, error)
}

type projector interface {
	GetProjection(types.Address) (nonce, balance uint64, err error)
}
