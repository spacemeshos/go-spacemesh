package grpcserver

import (
	"context"
	"time"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -package=grpcserver -destination=./mocks.go -source=./interface.go

// networkIdentity interface.
type networkIdentity interface {
	ID() p2p.Peer
}

// conservativeState is an API for reading state and transaction/mempool data.
type conservativeState interface {
	GetStateRoot() (types.Hash32, error)
	GetLayerStateRoot(types.LayerID) (types.Hash32, error)
	GetAllAccounts() ([]*types.Account, error)
	GetBalance(types.Address) (uint64, error)
	GetNonce(types.Address) (types.Nonce, error)
	GetProjection(types.Address) (uint64, uint64)
	GetMeshTransaction(types.TransactionID) (*types.MeshTransaction, error)
	GetMeshTransactions([]types.TransactionID) ([]*types.MeshTransaction, map[types.TransactionID]struct{})
	GetTransactionsByAddress(types.LayerID, types.LayerID, types.Address) ([]*types.MeshTransaction, error)
	Validation(raw types.RawTx) system.ValidationRequest
}

// syncer is the API to get sync status.
type syncer interface {
	IsSynced(context.Context) bool
}

// txValidator is the API to validate and cache transactions.
type txValidator interface {
	VerifyAndCacheTx(context.Context, []byte) error
}

// atxProvider is used by ActivationService to get ATXes.
type atxProvider interface {
	GetFullAtx(id types.ATXID) (*types.VerifiedActivationTx, error)
}

type postSetupProvider interface {
	Status() *activation.PostSetupStatus
	Providers() ([]activation.PostSetupProvider, error)
	Benchmark(p activation.PostSetupProvider) (int, error)
	Config() activation.PostConfig
}

// peerCounter is an api to get amount of connected peers.
type peerCounter interface {
	PeerCount() uint64
}

// genesisTimeAPI is an API to get genesis time and current layer of the system.
type genesisTimeAPI interface {
	GenesisTime() time.Time
	CurrentLayer() types.LayerID
}

// meshAPI is an api for getting mesh status about layers/blocks/rewards.
type meshAPI interface {
	EpochAtxs(types.EpochID) ([]types.ATXID, error)
	GetATXs(context.Context, []types.ATXID) (map[types.ATXID]*types.VerifiedActivationTx, []types.ATXID)
	GetLayer(types.LayerID) (*types.Layer, error)
	GetRewards(types.Address) ([]*types.Reward, error)
	LatestLayer() types.LayerID
	LatestLayerInState() types.LayerID
	ProcessedLayer() types.LayerID
	MeshHash(types.LayerID) (types.Hash32, error)
}

type oracle interface {
	ActiveSet(context.Context, types.EpochID) ([]types.ATXID, error)
}
