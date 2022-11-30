package util

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

// MockGrpcServer allows for simulating a GRPC server in tests.
// Typical usage:
// ```
// gtw := NewMockGrpcServer(t)
// <register required GRPC services>
// var eg errgroup.Group
// eg.Go(gtw.Serve)
// t.Cleanup(func() { require.NoError(t, eg.Wait()) })
// t.Cleanup(gtw.Stop)
// ```
// .
type MockGrpcServer struct {
	listener net.Listener
	*grpc.Server
	Port uint16
}

func NewMockGrpcServer(t testing.TB) *MockGrpcServer {
	lis, err := net.Listen("tcp", ":0")
	require.NoError(t, err)

	port, err := strconv.ParseUint(strings.TrimPrefix(lis.Addr().String(), "[::]:"), 10, 16)
	require.NoError(t, err)

	s := grpc.NewServer()

	return &MockGrpcServer{
		listener: lis,
		Server:   s,
		Port:     uint16(port),
	}
}

func (s *MockGrpcServer) Serve() error {
	return s.Server.Serve(s.listener)
}

func (s *MockGrpcServer) Target() string {
	return fmt.Sprintf("localhost:%d", s.Port)
}
