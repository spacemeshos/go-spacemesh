package grpcserver

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	pb "github.com/spacemeshos/api/release/go/spacemesh/v1"

	"github.com/spacemeshos/go-spacemesh/log"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	mu     sync.RWMutex
	port   int
	server *http.Server
}

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer(port int) *JSONHTTPServer {
	return &JSONHTTPServer{port: port}
}

// Shutdown stops the server.
func (s *JSONHTTPServer) Shutdown(ctx context.Context) error {
	log.Debug("stopping new json-http service...")
	server := s.getServer()
	if server != nil {
		if err := server.Shutdown(ctx); err != nil {
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
	services ...ServiceAPI,
) {
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
			err = pb.RegisterGatewayServiceHandlerServer(ctx, mux, typed)
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
			log.Error("registering %T with grpc gateway failed with %v", svc, err)
		}
		serviceCount++
	}

	close(started)

	// At least one service must be enabled
	if serviceCount == 0 {
		log.Error("not starting grpc gateway service; at least one service must be enabled")
		return
	}

	log.Info("starting grpc gateway server on port %d", s.port)
	s.setServer(&http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	})

	// This will block
	log.Error("error from grpc http listener: %v", s.getServer().ListenAndServe())
}

func (s *JSONHTTPServer) getServer() *http.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.server
}

func (s *JSONHTTPServer) setServer(server *http.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.server = server
}
