package grpcserver

import (
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SmesherService exposes endpoints to manage smeshing
type SmesherService struct {
	Mining api.MiningAPI
}

// RegisterService registers this service with a grpc server instance
func (s SmesherService) RegisterService(server *Server) {
	pb.RegisterSmesherServiceServer(server.GrpcServer, s)
}

// NewSmesherService creates a new grpc service using config data.
func NewSmesherService(miner api.MiningAPI) *SmesherService {
	return &SmesherService{
		Mining: miner,
	}
}

// IsSmeshing reports whether the node is smeshing
func (s SmesherService) IsSmeshing(ctx context.Context, in *empty.Empty) (*pb.IsSmeshingResponse, error) {
	log.Info("GRPC SmesherService.IsSmeshing")

	stat, _, _, _ := s.Mining.MiningStats()
	isSmeshing := stat == activation.InitDone
	return &pb.IsSmeshingResponse{IsSmeshing: isSmeshing}, nil
}

// StartSmeshing requests that the node begin smeshing
func (s SmesherService) StartSmeshing(ctx context.Context, in *pb.StartSmeshingRequest) (*pb.StartSmeshingResponse, error) {
	log.Info("GRPC SmesherService.StartSmeshing")

	if in.Coinbase == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Coinbase` must be provided")
	}
	if in.DataDir == "" {
		return nil, status.Errorf(codes.InvalidArgument, "`DataDir` must be provided")
	}
	if in.CommitmentSize == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`CommitmentSize` must be provided")
	}

	addr := types.BytesToAddress(in.Coinbase.Address)
	if err := s.Mining.StartPost(addr, in.DataDir, in.CommitmentSize.Value); err != nil {
		log.Error("error starting post: %s", err)
		return nil, status.Errorf(codes.Internal, "error initializing smeshing")
	}
	return &pb.StartSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// StopSmeshing requests that the node stop smeshing
func (s SmesherService) StopSmeshing(ctx context.Context, in *pb.StopSmeshingRequest) (*pb.StopSmeshingResponse, error) {
	log.Info("GRPC SmesherService.StopSmeshing")

	s.Mining.Stop()
	return &pb.StopSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// SmesherID returns the smesher ID of this node
func (s SmesherService) SmesherID(ctx context.Context, in *empty.Empty) (*pb.SmesherIDResponse, error) {
	log.Info("GRPC SmesherService.SmesherID")

	smesherID := s.Mining.GetSmesherID()
	return &pb.SmesherIDResponse{AccountId: &pb.AccountId{Address: smesherID.ToBytes()}}, nil
}

// Coinbase returns the current coinbase setting of this node
func (s SmesherService) Coinbase(ctx context.Context, in *empty.Empty) (*pb.CoinbaseResponse, error) {
	log.Info("GRPC SmesherService.Coinbase")

	_, _, coinbase, _ := s.Mining.MiningStats()
	addr, err := types.StringToAddress(coinbase)
	if err != nil {
		log.Error("error converting coinbase: %s", err)
		return nil, status.Errorf(codes.Internal, "error reading coinbase data")
	}
	return &pb.CoinbaseResponse{AccountId: &pb.AccountId{Address: addr.Bytes()}}, nil
}

// SetCoinbase sets the current coinbase setting of this node
func (s SmesherService) SetCoinbase(ctx context.Context, in *pb.SetCoinbaseRequest) (*pb.SetCoinbaseResponse, error) {
	log.Info("GRPC SmesherService.SetCoinbase")

	if in.Id == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Id` must be provided")
	}

	addr := types.BytesToAddress(in.Id.Address)
	s.Mining.SetCoinbaseAccount(addr)
	return &pb.SetCoinbaseResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// MinGas returns the current mingas setting of this node
func (s SmesherService) MinGas(ctx context.Context, in *empty.Empty) (*pb.MinGasResponse, error) {
	log.Info("GRPC SmesherService.MinGas")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// SetMinGas sets the mingas setting of this node
func (s SmesherService) SetMinGas(ctx context.Context, in *pb.SetMinGasRequest) (*pb.SetMinGasResponse, error) {
	log.Info("GRPC SmesherService.SetMinGas")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// PostStatus returns post data status
func (s SmesherService) PostStatus(ctx context.Context, in *empty.Empty) (*pb.PostStatusResponse, error) {
	log.Info("GRPC SmesherService.PostStatus")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// PostComputeProviders returns a list of available post compute providers
func (s SmesherService) PostComputeProviders(ctx context.Context, in *empty.Empty) (*pb.PostComputeProvidersResponse, error) {
	log.Info("GRPC SmesherService.PostComputeProviders")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// CreatePostData requests that the node begin post init
func (s SmesherService) CreatePostData(ctx context.Context, in *pb.CreatePostDataRequest) (*pb.CreatePostDataResponse, error) {
	log.Info("GRPC SmesherService.CreatePostData")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// StopPostDataCreationSession requests that the node stop ongoing post data creation
func (s SmesherService) StopPostDataCreationSession(ctx context.Context, in *pb.StopPostDataCreationSessionRequest) (*pb.StopPostDataCreationSessionResponse, error) {
	log.Info("GRPC SmesherService.StopPostDataCreationSession")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// STREAMS

// PostDataCreationProgressStream exposes a stream of updates during post init
func (s SmesherService) PostDataCreationProgressStream(request *empty.Empty, stream pb.SmesherService_PostDataCreationProgressStreamServer) error {
	log.Info("GRPC SmesherService.PostDataCreationProgressStream")
	return status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}
