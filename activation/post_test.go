package activation

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/post/config"
	"github.com/spacemeshos/post/initialization"
	"github.com/spacemeshos/post/shared"
	"github.com/stretchr/testify/require"
	"io/ioutil"
	"sync"
	"testing"
	"time"
)

var (
	id      = make([]byte, 32)
	postLog = log.NewDefault("post-test")
	cfg     = config.DefaultConfig()
)

func TestPostManager(t *testing.T) {
	req := require.New(t)
	tempdir, _ := ioutil.TempDir("", "post-test")

	mgr, err := NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	opts := &PostInitOpts{
		DataDir:           tempdir,
		NumLabels:         1 << 15,
		NumFiles:          1,
		ComputeProviderID: int(initialization.CPUProviderID()),
	}

	var lastStatus = &PostStatus{}
	go func() {
		for status := range mgr.PostDataCreationProgressStream() {
			req.Equal(opts, status.LastOpts)
			req.True(status.NumLabelsWritten > lastStatus.NumLabelsWritten)
			req.Empty(status.ErrorMessage)
			req.Empty(status.ErrorType)

			if status.NumLabelsWritten == shared.DataSize(opts.NumLabels, cfg.LabelSize) {
				req.Equal(filesStatusCompleted, status.FilesStatus)
				req.False(status.InitInProgress)
			} else {
				req.Equal(filesStatusPartial, status.FilesStatus)
				req.True(status.InitInProgress)

				// Compare the intermediate status update to the status queried directly.
				dStatus, err := mgr.PostStatus()
				req.NoError(err)
				req.Equal(status, dStatus)
			}

			lastStatus = status
		}
	}()

	// Create data.
	doneChan, err := mgr.CreatePostData(opts)
	req.NoError(err)

	// Compare the last status update to the status queried directly.
	<-doneChan
	status, err := mgr.PostStatus()
	req.NoError(err)
	req.Equal(lastStatus, status)

	// Try creating PoST data again.
	doneChan, err = mgr.CreatePostData(opts)
	req.EqualError(err, "already completed")
	req.Nil(doneChan)

	// Cleanup.
	err = mgr.StopPostDataCreationSession(true)
	req.NoError(err)

	// Try creating PoST data again.
	doneChan, err = mgr.CreatePostData(opts)
	req.NoError(err)
	<-doneChan

	// Check final status.
	status, err = mgr.PostStatus()
	req.NoError(err)
	req.Equal(lastStatus, status)
}

func TestPostManager_InitialStatus(t *testing.T) {
	req := require.New(t)
	tempdir, _ := ioutil.TempDir("", "post-test")

	mgr, err := NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	// Check the initial status.
	_, err = mgr.PostStatus()
	req.EqualError(err, errNotInitialized.Error())

	var lastStatus = &PostStatus{}
	go func() {
		for status := range mgr.PostDataCreationProgressStream() {
			lastStatus = status
		}
	}()

	// Create data.
	doneChan, err := mgr.CreatePostData(&PostInitOpts{
		DataDir:           tempdir,
		NumLabels:         1 << 10,
		NumFiles:          cfg.NumFiles,
		ComputeProviderID: int(initialization.CPUProviderID()),
	})
	req.NoError(err)

	// Compare the last status update to the status queried directly.
	<-doneChan
	status, err := mgr.PostStatus()
	req.NoError(err)
	req.Equal(lastStatus, status)

	// Re-instantiate PostManager.
	mgr, err = NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	// Check the initial status.
	status, err = mgr.PostStatus()
	req.EqualError(err, errNotInitialized.Error())
}

func TestPostManager_GenerateProof(t *testing.T) {
	req := require.New(t)
	tempdir, _ := ioutil.TempDir("", "post-test")
	ch := make([]byte, 32)

	mgr, err := NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.EqualError(err, errNotCompleted.Error())

	// Create data.
	doneChan, err := mgr.CreatePostData(&PostInitOpts{
		DataDir:           tempdir,
		NumLabels:         1 << 10,
		NumFiles:          cfg.NumFiles,
		ComputeProviderID: int(initialization.CPUProviderID()),
	})
	req.NoError(err)

	<-doneChan
	_, _, err = mgr.GenerateProof(make([]byte, 32))
	req.NoError(err)

	// Re-instantiate PostManager.
	mgr, err = NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	// Attempt to generate proof.
	_, _, err = mgr.GenerateProof(ch)
	req.EqualError(err, errNotCompleted.Error())
}

