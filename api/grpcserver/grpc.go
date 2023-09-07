package grpcserver

import (
	"net"
	"time"

	"golang.org/x/sync/errgroup"
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
	Listener string
	logger   log.Logger
	// BoundAddress contains the address that the server bound to, useful if
	// the server uses a dynamic port. It is set during startup and can be
	// safely accessed after Start has completed (I.E. the returned channel has
	// been waited on)
	BoundAddress string
	GrpcServer   *grpc.Server
	grp          errgroup.Group
}

// New creates and returns a new Server with port and interface.
func New(listener string, lg log.Logger, opts ...grpc.ServerOption) *Server {
	opts = append(opts, ServerOptions...)
	return &Server{
		Listener:   listener,
		logger:     lg,
		GrpcServer: grpc.NewServer(opts...),
	}
}

// Start starts the server.
func (s *Server) Start() error {
	s.logger.With().Info("starting grpc server",
		log.String("address", s.Listener),
		log.Array("services", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for svc := range s.GrpcServer.GetServiceInfo() {
				encoder.AppendString(svc)
			}
			return nil
		})),
	)
	lis, err := net.Listen("tcp", s.Listener)
	if err != nil {
		s.logger.Error("error listening: %v", err)
		return err
	}
	s.BoundAddress = lis.Addr().String()
	reflection.Register(s.GrpcServer)
	s.grp.Go(func() error {
		if err := s.GrpcServer.Serve(lis); err != nil {
			s.logger.Error("error stopping grpc server: %v", err)
			return err
		}
		return nil
	})
	return nil
}

// Close stops the server.
func (s *Server) Close() error {
	s.logger.Info("stopping the grpc server")
	s.GrpcServer.Stop()
	return s.grp.Wait()
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
