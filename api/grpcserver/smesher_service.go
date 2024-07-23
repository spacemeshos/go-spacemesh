package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/post/config"
	"go.uber.org/zap"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/signing"
)

// SmesherService exposes endpoints to manage smeshing.
type SmesherService struct {
	smeshingProvider activation.SmeshingProvider
	postSupervisor   postSupervisor
	grpcPostService  grpcPostService

	streamInterval time.Duration
	cmdCfg         *activation.PostSupervisorConfig
	postOpts       activation.PostSetupOpts
	sig            *signing.EdSigner
}

// RegisterService registers this service with a grpc server instance.
func (s *SmesherService) RegisterService(server *grpc.Server) {
	pb.RegisterSmesherServiceServer(server, s)
}

func (s *SmesherService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterSmesherServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (s *SmesherService) String() string {
	return "SmesherService"
}

// NewSmesherService creates a new grpc service using config data.
func NewSmesherService(
	smeshing activation.SmeshingProvider,
	postSupervisor postSupervisor,
	grpcPostService grpcPostService,
	streamInterval time.Duration,
	postOpts activation.PostSetupOpts,
	sig *signing.EdSigner,
) *SmesherService {
	return &SmesherService{
		smeshingProvider: smeshing,
		postSupervisor:   postSupervisor,
		grpcPostService:  grpcPostService,
		streamInterval:   streamInterval,
		postOpts:         postOpts,
		sig:              sig,
	}
}

// SetPostServiceConfig sets the post supervisor config.
func (s *SmesherService) SetPostServiceConfig(cfg activation.PostSupervisorConfig) {
	s.cmdCfg = &cfg
}

// IsSmeshing reports whether the node is smeshing.
func (s *SmesherService) IsSmeshing(context.Context, *emptypb.Empty) (*pb.IsSmeshingResponse, error) {
	if s.sig == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "node is not configured for supervised smeshing")
	}
	return &pb.IsSmeshingResponse{IsSmeshing: s.smeshingProvider.Smeshing()}, nil
}

