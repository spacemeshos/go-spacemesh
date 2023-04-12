package grpcserver

import (
	"net"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/spacemeshos/go-spacemesh/log"
)

// ServiceAPI allows individual grpc services to register the grpc server.
type ServiceAPI interface {
	RegisterService(*Server)
}

// Server is a very basic grpc server.
type Server struct {
	Listener   string
	GrpcServer *grpc.Server
}

// New creates and returns a new Server with port and interface.
func New(listener string, opts ...grpc.ServerOption) *Server {
	opts = append(opts, ServerOptions...)
	return &Server{
		Listener:   listener,
		GrpcServer: grpc.NewServer(opts...),
	}
}

// Start starts the server.
func (s *Server) Start() <-chan struct{} {
	log.With().Info("starting grpc server",
		log.String("address", s.Listener),
		log.Array("services", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for svc := range s.GrpcServer.GetServiceInfo() {
				encoder.AppendString(svc)
			}
			return nil
		})))

	started := make(chan struct{})
	go s.startInternal(started)

	return started
}

// Blocking, should be called in a goroutine.
func (s *Server) startInternal(started chan<- struct{}) {
	lis, err := net.Listen("tcp", s.Listener)
	if err != nil {
		log.Error("error listening: %v", err)
		return
	}
	reflection.Register(s.GrpcServer)
	log.Info("starting new grpc server on %s", s.Listener)
	close(started)
	if err := s.GrpcServer.Serve(lis); err != nil {
		log.Error("error stopping grpc server: %v", err)
	}
}

// Close stops the server.
func (s *Server) Close() error {
	log.Info("stopping the grpc server")
	s.GrpcServer.Stop()

	// We don't return any errors but we want to conform to io.Closer so return a nil error
	return nil
}

// ServerOptions are shared by all grpc servers.
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
