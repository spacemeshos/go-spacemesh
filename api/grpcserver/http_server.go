package grpcserver

import (
	"context"
	"fmt"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	gw "github.com/spacemeshos/api/release/go/spacemesh/v1"

	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	Port     int
	GrpcPort int
	server   *http.Server
}

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer(port int) *JSONHTTPServer {
	return &JSONHTTPServer{Port: port}
}

// Close stops the server.
func (s *JSONHTTPServer) Close() error {
	log.Debug("stopping new json-http service...")
	if s.server != nil {
		if err := s.server.Shutdown(cmdp.Ctx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
	}

	return nil
}

// StartService starts the json api server and listens for status (started, stopped).
func (s *JSONHTTPServer) StartService(
	ctx context.Context,
	services ...ServiceAPI,
) <-chan struct{} {
	started := make(chan struct{})

	// This will block, so run it in a goroutine
	go s.startInternal(
		ctx,
		started,
		services...)

	return started
}

func (s *JSONHTTPServer) startInternal(
	ctx context.Context,
	started chan<- struct{},
	services ...ServiceAPI) {
	ctx, cancel := context.WithCancel(ctx)

	// This will close all downstream connections when the server closes
	defer cancel()

	mux := runtime.NewServeMux()

	// register each individual, enabled service
	serviceCount := 0
	for _, svc := range services {
		var err error
		switch typed := svc.(type) {
		case *GatewayService:
			err = gw.RegisterGatewayServiceHandlerServer(ctx, mux, typed)
		case *GlobalStateService:
			err = gw.RegisterGlobalStateServiceHandlerServer(ctx, mux, typed)
		case *MeshService:
			err = gw.RegisterMeshServiceHandlerServer(ctx, mux, typed)
		case *NodeService:
			err = gw.RegisterNodeServiceHandlerServer(ctx, mux, typed)
		case *SmesherService:
			err = gw.RegisterSmesherServiceHandlerServer(ctx, mux, typed)
		case *TransactionService:
			err = gw.RegisterTransactionServiceHandlerServer(ctx, mux, typed)
		case *DebugService:
			err = gw.RegisterDebugServiceHandlerServer(ctx, mux, typed)
		}
		if err != nil {
			log.Error("error registering %T with grpc gateway, error %v", svc, err)
		}
		serviceCount++
	}

	close(started)

	// At least one service must be enabled
	if serviceCount == 0 {
		log.Error("not starting grpc gateway service; at least one service must be enabled")
		return
	}

	log.Info("starting grpc gateway server on port %d connected to grpc service", s.Port)
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}

	// This will block
	log.Error("error from grpc http listener: %v", s.server.ListenAndServe())
}
