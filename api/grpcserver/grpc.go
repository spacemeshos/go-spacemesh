package grpcserver

import (
	"fmt"
	"net"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// ServiceAPI allows individual grpc services to register the grpc server
type ServiceAPI interface {
	RegisterService(*Server)
}

// Server is a very basic grpc server
type Server struct {
	Port       int
	Interface  string
	GrpcServer *grpc.Server

	log log.Logger
}

// ServerOption type defines a callback that can set internal Server fields
type ServerOption func(s *Server)

// WithServerLogger set's the underlying logger to a custom logger.
// By default the logger is NoOp
func WithServerLogger(log log.Logger) ServerOption {
	return func(s *Server) {
		s.log = log
	}
}

// NewServerWithInterface creates and returns a new Server with port and interface
func NewServerWithInterface(port int, intfce string) *Server {
	return &Server{
		Port:       port,
		Interface:  intfce,
		GrpcServer: grpc.NewServer(ServerOptions...),
		log:        log.NewNop(),
	}
}

// NewServer creates and returns a new Server with port and interface and add custom options
func NewServer(port int, intfce string, options ...ServerOption) *Server {
	s := &Server{
		Port:       port,
		Interface:  intfce,
		GrpcServer: grpc.NewServer(ServerOptions...),
		log:        log.NewNop(),
	}
	for _, option := range options {
		option(s)
	}
	return s
}

// Start starts the server
func (s *Server) Start() {
	numServices := len(s.GrpcServer.GetServiceInfo())
	s.log.Info("starting new grpc server with %d registered service(s)", numServices)
	go s.startInternal()
}

// Blocking, should be called in a goroutine
func (s *Server) startInternal() {
	lis, err := net.Listen("tcp", fmt.Sprintf("%s:%d", s.Interface, s.Port))
	if err != nil {
		log.Error("error listening: %v", err)
		return
	}

	// SubscribeOnNewConnections reflection service on gRPC server
	reflection.Register(s.GrpcServer)

	// start serving - this blocks until err or server is stopped
	s.log.Info("starting new grpc server on %s:%d", s.Interface, s.Port)
	if err := s.GrpcServer.Serve(lis); err != nil {
		log.Error("error stopping grpc server: %v", err)
	}
}

// Close stops the server
func (s *Server) Close() {
	s.log.Info("Stopping new grpc server...")
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
