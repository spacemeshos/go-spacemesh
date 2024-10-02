package syncer

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/fetch"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -package=mocks -destination=./mocks/mocks.go -source=./interface.go

type layerTicker interface {
	CurrentLayer() types.LayerID
	LayerToTime(types.LayerID) time.Time
}

// fetchLogic is the interface between syncer and low-level fetching.
// It handles all data fetching related logic (for layer or for epoch, from all peers or from any random peer ...etc).
type fetchLogic interface {
	fetcher

	PollLayerData(context.Context, types.LayerID, ...p2p.Peer) error
	PollLayerOpinions(
		context.Context,
		types.LayerID,
		bool,
		[]p2p.Peer,
	) ([]*fetch.LayerOpinion, []*types.Certificate, error)
}

type atxSyncer interface {
	Download(context.Context, types.EpochID, time.Time) error
}

type malSyncer interface {
	EnsureInSync(parent context.Context, epochStart, epochEnd time.Time) error
	DownloadLoop(parent context.Context) error
}

// fetcher is the interface to the low-level fetching.
type fetcher interface {
	GetMaliciousIDs(context.Context, p2p.Peer) ([]types.NodeID, error)
	GetLayerData(context.Context, p2p.Peer, types.LayerID) ([]byte, error)
	GetLayerOpinions(context.Context, p2p.Peer, types.LayerID) ([]byte, error)
	GetCert(context.Context, types.LayerID, types.BlockID, []p2p.Peer) (*types.Certificate, error)

	system.AtxFetcher
	system.MalfeasanceProofFetcher
	GetBallots(context.Context, []types.BallotID) error
	GetBlocks(context.Context, []types.BlockID) error
	RegisterPeerHashes(peer p2p.Peer, hashes []types.Hash32)

	SelectBestShuffled(int) []p2p.Peer
	PeerEpochInfo(context.Context, p2p.Peer, types.EpochID) (*fetch.EpochData, error)
	PeerMeshHashes(context.Context, p2p.Peer, *fetch.MeshHashRequest) (*fetch.MeshHashes, error)
}

type layerPatrol interface {
	IsHareInCharge(types.LayerID) bool
}

type certHandler interface {
	HandleSyncedCertificate(context.Context, types.LayerID, *types.Certificate) error
}

type forkFinder interface {
	AddResynced(types.LayerID, types.Hash32)
	NeedResync(types.LayerID, types.Hash32) bool
	UpdateAgreement(p2p.Peer, types.LayerID, types.Hash32, time.Time)
	FindFork(context.Context, p2p.Peer, types.LayerID, types.Hash32) (types.LayerID, error)
	Purge(bool, ...p2p.Peer)
}
