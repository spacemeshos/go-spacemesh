package blocks

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type atxProvider interface {
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
}

type meshProvider interface {
	HasBlock(types.BlockID) bool
	AddBlockWithTXs(context.Context, *types.Block) error
}

type txProvider interface {
	GetMeshTransactions([]types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{})
}
