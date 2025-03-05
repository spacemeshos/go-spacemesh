package grpcserver

import (
	"fmt"
	"net"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func getFreePort(tb testing.TB) int {
	tb.Helper()

	l, err := net.Listen("tcp", ":0")
	require.NoError(tb, err, "Should be able to establish a connection on a port")
	defer l.Close()

	return l.Addr().(*net.TCPAddr).Port
}

func TestNewServersConfig(t *testing.T) {
	port1 := getFreePort(t)
	port2 := getFreePort(t)

	grpcService := New(
		fmt.Sprintf(":%d", port1),
		zaptest.NewLogger(t).Named("grpc"),
		DefaultTestConfig(t),
	)
	jsonService := NewJSONHTTPServer(
		zaptest.NewLogger(t).Named("grpc.JSON"),
		fmt.Sprintf(":%d", port2),
		[]string{},
		false,
		false,
	)

	require.Contains(t, grpcService.listener, strconv.Itoa(port1), "Expected same port")
	require.Contains(t, jsonService.listener, strconv.Itoa(port2), "Expected same port")
}
