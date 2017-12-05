package api

import (
	"github.com/UnrulyOS/go-unruly/api/config"
	"github.com/UnrulyOS/go-unruly/api/pb"
	"github.com/UnrulyOS/go-unruly/log"
	"strconv"

	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// A grpc server implementing the Unruly API

// server is used to implement UnrulyService.Echo.
type UnrulyGrpcService struct {
	Server *grpc.Server
	Port   uint
}

func (s UnrulyGrpcService) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{in.Value}, nil
}

func (s UnrulyGrpcService) StopService() {
	s.Server.Stop()
}

func NewGrpcService() *UnrulyGrpcService {
	port := config.ConfigValues.GrpcServerPort
	server := grpc.NewServer()
	return &UnrulyGrpcService{Server: server, Port: port}
}

// This is a blocking method designed to be called using a go routine
func (s UnrulyGrpcService) StartService() {

	port := config.ConfigValues.GrpcServerPort
	addr := ":" + strconv.Itoa(int(port))

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to listen: %v", err)
		return
	}

	pb.RegisterUnrulyServiceServer(s.Server, s)

	// Register reflection service on gRPC server
	reflection.Register(s.Server)

	log.Info("grpc API listening on port %d", port)

	// start serving - this blocks until err or server is stopped
	if err := s.Server.Serve(lis); err != nil {
		log.Error("failed to serve grpc: %v", err)
	}
}
