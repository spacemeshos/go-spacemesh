package grpcserver

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"net"
	"time"
)

// ServiceAPI allows individual grpc services to register the grpc server
type ServiceAPI interface {
	RegisterService(*Server)
}

// Server is a very basic grpc server
type Server struct {
	Port       int
	GrpcServer *grpc.Server
}

// NewServer creates and returns a new Server
func NewServer(port int) *Server {
	return &Server{
		Port:       port,
		GrpcServer: grpc.NewServer(ServerOptions...),
	}
}

// Start starts the server
func (s *Server) Start() error {
	log.Info("starting new grpc server")

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Port))
	if err != nil {
		return err
	}

	// SubscribeOnNewConnections reflection service on gRPC server
	reflection.Register(s.GrpcServer)

	// start serving - this blocks until err or server is stopped
	go func() {
		log.Info("starting new grpc server on port %d", s.Port)
		if err := s.GrpcServer.Serve(lis); err != nil {
			log.Error("error stopping grpc server: %v", err)
		}
	}()

	return nil
}

// Close stops the server
func (s *Server) Close() {
	log.Info("Stopping new grpc server...")
	s.GrpcServer.Stop()
}

// ServerOptions are shared by all grpc servers
var ServerOptions = []grpc.ServerOption{
	// XXX: this is done to prevent routers from cleaning up our connections (e.g aws load balances..)
	// TODO: these parameters work for now but we might need to revisit or add them as configuration
	// TODO: Configure maxconns, maxconcurrentcons ..
	grpc.KeepaliveParams(keepalive.ServerParameters{
		MaxConnectionIdle:     time.Minute * 120,
		MaxConnectionAge:      time.Minute * 180,
		MaxConnectionAgeGrace: time.Minute * 10,
		Time:                  time.Minute,
		Timeout:               time.Minute * 3,
	}),
}
