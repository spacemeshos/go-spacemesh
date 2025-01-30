package v2beta1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2beta1 "github.com/spacemeshos/api/release/go/spacemesh/v2beta1"
	"google.golang.org/grpc"
)

const (
	Smesher = "smesher_v2beta1"
)

func NewSmesherService(version, commit string) *SmesherService {
	return &SmesherService{
		appVersion: version,
		appCommit:  commit,
	}
}

type SmesherService struct {
	appVersion string
	appCommit  string
}

func (s *SmesherService) Version(ctx context.Context, _ *spacemeshv2beta1.VersionRequest) (*spacemeshv2beta1.VersionResponse, error) {
	return &spacemeshv2beta1.VersionResponse{
		Version: s.appVersion,
	}, nil
}

func (s *SmesherService) Build(ctx context.Context, _ *spacemeshv2beta1.BuildRequest) (*spacemeshv2beta1.BuildResponse, error) {
	return &spacemeshv2beta1.BuildResponse{
		Build: s.appCommit,
	}, nil
}

func (s *SmesherService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2beta1.RegisterSmesherServiceHandlerServer(context.Background(), mux, s)
}

func (s *SmesherService) RegisterService(server *grpc.Server) {
	spacemeshv2beta1.RegisterSmesherServiceServer(server, s)
}
