package grpcserver

import (
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GlobalStateService is a grpc server providing the GlobalStateService
type GlobalStateService struct {
	Network     api.NetworkAPI // P2P Swarm
	Tx          api.TxAPI      // Mesh
	GenTime     api.GenesisTimeAPI
	PeerCounter api.PeerCounter
	Syncer      api.Syncer
}

// RegisterService registers this service with a grpc server instance
func (s GlobalStateService) RegisterService(server *Server) {
	pb.RegisterGlobalStateServiceServer(server.GrpcServer, s)
}

// NewGlobalStateService creates a new grpc service using config data.
func NewGlobalStateService(
	net api.NetworkAPI, tx api.TxAPI, genTime api.GenesisTimeAPI,
	syncer api.Syncer) *GlobalStateService {
	return &GlobalStateService{
		Network:     net,
		Tx:          tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpc_server.GlobalStateService")),
		Syncer:      syncer,
	}
}

// GlobalStateHash returns the latest layer and its computed global state hash
func (s GlobalStateService) GlobalStateHash(ctx context.Context, in *pb.GlobalStateHashRequest) (*pb.GlobalStateHashResponse, error) {
	log.Info("GRPC GlobalStateService.GlobalStateHash")
	return nil, nil
}

// STREAMS

// AccountDataStream is a stub for a future server-side streaming RPC endpoint
func (s GlobalStateService) AccountDataStream(request *pb.AccountDataStreamRequest, stream pb.GlobalStateService_AccountDataStreamServer) error {
	log.Info("GRPC GlobalStateService.AccountDataStream")
	return nil
}
