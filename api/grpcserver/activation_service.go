package grpcserver

import (
	"context"
	"fmt"

	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
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
	log.Info("registering GRPC Activation Service")
	pb.RegisterActivationServiceServer(server.GrpcServer, s)
}

// Get implements v1.ActivationServiceServer.
func (s *activationService) Get(ctx context.Context, request *pb.GetRequest) (*pb.GetResponse, error) {
	var atxId types.ATXID
	l := len(request.Id)
	if l == 0 {
		id, err := s.atxProvider.MaxHeightAtx()
		if err != nil {
			return &pb.GetResponse{
				Atx: &pb.Activation{
					Id: &pb.ActivationId{Id: s.goldenAtx.Bytes()},
				},
			}, nil
		}
		atxId = id
	} else if l != types.ATXIDSize {
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid ATX ID length (%d), expected (%d)", l, types.ATXIDSize))
	} else {
		atxId = types.ATXID(types.BytesToHash(request.Id))
	}

	logger := log.GetLogger().WithFields(log.Stringer("id", atxId))

	atx, err := s.atxProvider.GetFullAtx(atxId)
	if err != nil || atx == nil {
		logger.With().Debug("failed to get the ATX", log.Err(err))
		return nil, status.Error(codes.NotFound, "id was not found")
	}

	id := atx.ID()
	if atxId != id {
		logger.With().Error("ID of the received ATX is different than requested", log.Stringer("received ID", id))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &pb.GetResponse{
		Atx: convertActivation(atx),
	}, nil
}
