package v2alpha1

import (
	"context"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	Node = "node_v2alpha1"
)

func NewNodeService(db sql.Executor) *NodeService {
	return &NodeService{db: db}
}

type NodeService struct {
	db sql.Executor
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

func (s *NodeService) Status(context.Context, *emptypb.Empty) (*spacemeshv2alpha1.NodeStatusResponse, error) {
	return &spacemeshv2alpha1.NodeStatusResponse{
		ConnectedPeers: 0,
		Status:         0,
		SyncedLayer:    0,
		VerifiedLayer:  0,
		HeadLayer:      0,
		CurrentLayer:   0,
		HeadBlockId:    0,
	}, nil
}
