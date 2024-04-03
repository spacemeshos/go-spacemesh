package v2alpha1

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/common/types"
)

// peerCounter is an api to get amount of connected peers.
type peerCounter interface {
	PeerCount() uint64
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
