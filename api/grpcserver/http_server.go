package grpcserver

import (
	"fmt"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	gw "github.com/spacemeshos/api/release/go/spacemesh/v1"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	Port     int
	GrpcPort int
	server   *http.Server
	log      log.Logger
}

// JSONHTTPServerOption type defines a callback that can set internal JSONHTTPServer fields
type JSONHTTPServerOption func(j *JSONHTTPServer)

// WithJSONHTTPServerLogger set's the underlying logger to a custom logger.
// By default the logger is NoOp
func WithJSONHTTPServerLogger(log log.Logger) JSONHTTPServerOption {
	return func(j *JSONHTTPServer) {
		j.log = log
	}
}

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer(port int, grpcPort int, options ...JSONHTTPServerOption) *JSONHTTPServer {
	j := &JSONHTTPServer{Port: port, GrpcPort: grpcPort, log: log.NewNop()}
	for _, option := range options {
		option(j)
	}
	return j
}

// Close stops the server.
func (s *JSONHTTPServer) Close() error {
	s.log.Debug("Stopping new json-http service...")
	if s.server != nil {
		if err := s.server.Shutdown(cmdp.Ctx); err != nil {
			return err
		}
	}
	return nil
}

// StartService starts the json api server and listens for status (started, stopped).
func (s *JSONHTTPServer) StartService(startNodeService bool, startMeshService bool,
	startGlobalStateService bool, startSmesherService bool, startTransactionService bool) {

	// This will block, so run it in a goroutine
	go s.startInternal(
		startNodeService, startMeshService, startGlobalStateService, startSmesherService,
		startTransactionService)
}

func (s *JSONHTTPServer) startInternal(startNodeService bool, startMeshService bool,
	startGlobalStateService bool, startSmesherService bool, startTransactionService bool) {
	ctx, cancel := context.WithCancel(cmdp.Ctx)

	// This will close all downstream connections when the server closes
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	jsonEndpoint := fmt.Sprintf("localhost:%d", s.GrpcPort)

	// register each individual, enabled service
	serviceCount := 0
	if startNodeService {
		if err := gw.RegisterNodeServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			s.log.Error("error registering NodeService with grpc gateway", err)
		} else {
			serviceCount++
			s.log.Info("registered NodeService with grpc gateway server")
		}
	}
	if startMeshService {
		if err := gw.RegisterMeshServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			s.log.Error("error registering MeshService with grpc gateway", err)
		} else {
			serviceCount++
			s.log.Info("registered MeshService with grpc gateway server")
		}
	}
	if startGlobalStateService {
		if err := gw.RegisterGlobalStateServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			s.log.Error("error registering GlobalStateService with grpc gateway", err)
		} else {
			serviceCount++
			s.log.Info("registered GlobalStateService with grpc gateway server")
		}
	}
	if startSmesherService {
		if err := gw.RegisterSmesherServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			s.log.Error("error registering SmesherService with grpc gateway", err)
		} else {
			serviceCount++
			s.log.Info("registered SmesherService with grpc gateway server")
		}
	}
	if startTransactionService {
		if err := gw.RegisterTransactionServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			s.log.Error("error registering TransactionService with grpc gateway", err)
		} else {
			serviceCount++
			s.log.Info("registered TransactionService with grpc gateway server")
		}
	}

	// At least one service must be enabled
	if serviceCount == 0 {
		s.log.Error("not starting grpc gateway service; at least one service must be enabled")
		return
	}

	s.log.Info("starting grpc gateway server on port %d connected to grpc service at %s", s.Port, jsonEndpoint)
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}

	// This will block
	s.log.Error("error from grpc http listener: %v", s.server.ListenAndServe())
}
