package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

const (
	Node = "node_v2alpha1"
)

// nodePeerCounter is an api to get current peer count.
type nodePeerCounter interface {
	PeerCount() uint64
}

// nodeMeshAPI is an api for getting mesh status.
type nodeMeshAPI interface {
	LatestLayer() types.LayerID
	LatestLayerInState() types.LayerID
	ProcessedLayer() types.LayerID
}

// nodeSyncer is an API to get sync status.
type nodeSyncer interface {
	IsSynced(context.Context) bool
}

func NewNodeService(peers nodePeerCounter, msh nodeMeshAPI, clock *timesync.NodeClock, syncer nodeSyncer) *NodeService {
	return &NodeService{
		mesh:        msh,
		clock:       clock,
		peerCounter: peers,
		syncer:      syncer,
	}
}

type NodeService struct {
	mesh        nodeMeshAPI
	clock       *timesync.NodeClock
	peerCounter nodePeerCounter
	syncer      nodeSyncer
}

func (s *NodeService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterNodeServiceServer(server, s)
}

func (s *NodeService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterNodeServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *NodeService) String() string {
	return "NodeService"
}

func (s *NodeService) Status(ctx context.Context, _ *spacemeshv2alpha1.NodeStatusRequest) (
	*spacemeshv2alpha1.NodeStatusResponse, error,
) {
	var status spacemeshv2alpha1.NodeStatusResponse_SyncStatus

	if s.syncer.IsSynced(ctx) {
		status = spacemeshv2alpha1.NodeStatusResponse_SYNC_STATUS_SYNCED
	} else {
		status = spacemeshv2alpha1.NodeStatusResponse_SYNC_STATUS_SYNCING
	}

	return &spacemeshv2alpha1.NodeStatusResponse{
		ConnectedPeers: s.peerCounter.PeerCount(),
		Status:         status,
		LatestLayer:    s.mesh.LatestLayer().Uint32(),        // latest layer node has seen from blocks
		AppliedLayer:   s.mesh.LatestLayerInState().Uint32(), // last layer node has applied to the state
		ProcessedLayer: s.mesh.ProcessedLayer().Uint32(),     // last layer whose votes have been processed
		CurrentLayer:   s.clock.CurrentLayer().Uint32(),      // current layer, based on clock time
	}, nil
}
