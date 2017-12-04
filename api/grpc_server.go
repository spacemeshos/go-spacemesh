package api

import (
	"github.com/UnrulyOS/go-unruly/api/config"
	"github.com/UnrulyOS/go-unruly/api/pb"
	"github.com/UnrulyOS/go-unruly/log"

	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// A grpc server implementing the Unruly API

// server is used to implement UnrulyService.Echo.
type UnrulyGrpcService struct{
	Server *grpc.Server
	Port uint
}

var Service *UnrulyGrpcService

func (s *UnrulyGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{in.Value}, nil
}

func StartGrpcServer() error {

	port := config.ConfigValues.GrpcServerPort
	addr := ":" + string(port)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen: %v", err)
		return err
	}

	server := grpc.NewServer()

	Service = &UnrulyGrpcService{Server:server, Port:port}

	pb.RegisterUnrulyServiceServer(server, Service)

	// Register reflection service on gRPC server
	reflection.Register(server)
	if err := server.Serve(lis); err != nil {
		log.Error("failed to serve grpc: %v", err)
	}

	return err
}
