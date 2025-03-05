package v1

import (
	"context"
	"fmt"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/events"
)

type SmeshingServiceDebugService struct {
	loggers map[string]*zap.AtomicLevel
}

func (s *SmeshingServiceDebugService) Path() string {
	return "/spacemesh.v1.DebugService/"
}

// RegisterService registers this service with a grpc server instance.
func (d *SmeshingServiceDebugService) RegisterService(server *grpc.Server) {
	pb.RegisterDebugServiceServer(server, d)
}

func (d *SmeshingServiceDebugService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterDebugServiceHandlerServer(context.Background(), mux, d)
}

// NewSmeshingServiceDebugService creates a new grpc service using config data.
func NewSmeshingServiceDebugService(loggers map[string]*zap.AtomicLevel) *SmeshingServiceDebugService {
	return &SmeshingServiceDebugService{
		loggers: loggers,
	}
}

// Accounts returns current counter and balance for all accounts.
func (d *SmeshingServiceDebugService) Accounts(
	ctx context.Context,
	in *pb.AccountsRequest,
) (*pb.AccountsResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method Accounts not implemented")
}

// NetworkInfo query provides NetworkInfoResponse.
func (d *SmeshingServiceDebugService) NetworkInfo(
	ctx context.Context,
	_ *emptypb.Empty,
) (*pb.NetworkInfoResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method NetworkInfo not implemented")
}

// ActiveSet query provides hare active set for the specified epoch.
func (d *SmeshingServiceDebugService) ActiveSet(
	ctx context.Context,
	req *pb.ActiveSetRequest,
) (*pb.ActiveSetResponse, error) {
	return nil, status.Error(codes.Unimplemented, "method ActiveSet not implemented")
}

// ProposalsStream streams all proposals confirmed by hare.
func (d *SmeshingServiceDebugService) ProposalsStream(
	_ *emptypb.Empty,
	stream pb.DebugService_ProposalsStreamServer,
) error {
	sub := events.SubscribeProposals()
	if sub == nil {
		return status.Errorf(codes.FailedPrecondition, "event reporting is not enabled")
	}
	eventch, fullch := consumeEvents[events.EventProposal](stream.Context(), sub)
	// send empty header after subscribing to the channel.
	// this is optional but allows subscriber to wait until stream is fully initialized.
	if err := stream.SendHeader(metadata.MD{}); err != nil {
		return status.Errorf(codes.Unavailable, "can't send header")
	}
	for {
		select {
		case <-stream.Context().Done():
			return nil
		case <-fullch:
			return status.Errorf(codes.Canceled, "buffer is full")
		case ev := <-eventch:
			if err := stream.Send(castEventProposal(&ev)); err != nil {
				return fmt.Errorf("send to stream: %w", err)
			}
		}
	}
}

func (d *SmeshingServiceDebugService) ChangeLogLevel(
	ctx context.Context,
	req *pb.ChangeLogLevelRequest,
) (*emptypb.Empty, error) {
	level, err := zap.ParseAtomicLevel(req.GetLevel())
	if err != nil {
		return nil, fmt.Errorf("parse level: %w", err)
	}

	if req.GetModule() == "*" {
		for _, logger := range d.loggers {
			logger.SetLevel(level.Level())
		}
		return nil, nil
	}

	logger, ok := d.loggers[req.GetModule()]
	if !ok {
		return nil, fmt.Errorf("cannot find logger %v", req.GetModule())
	}

	logger.SetLevel(level.Level())

	return &emptypb.Empty{}, nil
}
