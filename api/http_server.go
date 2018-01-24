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

// A json http server providing the Spacemesh API.
// Implemented as a grpc gateway.
// See https://github.com/grpc-ecosystem/grpc-gateway

type JsonHttpServer struct {
	Port   uint
	server *http.Server
	ctx    context.Context
	stop   chan bool
}

func NewJsonHttpServer() *JsonHttpServer {
	return &JsonHttpServer{Port: config.ConfigValues.JsonServerPort, stop: make(chan bool)}
}

// Send a stop signal to the listener
func (s JsonHttpServer) StopService() {
	s.stop <- true
}

// Listens on gracefully stopping the server in the same routine
func (s JsonHttpServer) listenStop() {
	<-s.stop
	log.Info("Shutting down json API server...")
	if err := s.server.Shutdown(s.ctx); err != nil {
		log.Error("Error during shutdown json API server : %v", err)
	}
}

// Start the json http API server and listen for status (started, stopped)
func (s JsonHttpServer) StartService(status chan bool) {
	go s.startInternal(status)
}

func (s JsonHttpServer) startInternal(status chan bool) {
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

	go func() { s.listenStop() }()

	if status != nil {
		status <- true
	}

	s.server = &http.Server{Addr: addr, Handler: mux}
	err := s.server.ListenAndServe()

	if err != nil {
		log.Info("listen and serve stopped with status. %v", err)
	}

	if status != nil {
		status <- true
	}
}
