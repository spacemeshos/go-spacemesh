package sync2

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/sync2/rangesync"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -package=sync2_test -destination=./mocks_test.go -source=./interface.go -exclude_interfaces itemID

type Fetcher interface {
	GetAtxs(context.Context, []types.ATXID, ...system.GetAtxOpt) error
	GetMalfeasanceProofsWithCallback(context.Context, []types.NodeID, func(types.NodeID, error)) error
	RegisterPeerHashes(peer p2p.Peer, hash []types.Hash32)
}

type HashSync interface {
	Load() error
	Start()
	Stop()
	StartAndSync(ctx context.Context) error
}

type HashSyncSource interface {
	CreateATXSync(name string, cfg Config, epoch types.EpochID) (HashSync, error)
}

type LayerTicker interface {
	CurrentLayer() types.LayerID
}

type Handler[T ItemID] interface {
	Register(peer p2p.Peer, k rangesync.KeyBytes) T
	Get(ctx context.Context, ids []T, callback func(T, error)) error
}
