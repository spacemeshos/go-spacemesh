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

// A json http server providing the SpaceMesh API.
// Implemented as a grpc gateway.
// See https://github.com/grpc-ecosystem/grpc-gateway

// todo: add http.server and support graceful shutdown
type JsonHttpServer struct {
	Port uint
}

func NewJsonHttpServer() *JsonHttpServer {
	return &JsonHttpServer{Port: config.ConfigValues.JsonServerPort}
}

func (s JsonHttpServer) Stop() {
	// todo: how to stop http from listening on the address?
	log.Info("Stopping json-http service...")
}

// Start the grpc server
func (s JsonHttpServer) StartService() {
	go s.startInternal()
}

func (s JsonHttpServer) startInternal() {
	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	portStr := strconv.Itoa(int(config.ConfigValues.GrpcServerPort))
	echoEndpoint := flag.String("api_endpoint", "localhost:"+portStr, "endpoint of api grpc service")

	if err := gw.RegisterSpaceMeshServiceHandlerFromEndpoint(ctx, mux, *echoEndpoint, opts); err != nil {
		log.Error("failed to register http endpoint with grpc: %v", err)
	}

	addr := ":" + strconv.Itoa(int(s.Port))

	log.Info("json API listening on port %d", s.Port)

	// this blocks until stops
	err := http.ListenAndServe(addr, mux)

	if err != nil {
		log.Error("failed to listen and serve: v%", err)
	}
}
