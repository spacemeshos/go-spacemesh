package sync2

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -package=sync2_test -destination=./mocks_test.go -source=./interface.go

type Fetcher interface {
	GetAtxs(context.Context, []types.ATXID, ...system.GetAtxOpt) error
	RegisterPeerHashes(peer p2p.Peer, hash []types.Hash32)
}

type HashSync interface {
	Load() error
	Start()
	Stop()
	StartAndSync(ctx context.Context) error
}

type HashSyncSource interface {
	CreateHashSync(name string, cfg Config, epoch types.EpochID) HashSync
}

type LayerTicker interface {
	CurrentLayer() types.LayerID
}
