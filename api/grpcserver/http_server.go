package grpcserver

import (
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"net/http"
	"strconv"

	gw "github.com/spacemeshos/api/release/go/spacemesh/v1"
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
func (s *JSONHTTPServer) StartService() {
	go s.startInternal()
}

func (s *JSONHTTPServer) startInternal() {
	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	grpcPortStr := strconv.Itoa(s.GrpcPort)

	echoEndpoint := "localhost:" + grpcPortStr
	if err := gw.RegisterNodeServiceHandlerFromEndpoint(context.Background(), mux, echoEndpoint, opts); err != nil {
		log.Error("failed to register http endpoint with grpc", err)
	}

	addr := ":" + strconv.Itoa(s.Port)

	log.Info("new json API listening on port %d", s.Port)

	s.server = &http.Server{Addr: addr, Handler: mux}
	err := s.server.ListenAndServe()

	if err != nil {
		log.Debug("listen and serve stopped with status: %v", err)
	}
}
