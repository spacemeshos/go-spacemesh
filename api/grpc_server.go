// Package api provides the local go-spacemesh API endpoints. e.g. json-http and grpc-http2
package api

import (
	"strconv"

	"github.com/spacemeshos/go-spacemesh/api/config"
	"github.com/spacemeshos/go-spacemesh/api/pb"
	"github.com/spacemeshos/go-spacemesh/log"

	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// SpaceMeshGrpcService is a grpc server providing the Spacemesh api
type SpaceMeshGrpcService struct {
	Server *grpc.Server
	Port   uint
}

// Echo returns the response for an echo api request
func (s SpaceMeshGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{Value: in.Value}, nil
}

// StopService stops the grpc service.
func (s SpaceMeshGrpcService) StopService() {
	log.Info("Stopping grpc service...")
	s.Server.Stop()
	log.Info("grpc service stopped...")

}

// NewGrpcService create a new grpc service using config data.
func NewGrpcService() *SpaceMeshGrpcService {
	port := config.ConfigValues.GrpcServerPort
	server := grpc.NewServer()
	return &SpaceMeshGrpcService{Server: server, Port: uint(port)}
}

// StartService starts the grpc service.
func (s SpaceMeshGrpcService) StartService(status chan bool) {
	go s.startServiceInternal(status)
}

// startServiceInternal is a blocking method designed to be called using a go routine.
func (s SpaceMeshGrpcService) startServiceInternal(status chan bool) {
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

	if status != nil {
		status <- true
	}

	// start serving - this blocks until err or server is stopped
	if err := s.Server.Serve(lis); err != nil {
		log.Error("grpc stopped serving", err)
	}

	if status != nil {
		status <- true
	}

}
