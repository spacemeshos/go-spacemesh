package v2alpha1

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
)

const (
	genTimeUnix   = 1000000
	layerDuration = 10 * time.Second
)

func launchServer(tb testing.TB, services ...grpcserver.ServiceAPI) (grpcserver.Config, func()) {
	cfg := grpcserver.DefaultTestConfig()
	// logger := zaptest.NewLogger(tb).Named("grpc")
	logger := zap.NewNop()
	grpc, err := grpcserver.NewWithServices(cfg.PublicListener, logger, cfg, services)
	require.NoError(tb, err)

	// start gRPC server
	require.NoError(tb, grpc.Start())

	// update config with bound addresses
	cfg.PublicListener = grpc.BoundAddress

	return cfg, func() { assert.NoError(tb, grpc.Close()) }
}

func dialGrpc(tb testing.TB, cfg grpcserver.Config) *grpc.ClientConn {
	tb.Helper()
	conn, err := grpc.NewClient(
		cfg.PublicListener,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(tb, err)

	// block until the clientConn is ready.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		s := conn.GetState()
		if s == connectivity.Ready {
			break
		}
		if s == connectivity.Idle {
			conn.Connect()
		}
		if !conn.WaitForStateChange(ctx, s) {
			tb.Fatalf("timeout waiting for connection to %s", conn.Target())
			return nil
		}
	}

	tb.Cleanup(func() { require.NoError(tb, conn.Close()) })
	return conn
}
