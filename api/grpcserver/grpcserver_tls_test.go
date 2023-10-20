package grpcserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

const (
	caCert     = "testdata/ca.crt"
	serverCert = "testdata/server.crt"
	serverKey  = "testdata/server.key"
	clientCert = "testdata/client.crt"
	clientKey  = "testdata/client.key"
)

func launchTLSServer(tb testing.TB, services ...ServiceAPI) (Config, func()) {
	pwd, err := os.Getwd()
	require.NoError(tb, err)
	caCert := filepath.Join(pwd, caCert)
	require.FileExists(tb, caCert)
	serverCert := filepath.Join(pwd, serverCert)
	require.FileExists(tb, serverCert)
	serverKey := filepath.Join(pwd, serverKey)
	require.FileExists(tb, serverKey)

	cfg := DefaultTestConfig()
	cfg.TLSCACert = caCert
	cfg.TLSCert = serverCert
	cfg.TLSKey = serverKey

	grpcService, err := NewTLS(zaptest.NewLogger(tb).Named("grpc.TLS"), cfg, services)
	require.NoError(tb, err)

	// start gRPC server
	require.NoError(tb, grpcService.Start())

	// update config with bound addresses
	cfg.TLSListener = grpcService.BoundAddress

	return cfg, func() { assert.NoError(tb, grpcService.Close()) }
}
