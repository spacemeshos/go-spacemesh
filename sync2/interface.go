package sync2

import (
	"context"

	"github.com/libp2p/go-libp2p/core/host"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch/peers"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -package=sync2_test -destination=./mocks_test.go -source=./interface.go

type Fetcher interface {
	system.AtxFetcher
	Host() host.Host
	Peers() *peers.Peers
	RegisterPeerHash(peer p2p.Peer, hash types.Hash32)
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
