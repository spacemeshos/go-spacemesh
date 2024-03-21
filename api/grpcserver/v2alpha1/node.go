package v2alpha1

import (
	"context"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

const (
	Node = "node_v2alpha1"
)

func NewNodeService(peers peerCounter,
	msh meshAPI,
	genTime genesisTimeAPI,
	syncer syncer) *NodeService {
	return &NodeService{
		mesh:        msh,
		genTime:     genTime,
		peerCounter: peers,
		syncer:      syncer,
	}
}

type NodeService struct {
	mesh        meshAPI
	genTime     genesisTimeAPI
	peerCounter peerCounter
	syncer      syncer
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

func (s *NodeService) Status(ctx context.Context, _ *emptypb.Empty) (*spacemeshv2alpha1.NodeStatusResponse, error) {
	curLayer, latestLayer, verifiedLayer := s.getLayers()
	layer, err := s.mesh.GetLayer(s.mesh.LatestLayer())
	if err != nil {
		return nil, err
	}

	var blockId types.BlockID
	if len(layer.BlocksIDs()) > 0 {
		blockId = layer.BlocksIDs()[0]
	}

	// Get more info from syncer
	status := spacemeshv2alpha1.NodeStatusResponse_SYNC_STATUS_UNSPECIFIED
	if s.syncer.IsSynced(ctx) {
		status = spacemeshv2alpha1.NodeStatusResponse_SYNC_STATUS_SYNCED
	} else {
		status = spacemeshv2alpha1.NodeStatusResponse_SYNC_STATUS_SYNCING
	}

	return &spacemeshv2alpha1.NodeStatusResponse{
		ConnectedPeers: s.peerCounter.PeerCount(),
		Status:         status,
		SyncedLayer:    latestLayer,   // last layer node has synced
		VerifiedLayer:  verifiedLayer, // last layer node has verified
		HeadLayer:      latestLayer,   // current chain head; the last layer the node has gossiped or seen
		CurrentLayer:   curLayer,      // current layer, based on clock time
		HeadBlockId:    blockId.Bytes(),
	}, nil
}

func (s *NodeService) getLayers() (curLayer, latestLayer, verifiedLayer uint32) {
	// We cannot get meaningful data from the mesh during the genesis epochs since there are no blocks in these
	// epochs, so just return the current layer instead
	curLayerObj := s.genTime.CurrentLayer()
	curLayer = curLayerObj.Uint32()
	if curLayerObj <= types.GetEffectiveGenesis() {
		latestLayer = s.mesh.LatestLayer().Uint32()
		verifiedLayer = latestLayer
	} else {
		latestLayer = s.mesh.LatestLayer().Uint32()
		verifiedLayer = s.mesh.LatestLayerInState().Uint32()
	}
	return
}
