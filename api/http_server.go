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
// It is implemented using a grpc-gateway. See https://github.com/grpc-ecosystem/grpc-gateway .
type JSONHTTPServer struct {
	Port   uint
	server *http.Server
	ctx    context.Context
	stop   chan bool
}

// NewJSONHTTPServer creates a new json http server.
func NewJSONHTTPServer() *JSONHTTPServer {
	return &JSONHTTPServer{Port: uint(config.ConfigValues.JSONServerPort), stop: make(chan bool)}
}

// StopService stops the server.
func (s JSONHTTPServer) StopService() {
	log.Debug("Stopping json-http service...")
	s.stop <- true
}

// Listens on gracefully stopping the server in the same routine.
func (s JSONHTTPServer) listenStop() {
	<-s.stop
	log.Debug("Shutting down json API server...")
	if err := s.server.Shutdown(s.ctx); err != nil {
		log.Error("Error during shutdown json API server : %v", err)
	}
}

// StartService starts the json api server and listens for status (started, stopped).
func (s JSONHTTPServer) StartService(status chan bool) {
	go s.startInternal(status)
}

func (s JSONHTTPServer) startInternal(status chan bool) {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.ctx = ctx

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	portStr := strconv.Itoa(int(config.ConfigValues.GrpcServerPort))

	const endpoint = "api_endpoint"
	var echoEndpoint string

	fl := flag.Lookup(endpoint)
	if fl != nil {
		flag.Set(endpoint,"localhost:"+portStr)
		echoEndpoint = fl.Value.String()
	} else {
		echoEndpoint = *flag.String(endpoint, "localhost:"+portStr, "endpoint of api grpc service")
	}
	if err := gw.RegisterSpaceMeshServiceHandlerFromEndpoint(ctx, mux, echoEndpoint, opts); err != nil {
		log.Error("failed to register http endpoint with grpc", err)
	}

	addr := ":" + strconv.Itoa(int(s.Port))

	log.Debug("json API listening on port %d", s.Port)

	go func() { s.listenStop() }()

	if status != nil {
		status <- true
	}

	s.server = &http.Server{Addr: addr, Handler: mux}
	err := s.server.ListenAndServe()

	if err != nil {
		log.Debug("listen and serve stopped with status. %v", err)
	}

	if status != nil {
		status <- true
	}
}
