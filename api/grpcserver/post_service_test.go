package grpcserver

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
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

func initPost(tb *testing.T, log *zap.Logger, dir string) {
	tb.Helper()

	cfg := activation.DefaultPostConfig()

	sig, err := signing.NewEdSigner()
	require.NoError(tb, err)
	id := sig.NodeID()

	opts := activation.DefaultPostSetupOpts()
	opts.DataDir = dir
	opts.ProviderID.SetInt64(int64(initialization.CPUProviderID()))
	opts.Scrypt.N = 2 // Speedup initialization in tests.

	goldenATXID := types.ATXID{2, 3, 4}

	cdb := datastore.NewCachedDB(sql.InMemory(), logtest.New(tb))
	provingOpts := activation.DefaultPostProvingOpts()
	provingOpts.Flags = config.RecommendedPowFlags()
	mgr, err := activation.NewPostSetupManager(id, cfg, log.Named("post mgr"), cdb, goldenATXID, provingOpts)
	require.NoError(tb, err)

	ctx, cancel := context.WithCancel(context.Background())
	tb.Cleanup(cancel)

	var eg errgroup.Group
	lastStatus := &activation.PostSetupStatus{}
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

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
				require.Equal(tb, activation.PostSetupStateInProgress, status.State)
			}
		}
	})

	// Create data.
	require.NoError(tb, mgr.PrepareInitializer(context.Background(), opts))
	require.NoError(tb, mgr.StartSession(context.Background()))
	require.NoError(tb, eg.Wait())
	require.Equal(tb, activation.PostSetupStateComplete, mgr.Status().State)
}

func launchPostService(tb testing.TB, log *zap.Logger, cfg Config, postDir string) func() {
	path, err := exec.Command("go", "env", "GOMOD").Output()
	require.NoError(tb, err)

	dir := filepath.Dir(string(path))
	cmd := exec.Command("./build/service",
		"--dir", postDir,
		"--address", fmt.Sprintf("http://%s", cfg.PublicListener),
		"--pow-difficulty", "ffffffffffffffffff0000000000000000000000000000000000000000000000",
		"--randomx-mode", "light",
		"-n", "2", // Speedup initialization in tests.
	)
	cmd.Dir = dir
	pipe, err := cmd.StderrPipe()
	require.NoError(tb, err)
	require.NoError(tb, cmd.Start())

	var eg errgroup.Group
	eg.Go(func() error {
		for {
			buf := make([]byte, 1024)
			n, err := pipe.Read(buf)
			switch err {
			case nil:
			case io.EOF:
				return nil
			default:
				return err
			}

			tb.Log(string(buf[:n]))
		}
	})

	return func() {
		assert.NoError(tb, cmd.Process.Kill())
		assert.NoError(tb, eg.Wait())
	}
}

func Test_GenerateProof(t *testing.T) {
	t.Parallel()

	log := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	con := NewMockpostConnectionListener(ctrl)
	svc := NewPostService(log, con)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	var client activation.PostClient
	connected := make(chan struct{})
	con.EXPECT().Connected(gomock.Any()).DoAndReturn(func(c activation.PostClient) {
		client = c
		close(connected)
	}).Times(1)
	con.EXPECT().Disconnected(gomock.Any()).Times(1)

	postDir := t.TempDir()
	initPost(t, log.Named("post"), postDir)
	postCleanup := launchPostService(t, log.Named("post"), cfg, postDir)
	t.Cleanup(postCleanup)

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		require.Fail(t, "timed out waiting for connection")
	}

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
	time.Sleep(1 * time.Second) // wait for connection to be dropped

	proof, meta, err = client.Proof(context.Background(), challenge)
	require.ErrorContains(t, err, "client closed")
	require.Nil(t, proof)
	require.Nil(t, meta)
}

func Test_Cancel_GenerateProof(t *testing.T) {
	t.Parallel()

	log := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	con := NewMockpostConnectionListener(ctrl)
	svc := NewPostService(log, con)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	var client activation.PostClient
	connected := make(chan struct{})
	con.EXPECT().Connected(gomock.Any()).DoAndReturn(func(c activation.PostClient) {
		client = c
		close(connected)
	}).Times(1)
	con.EXPECT().Disconnected(gomock.Any()).Times(1)

	postDir := t.TempDir()
	initPost(t, log.Named("post"), postDir)
	t.Cleanup(launchPostService(t, log.Named("post"), cfg, postDir))

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		require.Fail(t, "timed out waiting for connection")
	}

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
	t.Parallel()

	log := zaptest.NewLogger(t)
	ctrl := gomock.NewController(t)
	con := NewMockpostConnectionListener(ctrl)
	svc := NewPostService(log, con)
	cfg, cleanup := launchServer(t, svc)
	t.Cleanup(cleanup)

	var client activation.PostClient
	connected := make(chan struct{})
	con.EXPECT().Connected(gomock.Any()).DoAndReturn(func(c activation.PostClient) {
		client = c
		close(connected)
	}).Times(1)
	con.EXPECT().Disconnected(gomock.Any()).Times(1)

	// all but one should fail to connect but the node should be able to generate a proof
	// from the one that does connect
	postDir1 := t.TempDir()
	initPost(t, log.Named("post1"), postDir1)
	t.Cleanup(launchPostService(t, log.Named("post1"), cfg, postDir1))

	postDir2 := t.TempDir()
	initPost(t, log.Named("post2"), postDir2)
	t.Cleanup(launchPostService(t, log.Named("post2"), cfg, postDir2))

	postDir3 := t.TempDir()
	initPost(t, log.Named("post3"), postDir3)
	t.Cleanup(launchPostService(t, log.Named("post3"), cfg, postDir3))

	select {
	case <-connected:
	case <-time.After(5 * time.Second):
		require.Fail(t, "timed out waiting for connection")
	}

	challenge := make([]byte, 32)
	for i := range challenge {
		challenge[i] = byte(0xca)
	}

	proof, meta, err := client.Proof(context.Background(), challenge)
	require.NoError(t, err)
	require.NotNil(t, proof)
	require.NotNil(t, meta)
}
