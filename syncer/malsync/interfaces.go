package malsync

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interfaces.go

type fetcher interface {
	SelectBestShuffled(int) []p2p.Peer
	LegacyMaliciousIDs(context.Context, p2p.Peer) ([]types.NodeID, error)
	MaliciousIDs(context.Context, p2p.Peer) ([]types.NodeID, error)
	LegacyMalfeasanceProofs(context.Context, []types.NodeID) error
	MalfeasanceProofs(context.Context, []types.NodeID) error
}

type layerTicker interface {
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}
