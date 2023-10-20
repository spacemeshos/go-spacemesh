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
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
)

func initPost(tb testing.TB, log *zap.Logger, opts activation.PostSetupOpts) types.NodeID {
	tb.Helper()

	cfg := activation.DefaultPostConfig()

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	id := sig.NodeID()

	goldenATXID := types.ATXID{2, 3, 4}

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
	mgr, err := activation.NewPostSetupManager(id, cfg, log.Named("manager"), cdb, goldenATXID)
	require.NoError(tb, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var eg errgroup.Group
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

		lastStatus := &activation.PostSetupStatus{}
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				status := mgr.Status()
				require.GreaterOrEqual(tb, status.NumLabelsWritten, lastStatus.NumLabelsWritten)

				if status.NumLabelsWritten == uint64(opts.NumUnits)*cfg.LabelsPerUnit {
					return nil
				}
				require.Contains(tb, []activation.PostSetupState{activation.PostSetupStatePrepared, activation.PostSetupStateInProgress}, status.State)
				lastStatus = status
			}
		}
	})

	// Create data.
	require.NoError(tb, mgr.PrepareInitializer(context.Background(), opts))
	require.NoError(tb, mgr.StartSession(context.Background()))
	require.NoError(tb, eg.Wait())
	require.Equal(tb, activation.PostSetupStateComplete, mgr.Status().State)
	return id
}

func launchPostSupervisor(tb testing.TB, log *zap.Logger, cfg Config, postOpts activation.PostSetupOpts) func() {
	cmdCfg := activation.DefaultTestPostServiceConfig()
	cmdCfg.NodeAddress = fmt.Sprintf("http://%s", cfg.PublicListener)
	postCfg := activation.DefaultPostConfig()
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.RandomXMode = activation.PostRandomXModeLight

	ps, err := activation.NewPostSupervisor(log, cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(tb, err)
	require.NotNil(tb, ps)
	require.NoError(tb, ps.Start())
	return func() { assert.NoError(tb, ps.Stop()) }
}

func launchPostSupervisorTLS(tb testing.TB, log *zap.Logger, cfg Config, postOpts activation.PostSetupOpts) func() {
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

	ps, err := activation.NewPostSupervisor(log, cmdCfg, postCfg, postOpts, provingOpts)
	require.NoError(tb, err)
	require.NotNil(tb, ps)
	require.NoError(tb, ps.Start())
	return func() { assert.NoError(tb, ps.Stop()) }
}

func Test_GenerateProof(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	id := initPost(t, log.Named("post"), opts)
	postCleanup := launchPostSupervisor(t, log.Named("supervisor"), cfg, opts)
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
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	id := initPost(t, log.Named("post"), opts)
	postCleanup := launchPostSupervisorTLS(t, log.Named("supervisor"), cfg, opts)
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

func Test_Cancel_GenerateProof(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	opts.Scrypt.N = 2 // Speedup initialization in tests.
	id := initPost(t, log.Named("post"), opts)
	t.Cleanup(launchPostSupervisor(t, log.Named("supervisor"), cfg, opts))

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

func Test_GenerateProof_MultipleServices(t *testing.T) {
	log := zaptest.NewLogger(t)
	svc := NewPostService(log)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	// all but one should not be able to register to the node (i.e. open a stream to it).
	id := initPost(t, log.Named("post1"), opts)
	t.Cleanup(launchPostSupervisor(t, log.Named("supervisor1"), cfg, opts))

	opts.DataDir = t.TempDir()
	initPost(t, log.Named("post2"), opts)
	t.Cleanup(launchPostSupervisor(t, log.Named("supervisor2"), cfg, opts))

	opts.DataDir = t.TempDir()
	initPost(t, log.Named("post3"), opts)
	t.Cleanup(launchPostSupervisor(t, log.Named("supervisor3"), cfg, opts))

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
