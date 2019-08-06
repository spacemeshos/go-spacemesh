package main

import (
	"testing"
	"time"
)

func TestSpacemeshApp_TestSyncCmd(t *testing.T) {
	syncApp := NewSyncApp()
	defer syncApp.Cleanup()
	syncApp.Initialize(Cmd)
	syncApp.Config.DataDir = "bin/data/"
	remote = true
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
