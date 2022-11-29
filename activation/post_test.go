package activation

import (
	"context"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/errgroup"

	atypes "github.com/spacemeshos/go-spacemesh/activation/types"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log/logtest"
)

var id = types.NodeID{}

func getTestConfig(t *testing.T) (atypes.PostConfig, atypes.PostSetupOpts) {
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
	lastStatus := &atypes.PostSetupStatus{}
	eg.Go(func() error {
		timer := time.NewTicker(50 * time.Millisecond)
		defer timer.Stop()

		for {
			select {
			case <-ctx.Done():
				return nil
			case <-timer.C:
				status := mgr.Status()
				req.GreaterOrEqual(status.NumLabelsWritten, lastStatus.NumLabelsWritten)
				req.Equal(opts, *status.LastOpts)
				req.Nil(status.LastError)

				if status.NumLabelsWritten < uint64(opts.NumUnits)*cfg.LabelsPerUnit {
					req.Equal(atypes.PostSetupStateInProgress, status.State)
				}
			}
		}
	})

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	cancel()
	eg.Wait()

	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	req.Equal(atypes.PostSetupStateComplete, mgr.Status().State)

	// Create data (same opts).
	doneChan, err = mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())

	// Cleanup.
	req.NoError(mgr.StopSession(true))

	// Create data (same opts, after deletion).
	doneChan, err = mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	req.Equal(atypes.PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify the initial status.
	status := mgr.Status()
	req.Equal(atypes.PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)
	req.Nil(status.LastOpts)
	req.Nil(status.LastError)

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	req.Equal(atypes.PostSetupStateComplete, mgr.Status().State)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify the initial status.
	status = mgr.Status()
	req.Equal(atypes.PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)
	req.Nil(status.LastOpts)
	req.Nil(status.LastError)
}

func TestPostSetupManager_GenerateProof(t *testing.T) {
	req := require.New(t)
	ch := make([]byte, 32)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch, goldenATXID)
	req.EqualError(err, errNotComplete.Error())

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Generate proof.
	_, _, err = mgr.GenerateProof(ch, goldenATXID)
	req.NoError(err)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch, goldenATXID)
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_GetPow(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to get nonce.
	_, err = mgr.GetPowNonce()
	req.EqualError(err, errNotComplete.Error())

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Get nonce.
	nonce, err := mgr.GetPowNonce()
	req.NoError(err)
	req.NotZero(nonce)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Attempt to get nonce.
	_, err = mgr.GetPowNonce()
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
	req.Equal(atypes.PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)
	req.Nil(status.LastOpts)

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Verify state.
	req.Equal(atypes.PostSetupStateComplete, mgr.Status().State)

	// Stop without file deletion.
	req.NoError(mgr.StopSession(false))

	// Verify state.
	req.Equal(atypes.PostSetupStateComplete, mgr.Status().State)

	// Stop with file deletion.
	req.NoError(mgr.StopSession(true))

	// Verify state.
	req.Equal(atypes.PostSetupStateNotStarted, mgr.Status().State)

	// Create data again.
	doneChan, err = mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Verify state.
	req.Equal(atypes.PostSetupStateComplete, mgr.Status().State)
}

func TestPostSetupManager_Stop_WhileInProgress(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	opts.NumUnits *= 10
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)

	// Wait a bit for the setup to proceed.
	time.Sleep(100 * time.Millisecond)

	// Verify the intermediate status.
	status := mgr.Status()
	req.Equal(&opts, status.LastOpts)
	req.Equal(atypes.PostSetupStateInProgress, status.State)

	// Stop without files deletion.
	req.NoError(mgr.StopSession(false))

	select {
	case <-doneChan:
	default:
		req.Fail("`StopSession` is expected to block until `StartSession` is done")
	}

	// Verify status.
	status = mgr.Status()
	req.Nil(status.LastOpts)
	req.Equal(atypes.PostSetupStateNotStarted, status.State)
	req.Zero(status.NumLabelsWritten)

	// Continue to create data.
	doneChan, err = mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Verify status.
	status = mgr.Status()
	req.Equal(&opts, status.LastOpts)
	req.Equal(atypes.PostSetupStateComplete, status.State)
	req.Equal(uint64(opts.NumUnits)*cfg.LabelsPerUnit, status.NumLabelsWritten)
}
