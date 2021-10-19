package system

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// Fetcher is a general interface that defines a component capable of fetching data from remote peers.
type Fetcher interface {
	FetchBlock(context.Context, types.BlockID) error
	FetchAtx(context.Context, types.ATXID) error
	GetPoetProof(context.Context, types.Hash32) error
	GetTxs(context.Context, []types.TransactionID) error
	GetBlocks(context.Context, []types.BlockID) error
	GetAtxs(context.Context, []types.ATXID) error
}
