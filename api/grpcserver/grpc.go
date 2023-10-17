package grpcserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	grpczap "github.com/grpc-ecosystem/go-grpc-middleware/logging/zap"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	grpctags "github.com/grpc-ecosystem/go-grpc-middleware/tags"
	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/sync/errgroup"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
)

// ServiceAPI allows individual grpc services to register the grpc server.
type ServiceAPI interface {
	RegisterService(*grpc.Server)
	RegisterHandlerService(*runtime.ServeMux) error
	String() string
}

// Server is a very basic grpc server.
type Server struct {
	listener string
	logger   *zap.Logger
	// BoundAddress contains the address that the server bound to, useful if
	// the server uses a dynamic port. It is set during startup and can be
	// safely accessed after Start has completed (I.E. the returned channel has
	// been waited on)
	BoundAddress string
	GrpcServer   *grpc.Server
	grp          errgroup.Group
}

func unaryGrpcLogStart(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	ctxzap.Info(ctx, "started unary call")
	return handler(ctx, req)
}

func streamingGrpcLogStart(srv any, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	ctxzap.Info(stream.Context(), "started streaming call")
	return handler(srv, stream)
}

// NewPublic creates a new Server listening on the PublicListener address with the given logger and config.
// Services passed in the svc slice are registered with the server.
func NewPublic(logger *zap.Logger, config Config, svc []ServiceAPI) (*Server, error) {
	if len(svc) == 0 {
		return nil, errors.New("no services to register")
	}

	server := New(config.PublicListener, logger, config)
	for _, s := range svc {
		s.RegisterService(server.GrpcServer)
	}
	return server, nil
}

// NewPrivate creates new Server listening on the PrivateListener address with the given logger and config.
// Services passed in the svc slice are registered with the server.
func NewPrivate(logger *zap.Logger, config Config, svc []ServiceAPI) (*Server, error) {
	if len(svc) == 0 {
		return nil, errors.New("no services to register")
	}

	server := New(config.PrivateListener, logger, config)
	for _, s := range svc {
		s.RegisterService(server.GrpcServer)
	}
	return server, nil
}

// NewTLS creates a new Server listening on the TLSListener address with the given logger and config.
// Services passed in the svc slice are registered with the server.
func NewTLS(logger *zap.Logger, config Config, svc []ServiceAPI) (*Server, error) {
	if len(svc) == 0 {
		return nil, errors.New("no services to register")
	}

	serverCert, err := tls.LoadX509KeyPair(config.TLSCert, config.TLSKey)
	if err != nil {
		return nil, fmt.Errorf("load server certificate: %w", err)
	}
	caCert, err := os.ReadFile(config.TLSCACert)
	if err != nil {
		return nil, fmt.Errorf("load ca certificate: %w", err)
	}

	certPool := x509.NewCertPool()
	if !certPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("setup CA certificate")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    certPool,
	}

	server := New(config.TLSListener, logger, config, grpc.Creds(credentials.NewTLS(tlsConfig)))
	for _, s := range svc {
		s.RegisterService(server.GrpcServer)
	}
	return server, nil
}

// New creates and returns a new Server listening on the given address.
// The server is configured with the given logger and config. Additional grpc options can be passed.
func New(listener string, logger *zap.Logger, config Config, grpcOpts ...grpc.ServerOption) *Server {
	opts := []grpc.ServerOption{
		grpc.ChainStreamInterceptor(grpctags.StreamServerInterceptor(), grpczap.StreamServerInterceptor(logger), streamingGrpcLogStart),
		grpc.ChainUnaryInterceptor(grpctags.UnaryServerInterceptor(), grpczap.UnaryServerInterceptor(logger), unaryGrpcLogStart),
		grpc.MaxSendMsgSize(config.GrpcSendMsgSize),
		grpc.MaxRecvMsgSize(config.GrpcRecvMsgSize),
	}

	// this is done to prevent routers from cleaning up our connections (e.g aws load balances..)
	// TODO: these parameters work for now but we might need to revisit or add them as configuration
	// TODO: Configure maxconns, maxconcurrentcons ..
	opts = append(opts,
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionIdle:     time.Minute * 120,
			MaxConnectionAge:      time.Minute * 180,
			MaxConnectionAgeGrace: time.Minute * 10,
			Time:                  time.Minute,
			Timeout:               time.Minute * 3,
		}),
	)

	opts = append(opts, grpcOpts...)
	return &Server{
		listener:   listener,
		logger:     logger,
		GrpcServer: grpc.NewServer(opts...),
	}
}

// Start starts the server.
func (s *Server) Start() error {
	s.logger.Info("starting grpc server",
		zap.String("address", s.listener),
		zap.Array("services", zapcore.ArrayMarshalerFunc(func(encoder zapcore.ArrayEncoder) error {
			for svc := range s.GrpcServer.GetServiceInfo() {
				encoder.AppendString(svc)
			}
			return nil
		})),
	)
	lis, err := net.Listen("tcp", s.listener)
	if err != nil {
		s.logger.Error("start listen server", zap.Error(err))
		return err
	}
	s.BoundAddress = lis.Addr().String()
	reflection.Register(s.GrpcServer)
	s.grp.Go(func() error {
		if err := s.GrpcServer.Serve(lis); err != nil {
			s.logger.Error("serving grpc server", zap.Error(err))
			return err
		}
		return nil
	})
	return nil
}

// Close stops the server.
func (s *Server) Close() error {
	s.logger.Info("stopping the grpc server")
	// GracefulStop waits for all connections to be closed before closing the
	// server and returning. If there are long running stream connections then
	// GracefulStop will never return. So we call it in a background thread,
	// wait a bit and then call Stop which will forcefully close any remaining
	// connections.
	s.grp.Go(func() error {
		s.GrpcServer.GracefulStop()
		return nil
	})
	time.Sleep(time.Second * 1)
	s.GrpcServer.Stop()
	return s.grp.Wait()
}
