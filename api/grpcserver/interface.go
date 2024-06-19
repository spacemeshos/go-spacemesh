package grpcserver

import (
	"context"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	ma "github.com/multiformats/go-multiaddr"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/malfeasance/wire"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/peerinfo"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/system"
)

//go:generate mockgen -typed -package=grpcserver -destination=./mocks.go -source=./interface.go

// networkInfo interface.
type networkInfo interface {
	PeerInfo() peerinfo.PeerInfo
	ID() p2p.Peer
	ListenAddresses() []ma.Multiaddr
	KnownAddresses() []ma.Multiaddr
	NATDeviceType() (udpNATType, tcpNATType network.NATDeviceType)
	Reachability() network.Reachability
	DHTServerEnabled() bool
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
	GetAtx(id types.ATXID) (*types.ActivationTx, error)
	MaxHeightAtx() (types.ATXID, error)
	GetMalfeasanceProof(id types.NodeID) (*wire.MalfeasanceProof, error)
}

type postState interface {
	// PostStates returns the current state of all registered IDs.
	PostStates() map[types.IdentityDescriptor]types.PostState
}

type postSupervisor interface {
	Start(cmdCfg activation.PostSupervisorConfig, opts activation.PostSetupOpts, sig *signing.EdSigner) error
	Stop(deleteFiles bool) error

	Config() activation.PostConfig
	Status() *activation.PostSetupStatus
	Providers() ([]activation.PostSetupProvider, error)
	Benchmark(p activation.PostSetupProvider) (int, error)
}

type grpcPostService interface {
	AllowConnections(allow bool)
}

// peerCounter is an api to get amount of connected peers.
type peerCounter interface {
	PeerCount() uint64
}

// Peers is an api to get peer related info.
type peers interface {
	ConnectedPeerInfo(p2p.Peer) *p2p.PeerInfo
	GetPeers() []p2p.Peer
}

// genesisTimeAPI is an API to get genesis time and current layer of the system.
type genesisTimeAPI interface {
	GenesisTime() time.Time
	CurrentLayer() types.LayerID
}

// meshAPI is an api for getting mesh status about layers/blocks/rewards.
type meshAPI interface {
	GetLayer(types.LayerID) (*types.Layer, error)
	GetLayerVerified(types.LayerID) (*types.Block, error)
	GetRewardsByCoinbase(types.Address) ([]*types.Reward, error)
	GetRewardsBySmesherId(id types.NodeID) ([]*types.Reward, error)
	LatestLayer() types.LayerID
	LatestLayerInState() types.LayerID
	ProcessedLayer() types.LayerID
	MeshHash(types.LayerID) (types.Hash32, error)
}

type oracle interface {
	ActiveSet(context.Context, types.EpochID) ([]types.ATXID, error)
}
