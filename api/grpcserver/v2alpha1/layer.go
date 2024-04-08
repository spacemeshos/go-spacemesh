package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"google.golang.org/grpc"

	"github.com/spacemeshos/go-spacemesh/sql"
)

const (
	Layer       = "layer_v2alpha1"
	LayerStream = "layer_stream_v2alpha1"
)

func NewLayerStreamService(db sql.Executor) *LayerStreamService {
	return &LayerStreamService{db: db}
}

type LayerStreamService struct {
	db sql.Executor
}

func (s *LayerStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterLayerStreamServiceServer(server, s)
}

func (s *LayerStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterLayerStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *LayerStreamService) Stream(
	request *spacemeshv2alpha1.LayerStreamRequest,
	stream spacemeshv2alpha1.LayerStreamService_StreamServer,
) error {
	return nil
}

func (s *LayerStreamService) String() string {
	return "LayerStreamService"
}

func NewLayerService(db sql.Executor) *LayerService {
	return &LayerService{db: db}
}

type LayerService struct {
	db sql.Executor
}

func (s *LayerService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterLayerServiceServer(server, s)
}

func (s *LayerService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterLayerServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *LayerService) String() string {
	return "LayerService"
}

func (s *LayerService) List(
	ctx context.Context,
	request *spacemeshv2alpha1.LayerRequest,
) (*spacemeshv2alpha1.LayerList, error) {
	return &spacemeshv2alpha1.LayerList{}, nil
}
