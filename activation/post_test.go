package activation

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

var id = types.NodeID{}

func getTestConfig(t *testing.T) (PostConfig, PostSetupOpts) {
	cfg := DefaultPostConfig()

	opts := DefaultPostSetupOpts()
	opts.DataDir = t.TempDir()
	opts.NumUnits = cfg.MinNumUnits
	opts.ComputeProviderID = int(initialization.CPUProviderID())

	return cfg, opts
}

func TestPostSetupManager(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var eg errgroup.Group
	lastStatus := &PostSetupStatus{}
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
				status := mgr.Status()
				req.GreaterOrEqual(status.NumLabelsWritten, lastStatus.NumLabelsWritten)

				if status.NumLabelsWritten < uint64(opts.NumUnits)*cfg.LabelsPerUnit {
					req.Equal(PostSetupStateInProgress, status.State)
				}
			}
		}
	})

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))
	cancel()
	eg.Wait()

	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Create data (same opts).
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))

	// Cleanup.
	req.NoError(mgr.Reset())

	// Create data (same opts, after deletion).
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify the initial status.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))
	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify the initial status.
	status = mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)
}

func TestPostSetupManager_GenerateProof(t *testing.T) {
	req := require.New(t)
	ch := make([]byte, 32)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.EqualError(err, errNotComplete.Error())

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))

	// Generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.NoError(err)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_VRFNonce(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to get nonce.
	_, err = mgr.VRFNonce()
	req.ErrorIs(err, errNotComplete)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))

	// Get nonce.
	nonce, err := mgr.VRFNonce()
	req.NoError(err)
	req.NotZero(nonce)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to get nonce.
	_, err = mgr.VRFNonce()
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_Stop(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify state.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Create data.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)

	// Reset.
	req.NoError(mgr.Reset())

	// Verify state.
	req.Equal(PostSetupStateNotStarted, mgr.Status().State)

	// Create data again.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))

	// Verify state.
	req.Equal(PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_Stop_WhileInProgress(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	opts.NumUnits *= 10

	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Create data.
	ctx, cancel := context.WithCancel(context.Background())
	var eg errgroup.Group
	eg.Go(func() error {
		return mgr.StartSession(ctx, opts, goldenATXID.Bytes())
	})

	// Wait a bit for the setup to proceed.
	time.Sleep(100 * time.Millisecond)

	// Verify the intermediate status.
	status := mgr.Status()
	req.Equal(PostSetupStateInProgress, status.State)

	// Stop initialization.
	cancel()

	req.ErrorIs(eg.Wait(), context.Canceled)

	// Verify status.
	status = mgr.Status()
	req.Equal(PostSetupStateStopped, status.State)
	req.LessOrEqual(status.NumLabelsWritten, uint64(opts.NumUnits)*cfg.LabelsPerUnit)

	// Continue to create data.
	req.NoError(mgr.StartSession(context.Background(), opts, goldenATXID.Bytes()))

	// Verify status.
	status = mgr.Status()
	req.Equal(PostSetupStateComplete, status.State)
	req.Equal(uint64(opts.NumUnits)*cfg.LabelsPerUnit, status.NumLabelsWritten)
}
