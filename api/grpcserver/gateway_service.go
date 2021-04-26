package grpcserver

import (
	"context"

	"github.com/spacemeshos/go-spacemesh/activation"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GatewayService exposes transaction data, and a submit tx endpoint
type GatewayService struct {
	Network api.NetworkAPI // P2P Swarm
}

// RegisterService registers this service with a grpc server instance
func (s GatewayService) RegisterService(server *Server) {
	pb.RegisterGatewayServiceServer(server.GrpcServer, s)
}

// NewGatewayService creates a new grpc service using config data.
func NewGatewayService(net api.NetworkAPI) *GatewayService {
	return &GatewayService{
		Network: net,
	}
}

// BroadcastPoet accepts a binary poet message to broadcast to the network
func (s GatewayService) BroadcastPoet(ctx context.Context, in *pb.BroadcastPoetRequest) (*pb.BroadcastPoetResponse, error) {
	log.Info("GRPC GatewayService.BroadcastPoet")

	if len(in.Data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "`Data` payload empty")
	}

	// Note that we broadcast a poet message regardless of whether or not we are currently in sync
	if err := s.Network.Broadcast(ctx, activation.PoetProofProtocol, in.Data); err != nil {
		log.Error("failed to broadcast poet message: %s", err)
		return nil, status.Errorf(codes.Internal, "failed to broadcast message")
	}
	log.Info("GRPC GatewayService.BroadcastPoet broadcast succeeded")

	return &pb.BroadcastPoetResponse{Status: &rpcstatus.Status{Code: int32(code.Code_OK)}}, nil
}
