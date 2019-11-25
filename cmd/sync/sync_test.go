package main

import (
	"github.com/spacemeshos/go-spacemesh/log"
	"testing"
	"time"
)

func TestSpacemeshApp_TestSyncCmd(t *testing.T) {
	//t.Skip("skipped until sync test cloud resources are updated")
	syncApp := NewSyncApp()
	defer syncApp.Cleanup()
	syncApp.Initialize(Cmd)
	syncApp.Config.DataDir = "bin/data/"
	lg := log.New("", "", "")

	if err := GetData(syncApp.Config.DataDir, lg); err != nil {
		t.Error("could not download data for test", err)
		return
	}

	go syncApp.Start(Cmd, nil)

	time.Sleep(20 * time.Second)
	timeout := time.After(60 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if _, err := syncApp.sync.GetLayer(50); err == nil {
				t.Log("done!")
				return
			}
		}
	}

}
