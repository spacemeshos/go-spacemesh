package activation

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"sync"
	"testing"
	"time"
)

var (
	id      = make([]byte, 32)
	postLog = log.NewDefault("post-test")

	cfg             PostConfig
	opts            PostSetupOpts
	longerSetupOpts PostSetupOpts
)

func init() {
	cfg = DefaultPostConfig()

	opts = DefaultPostSetupOpts()
	opts.DataDir, _ = ioutil.TempDir("", "post-test")
	opts.NumUnits = cfg.MinNumUnits
	opts.ComputeProviderID = initialization.CPUProviderID()

	longerSetupOpts = opts
	longerSetupOpts.NumUnits = opts.NumUnits * 10
}

func TestPostSetupManager(t *testing.T) {
	req := require.New(t)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	var lastStatus = &PostSetupStatus{}
	go func() {
		for status := range mgr.StatusChan() {
			req.True(status.NumLabelsWritten >= lastStatus.NumLabelsWritten)
			req.Equal(opts, *status.LastOpts)
			req.Nil(status.LastError)

			if uint(status.NumLabelsWritten) == opts.NumUnits*cfg.LabelsPerUnit {
				// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
				//req.Equal(postSetupStateComplete, status.State)
			} else {
				req.Equal(postSetupStateInProgress, status.State)
			}

			// Compare the chan status to a status queried directly.
			req.Equal(status, mgr.Status())

			lastStatus = status
		}
	}()

	// Create data.
	doneChan, err := mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
	// req.Equal(lastStatus, mgr.Status())

	// Create data (same opts).
	doneChan, err = mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())

	// Cleanup.
	err = mgr.StopSession(true)
	req.NoError(err)

	// Create data (same opts, after deletion).
	doneChan, err = mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
	// req.Equal(lastStatus, mgr.Status())
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	req := require.New(t)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Verify the initial status.
	status := mgr.Status()
	req.Equal(postSetupStateNotStarted, status.State)
	req.Equal(uint64(0), status.NumLabelsWritten)
	req.Equal((*PostSetupOpts)(nil), status.LastOpts)
	req.Equal(nil, status.LastError)

	//var lastStatus = &PostSetupStatus{}
	//go func() {
	//	for status := range mgr.StatusChan() {
	//		lastStatus = status
	//	}
	//}()

	// Create data.
	doneChan, err := mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan

	// Compare the last status update to the status queried directly.
	// TODO(moshababo): fix the following failure. `status.State` changes to `postSetupStateComplete` only after the channel event was triggered.
	//req.Equal(lastStatus, mgr.Status())

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Verify the initial status.
	status = mgr.Status()
	req.Equal(postSetupStateNotStarted, status.State)
	req.Equal(uint64(0), status.NumLabelsWritten)
	req.Equal((*PostSetupOpts)(nil), status.LastOpts)
	req.Equal(nil, status.LastError)
}

func TestPostSetupManager_GenerateProof(t *testing.T) {
	req := require.New(t)
	ch := make([]byte, 32)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.EqualError(err, errNotComplete.Error())

	// Create data.
	doneChan, err := mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan

	// Generate proof.
	_, _, err = mgr.GenerateProof(make([]byte, 32))
	req.NoError(err)

	// Re-instantiate `PostSetupManager`.
	mgr, err = NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.ErrorIs(err, errNotComplete)
}

func TestPostSetupManager_StatusChan_BeforeSessionStarted(t *testing.T) {
	req := require.New(t)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Verify that the status stream works properly when called *before* session started.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		ch := mgr.StatusChan()
		var prevStatus = (*PostSetupStatus)(nil)
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
	time.Sleep(1 * time.Second) // Short delay.
	doneChan, err := mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan
	wg.Wait()

	// Cleanup.
	err = mgr.StopSession(true)
	req.NoError(err)
}

func TestPostSetupManager_StatusChan_AfterSessionStarted(t *testing.T) {
	req := require.New(t)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Verify that the status stream works properly when called *after* session started (yet before it ended).
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		time.Sleep(100 * time.Millisecond) // Short delay.

		ch := mgr.StatusChan()
		var prevStatus = (*PostSetupStatus)(nil)
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
				req.Equal(longerSetupOpts.NumUnits*cfg.LabelsPerUnit, uint(prevStatus.NumLabelsWritten))
				break
			}
		}
		wg.Done()
	}()

	// Create data.
	doneChan, err := mgr.StartSession(longerSetupOpts)
	req.NoError(err)
	<-doneChan
	wg.Wait()

	// Cleanup.
	err = mgr.StopSession(true)
	req.NoError(err)
}

func TestPostSetupManager_Stop(t *testing.T) {
	req := require.New(t)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Verify state.
	status := mgr.Status()
	req.Equal(postSetupStateNotStarted, status.State)

	// Create data.
	doneChan, err := mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan

	// Verify state.
	status = mgr.Status()
	req.Equal(postSetupStateComplete, status.State)

	// Stop without file deletion.
	err = mgr.StopSession(false)
	req.NoError(err)

	// Verify state.
	status = mgr.Status()
	req.Equal(postSetupStateComplete, status.State)

	// Stop with file deletion.
	err = mgr.StopSession(true)
	req.NoError(err)

	// Verify state.
	status = mgr.Status()
	req.Equal(postSetupStateNotStarted, status.State)

	// Create data again.
	doneChan, err = mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan

	// Verify state.
	status = mgr.Status()
	req.Equal(postSetupStateComplete, status.State)
}

func TestPostSetupManager_Stop_WhileInProgress(t *testing.T) {
	req := require.New(t)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Create data.
	doneChan, err := mgr.StartSession(longerSetupOpts)
	req.NoError(err)

	// Wait a bit for the setup to proceed.
	time.Sleep(100 * time.Millisecond)

	// Verify the intermediate status.
	status := mgr.Status()
	req.Equal(&longerSetupOpts, status.LastOpts)
	req.Equal(postSetupStateInProgress, status.State)

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
	req.Equal((*PostSetupOpts)(nil), status.LastOpts)
	req.Equal(postSetupStateNotStarted, status.State)
	req.Equal(uint64(0), status.NumLabelsWritten)

	// Continue to create data.
	doneChan, err = mgr.StartSession(longerSetupOpts)
	req.NoError(err)
	<-doneChan

	// Verify status.
	status = mgr.Status()
	req.Equal(&longerSetupOpts, status.LastOpts)
	req.Equal(postSetupStateComplete, status.State)
	req.Equal(longerSetupOpts.NumUnits*cfg.LabelsPerUnit, uint(status.NumLabelsWritten))
}
