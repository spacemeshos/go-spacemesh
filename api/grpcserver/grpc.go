package grpcserver

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"net"
	"strconv"
	"time"
)

// ServiceAPI
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
func (s *Server) Start() {
	log.Info("starting new grpc server")
	go s.startServiceInternal()
}

// This is a blocking method designed to be called using a go routine
func (s *Server) startServiceInternal() {
	addr := ":" + strconv.Itoa(s.Port)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to start grpc server: %v", err)
		return
	}

	// SubscribeOnNewConnections reflection service on gRPC server
	reflection.Register(s.GrpcServer)

	log.Info("new grpc server listening on port %d", s.Port)

	// start serving - this blocks until err or server is stopped
	if err := s.GrpcServer.Serve(lis); err != nil {
		log.Error("error stopping grpc server: %v", err)
	}
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
