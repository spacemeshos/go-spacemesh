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
	Port uint
	server *http.Server
	ctx context.Context
}

func NewJsonHttpServer() *JsonHttpServer {
	return &JsonHttpServer{Port: config.ConfigValues.JsonServerPort}
}

func (s JsonHttpServer) Stop() {
	log.Info("Stopping json-http service...")

	// todo: fixme - this is panicing
	//s.server.Shutdown(s.ctx)
}

// Start the grpc server
func (s JsonHttpServer) StartService(callback chan bool) {
	go s.startInternal(callback)
}

func (s JsonHttpServer) startInternal(callback chan bool) {
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
