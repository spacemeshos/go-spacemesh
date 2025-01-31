package v2beta1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2beta1 "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"google.golang.org/grpc"
)

const (
	Smeshing = "smeshing_v2beta1"
)

func NewSmeshingService(version, commit string) *SmeshingService {
	return &SmeshingService{
		appVersion: version,
		appCommit:  commit,
	}
}

type SmeshingService struct {
	appVersion string
	appCommit  string
}

func (s *SmeshingService) Version(ctx context.Context, _ *spacemeshv2beta1.SmeshingVersionRequest) (*spacemeshv2beta1.SmeshingVersionResponse, error) {
	return &spacemeshv2beta1.SmeshingVersionResponse{
		Version: s.appVersion,
	}, nil
}

func (s *SmeshingService) Build(ctx context.Context, _ *spacemeshv2beta1.SmeshingBuildRequest) (*spacemeshv2beta1.SmeshingBuildResponse, error) {
	return &spacemeshv2beta1.SmeshingBuildResponse{
		Build: s.appCommit,
	}, nil
}

func (s *SmeshingService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2beta1.RegisterSmeshingServiceHandlerServer(context.Background(), mux, s)
}

func (s *SmeshingService) RegisterService(server *grpc.Server) {
	spacemeshv2beta1.RegisterSmeshingServiceServer(server, s)
}
