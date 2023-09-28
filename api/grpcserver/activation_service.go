package grpcserver

import (
	"context"
	"errors"
	"fmt"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/sql"
)

type activationService struct {
	goldenAtx   types.ATXID
	atxProvider atxProvider
}

func NewActivationService(atxProvider atxProvider, goldenAtx types.ATXID) *activationService {
	return &activationService{
		goldenAtx:   goldenAtx,
		atxProvider: atxProvider,
	}
}

// RegisterService implements ServiceAPI.
func (s *activationService) RegisterService(server *Server) {
	pb.RegisterActivationServiceServer(server.GrpcServer, s)
}

// Get implements v1.ActivationServiceServer.
func (s *activationService) Get(ctx context.Context, request *pb.GetRequest) (*pb.GetResponse, error) {
	if l := len(request.Id); l != types.ATXIDSize {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid ATX ID length (%d), expected (%d)", l, types.ATXIDSize))
	}

	atxId := types.ATXID(types.BytesToHash(request.Id))
	atx, err := s.atxProvider.GetFullAtx(atxId)
	if err != nil || atx == nil {
		ctxzap.Debug(ctx, "failed to get ATX",
			zap.Stringer("id", atxId),
			zap.Error(err),
		)
		return nil, status.Error(codes.NotFound, "id was not found")
	}
	proof, err := s.atxProvider.GetMalfeasanceProof(atx.SmesherID)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		ctxzap.Error(ctx, "failed to get malfeasance proof",
			zap.Stringer("smesher", atx.SmesherID),
			zap.Stringer("smesher", atx.SmesherID),
			zap.Stringer("id", atxId),
			zap.Error(err),
		)
		return nil, status.Error(codes.NotFound, "id was not found")
	}
	resp := &pb.GetResponse{
		Atx: convertActivation(atx),
	}
	if proof != nil {
		resp.MalfeasanceProof = events.ToMalfeasancePB(atx.SmesherID, proof, false)
	}
	return resp, nil
}

func (s *activationService) Highest(ctx context.Context, req *emptypb.Empty) (*pb.HighestResponse, error) {
	highest, err := s.atxProvider.MaxHeightAtx()
	if err != nil {
		return &pb.HighestResponse{
			Atx: &pb.Activation{
				Id: &pb.ActivationId{Id: s.goldenAtx.Bytes()},
			},
		}, nil
	}
	atx, err := s.atxProvider.GetFullAtx(highest)
	if err != nil || atx == nil {
		return nil, status.Error(codes.NotFound, fmt.Sprintf("atx id %v not found: %v", highest, err.Error()))
	}
	return &pb.HighestResponse{
		Atx: convertActivation(atx),
	}, nil
}
