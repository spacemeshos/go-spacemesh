package grpcserver

import (
	"fmt"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	gw "github.com/spacemeshos/api/release/go/spacemesh/v1"
	cmdp "github.com/spacemeshos/go-spacemesh/cmd"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"net/http"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
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
	log.Debug("Stopping new json-http service...")
	if s.server != nil {
		if err := s.server.Shutdown(context.TODO()); err != nil {
			return err
		}
	}
	return nil
}

// StartService starts the json api server and listens for status (started, stopped).
func (s *JSONHTTPServer) StartService(startNodeService bool, startMeshService bool) {
	go s.startInternal(startNodeService, startMeshService)
}

func (s *JSONHTTPServer) startInternal(startNodeService bool, startMeshService bool) {
	ctx, cancel := context.WithCancel(cmdp.Ctx)
	defer cancel()
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	jsonEndpoint := fmt.Sprintf("localhost:%d", s.GrpcPort)

	// register each individual, enabled service
	if startNodeService {
		if err := gw.RegisterNodeServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering NodeService with grpc gateway", err)
		}
		log.Info("registered NodeService with grpc gateway server")
	}
	if startMeshService {
		if err := gw.RegisterMeshServiceHandlerFromEndpoint(ctx, mux, jsonEndpoint, opts); err != nil {
			log.Error("error registering MeshService with grpc gateway", err)
		}
		log.Info("registered MeshService with grpc gateway server")
	}

	log.Info("starting grpc gateway server on port %d connected to grpc service at %s", s.Port, jsonEndpoint)
	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.Port),
		Handler: mux,
	}

	// This call is blocking, and only returns an error
	log.Error("error from grpc http listener: %v", s.server.ListenAndServe())
}
