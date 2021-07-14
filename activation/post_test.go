package activation

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/initialization"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"testing"
)

var (
	id      = make([]byte, 32)
	postLog = log.NewDefault("post-test")

	cfg  PostConfig
	opts PostSetupOpts
)

func init() {
	cfg = DefaultPostConfig()

	opts = DefaultPostSetupOpts()
	opts.DataDir, _ = ioutil.TempDir("", "post-test")
	opts.NumUnits = cfg.MinNumUnits
	opts.ComputeProviderID = initialization.CPUProviderID()
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
				// TODO(moshababo): fix the following failure. `status.State` changes to `PostSetupStateComplete` only after the channel event was triggered.
				//req.Equal(PostSetupStateComplete, status.State)
			} else {
				req.Equal(PostSetupStateInProgress, status.State)
			}

			// Compare the chan status to a status queried directly.
			req.Equal(status, mgr.Status())

			lastStatus = status
		}
	}()

	// Start session.
	doneChan, err := mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	// TODO(moshababo): fix the following failure. `status.State` changes to `PostSetupStateComplete` only after the channel event was triggered.
	// req.Equal(lastStatus, mgr.Status())

	// Start session again.
	doneChan, err = mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())

	// Cleanup.
	err = mgr.StopSession(true)
	req.NoError(err)

	// Start session again.
	doneChan, err = mgr.StartSession(opts)
	req.NoError(err)
	<-doneChan
	req.Equal(opts, *mgr.LastOpts())
	req.NoError(mgr.LastError())
	// TODO(moshababo): fix the following failure. `status.State` changes to `PostSetupStateComplete` only after the channel event was triggered.
	// req.Equal(lastStatus, mgr.Status())
}

func TestPostSetupManager_InitialStatus(t *testing.T) {
	req := require.New(t)

	mgr, err := NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Check the initial status.
	status := mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
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
	// TODO(moshababo): fix the following failure. `status.State` changes to `PostSetupStateComplete` only after the channel event was triggered.
	//req.Equal(lastStatus, mgr.Status())

	// Re-instantiate PostManager.
	mgr, err = NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Check the initial status.
	status = mgr.Status()
	req.Equal(PostSetupStateNotStarted, status.State)
	req.Equal(uint64(0), status.NumLabelsWritten)
	req.Equal((*PostSetupOpts)(nil), status.LastOpts)
	req.Equal(nil, status.LastError)
}

func TestPostManager_GenerateProof(t *testing.T) {
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

	// Re-instantiate PostSetupManager.
	mgr, err = NewPostSetupManager(id, cfg, postLog)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.EqualError(err, errNotComplete.Error())
}

//func TestPostSetupManager_Progress(t *testing.T) {
//	req := require.New(t)
//	tempdir, _ := ioutil.TempDir("", "post-test")
//
//	mgr, err := NewPostSetupManager(id, cfg, postLog)
//	req.NoError(err)
//
//	// Check that the progress stream works properly when called *before* session started.
//	var wg sync.WaitGroup
//	wg.Add(1)
//	go func() {
//		for {
//			_, more := <-mgr.StatusChan()
//			if !more {
//				wg.Done()
//				break
//			}
//		}
//	}()
//
//	// Create data.
//	time.Sleep(1 * time.Second) // Short delay.
//	doneChan, err := mgr.StartSession(opts)
//	req.NoError(err)
//	<-doneChan
//	wg.Wait()
//
//	// Cleanup.
//	err = mgr.StopSession(true)
//	req.NoError(err)
//
//	// Check that the progress stream works properly when called *after* session started.
//	wg.Add(1)
//	go func() {
//		time.Sleep(1 * time.Second) // Short delay.
//		for {
//			_, more := <-mgr.StatusChan()
//			if !more {
//				wg.Done()
//				break
//			}
//		}
//	}()
//
//	// Create data.
//	doneChan, err = mgr.StartSession(opts)
//	req.NoError(err)
//	<-doneChan
//	wg.Wait()
//}
//
//func TestPostManager_Stop(t *testing.T) {
//	req := require.New(t)
//	tempdir, _ := ioutil.TempDir("", "post-test")
//
//	mgr, err := NewPostManager(id, *cfg, postLog)
//	req.NoError(err)
//
//	opts := &PoSTSetupOpts{
//		DataDir:           tempdir,
//		NumLabels:         1 << 15,
//		NumFiles:          1,
//		ComputeProviderID: int(initialization.CPUProviderID()),
//	}
//
//	// Create data.
//	doneChan, err := mgr.CreatePostData(opts)
//	req.NoError(err)
//	<-doneChan
//
//	// Try again.
//	doneChan, err = mgr.CreatePostData(opts)
//	req.EqualError(err, "already completed")
//	req.Nil(doneChan)
//
//	// Stop without file deletion.
//	err = mgr.StopPostDataCreationSession(false)
//	req.NoError(err)
//
//	// Try again.
//	doneChan, err = mgr.CreatePostData(opts)
//	req.EqualError(err, "already completed")
//	req.Nil(doneChan)
//
//	// Stop with file deletion.
//	err = mgr.StopPostDataCreationSession(true)
//	req.NoError(err)
//
//	// Try again.
//	doneChan, err = mgr.CreatePostData(opts)
//	req.NoError(err)
//	<-doneChan
//
//	// Try again.
//	doneChan, err = mgr.CreatePostData(opts)
//	req.EqualError(err, "already completed")
//	req.Nil(doneChan)
//}
//
//func TestPostManager_StopInProgress(t *testing.T) {
//	req := require.New(t)
//	tempdir, _ := ioutil.TempDir("", "post-test")
//
//	mgr, err := NewPostManager(id, *cfg, postLog)
//	req.NoError(err)
//
//	opts := &PoSTSetupOpts{
//		DataDir:           tempdir,
//		NumLabels:         1 << 15,
//		NumFiles:          1,
//		ComputeProviderID: int(initialization.CPUProviderID()),
//	}
//
//	// Create data.
//	doneChan, err := mgr.CreatePostData(opts)
//	req.NoError(err)
//
//	// Wait a bit for the init to progress.
//	time.Sleep(1 * time.Second)
//
//	// Check an intermediate status.
//	status, err := mgr.PostStatus()
//	req.NoError(err)
//	req.Equal(opts, status.SessionOpts)
//	req.True(status.InitInProgress)
//	req.Equal(filesStatusPartial, status.FilesStatus)
//
//	// Stop without files deletion.
//	err = mgr.StopPostDataCreationSession(false)
//	req.NoError(err)
//
//	select {
//	case <-doneChan:
//	default:
//		req.Fail("StopPostDataCreationSession is expected to block until CreatePostData is done")
//	}
//
//	// Check status after stop.
//	status, err = mgr.PostStatus()
//	req.NoError(err)
//	req.Equal(opts, status.SessionOpts)
//	req.False(status.InitInProgress)
//	req.Equal(filesStatusPartial, status.FilesStatus)
//	req.True(status.NumLabelsWritten > 0 && status.NumLabelsWritten < shared.DataSize(opts.NumLabels, cfg.LabelSize))
//
//	// Continue creating PoST data.
//	doneChan, err = mgr.CreatePostData(opts)
//	req.NoError(err)
//	<-doneChan
//}
