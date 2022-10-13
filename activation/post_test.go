package activation

import (
	"sync"
	"testing"
	"time"

	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"

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
	opts.ComputeProviderID = initialization.CPUProviderID()

	return cfg, opts
}

func TestPostSetupManager(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	lastStatus := &atypes.PostSetupStatus{}
	go func() {
		for status := range mgr.StatusChan() {
			req.True(status.NumLabelsWritten >= lastStatus.NumLabelsWritten)
			req.Equal(opts, *status.LastOpts)
			req.Nil(status.LastError)

			if uint(status.NumLabelsWritten) == opts.NumUnits*cfg.LabelsPerUnit {
				// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
				// req.Equal(postSetupStateComplete, status.State)
			} else {
				req.Equal(atypes.PostSetupStateInProgress, status.State)
			}

			lastStatus = status
		}
	}()

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
	// req.Equal(lastStatus, mgr.Status())

	// Create data (same opts).
	doneChan, err = mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())

	// Cleanup.
	err = mgr.StopSession(true)
	req.NoError(err)

	// Create data (same opts, after deletion).
	doneChan, err = mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
	// req.Equal(lastStatus, mgr.Status())
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

	// Compare the last status update to the status queried directly.
	// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
	// req.Equal(lastStatus, mgr.Status())

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

func TestPostSetupManager_StatusChan_BeforeSessionStarted(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify that the status stream works properly when called *before* session started.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ch := mgr.StatusChan()
		var prevStatus *atypes.PostSetupStatus
		for {
			status, more := <-ch
			if more {
				if prevStatus == nil {
					// Verify initial status.
					req.Equal(uint64(0), status.NumLabelsWritten)
				}
				prevStatus = status
			} else {
				// Verify last status.
				req.Equal(opts.NumUnits*cfg.LabelsPerUnit, uint(prevStatus.NumLabelsWritten))
				break
			}
		}
	}()

	// Create data.
	time.Sleep(1 * time.Second) // Short delay.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	wg.Wait()

	// Cleanup.
	err = mgr.StopSession(true)
	req.NoError(err)
}

func TestPostSetupManager_StatusChan_AfterSessionStarted(t *testing.T) {
	req := require.New(t)

	cdb := newCachedDB(t)
	cfg, opts := getTestConfig(t)
	opts.NumUnits *= 10
	mgr, err := NewPostSetupManager(id, cfg, logtest.New(t), cdb, goldenATXID)
	req.NoError(err)

	// Verify that the status stream works properly when called *after* session started (yet before it ended).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		time.Sleep(100 * time.Millisecond) // Short delay.

		ch := mgr.StatusChan()
		var prevStatus *atypes.PostSetupStatus
		for {
			status, more := <-ch
			if more {
				if prevStatus == nil {
					// Verify initial status.
					req.Equal(uint64(0), status.NumLabelsWritten)
				}
				prevStatus = status
			} else {
				// Verify last status.
				req.Equal(opts.NumUnits*cfg.LabelsPerUnit, uint(prevStatus.NumLabelsWritten))
				break
			}
		}
		wg.Done()
	}()

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan
	wg.Wait()

	// Cleanup.
	err = mgr.StopSession(true)
	req.NoError(err)
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

	// Create data.
	doneChan, err := mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Verify state.
	status = mgr.Status()
	req.Equal(atypes.PostSetupStateComplete, status.State)

	// Stop without file deletion.
	err = mgr.StopSession(false)
	req.NoError(err)

	// Verify state.
	status = mgr.Status()
	req.Equal(atypes.PostSetupStateComplete, status.State)

	// Stop with file deletion.
	err = mgr.StopSession(true)
	req.NoError(err)

	// Verify state.
	status = mgr.Status()
	req.Equal(atypes.PostSetupStateNotStarted, status.State)

	// Create data again.
	doneChan, err = mgr.StartSession(opts, goldenATXID)
	req.NoError(err)
	<-doneChan

	// Verify state.
	status = mgr.Status()
	req.Equal(atypes.PostSetupStateComplete, status.State)
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
	err = mgr.StopSession(false)
	req.NoError(err)

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
	req.Equal(opts.NumUnits*cfg.LabelsPerUnit, uint(status.NumLabelsWritten))
}
