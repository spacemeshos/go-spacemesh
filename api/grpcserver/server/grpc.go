package server

import (
	"fmt"
	"net"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// API allows individual grpc services to register the grpc server
type API interface {
	Register(*Server)
}

// Server is a very basic grpc server
type Server struct {
	// Port is the grpc server port listening on requests
	Port int

	// Interface is the nic interface name that the grpc server will bind to
	Interface string

	// GrpcServer pointer to a grpc.Server struct holding the underlying
	// fields to dispatch, serve grpc requests
	GrpcServer *grpc.Server

	options []grpc.ServerOption
	log     log.Logger
}

// Option type definds an callback that can set internal Server fields
type Option func(s *Server)

// WithLogger set's the underlying logger to a custom logger. By default the logger is NoOp
func WithLogger(log log.Logger) Option {
	return func(s *Server) {
		s.log = log
	}
}

// WithServerOptions set's the underlying grpc server options to use these cutom options
func WithServerOptions(options []grpc.ServerOption) Option {
	return func(s *Server) {
		s.options = options
	}
}

// WithInterface set's the interface that the underlying grpc server will bind to
// By default if no interface is specified the implementation will bind to the first bindable interface
// from the list of nics found much like ":8080" does in the std net package
func WithInterface(ifname string) Option {
	return func(s *Server) {
		s.Interface = ifname
	}
}

// New creates and returns a new grpc Server
func New(port int, options ...Option) *Server {
	s := &Server{
		Port: port,
		log:  log.NewNop(),
		options: []grpc.ServerOption{
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
		},
	}
	for _, option := range options {
		option(s)
	}
	s.GrpcServer = grpc.NewServer(s.options...)
	return s
}

// Start starts the server
func (s *Server) Start() {
	numServices := len(s.GrpcServer.GetServiceInfo())
	log.Info("starting new grpc server with %d registered service(s)", numServices)
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
