package gateway

import (
	"fmt"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	gw "github.com/spacemeshos/api/release/go/spacemesh/v1"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	Port     int
	GrpcPort int
	server   *http.Server

	log log.Logger
}

// WithLogger set's the underlying logger to a custom logger. By default the logger is NoOp
func WithLogger(log log.Logger) Option {
	return func(j *JSONHTTPServer) {
		j.log = log
	}
}

// Option type definds an callback that can set internal JSONHTTPServer fields
type Option func(j *JSONHTTPServer)

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer(port int, grpcPort int, options ...Option) *JSONHTTPServer {
	server := &JSONHTTPServer{Port: port, GrpcPort: grpcPort, log: log.NewNop()}
	for _, option := range options {
		option(server)
	}
	return server
}

// Close stops the server.
func (j *JSONHTTPServer) Close() error {
	j.log.Debug("Stopping new json-http service...")
	if j.server != nil {
		if err := j.server.Shutdown(cmdp.Ctx); err != nil {
			return err
		}
	}
	return nil
}

// StartService starts the json api server and listens for status (started, stopped).
func (j *JSONHTTPServer) StartService(startNodeService bool, startMeshService bool,
	startGlobalStateService bool, startSmesherService bool, startTransactionService bool) {

	// This will block, so run it in a goroutine
	go j.startInternal(
		startNodeService, startMeshService, startGlobalStateService, startSmesherService,
		startTransactionService)
}

func (j *JSONHTTPServer) startInternal(startNodeService bool, startMeshService bool,
	startGlobalStateService bool, startSmesherService bool, startTransactionService bool) {
	ctx, cancel := context.WithCancel(cmdp.Ctx)

	// This will close all downstream connections when the server closes
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	jsonEndpoint := fmt.Sprintf("localhost:%d", j.GrpcPort)

	// register each individual, enabled service
	serviceCount := 0
	if startNodeService {
		if err := gw.RegisterNodeServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			j.log.Error("error registering NodeService with grpc gateway", err)
		} else {
			serviceCount++
			j.log.Info("registered NodeService with grpc gateway server")
		}
	}
	if startMeshService {
		if err := gw.RegisterMeshServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			j.log.Error("error registering MeshService with grpc gateway", err)
		} else {
			serviceCount++
			j.log.Info("registered MeshService with grpc gateway server")
		}
	}
	if startGlobalStateService {
		if err := gw.RegisterGlobalStateServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			j.log.Error("error registering GlobalStateService with grpc gateway", err)
		} else {
			serviceCount++
			j.log.Info("registered GlobalStateService with grpc gateway server")
		}
	}
	if startSmesherService {
		if err := gw.RegisterSmesherServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			j.log.Error("error registering SmesherService with grpc gateway", err)
		} else {
			serviceCount++
			j.log.Info("registered SmesherService with grpc gateway server")
		}
	}
	if startTransactionService {
		if err := gw.RegisterTransactionServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			j.log.Error("error registering TransactionService with grpc gateway", err)
		} else {
			serviceCount++
			j.log.Info("registered TransactionService with grpc gateway server")
		}
	}

	// At least one service must be enabled
	if serviceCount == 0 {
		j.log.Error("not starting grpc gateway service; at least one service must be enabled")
		return
	}

	j.log.Info("starting grpc gateway server on port %d connected to grpc service at %s", j.Port, jsonEndpoint)
	j.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", j.Port),
		Handler: mux,
	}

	// This will block
	j.log.Error("error from grpc http listener: %v", j.server.ListenAndServe())
}
