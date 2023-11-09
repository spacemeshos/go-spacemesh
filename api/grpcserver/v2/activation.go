package v2

import (
	"context"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"google.golang.org/grpc"

	spacemeshv2 "github.com/spacemeshos/api/release/go/spacemesh/v2"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func NewActivationStreamService(db *sql.Database) *ActivationStreamService {
	return &ActivationStreamService{db: db}
}

type ActivationStreamService struct {
	db *sql.Database
}

var _ grpcserver.ServiceAPI = (*ActivationStreamService)(nil)

func (s *ActivationStreamService) RegisterService(server *grpc.Server) {
	spacemeshv2.RegisterActivationStreamServiceServer(server, s)
}

func (s *ActivationStreamService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2.RegisterActivationStreamServiceHandlerServer(context.Background(), mux, s)
}

func (s *ActivationStreamService) String() string {
	return "ActivationStreamService"
}

func NewActivationService(db *sql.Database) *ActivationService {
	return &ActivationService{db: db}
}

type ActivationService struct {
	db *sql.Database
}

var _ grpcserver.ServiceAPI = (*ActivationService)(nil)

func (s *ActivationService) RegisterService(server *grpc.Server) {
	spacemeshv2.RegisterActivationServiceServer(server, s)
}

func (s *ActivationService) RegisterHandlerService(mux *runtime.ServeMux) error {
	return spacemeshv2.RegisterActivationServiceHandlerServer(context.Background(), mux, s)
}

// String returns the service name.
func (s *ActivationService) String() string {
	return "ActivationService"
}
