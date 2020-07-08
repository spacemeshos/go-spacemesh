package grpcserver

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
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

// Account returns data for one account
func (s GlobalStateService) Account(ctx context.Context, in *pb.AccountRequest) (*pb.AccountResponse, error) {
	log.Info("GRPC GlobalStateService.Account")
	return nil, nil
}

// AccountDataQuery returns historical account data such as rewards and receipts
func (s GlobalStateService) AccountDataQuery(ctx context.Context, in *pb.AccountDataQueryRequest) (*pb.AccountDataQueryResponse, error) {
	log.Info("GRPC GlobalStateService.AccountDataQuery")
	return nil, nil
}

// SmesherDataQuery returns historical info on smesher rewards
func (s GlobalStateService) SmesherDataQuery(ctx context.Context, in *pb.SmesherDataQueryRequest) (*pb.SmesherDataQueryResponse, error) {
	log.Info("GRPC GlobalStateService.SmesherDataQuery")
	return nil, nil
}

// STREAMS

// AccountDataStream exposes a stream of account-related data
func (s GlobalStateService) AccountDataStream(request *pb.AccountDataStreamRequest, stream pb.GlobalStateService_AccountDataStreamServer) error {
	log.Info("GRPC GlobalStateService.AccountDataStream")
	return nil
}

// SmesherRewardStream exposes a stream of smesher rewards
func (s GlobalStateService) SmesherRewardStream(request *pb.SmesherRewardStreamRequest, stream pb.GlobalStateService_SmesherRewardStreamServer) error {
	log.Info("GRPC GlobalStateService.SmesherRewardStream")
	return nil
}

// AppEventStream exposes a stream of emitted app events
func (s GlobalStateService) AppEventStream(request *pb.AppEventStreamRequest, stream pb.GlobalStateService_AppEventStreamServer) error {
	log.Info("GRPC GlobalStateService.AppEventStream")
	return nil
}

// GlobalStateStream exposes a stream of global data data items: rewards, receipts, account info, global state hash
func (s GlobalStateService) GlobalStateStream(request *pb.GlobalStateStreamRequest, stream pb.GlobalStateService_GlobalStateStreamServer) error {
	log.Info("GRPC GlobalStateService.GlobalStateStream")
	return nil
}
