package api

import (
	"flag"
	"github.com/UnrulyOS/go-unruly/api/config"
	"github.com/UnrulyOS/go-unruly/log"
	"github.com/grpc-ecosystem/grpc-gateway/runtime"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"net/http"
	"strconv"

	gw "github.com/UnrulyOS/go-unruly/api/pb"
)

// A json http server providing the Unruly API.
// Implemented as a grpc gateway. See https://github.com/grpc-ecosystem/grpc-gateway

type JsonHttpServer struct {
	Port uint
}

func NewJsonHttpServer() *JsonHttpServer {
	return &JsonHttpServer{Port: config.ConfigValues.JsonServerPort}
}

func (s JsonHttpServer) Stop() {
	// todo: how to stop http from listening on the address?
}

// This blocks - call using a go routine
func (s JsonHttpServer) Start() {

	ctx := context.Background()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	mux := runtime.NewServeMux()
	opts := []grpc.DialOption{grpc.WithInsecure()}

	// register the http server on the local grpc server
	portStr := strconv.Itoa(int(config.ConfigValues.GrpcServerPort))
	echoEndpoint := flag.String("api_endpoint", "localhost:"+portStr, "endpoint of api grpc service")

	if err := gw.RegisterUnrulyServiceHandlerFromEndpoint(ctx, mux, *echoEndpoint, opts); err != nil {
		log.Error("Failed to register http endpoint with grpc: %v", err)
	}

	addr := ":" + strconv.Itoa(int(s.Port))

	log.Info("Json API listening on port %d", s.Port)

	// this blocks until stops
	err := http.ListenAndServe(addr, mux)

	if err != nil {
		log.Error("Failed to listen and serve: v%", err)
	}
}
