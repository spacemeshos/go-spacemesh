package grpcserver

import (
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/peers"
	"golang.org/x/net/context"
)

// SmesherService exposes endpoints to manage smeshing
type SmesherService struct {
	Network     api.NetworkAPI // P2P Swarm
	Tx          api.TxAPI      // Mesh
	GenTime     api.GenesisTimeAPI
	PeerCounter api.PeerCounter
	Syncer      api.Syncer
}

// RegisterService registers this service with a grpc server instance
func (s SmesherService) RegisterService(server *Server) {
	pb.RegisterSmesherServiceServer(server.GrpcServer, s)
}

// NewSmesherService creates a new grpc service using config data.
func NewSmesherService(
	net api.NetworkAPI, tx api.TxAPI, genTime api.GenesisTimeAPI,
	syncer api.Syncer) *SmesherService {
	return &SmesherService{
		Network:     net,
		Tx:          tx,
		GenTime:     genTime,
		PeerCounter: peers.NewPeers(net, log.NewDefault("grpc_server.SmesherService")),
		Syncer:      syncer,
	}
}

// IsSmeshing reports whether the node is smeshing
func (s SmesherService) IsSmeshing(ctx context.Context, in *empty.Empty) (*pb.IsSmeshingResponse, error) {
	log.Info("GRPC SmesherService.IsSmeshing")
	return nil, nil
}

// StartSmeshing requests that the node begin smeshing
func (s SmesherService) StartSmeshing(ctx context.Context, in *empty.Empty) (*pb.StartSmeshingResponse, error) {
	log.Info("GRPC SmesherService.StartSmeshing")
	return nil, nil
}

// StopSmeshing requests that the node stop smeshing
func (s SmesherService) StopSmeshing(ctx context.Context, in *pb.StopSmeshingRequest) (*pb.StopSmeshingResponse, error) {
	log.Info("GRPC SmesherService.StopSmeshing")
	return nil, nil
}

// SmesherId returns the smesher ID of this node
func (s SmesherService) SmesherId(ctx context.Context, in *empty.Empty) (*pb.SmesherIdResponse, error) {
	log.Info("GRPC SmesherService.SmesherId")
	return nil, nil
}

// Coinbase returns the current coinbase setting of this node
func (s SmesherService) Coinbase(ctx context.Context, in *empty.Empty) (*pb.CoinbaseResponse, error) {
	log.Info("GRPC SmesherService.Coinbase")
	return nil, nil
}

// SetCoinbase sets the current coinbase setting of this node
func (s SmesherService) SetCoinbase(ctx context.Context, in *pb.SetCoinbaseRequest) (*pb.SetCoinbaseResponse, error) {
	log.Info("GRPC SmesherService.SetCoinbase")
	return nil, nil
}

// MinGas returns the current mingas setting of this node
func (s SmesherService) MinGas(ctx context.Context, in *empty.Empty) (*pb.MinGasResponse, error) {
	log.Info("GRPC SmesherService.MinGas")
	return nil, nil
}

// SetMinGas sets the mingas setting of this node
func (s SmesherService) SetMinGas(ctx context.Context, in *pb.SetMinGasRequest) (*pb.SetMinGasResponse, error) {
	log.Info("GRPC SmesherService.SetMinGas")
	return nil, nil
}

// PostStatus returns post data status
func (s SmesherService) PostStatus(ctx context.Context, in *empty.Empty) (*pb.PostStatusResponse, error) {
	log.Info("GRPC SmesherService.PostStatus")
	return nil, nil
}

// PostComputeProviders returns a list of available post compute providers
func (s SmesherService) PostComputeProviders(ctx context.Context, in *empty.Empty) (*pb.PostComputeProvidersResponse, error) {
	log.Info("GRPC SmesherService.PostComputeProviders")
	return nil, nil
}

// CreatePostData requests that the node begin post init
func (s SmesherService) CreatePostData(ctx context.Context, in *pb.CreatePostDataRequest) (*pb.CreatePostDataResponse, error) {
	log.Info("GRPC SmesherService.CreatePostData")
	return nil, nil
}

// StopPostDataCreationSession requests that the node stop ongoing post data creation
func (s SmesherService) StopPostDataCreationSession(ctx context.Context, in *pb.StopPostDataCreationSessionRequest) (*pb.StopPostDataCreationSessionResponse, error) {
	log.Info("GRPC SmesherService.StopPostDataCreationSession")
	return nil, nil
}

// STREAMS

// PostDataCreationProgressStream exposes a stream of updates during post init
func (s SmesherService) PostDataCreationProgressStream(request *empty.Empty, stream pb.SmesherService_PostDataCreationProgressStreamServer) error {
	log.Info("GRPC SmesherService.PostDataCreationProgressStream")
	return nil
}
