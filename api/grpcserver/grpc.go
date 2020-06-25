package grpcserver

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"
	"net"
	"strconv"
	"time"
)

// Global singleton (not thread-safe)
type Server struct {
	s    *grpc.Server
	port int
}

var server = Server{}

// ServiceServer is an interface that knits together the various grpc service servers,
// allowing the glue code in this file to manage them all.
type ServiceServer interface {
	Port() int
	registerService(*grpc.Server)
	Close() error
}

// Service is a barebones struct that all service servers embed. It stores properties
// used by all service servers.
type Service struct {
	port int
}

// Port is a getter for port
func (s Service) Port() int { return s.port }

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

// StartService starts the grpc service.
func StartService(s ServiceServer) error {
	log.Info("attempting to start grpc server for service %T on port %d", s, s.Port())

	// If the server isn't running yet, create it
	if server.s == nil {
		log.Info("starting new grpc server")
		server.port = s.Port()
		server.s = grpc.NewServer(ServerOptions...)
		go startServiceInternal(server.s, s.Port())
	} else if s.Port() != server.port {
		// Make sure the port matches
		return errors.New(fmt.Sprintf("port %d does not match running server port %d", s.Port(), server.port))
	} else {
		log.Info("grpc server is already running, not starting another")
	}

	// Register the service to the server
	s.registerService(server.s)

	return nil
}

// This is a blocking method designed to be called using a go routine
func startServiceInternal(s *grpc.Server, port int) {
	addr := ":" + strconv.Itoa(port)

	lis, err := net.Listen("tcp", addr)
	if err != nil {
		log.Error("failed to start grpc server: %v", err)
		return
	}

	// SubscribeOnNewConnections reflection service on gRPC server
	reflection.Register(s)

	log.Info("new grpc server listening on port %d", port)

	// start serving - this blocks until err or server is stopped
	if err := s.Serve(lis); err != nil {
		log.Error("error stopping grpc server: %v", err)
	}
}

// Close stops all GRPC services being served by this server.
func (s Service) Close() error {
	log.Info("Stopping new grpc server...")
	if server.s == nil {
		log.Info("grpc server already stopped")
	} else {
		server.s.Stop()
		server.s = nil
		log.Info("new grpc server stopped")
	}
	return nil
}