func TestPostManager_Progress(t *testing.T) {
	req := require.New(t)
	tempdir, _ := ioutil.TempDir("", "post-test")

	mgr, err := NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	opts := &PostInitOpts{
		DataDir:           tempdir,
		NumLabels:         1 << 15,
		NumFiles:          1,
		ComputeProviderID: int(initialization.CPUProviderID()),
	}

	// Check that the progress stream works properly when called *before* init started.
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		for {
			_, more := <-mgr.PostDataCreationProgressStream()
			if !more {
				wg.Done()
				break
			}
		}
	}()

	// Create data.
	time.Sleep(1 * time.Second) // Short delay.
	doneChan, err := mgr.CreatePostData(opts)
	req.NoError(err)
	<-doneChan
	wg.Wait()

	// Cleanup.
	err = mgr.StopPostDataCreationSession(true)
	req.NoError(err)

	// Check that the progress stream works properly when called *after* init started.
	wg.Add(1)
	go func() {
		time.Sleep(1 * time.Second) // Short delay.
		for {
			_, more := <-mgr.PostDataCreationProgressStream()
			if !more {
				wg.Done()
				break
			}
		}
	}()

	// Create data.
	doneChan, err = mgr.CreatePostData(opts)
	req.NoError(err)
	<-doneChan
	wg.Wait()
}

func TestPostManager_Stop(t *testing.T) {
	req := require.New(t)
	tempdir, _ := ioutil.TempDir("", "post-test")

	mgr, err := NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	opts := &PostInitOpts{
		DataDir:           tempdir,
		NumLabels:         1 << 15,
		NumFiles:          1,
		ComputeProviderID: int(initialization.CPUProviderID()),
	}

	// Create data.
	doneChan, err := mgr.CreatePostData(opts)
	req.NoError(err)
	<-doneChan

	// Try again.
	doneChan, err = mgr.CreatePostData(opts)
	req.EqualError(err, "already completed")
	req.Nil(doneChan)

	// Stop without file deletion.
	err = mgr.StopPostDataCreationSession(false)
	req.NoError(err)

	// Try again.
	doneChan, err = mgr.CreatePostData(opts)
	req.EqualError(err, "already completed")
	req.Nil(doneChan)

	// Stop with file deletion.
	err = mgr.StopPostDataCreationSession(true)
	req.NoError(err)

	// Try again.
	doneChan, err = mgr.CreatePostData(opts)
	req.NoError(err)
	<-doneChan

	// Try again.
	doneChan, err = mgr.CreatePostData(opts)
	req.EqualError(err, "already completed")
	req.Nil(doneChan)
}

func TestPostManager_StopInProgress(t *testing.T) {
	req := require.New(t)
	tempdir, _ := ioutil.TempDir("", "post-test")

	mgr, err := NewPostManager(id, *cfg, postLog)
	req.NoError(err)

	opts := &PostInitOpts{
		DataDir:           tempdir,
		NumLabels:         1 << 15,
		NumFiles:          1,
		ComputeProviderID: int(initialization.CPUProviderID()),
	}

	// Create data.
	doneChan, err := mgr.CreatePostData(opts)
	req.NoError(err)

	// Wait a bit for the init to progress.
	time.Sleep(1 * time.Second)

	// Check an intermediate status.
	status, err := mgr.PostStatus()
	req.NoError(err)
	req.Equal(opts, status.LastOpts)
	req.True(status.InitInProgress)
	req.Equal(filesStatusPartial, status.FilesStatus)

	// Stop without files deletion.
	err = mgr.StopPostDataCreationSession(false)
	req.NoError(err)

	select {
	case <-doneChan:
	default:
		req.Fail("StopPostDataCreationSession is expected to block until CreatePostData is done")
	}

	// Check status after stop.
	status, err = mgr.PostStatus()
	req.NoError(err)
	req.Equal(opts, status.LastOpts)
	req.False(status.InitInProgress)
	req.Equal(filesStatusPartial, status.FilesStatus)
	req.True(status.NumLabelsWritten > 0 && status.NumLabelsWritten < shared.DataSize(opts.NumLabels, cfg.LabelSize))

	// Continue creating PoST data.
	doneChan, err = mgr.CreatePostData(opts)
	req.NoError(err)
	<-doneChan
}
