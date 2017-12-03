package api

import (
	"github.com/UnrulyOS/go-unruly/api/pb"
	"github.com/UnrulyOS/go-unruly/log"
	"net"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

const (
	port = ":50051"
)

// server is used to implement UnrulyService.Echo.
type server struct{}

func (s *server) Echo(ctx context.Context, in *pb.SimpleMessage) (*pb.SimpleMessage, error) {
	return &pb.SimpleMessage{in.Value}, nil
}

func startGrpcServer() {
	lis, err := net.Listen("tcp", port)
	if err != nil {
		log.Error("failed to listen: %v", err)
		return
	}

	s := grpc.NewServer()
	pb.RegisterUnrulyServiceServer(s, &server{})
	// Register reflection service on gRPC server.
	reflection.Register(s)
	if err := s.Serve(lis); err != nil {
		log.Error("failed to serve: %v", err)
	}
}

// todo: implement http-rest endpoint here
