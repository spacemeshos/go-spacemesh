package grpcserver

import (
	"context"
	"errors"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

// GatewayService exposes transaction data, and a submit tx endpoint.
type GatewayService struct {
	verifier api.ChallengeVerifier
}

// RegisterService registers this service with a grpc server instance.
func (s *GatewayService) RegisterService(server *Server) {
	pb.RegisterGatewayServiceServer(server.GrpcServer, s)
}

// NewGatewayService creates a new grpc service using config data.
func NewGatewayService(verifier api.ChallengeVerifier) *GatewayService {
	return &GatewayService{verifier: verifier}
}

// VerifyChallenge implements v1.GatewayServiceServer.
func (s *GatewayService) VerifyChallenge(ctx context.Context, in *pb.VerifyChallengeRequest) (*pb.VerifyChallengeResponse, error) {
	ctx = log.WithNewRequestID(ctx)
	var sig types.EdSignature
	copy(sig[:], in.Signature)
	result, err := s.verifier.Verify(ctx, in.Challenge, sig)
	if err == nil {
		return &pb.VerifyChallengeResponse{Hash: result.Hash.Bytes(), NodeId: result.NodeID.Bytes()}, nil
	}
	log.GetLogger().WithContext(ctx).With().Info("Challenge verification failed", log.Err(err))

	if errors.Is(err, &activation.VerifyError{}) {
		return nil, status.Error(codes.Unavailable, err.Error())
	}

	return nil, status.Error(codes.InvalidArgument, err.Error())
}
