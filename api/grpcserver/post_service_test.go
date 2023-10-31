package grpcserver

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func launchPostSupervisor(
	tb testing.TB,
	log *zap.Logger,
	cfg Config,
	postOpts activation.PostSetupOpts,
) (types.NodeID, func()) {
	cmdCfg := activation.DefaultTestPostServiceConfig()
	cmdCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)
	postCfg := activation.DefaultPostConfig()
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = tb.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	id := sig.NodeID()
	goldenATXID := types.RandomATXID()

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
	mgr, err := activation.NewPostSetupManager(id, postCfg, log.Named("post manager"), cdb, goldenATXID)
	require.NoError(tb, err)

	syncer := activation.NewMocksyncer(gomock.NewController(tb))
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	})

	// start post supervisor
	ps, err := activation.NewPostSupervisor(log, cmdCfg, postCfg, provingOpts, mgr, syncer)
	require.NoError(tb, err)
	require.NotNil(tb, ps)
	require.NoError(tb, ps.Start(postOpts))
	return id, func() { assert.NoError(tb, ps.Stop(false)) }
}

func launchPostSupervisorTLS(
	tb testing.TB,
	log *zap.Logger,
	cfg Config,
	postOpts activation.PostSetupOpts,
) (types.NodeID, func()) {
	pwd, err := os.Getwd()
	require.NoError(tb, err)
	caCert := filepath.Join(pwd, caCert)
	require.FileExists(tb, caCert)
	clientCert := filepath.Join(pwd, clientCert)
	require.FileExists(tb, clientCert)
	clientKey := filepath.Join(pwd, clientKey)
	require.FileExists(tb, clientKey)

	cmdCfg := activation.DefaultTestPostServiceConfig()
	cmdCfg.CACert = caCert
	cmdCfg.Cert = clientCert
	cmdCfg.Key = clientKey
	cmdCfg.NodeAddress = fmt.Sprintf("https://%s", cfg.TLSListener)
	postCfg := activation.DefaultPostConfig()
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = tb.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	id := sig.NodeID()
	goldenATXID := types.RandomATXID()

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
	mgr, err := activation.NewPostSetupManager(id, postCfg, log.Named("post manager"), cdb, goldenATXID)
	require.NoError(tb, err)

	syncer := activation.NewMocksyncer(gomock.NewController(tb))
	syncer.EXPECT().RegisterForATXSynced().DoAndReturn(func() <-chan struct{} {
		ch := make(chan struct{})
		close(ch)
		return ch
	})

	ps, err := activation.NewPostSupervisor(log, cmdCfg, postCfg, provingOpts, mgr, syncer)
	require.NoError(tb, err)
	require.NotNil(tb, ps)
	require.NoError(tb, ps.Start(postOpts))
	return id, func() { assert.NoError(tb, ps.Stop(false)) }
}

func Test_GenerateProof(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor"), cfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	proof, meta, err := client.Proof(context.Background(), challenge)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.NotNil(t, meta)

	// drop connection
	postCleanup()
	require.Eventually(t, func() bool {
		proof, meta, err = client.Proof(context.Background(), challenge)
		return err != nil
	}, 5*time.Second, 100*time.Millisecond)

	require.ErrorContains(t, err, "post client closed")
	require.Nil(t, proof)
	require.Nil(t, meta)
}

func Test_GenerateProof_TLS(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchTLSServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	id, postCleanup := launchPostSupervisorTLS(t, log.Named("supervisor"), cfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	proof, meta, err := client.Proof(context.Background(), challenge)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.NotNil(t, meta)

	// drop connection
	postCleanup()
	require.Eventually(t, func() bool {
		proof, meta, err = client.Proof(context.Background(), challenge)
		return err != nil
	}, 5*time.Second, 100*time.Millisecond)

	require.ErrorContains(t, err, "post client closed")
	require.Nil(t, proof)
	require.Nil(t, meta)
}

func Test_GenerateProof_Cancel(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor"), cfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	// cancel on sending
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	proof, meta, err := client.Proof(ctx, challenge)
	require.ErrorIs(t, err, context.Canceled)
	require.Nil(t, proof)
	require.Nil(t, meta)
}

func Test_Metadata(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor"), cfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	meta, err := client.Info(context.Background())
	require.NoError(t, err)
	require.NotNil(t, meta)
	require.Equal(t, id, meta.NodeID)
	require.NotEmpty(t, meta.CommitmentATX)
	require.NotNil(t, meta.Nonce)
	require.Equal(t, opts.NumUnits, meta.NumUnits)

	// drop connection
	postCleanup()
	require.Eventually(t, func() bool {
		meta, err = client.Info(context.Background())
		return err != nil
	}, 5*time.Second, 100*time.Millisecond)

	require.ErrorContains(t, err, "post client closed")
	require.Nil(t, meta)
}

func Test_GenerateProof_MultipleServices(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetUint32(initialization.CPUProviderID())
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	// all but one should not be able to register to the node (i.e. open a stream to it).
	id, postCleanup := launchPostSupervisor(t, log.Named("supervisor1"), cfg, opts)
	t.Cleanup(postCleanup)

	opts.DataDir = t.TempDir()
	_, postCleanup = launchPostSupervisor(t, log.Named("supervisor2"), cfg, opts)
	t.Cleanup(postCleanup)

	opts.DataDir = t.TempDir()
	_, postCleanup = launchPostSupervisor(t, log.Named("supervisor3"), cfg, opts)
	t.Cleanup(postCleanup)

	var client activation.PostClient
	require.Eventually(t, func() bool {
		var err error
		client, err = svc.Client(id)
		return err == nil
	}, 10*time.Second, 100*time.Millisecond, "timed out waiting for connection")

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	proof, meta, err := client.Proof(context.Background(), challenge)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.NotNil(t, meta)
}
