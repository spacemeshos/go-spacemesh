package grpcserver

import (
	"context"
	"fmt"
	"net/http"
	"sync"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	gw "github.com/spacemeshos/api/release/go/spacemesh/v1"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	mu       sync.RWMutex
	Port     int
	GrpcPort int
	server   *http.Server
}

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer(port int, grpcPort int) *JSONHTTPServer {
	return &JSONHTTPServer{Port: port, GrpcPort: grpcPort}
}

// Close stops the server.
func (s *JSONHTTPServer) Close() error {
	log.Debug("stopping new json-http service...")
	server := s.getServer()
	if server != nil {
		cmdp.Mu.RLock()
		ctx := cmdp.Ctx
		cmdp.Mu.RUnlock()

		if err := server.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}

// StartService starts the json api server and listens for status (started, stopped).
func (s *JSONHTTPServer) StartService(
	ctx context.Context,
	startDebugService bool,
	startGatewayService bool,
	startGlobalStateService bool,
	startMeshService bool,
	startNodeService bool,
	startSmesherService bool,
	startTransactionService bool,
) <-chan struct{} {
	started := make(chan struct{})

	// This will block, so run it in a goroutine
	go s.startInternal(
		ctx,
		started,
		startDebugService,
		startGatewayService,
		startGlobalStateService,
		startMeshService,
		startNodeService,
		startSmesherService,
		startTransactionService)

	return started
}

func (s *JSONHTTPServer) startInternal(
	ctx context.Context,
	started chan<- struct{},
	startDebugService bool,
	startGatewayService bool,
	startGlobalStateService bool,
	startMeshService bool,
	startNodeService bool,
	startSmesherService bool,
	startTransactionService bool) {
	ctx, cancel := context.WithCancel(ctx)

	// This will close all downstream connections when the server closes
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	jsonEndpoint := fmt.Sprintf("localhost:%d", s.GrpcPort)

	// register each individual, enabled service
	serviceCount := 0
	if startGatewayService {
		if err := gw.RegisterGatewayServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering GatewayService with grpc gateway", err)
		} else {
			serviceCount++
			log.Info("registered GatewayService with grpc gateway server")
		}
	}
	if startGlobalStateService {
		if err := gw.RegisterGlobalStateServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering GlobalStateService with grpc gateway", err)
		} else {
			serviceCount++
			log.Info("registered GlobalStateService with grpc gateway server")
		}
	}
	if startMeshService {
		if err := gw.RegisterMeshServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering MeshService with grpc gateway", err)
		} else {
			serviceCount++
			log.Info("registered MeshService with grpc gateway server")
		}
	}
	if startNodeService {
		if err := gw.RegisterNodeServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering NodeService with grpc gateway", err)
		} else {
			serviceCount++
			log.Info("registered NodeService with grpc gateway server")
		}
	}
	if startSmesherService {
		if err := gw.RegisterSmesherServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering SmesherService with grpc gateway", err)
		} else {
			serviceCount++
			log.Info("registered SmesherService with grpc gateway server")
		}
	}
	if startTransactionService {
		if err := gw.RegisterTransactionServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering TransactionService with grpc gateway", err)
		} else {
			serviceCount++
			log.Info("registered TransactionService with grpc gateway server")
		}
	}
	if startDebugService {
		if err := gw.RegisterDebugServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering DebugService with grpc gateway", err)
		} else {
			serviceCount++
			log.Info("registered DebugService with grpc gateway server")
		}
	}

	close(started)

	// At least one service must be enabled
	if serviceCount == 0 {
		log.Error("not starting grpc gateway service; at least one service must be enabled")
		return
	}

	log.Info("starting grpc gateway server on port %d connected to grpc service at %s", s.Port, jsonEndpoint)
	s.setServer(&http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
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
