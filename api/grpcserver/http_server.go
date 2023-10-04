package grpcserver

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/log"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	listener string
	logger   log.Logger

	// BoundAddress contains the address that the server bound to, useful if
	// the server uses a dynamic port. It is set during startup and can be
	// safely accessed after Start has completed (I.E. the returned channel has
	// been waited on)
	BoundAddress string
	server       *http.Server
	grp          errgroup.Group
}

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer(listener string, lg log.Logger) *JSONHTTPServer {
	return &JSONHTTPServer{
		logger:   lg,
		listener: listener,
	}
}

// Shutdown stops the server.
func (s *JSONHTTPServer) Shutdown(ctx context.Context) error {
	s.logger.Debug("stopping json-http service...")
	if s.server != nil {
		err := s.server.Shutdown(ctx)
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}
	err := s.grp.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// StartService starts the json api server and listens for status (started, stopped).
func (s *JSONHTTPServer) StartService(
	ctx context.Context,
	services ...ServiceAPI,
) error {
	// At least one service must be enabled
	if len(services) == 0 {
		s.logger.Error("not starting grpc gateway service; at least one service must be enabled")
		return errors.New("no services provided")
	}
	ctx, cancel := context.WithCancel(ctx)

	// This will close all downstream connections when the server closes
	defer cancel()

	mux := runtime.NewServeMux()

	// register each individual, enabled service
	serviceCount := 0
	for _, svc := range services {
		var err error
		switch typed := svc.(type) {
		case *GlobalStateService:
			err = pb.RegisterGlobalStateServiceHandlerServer(ctx, mux, typed)
		case *MeshService:
			err = pb.RegisterMeshServiceHandlerServer(ctx, mux, typed)
		case *NodeService:
			err = pb.RegisterNodeServiceHandlerServer(ctx, mux, typed)
		case *SmesherService:
			err = pb.RegisterSmesherServiceHandlerServer(ctx, mux, typed)
		case *TransactionService:
			err = pb.RegisterTransactionServiceHandlerServer(ctx, mux, typed)
		case *DebugService:
			err = pb.RegisterDebugServiceHandlerServer(ctx, mux, typed)
		}
		if err != nil {
			s.logger.Error("registering %T with grpc gateway failed with %v", svc, err)
			return err
		}
		serviceCount++
	}

	s.logger.With().Info("starting grpc gateway server", log.String("address", s.listener))

	lis, err := net.Listen("tcp", s.listener)
	if err != nil {
		s.logger.Error("error listening: %v", err)
		return err
	}
	s.BoundAddress = lis.Addr().String()
	s.server = &http.Server{
		Handler: mux,
	}
	s.grp.Go(func() error {
		if err := s.server.Serve(lis); err != nil {
			s.logger.Error("error from grpc http server: %v", err)
			return nil
		}
		return nil
	})
	return nil
}
