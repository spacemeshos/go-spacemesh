package grpcserver

import (
	"context"
	"errors"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/genproto/googleapis/rpc/code"
	rpcstatus "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// GatewayService exposes transaction data, and a submit tx endpoint.
type GatewayService struct {
	publisher api.Publisher
	verifier  api.ChallengeVerifier
}

// RegisterService registers this service with a grpc server instance.
func (s *GatewayService) RegisterService(server *Server) {
	pb.RegisterGatewayServiceServer(server.GrpcServer, s)
}

// NewGatewayService creates a new grpc service using config data.
func NewGatewayService(publisher api.Publisher, verifier api.ChallengeVerifier) *GatewayService {
	return &GatewayService{
		publisher: publisher,
		verifier:  verifier,
	}
}

// BroadcastPoet accepts a binary poet message to broadcast to the network.
func (s *GatewayService) BroadcastPoet(ctx context.Context, in *pb.BroadcastPoetRequest) (*pb.BroadcastPoetResponse, error) {
	log.Info("GRPC GatewayService.BroadcastPoet")

	if len(in.Data) == 0 {
		return nil, status.Error(codes.InvalidArgument, "`Data` payload empty")
	}

	// Note that we broadcast a poet message regardless of whether or not we are currently in sync
	if err := s.publisher.Publish(ctx, pubsub.PoetProofProtocol, in.Data); err != nil {
		log.Error("failed to broadcast poet message: %s", err)
		return nil, status.Errorf(codes.Internal, "failed to broadcast message")
	}

	log.Info("GRPC GatewayService.BroadcastPoet broadcast succeeded")

	return &pb.BroadcastPoetResponse{Status: &rpcstatus.Status{Code: int32(code.Code_OK)}}, nil
}

// VerifyChallenge implements v1.GatewayServiceServer.
func (s *GatewayService) VerifyChallenge(ctx context.Context, in *pb.VerifyChallengeRequest) (*pb.VerifyChallengeResponse, error) {
	log.Info("GRPC GatewayService.VerifyChallenge")
	result, err := s.verifier.Verify(ctx, in.Challenge, in.Signature)
	if err == nil {
		return &pb.VerifyChallengeResponse{Hash: result.Hash.Bytes(), NodeId: result.NodeID.Bytes()}, nil
	}

	if errors.Is(err, &activation.VerifyError{}) {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	return nil, status.Error(codes.InvalidArgument, err.Error())
}
