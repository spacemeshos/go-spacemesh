package mesh

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	ApplyLayer(*types.Block) ([]*types.Transaction, error)
	GetStateRoot() (types.Hash32, error)
	RevertState(types.LayerID) (types.Hash32, error)
	LinkTXsWithProposal(types.LayerID, types.ProposalID, []types.TransactionID) error
	LinkTXsWithBlock(types.LayerID, types.BlockID, []types.TransactionID) error
}

type tortoise interface {
	OnBallot(*types.Ballot)
	OnBlock(*types.Block)
	HandleIncomingLayer(context.Context, types.LayerID) types.LayerID
}
