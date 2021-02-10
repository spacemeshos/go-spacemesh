package main

import (
	"testing"
	"time"

	"github.com/spacemeshos/go-spacemesh/log"
)

func TestSpacemeshApp_TestSyncCmd(t *testing.T) {
	t.Skip("skipped until sync test cloud resources are updated")
	syncApp := newSyncApp()
	defer syncApp.Cleanup()
	syncApp.Initialize(cmd)
	syncApp.Config.DataDirParent = "bin/data/"
	lg := log.NewDefault("")

	if err := getData(syncApp.Config.DataDir(), version, lg); err != nil {
		t.Error("could not download data for test", err)
		return
	}

	go syncApp.start(cmd, nil)

	time.Sleep(20 * time.Second)
	timeout := time.After(60 * time.Second)
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
