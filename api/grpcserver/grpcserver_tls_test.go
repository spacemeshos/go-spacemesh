package grpcserver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func launchTLSServer(tb testing.TB, services ...ServiceAPI) (Config, func()) {
	cfg := DefaultTestConfig()
	cfg.TLSListener = "127.0.0.1:0" // run on random port

	grpcService, err := NewTLS(zaptest.NewLogger(tb).Named("grpc.TLS"), cfg, services)
	require.NoError(tb, err)

	// start gRPC server
	require.NoError(tb, grpcService.Start())

	// update config with bound addresses
	cfg.TLSListener = grpcService.BoundAddress

	return cfg, func() { assert.NoError(tb, grpcService.Close()) }
}
