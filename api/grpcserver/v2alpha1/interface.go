package v2alpha1

import (
	"context"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// genesisTimeAPI is an API to get genesis time and current layer of the system.
type genesisTimeAPI interface {
	GenesisTime() time.Time
	CurrentLayer() types.LayerID
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

// syncer is the API to get sync status.
type syncer interface {
	IsSynced(context.Context) bool
}
