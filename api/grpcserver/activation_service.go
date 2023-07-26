package grpcserver

import (
	"context"
	"fmt"

	"github.com/golang/protobuf/ptypes/empty"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type activationService struct {
	logger      log.Logger
	goldenAtx   types.ATXID
	atxProvider atxProvider
}

func NewActivationService(atxProvider atxProvider, goldenAtx types.ATXID, lg log.Logger) *activationService {
	return &activationService{
		logger:      lg,
		goldenAtx:   goldenAtx,
		atxProvider: atxProvider,
	}
}

// RegisterService implements ServiceAPI.
func (s *activationService) RegisterService(server *Server) {
	s.logger.Info("registering GRPC Activation Service")
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
		s.logger.With().Debug("failed to get the ATX", log.Err(err), log.Stringer("id", atxId))
		return nil, status.Error(codes.NotFound, "id was not found")
	}
	return &pb.GetResponse{
		Atx: convertActivation(atx),
	}, nil
}

func (s *activationService) Highest(ctx context.Context, req *empty.Empty) (*pb.HighestResponse, error) {
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
