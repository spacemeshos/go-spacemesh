package main

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"os"
	"testing"
	"time"
)

func TestSpacemeshApp_TestSyncCmd(t *testing.T) {
	t.Skip("skipped until sync test cloud resources are updated")

	if testing.Short() {
		t.Skip()
	}
	version = "samples"
	syncApp := newSyncApp()
	defer syncApp.Cleanup()
	syncApp.Initialize(cmd)
	syncApp.Config.DataDirParent = "bin/data/"
	lg := log.NewDefault("")

	defer func() {
		err := os.RemoveAll(syncApp.Config.DataDirParent)
		if err != nil {
			t.Error("failed cleaning up", err)
		}
	}()
	if err := getData(syncApp.Config.DataDir(), version, lg); err != nil {
		t.Error("could not download data for test", err)
		return
	}

	go syncApp.start(cmd, nil)

	time.Sleep(5 * time.Second)
	timeout := time.After(20 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if syncApp.sync.ProcessedLayer() > 20 {
				t.Log("done!")
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

}
