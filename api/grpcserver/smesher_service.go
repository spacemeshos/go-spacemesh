package grpcserver

import (
	"fmt"
	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SmesherService exposes endpoints to manage smeshing
type SmesherService struct {
	post     api.PostAPI
	smeshing api.SmeshingAPI
}

// RegisterService registers this service with a grpc server instance
func (s SmesherService) RegisterService(server *Server) {
	pb.RegisterSmesherServiceServer(server.GrpcServer, s)
}

// NewSmesherService creates a new grpc service using config data.
func NewSmesherService(post api.PostAPI, smeshing api.SmeshingAPI) *SmesherService {
	return &SmesherService{post, smeshing}
}

// IsSmeshing reports whether the node is smeshing
func (s SmesherService) IsSmeshing(context.Context, *empty.Empty) (*pb.IsSmeshingResponse, error) {
	log.Info("GRPC SmesherService.IsSmeshing")

	return &pb.IsSmeshingResponse{IsSmeshing: s.smeshing.Smeshing()}, nil
}

// StartSmeshing requests that the node begin smeshing
func (s SmesherService) StartSmeshing(_ context.Context, in *pb.StartSmeshingRequest) (*pb.StartSmeshingResponse, error) {
	log.Info("GRPC SmesherService.StartSmeshing")

	if in.Coinbase == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Coinbase` must be provided")
	}

	if in.Opts == nil {
		return nil, status.Error(codes.InvalidArgument, "`Opts` must be provided")
	}

	if in.Opts.DataDir == "" {
		return nil, status.Error(codes.InvalidArgument, "`Opts.DataDir` must be provided")
	}

	if in.Opts.NumUnits == 0 {
		return nil, status.Error(codes.InvalidArgument, "`Opts.NumUnits` must be provided")
	}

	if in.Opts.NumFiles == 0 {
		return nil, status.Error(codes.InvalidArgument, "`Opts.NumFiles` must be provided")
	}

	coinbaseAddr := types.BytesToAddress(in.Coinbase.Address)
	opts := &activation.PostInitOpts{
		DataDir:           in.Opts.DataDir,
		NumUnits:          uint(in.Opts.NumUnits),
		NumFiles:          uint(in.Opts.NumFiles),
		ComputeProviderID: int(in.Opts.ComputeProviderId),
		Throttle:          in.Opts.Throttle,
	}

	if err := s.smeshing.StartSmeshing(coinbaseAddr, opts); err != nil {
		err := fmt.Sprintf("failed to start smeshing: %v", err)
		log.Error(err)
		return nil, status.Error(codes.Internal, err)
	}

	return &pb.StartSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// StopSmeshing requests that the node stop smeshing
func (s SmesherService) StopSmeshing(ctx context.Context, in *pb.StopSmeshingRequest) (*pb.StopSmeshingResponse, error) {
	log.Info("GRPC SmesherService.StopSmeshing")

	if err := s.smeshing.StopSmeshing(in.DeleteFiles); err != nil {
		err := fmt.Sprintf("failed to stop smeshing: %v", err)
		log.Error(err)
		return nil, status.Error(codes.Internal, err)
	}

	return &pb.StopSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// PostComputeProviders returns a list of available post compute providers
func (s SmesherService) PostComputeProviders(context.Context, *empty.Empty) (*pb.PostComputeProvidersResponse, error) {
	log.Info("GRPC SmesherService.PostComputeProviders")

	providers := s.post.PostComputeProviders()

	res := &pb.PostComputeProvidersResponse{}
	res.PostComputeProvider = make([]*pb.PostComputeProvider, len(providers))
	for i, p := range providers {
		hs, err := p.Benchmark()
		if err != nil {
			log.Error("failed to benchmark provider: %v", err)
			return nil, status.Error(codes.Internal, "failed to benchmark provider")
		}

		res.PostComputeProvider[i] = &pb.PostComputeProvider{
			Id:          uint32(p.ID),
			Model:       p.Model,
			ComputeApi:  pb.ComputeApiClass(p.ComputeAPI), // assuming enum values match.
			Performance: uint64(hs),
		}
	}

	return res, nil
}

// PostDataCreationProgressStream exposes a stream of updates during post init
func (s SmesherService) PostDataCreationProgressStream(_ *empty.Empty, stream pb.SmesherService_PostDataCreationProgressStreamServer) error {
	log.Info("GRPC SmesherService.PostDataCreationProgressStream")

	statusChan := s.post.PostDataCreationProgressStream()
	for {
		select {
		case status, more := <-statusChan:
			if !more {
				return nil
			}
			if err := stream.Send(&pb.PostDataCreationProgressStreamResponse{Status: statusToPbStatus(status)}); err != nil {
				return err
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// SmesherID returns the smesher ID of this node
func (s SmesherService) SmesherID(context.Context, *empty.Empty) (*pb.SmesherIDResponse, error) {
	log.Info("GRPC SmesherService.SmesherID")

	addr := util.Hex2Bytes(s.smeshing.SmesherID().Key)
	return &pb.SmesherIDResponse{AccountId: &pb.AccountId{Address: addr}}, nil
}

// Coinbase returns the current coinbase setting of this node
func (s SmesherService) Coinbase(context.Context, *empty.Empty) (*pb.CoinbaseResponse, error) {
	log.Info("GRPC SmesherService.Coinbase")

	addr := s.smeshing.Coinbase()
	return &pb.CoinbaseResponse{AccountId: &pb.AccountId{Address: addr.Bytes()}}, nil
}

// SetCoinbase sets the current coinbase setting of this node
func (s SmesherService) SetCoinbase(_ context.Context, in *pb.SetCoinbaseRequest) (*pb.SetCoinbaseResponse, error) {
	log.Info("GRPC SmesherService.SetCoinbase")

	if in.Id == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Id` must be provided")
	}

	addr := types.BytesToAddress(in.Id.Address)
	s.smeshing.SetCoinbase(addr)

	return &pb.SetCoinbaseResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// MinGas returns the current mingas setting of this node
func (s SmesherService) MinGas(context.Context, *empty.Empty) (*pb.MinGasResponse, error) {
	log.Info("GRPC SmesherService.MinGas")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// SetMinGas sets the mingas setting of this node
func (s SmesherService) SetMinGas(context.Context, *pb.SetMinGasRequest) (*pb.SetMinGasResponse, error) {
	log.Info("GRPC SmesherService.SetMinGas")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// EstimatedRewards returns estimated smeshing rewards over the next epoch
func (s SmesherService) EstimatedRewards(context.Context, *pb.EstimatedRewardsRequest) (*pb.EstimatedRewardsResponse, error) {
	log.Info("GRPC SmesherService.EstimatedRewards")
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// EstimatedRewards returns estimated smeshing rewards over the next epoch
func (s SmesherService) Config(context.Context, *empty.Empty) (*pb.ConfigResponse, error) {
	log.Info("GRPC SmesherService.Config")

	cfg := s.post.Config()

	return &pb.ConfigResponse{
		BitsPerLabel:  uint32(cfg.BitsPerLabel),
		LabelsPerUnit: uint64(cfg.LabelsPerUnit),
		MinNumUnits:   uint32(cfg.MinNumUnits),
		MaxNumUnits:   uint32(cfg.MaxNumUnits),
	}, nil
}

func statusToPbStatus(status *activation.PostStatus) *pb.PostStatus {
	pbStatus := &pb.PostStatus{}
	pbStatus.InitInProgress = status.InitInProgress
	pbStatus.LabelsWritten = status.NumLabelsWritten
	pbStatus.ErrorType = pb.PostStatus_ErrorType(status.ErrorType) // assuming enum values match.
	pbStatus.ErrorMessage = status.ErrorMessage
	if status.LastOpts != nil {
		pbStatus.LastOpts = &pb.PostInitOpts{
			DataDir:           status.LastOpts.DataDir,
			NumUnits:          uint32(status.LastOpts.NumUnits),
			NumFiles:          uint32(status.LastOpts.NumFiles),
			ComputeProviderId: uint32(status.LastOpts.ComputeProviderID),
			Throttle:          status.LastOpts.Throttle,
		}
	}
	return pbStatus
}
