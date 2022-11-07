package grpcserver

import (
	context "context"
	"fmt"

	v1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/spacemeshos/go-spacemesh/api"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

type activationService struct {
	atxProvider api.AtxProvider
}

func NewActivationService(atxProvider api.AtxProvider) *activationService {
	return &activationService{
		atxProvider: atxProvider,
	}
}

// RegisterService implements ServiceAPI.
func (s *activationService) RegisterService(server *Server) {
	log.Info("registering GRPC Activation Service")
	v1.RegisterActivationServiceServer(server.GrpcServer, s)
}

// Get implements v1.ActivationServiceServer.
func (s *activationService) Get(ctx context.Context, request *v1.GetRequest) (*v1.GetResponse, error) {
	logger := log.GetLogger().WithFields(log.Binary("id", request.Id))

	if l := len(request.Id); l != types.ATXIDSize {
		logger.Debug("invalid ATX ID length")
		return nil, status.Error(codes.InvalidArgument, fmt.Sprintf("invalid ATX ID length (%d), expected (%d)", l, types.ATXIDSize))
	}

	atxId := types.ATXID(types.BytesToHash(request.Id))
	atx, err := s.atxProvider.GetFullAtx(atxId)
	if err != nil || atx == nil {
		logger.Debug("failed to get the ATX (%v)", err)
		return nil, status.Error(codes.NotFound, "id was not found")
	}

	id := atx.ID()
	if atxId != id {
		logger.With().Error("ID of the received ATX is different than requested", log.Binary("received ID", id.Bytes()))
		return nil, status.Error(codes.Internal, "internal error")
	}

	return &v1.GetResponse{
		Atx: convertActivation(atx),
	}, nil
}
