package mesh

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	ApplyLayer(context.Context, *types.Block) error
	GetStateRoot() (types.Hash32, error)
	RevertState(types.LayerID) error
	LinkTXsWithProposal(types.LayerID, types.ProposalID, []types.TransactionID) error
	LinkTXsWithBlock(types.LayerID, types.BlockID, []types.TransactionID) error
}
