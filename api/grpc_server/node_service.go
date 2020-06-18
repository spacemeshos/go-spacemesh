package grpc_server

import (
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	"google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
)

// NodeService is a grpc server providing the NodeService
type NodeService struct {
	Service
	Network     api.NetworkAPI // P2P Swarm
	Tx          TxAPI          // Mesh
	GenTime     api.GenesisTimeAPI
	PeerCounter PeerCounter
	Syncer      api.Syncer
}

func (s NodeService) registerService() {
	pb.RegisterNodeServiceServer(s.server, s)
}

// NewNodeService creates a new grpc_server service using config data.
func NewNodeService(
	port int, net api.NetworkAPI, tx TxAPI, genTime api.GenesisTimeAPI,
	syncer api.Syncer) *NodeService {
	server := grpc.NewServer(ServerOptions...)
	return &NodeService{
		Service: Service{
			server: server,
			port:   uint(port),
		},
		Network:     net,
		Tx:          tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpc_server")),
		Syncer:      syncer,
	}
}

// Echo returns the response for an echo api request
func (s NodeService) Echo(ctx context.Context, in *pb.EchoRequest) (*pb.EchoResponse, error) {
	log.Info("GRPC NodeService.Echo")
	return &pb.EchoResponse{Msg: &pb.SimpleString{Value: in.Msg.Value}}, nil
}

// Version returns the version of the node software as a semver string
func (s NodeService) Version(ctx context.Context, in *empty.Empty) (*pb.VersionResponse, error) {
	log.Info("GRPC NodeService.Version")
	return &pb.VersionResponse{
		VersionString: &pb.SimpleString{Value: cmd.Version},
	}, nil
}

// Build returns the build of the node software
func (s NodeService) Build(ctx context.Context, in *empty.Empty) (*pb.BuildResponse, error) {
	log.Info("GRPC NodeService.Build")
	return &pb.BuildResponse{
		BuildString: &pb.SimpleString{Value: cmd.Commit},
	}, nil
}

// GetNodeStatus returns a status object providing information about the connected peers, sync status,
// current and verified layer
func (s NodeService) Status(ctx context.Context, request *pb.StatusRequest) (*pb.StatusResponse, error) {
	log.Info("GRPC NodeService.Status")
	return &pb.StatusResponse{
		Status: &pb.NodeStatus{
			ConnectedPeers: s.PeerCounter.PeerCount(),
			IsSynced:       s.Syncer.IsSynced(),
			SyncedLayer:    s.Tx.LatestLayer().Uint64(),
			TopLayer:       s.GenTime.GetCurrentLayer().Uint64(),
			VerifiedLayer:  s.Tx.LatestLayerInState().Uint64(),
		},
	}, nil
}

// SyncStart requests that the node start syncing the mesh (if it isn't already syncing)
func (s NodeService) SyncStart(ctx context.Context, request *pb.SyncStartRequest) (*pb.SyncStartResponse, error) {
	log.Info("GRPC NodeService.SyncStart")
	s.Syncer.Start()
	return &pb.SyncStartResponse{
		Status: &status.Status{Code: int32(code.Code_OK)},
	}, nil
}

// Shutdown requests a graceful shutdown
func (s NodeService) Shutdown(ctx context.Context, request *pb.ShutdownRequest) (*pb.ShutdownResponse, error) {
	log.Info("GRPC NodeService.Shutdown")
	cmd.Cancel()
	return &pb.ShutdownResponse{
		Status: &status.Status{Code: int32(code.Code_OK)},
	}, nil
}

// STREAMS

// StatusStream is a stub for a future server-side streaming RPC endpoint
func (s NodeService) StatusStream(request *pb.StatusStreamRequest, stream pb.NodeService_StatusStreamServer) error {
	log.Info("GRPC NodeService.StatusStream")
	return nil
}

// ErrorStream is a stub for a future server-side streaming RPC endpoint
func (s NodeService) ErrorStream(request *pb.ErrorStreamRequest, stream pb.NodeService_ErrorStreamServer) error {
	log.Info("GRPC NodeService.ErrorStream")
	return nil
}