// StartSmeshing requests that the node begin smeshing.
func (s *SmesherService) StartSmeshing(
	ctx context.Context,
	in *pb.StartSmeshingRequest,
) (*pb.StartSmeshingResponse, error) {
	if s.sig == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "node is not configured for supervised smeshing")
	}
	if s.cmdCfg == nil {
		return nil, status.Errorf(codes.FailedPrecondition, "post supervisor config is not set")
	}
	opts, err := s.postSetupOpts(in.Opts)
	if err != nil {
		status.Error(codes.InvalidArgument, err.Error())
	}

	if in.Coinbase == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Coinbase` must be provided")
	}
	coinbaseAddr, err := types.StringToAddress(in.Coinbase.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse in.Coinbase.Address `%s`: %w", in.Coinbase.Address, err)
	}
	s.grpcPostService.AllowConnections(true)

	if err := s.postSupervisor.Start(*s.cmdCfg, opts, s.sig); err != nil {
		ctxzap.Error(ctx, "failed to start post supervisor", zap.Error(err))
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to start post supervisor: %v", err))
	}
	if err := s.smeshingProvider.StartSmeshing(coinbaseAddr); err != nil {
		ctxzap.Error(ctx, "failed to start smeshing", zap.Error(err))
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to start smeshing: %v", err))
	}
	return &pb.StartSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

func (s *SmesherService) postSetupOpts(in *pb.PostSetupOpts) (activation.PostSetupOpts, error) {
	if in == nil {
		return activation.PostSetupOpts{}, errors.New("`Opts` must be provided")
	}
	if in.DataDir == "" {
		return activation.PostSetupOpts{}, errors.New("`Opts.DataDir` must be provided")
	}
	if in.NumUnits == 0 {
		return activation.PostSetupOpts{}, errors.New("`Opts.NumUnits` must be provided")
	}
	if in.MaxFileSize == 0 {
		return activation.PostSetupOpts{}, errors.New("`Opts.MaxFileSize` must be provided")
	}

	// Overlay default with api provided opts
	opts := s.postOpts // TODO(mafa): fetch from post supervisor instead
	opts.DataDir = in.DataDir
	opts.NumUnits = in.NumUnits
	opts.MaxFileSize = in.MaxFileSize
	if in.ProviderId != nil {
		opts.ProviderID.SetUint32(*in.ProviderId)
	}
	opts.Throttle = in.Throttle
	return opts, nil
}

// StopSmeshing requests that the node stop smeshing.
func (s *SmesherService) StopSmeshing(
	ctx context.Context,
	in *pb.StopSmeshingRequest,
) (*pb.StopSmeshingResponse, error) {
	if err := s.smeshingProvider.StopSmeshing(in.DeleteFiles); err != nil {
		ctxzap.Error(ctx, "failed to stop smeshing", zap.Error(err))
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to stop smeshing: %v", err))
	}
	if err := s.postSupervisor.Stop(in.DeleteFiles); err != nil {
		ctxzap.Error(ctx, "failed to stop post supervisor", zap.Error(err))
		return nil, status.Error(codes.Internal, fmt.Sprintf("failed to stop post supervisor: %v", err))
	}
	return &pb.StopSmeshingResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// SmesherID returns the smesher ID of this node.
func (s *SmesherService) SmesherID(context.Context, *emptypb.Empty) (*pb.SmesherIDResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "this endpoint has been deprecated, use `SmesherIDs` instead")
}

func (s *SmesherService) SmesherIDs(context.Context, *emptypb.Empty) (*pb.SmesherIDsResponse, error) {
	ids := s.smeshingProvider.SmesherIDs()
	res := &pb.SmesherIDsResponse{}
	for _, id := range ids {
		res.PublicKeys = append(res.PublicKeys, id.Bytes())
	}
	return res, nil
}

// Coinbase returns the current coinbase setting of this node.
func (s *SmesherService) Coinbase(context.Context, *emptypb.Empty) (*pb.CoinbaseResponse, error) {
	return &pb.CoinbaseResponse{AccountId: &pb.AccountId{Address: s.smeshingProvider.Coinbase().String()}}, nil
}

// SetCoinbase sets the current coinbase setting of this node.
func (s *SmesherService) SetCoinbase(_ context.Context, in *pb.SetCoinbaseRequest) (*pb.SetCoinbaseResponse, error) {
	if in.Id == nil {
		return nil, status.Errorf(codes.InvalidArgument, "`Id` must be provided")
	}

	addr, err := types.StringToAddress(in.Id.Address)
	if err != nil {
		return nil, fmt.Errorf("failed to parse in.Id.Address `%s`: %w", in.Id.Address, err)
	}
	s.smeshingProvider.SetCoinbase(addr)

	return &pb.SetCoinbaseResponse{
		Status: &rpcstatus.Status{Code: int32(code.Code_OK)},
	}, nil
}

// MinGas returns the current mingas setting of this node.
func (s *SmesherService) MinGas(context.Context, *emptypb.Empty) (*pb.MinGasResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// SetMinGas sets the mingas setting of this node.
func (s *SmesherService) SetMinGas(context.Context, *pb.SetMinGasRequest) (*pb.SetMinGasResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// EstimatedRewards returns estimated smeshing rewards over the next epoch.
func (s *SmesherService) EstimatedRewards(
	context.Context,
	*pb.EstimatedRewardsRequest,
) (*pb.EstimatedRewardsResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "this endpoint is not implemented")
}

// PostSetupStatus returns post data status.
func (s *SmesherService) PostSetupStatus(ctx context.Context, _ *emptypb.Empty) (*pb.PostSetupStatusResponse, error) {
	status := s.postSupervisor.Status()
	return &pb.PostSetupStatusResponse{Status: statusToPbStatus(status)}, nil
}

// PostSetupStatusStream exposes a stream of status updates during post setup.
func (s *SmesherService) PostSetupStatusStream(
	_ *emptypb.Empty,
	stream pb.SmesherService_PostSetupStatusStreamServer,
) error {
	timer := time.NewTicker(s.streamInterval)
	defer timer.Stop()

	for {
		select {
		case <-timer.C:
			status := s.postSupervisor.Status()
			if err := stream.Send(&pb.PostSetupStatusStreamResponse{Status: statusToPbStatus(status)}); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		case <-stream.Context().Done():
			return nil
		}
	}
}

// PostSetupProviders returns a list of available Post setup compute providers.
func (s *SmesherService) PostSetupProviders(
	ctx context.Context,
	in *pb.PostSetupProvidersRequest,
) (*pb.PostSetupProvidersResponse, error) {
	providers, err := s.postSupervisor.Providers()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get OpenCL providers: %v", err)
	}

	res := &pb.PostSetupProvidersResponse{}
	res.Providers = make([]*pb.PostSetupProvider, len(providers))
	for i, p := range providers {
		var hashesPerSec int
		if in.Benchmark {
			var err error
			hashesPerSec, err = s.postSupervisor.Benchmark(p)
			if err != nil {
				ctxzap.Error(ctx, "failed to benchmark provider", zap.Error(err))
				return nil, status.Error(codes.Internal, "failed to benchmark provider")
			}
		}

		res.Providers[i] = &pb.PostSetupProvider{
			Id:          uint32(p.ID),
			Model:       p.Model,
			DeviceType:  pb.PostSetupProvider_DeviceType(p.DeviceType),
			Performance: uint64(hashesPerSec),
		}
	}

	return res, nil
}

// PostConfig returns the Post protocol config.
func (s *SmesherService) PostConfig(context.Context, *emptypb.Empty) (*pb.PostConfigResponse, error) {
	cfg := s.postSupervisor.Config()

	return &pb.PostConfigResponse{
		BitsPerLabel:  config.BitsPerLabel,
		LabelsPerUnit: cfg.LabelsPerUnit,
		MinNumUnits:   cfg.MinNumUnits,
		MaxNumUnits:   cfg.MaxNumUnits,
		K1:            uint32(cfg.K1),
		K2:            uint32(cfg.K2),
	}, nil
}

func statusToPbStatus(status *activation.PostSetupStatus) *pb.PostSetupStatus {
	pbStatus := &pb.PostSetupStatus{}

	pbStatus.State = pb.PostSetupStatus_State(status.State) // assuming enum values match.
	pbStatus.NumLabelsWritten = status.NumLabelsWritten

	if status.LastOpts != nil {
		var providerID *uint32
		if status.LastOpts.ProviderID.Value() != nil {
			providerID = new(uint32)
			*providerID = *status.LastOpts.ProviderID.Value()
		}

		pbStatus.Opts = &pb.PostSetupOpts{
			DataDir:     status.LastOpts.DataDir,
			NumUnits:    status.LastOpts.NumUnits,
			MaxFileSize: status.LastOpts.MaxFileSize,
			ProviderId:  providerID,
			Throttle:    status.LastOpts.Throttle,
		}
	}

	return pbStatus
}
