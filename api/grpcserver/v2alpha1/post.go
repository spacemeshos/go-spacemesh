package v2alpha1

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	spacemeshv2alpha1 "github.com/spacemeshos/api/release/go/spacemesh/v2alpha1"
	"github.com/spacemeshos/post/config"
	"google.golang.org/grpc"

	"github.com/spacemeshos/go-spacemesh/activation"
)

const (
	Post = "post_v2alpha1"
)

func NewPostService(postCfg activation.PostConfig) *PostService {
	return &PostService{
		postConfig: postCfg,
	}
}

type PostService struct {
	postConfig activation.PostConfig
}

func (s *PostService) RegisterService(server *grpc.Server) {
	spacemeshv2alpha1.RegisterPostServiceServer(server, s)
}

func (s *PostService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2alpha1.RegisterPostServiceHandlerServer(context.Background(), mux, s)
}

func (s *PostService) String() string {
	return "PostService"
}

func (s *PostService) Config(
	_ context.Context,
	_ *spacemeshv2alpha1.PostConfigRequest,
) (*spacemeshv2alpha1.PostConfigResponse, error) {
	return &spacemeshv2alpha1.PostConfigResponse{
		BitsPerLabel:  config.BitsPerLabel,
		LabelsPerUnit: s.postConfig.LabelsPerUnit,
		MinNumUnits:   s.postConfig.MinNumUnits,
		MaxNumUnits:   s.postConfig.MaxNumUnits,
		K1:            uint32(s.postConfig.K1),
		K2:            uint32(s.postConfig.K2),
	}, nil
}
