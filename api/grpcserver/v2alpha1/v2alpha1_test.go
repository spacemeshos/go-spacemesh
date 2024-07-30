package v2alpha1

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/spacemeshos/go-spacemesh/api/grpcserver"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const (
	genTimeUnix   = 1000000
	layerDuration = 10 * time.Second
)

var genesisID = types.Hash20{}

func launchServer(tb testing.TB, services ...grpcserver.ServiceAPI) (grpcserver.Config, func()) {
	cfg := grpcserver.DefaultTestConfig()
	grpc, err := grpcserver.NewWithServices(cfg.PublicListener, zaptest.NewLogger(tb).Named("grpc"), cfg, services)
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
	tb.Cleanup(func() { require.NoError(tb, conn.Close()) })
	return conn
}
