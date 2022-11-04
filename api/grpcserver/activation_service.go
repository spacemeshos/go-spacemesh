package grpcserver

import (
	context "context"

	v1 "github.com/spacemeshos/api/release/go/spacemesh/v1"
)

type ActivationService struct{}

// RegisterService implements ServiceAPI.
func (s *ActivationService) RegisterService(server *Server) {
	v1.RegisterActivationServiceServer(server.GrpcServer, s)
}

// Get implements v1.ActivationServiceServer.
func (*ActivationService) Get(context.Context, *v1.GetRequest) (*v1.GetResponse, error) {
	panic("unimplemented")
}
