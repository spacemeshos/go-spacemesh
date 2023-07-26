package grpcserver

import (
	"net"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// ServiceAPI allows individual grpc services to register the grpc server.
type ServiceAPI interface {
	RegisterService(*Server)
}

// Server is a very basic grpc server.
type Server struct {
	Listener   string
	logger     *zap.Logger
	GrpcServer *grpc.Server
}

// New creates and returns a new Server with port and interface.
func New(listener string, lg *zap.Logger, opts ...grpc.ServerOption) *Server {
	opts = append(opts, ServerOptions...)
	return &Server{
		Listener:   listener,
		logger:     lg,
		GrpcServer: grpc.NewServer(opts...),
	}
}

// Start starts the server.
func (s *Server) Start() <-chan struct{} {
	s.logger.With().Info("starting grpc server",
		zap.String("address", s.Listener),
		zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
			for svc := range s.GrpcServer.GetServiceInfo() {
				encoder.AppendString(svc)
			}
			return nil
		})),
	)

	started := make(chan struct{})
	go s.startInternal(started)

	return started
}

// Blocking, should be called in a goroutine.
func (s *Server) startInternal(started chan<- struct{}) {
	lis, err := net.Listen("tcp", s.Listener)
	if err != nil {
		s.logger.Error("error listening: %v", zap.Error(err))
		return
	}
	reflection.Register(s.GrpcServer)
	s.logger.Sugar().Infof("starting new grpc server on %s", s.Listener)
	close(started)
	if err := s.GrpcServer.Serve(lis); err != nil {
		s.logger.Error("error stopping grpc server: %v", zap.Error(err))
	}
}

// Close stops the server.
func (s *Server) Close() error {
	s.logger.Info("stopping the grpc server")
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
