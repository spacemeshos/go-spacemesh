package mesh

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type conservativeState interface {
	ApplyLayer(types.LayerID, types.BlockID, []types.TransactionID, map[types.Address]uint64) ([]*types.Transaction, error)
	GetStateRoot() types.Hash32
	Rewind(types.LayerID) (types.Hash32, error)
	StoreTransactionsFromMemPool(types.LayerID, types.BlockID, []types.TransactionID) error
	ReinsertTxsToMemPool([]types.TransactionID) error
}

type tortoise interface {
	OnBallot(*types.Ballot)
	OnBlock(*types.Block)
	HandleIncomingLayer(context.Context, types.LayerID) types.LayerID
}
