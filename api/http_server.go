package api

import (
	"flag"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"net/http"
	"strconv"

	gw "github.com/spacemeshos/go-spacemesh/api/pb"
)

// JSONHTTPServer is a JSON http server providing the Spacemesh API.
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway
type JSONHTTPServer struct {
	Port   uint
	server *http.Server
	ctx    context.Context
}

// NewJSONHTTPServer creates a new json http server
func NewJSONHTTPServer() *JSONHTTPServer {
	return &JSONHTTPServer{Port: config.ConfigValues.JSONServerPort}
}

// Stop stops the server
func (s JSONHTTPServer) Stop() {
	log.Info("Stopping json-http service...")

	// todo: fixme - this is panicking
	//s.server.Shutdown(s.ctx)
}

// StartService starts the json server.
func (s JSONHTTPServer) StartService(callback chan bool) {
	go s.startInternal(callback)
}

func (s JSONHTTPServer) startInternal(callback chan bool) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.ctx = ctx

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	portStr := strconv.Itoa(int(config.ConfigValues.GrpcServerPort))
	echoEndpoint := flag.String("api_endpoint", "localhost:"+portStr, "endpoint of api grpc service")

	if err := gw.RegisterSpaceMeshServiceHandlerFromEndpoint(ctx, mux, *echoEndpoint, opts); err != nil {
		log.Error("failed to register http endpoint with grpc", err)
	}

	addr := ":" + strconv.Itoa(int(s.Port))

	log.Info("json API listening on port %d", s.Port)

	if callback != nil {
		callback <- true
	}

	s.server = &http.Server{Addr: addr, Handler: mux}
	err := s.server.ListenAndServe()

	if err != nil {
		log.Info("listen and serve stopped with status. %v", err)
	}
}
