package grpcserver

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

// PostInfoService provides information about connected PostServices.
type PostInfoService struct {
	log *zap.Logger

	builder atxBuilder
}

// RegisterService registers this service with a grpc server instance.
func (s *PostInfoService) RegisterService(server *grpc.Server) {
	pb.RegisterPostInfoServiceServer(server, s)
}

func (s *PostInfoService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return pb.RegisterPostInfoServiceHandlerServer(context.Background(), mux, s)
}

// String returns the name of this service.
func (s *PostInfoService) String() string {
	return "PostInfoService"
}

// NewPostInfoService creates a new instance of the post info grpc service.
func NewPostInfoService(log *zap.Logger, builder atxBuilder) *PostInfoService {
	return &PostInfoService{
		log:     log,
		builder: builder,
	}
}

func (s *PostInfoService) ServiceStates(context.Context, *pb.ServiceStatesRequest) (*pb.ServiceStatesResponse, error) {
	return &pb.ServiceStatesResponse{}, nil
}
