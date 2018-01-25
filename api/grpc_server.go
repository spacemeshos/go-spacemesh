// Package api provides the local go-spacemesh API endpoints. e.g. json-http and grpc-http2
package api

import (
	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/log"
	"strconv"

	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// A grpc server implementing the Spacemesh API

// server is used to implement SpaceMeshService.Echo.
type SpaceMeshGrpcService struct {
	Server *grpc.Server
	Port   uint
}

func (s SpaceMeshGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{Value: in.Value}, nil
}

func (s SpaceMeshGrpcService) StopService() {
	log.Info("Stopping grpc service...")
	s.Server.Stop()
}

func NewGrpcService() *SpaceMeshGrpcService {
	port := config.ConfigValues.GrpcServerPort
	server := grpc.NewServer()
	return &SpaceMeshGrpcService{Server: server, Port: uint(port)}
}

func (s SpaceMeshGrpcService) StartService(started chan bool) {
	go s.startServiceInternal(started)
}

// This is a blocking method designed to be called using a go routine
func (s SpaceMeshGrpcService) startServiceInternal(started chan bool) {
	port := config.ConfigValues.GrpcServerPort
	addr := ":" + strconv.Itoa(int(port))

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen", err)
		return
	}

	pb.RegisterSpaceMeshServiceServer(s.Server, s)

	// Register reflection service on gRPC server
	reflection.Register(s.Server)

	log.Info("grpc API listening on port %d", port)

	if started != nil {
		started <- true
	}

	// start serving - this blocks until err or server is stopped
	if err := s.Server.Serve(lis); err != nil {
		log.Error("grpc stopped serving", err)
	}
}
