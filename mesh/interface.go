package mesh

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	vm "github.com/spacemeshos/go-spacemesh/genvm"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	UpdateCache(context.Context, types.LayerID, types.BlockID, []types.TransactionWithResult, []types.Transaction) error
	RevertCache(types.LayerID) error
	LinkTXsWithProposal(types.LayerID, types.ProposalID, []types.TransactionID) error
	LinkTXsWithBlock(types.LayerID, types.BlockID, []types.TransactionID) error
}

type vmState interface {
	GetStateRoot() (types.Hash32, error)
	Revert(types.LayerID) error
	Apply(vm.ApplyContext, []types.Transaction, []types.AnyReward) ([]types.Transaction, []types.TransactionWithResult, error)
}
