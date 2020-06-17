package grpc_server

import (
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// GlobalStateService is a grpc server providing the GlobalState service
type GlobalStateService struct {
	Service
	Tx      TxAPI // Mesh
	GenTime GenesisTimeAPI
}

func (s GlobalStateService) registerService() {
	pb.RegisterGlobalStateServiceServer(s.server, s)
}

// NewNodeService creates a new grpc_server service using config data.
func NewGlobalStateService(port int, tx TxAPI, genTime GenesisTimeAPI) *GlobalStateService {
	server := grpc.NewServer(ServerOptions...)
	return &GlobalStateService{
		Service: Service{
			server: server,
			port:   uint(port),
		},
		Tx:      tx,
		GenTime: genTime,
	}
}

// GetStateRoot returns current state root
func (s GlobalStateService) GlobalStateHash(ctx context.Context,
	request *pb.GlobalStateHashRequest) (*pb.GlobalStateHashResponse, error) {
	log.Info("GRPC GlobalStateService.GlobalStateHash")
	return &pb.GlobalStateHashResponse{
		Response: &pb.GlobalStateHash{
			RootHash:    s.Tx.GetStateRoot().Bytes(),
			LayerNumber: s.GenTime.GetCurrentLayer().Uint64(),
		}}, nil
}

func (s GlobalStateService) Account(ctx context.Context, request *pb.AccountRequest) (
	*pb.AccountResponse, error) {
	log.Info("GRPC GlobalStateService.Account")
	return nil, nil
}

func (s GlobalStateService) AccountDataQuery(ctx context.Context, request *pb.AccountDataQueryRequest) (
	*pb.AccountDataQueryResponse, error) {
	log.Info("GRPC GlobalStateService.AccountDataQuery")
	return nil, nil
}

func (s GlobalStateService) SmesherDataQuery(ctx context.Context, request *pb.SmesherDataQueryRequest) (
	*pb.SmesherDataQueryResponse, error) {
	log.Info("GRPC GlobalStateService.SmesherDataQuery")
	return nil, nil
}

// STREAMS

func (s GlobalStateService) AccountDataStream(request *pb.AccountDataStreamRequest, stream pb.GlobalStateService_AccountDataStreamServer) error {
	log.Info("GRPC GlobalStateService.AccountDataStream")
	return nil
}

func (s GlobalStateService) SmesherRewardStream(request *pb.SmesherRewardStreamRequest, stream pb.GlobalStateService_SmesherRewardStreamServer) error {
	log.Info("GRPC GlobalStateService.SmesherRewardStream")
	return nil

}

func (s GlobalStateService) AppEventStream(request *pb.AppEventStreamRequest, stream pb.GlobalStateService_AppEventStreamServer) error {
	log.Info("GRPC GlobalStateService.AppEventStream")
	return nil
}

func (s GlobalStateService) GlobalStateStream(request *pb.GlobalStateStreamRequest, stream pb.GlobalStateService_GlobalStateStreamServer) error {
	log.Info("GRPC GlobalStateService.GlobalStateStream")
	return nil
}
