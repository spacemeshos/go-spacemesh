package smesher

import (
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/api/grpcserver/server"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// Smesher exposes endpoints to manage smeshing
type Smesher struct {
	Mining api.MiningAPI

	log log.Logger
}

var _ server.API = (*Smesher)(nil)

// Register registers this service with a grpc server instance
func (s Smesher) Register(svr *server.Server) {
	pb.RegisterSmesherServiceServer(svr.GrpcServer, s)
}

// Option type definds an callback that can set internal Smesher fields
type Option func(s *Smesher)

// WithLogger set's the underlying logger to a custom logger. By default the logger is NoOp
func WithLogger(log log.Logger) Option {
	return func(s *Smesher) {
		s.log = log
	}
}

// New creates a new grpc service using config data.
func New(miner api.MiningAPI, options ...Option) *Smesher {
	s := &Smesher{
		Mining: miner,
		log:    log.NewNop(),
	}
	for _, option := range options {
		option(s)
	}
	return s
}

// IsSmeshing reports whether the node is smeshing
func (s Smesher) IsSmeshing(ctx context.Context, in *empty.Empty) (*pb.IsSmeshingResponse, error) {
	s.log.Info("GRPC SmesherService.IsSmeshing")

	stat, _, _, _ := s.Mining.MiningStats()
	isSmeshing := stat == activation.InitDone
	return &pb.IsSmeshingResponse{IsSmeshing: isSmeshing}, nil
}

// StartSmeshing requests that the node begin smeshing
func (s Smesher) StartSmeshing(ctx context.Context, in *pb.StartSmeshingRequest) (*pb.StartSmeshingResponse, error) {
	s.log.Info("GRPC SmesherService.StartSmeshing")

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
		s.log.Error("error starting post: %s", err)
		return nil, status.Errorf(codes.Internal, "error initializing smeshing")
	}
	return &pb.StartSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// StopSmeshing requests that the node stop smeshing
func (s Smesher) StopSmeshing(ctx context.Context, in *pb.StopSmeshingRequest) (*pb.StopSmeshingResponse, error) {
	s.log.Info("GRPC SmesherService.StopSmeshing")

	s.Mining.Stop()
	return &pb.StopSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// SmesherID returns the smesher ID of this node
func (s Smesher) SmesherID(ctx context.Context, in *empty.Empty) (*pb.SmesherIDResponse, error) {
	s.log.Info("GRPC SmesherService.SmesherID")

	smesherID := s.Mining.GetSmesherID()
	return &pb.SmesherIDResponse{AccountId: &pb.AccountId{Address: smesherID.ToBytes()}}, nil
}

// Coinbase returns the current coinbase setting of this node
func (s Smesher) Coinbase(ctx context.Context, in *empty.Empty) (*pb.CoinbaseResponse, error) {
	s.log.Info("GRPC SmesherService.Coinbase")

	_, _, coinbase, _ := s.Mining.MiningStats()
	addr, err := types.StringToAddress(coinbase)
	if err != nil {
		s.log.Error("error converting coinbase: %s", err)
		return nil, status.Errorf(codes.Internal, "error reading coinbase data")
	}
	return &pb.CoinbaseResponse{AccountId: &pb.AccountId{Address: addr.Bytes()}}, nil
}

// SetCoinbase sets the current coinbase setting of this node
func (s Smesher) SetCoinbase(ctx context.Context, in *pb.SetCoinbaseRequest) (*pb.SetCoinbaseResponse, error) {
	s.log.Info("GRPC SmesherService.SetCoinbase")

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
func (s Smesher) MinGas(ctx context.Context, in *empty.Empty) (*pb.MinGasResponse, error) {
	s.log.Info("GRPC SmesherService.MinGas")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// SetMinGas sets the mingas setting of this node
func (s Smesher) SetMinGas(ctx context.Context, in *pb.SetMinGasRequest) (*pb.SetMinGasResponse, error) {
	s.log.Info("GRPC SmesherService.SetMinGas")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// PostStatus returns post data status
func (s Smesher) PostStatus(ctx context.Context, in *empty.Empty) (*pb.PostStatusResponse, error) {
	s.log.Info("GRPC SmesherService.PostStatus")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// PostComputeProviders returns a list of available post compute providers
func (s Smesher) PostComputeProviders(ctx context.Context, in *empty.Empty) (*pb.PostComputeProvidersResponse, error) {
	s.log.Info("GRPC SmesherService.PostComputeProviders")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// CreatePostData requests that the node begin post init
func (s Smesher) CreatePostData(ctx context.Context, in *pb.CreatePostDataRequest) (*pb.CreatePostDataResponse, error) {
	s.log.Info("GRPC SmesherService.CreatePostData")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// StopPostDataCreationSession requests that the node stop ongoing post data creation
func (s Smesher) StopPostDataCreationSession(ctx context.Context, in *pb.StopPostDataCreationSessionRequest) (*pb.StopPostDataCreationSessionResponse, error) {
	s.log.Info("GRPC SmesherService.StopPostDataCreationSession")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// STREAMS

// PostDataCreationProgressStream exposes a stream of updates during post init
func (s Smesher) PostDataCreationProgressStream(request *empty.Empty, stream pb.SmesherService_PostDataCreationProgressStreamServer) error {
	s.log.Info("GRPC SmesherService.PostDataCreationProgressStream")
	return status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}
