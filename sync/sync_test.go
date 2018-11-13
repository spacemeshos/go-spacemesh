package sync

import (
	"fmt"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestSyncer_Status(t *testing.T) {
	sync := NewSync(nil, nil, nil, Configuration{1, 1, 100 * time.Millisecond, 1})
	assert.True(t, sync.Status() == IDLE, "status was running")
}

func TestSyncer_Start(t *testing.T) {
	layers := NewMockLayers()
	sync := NewSync(&PeersMocks{}, layers, nil, Configuration{1, 1, 1 * time.Millisecond, 1})
	fmt.Println(sync.Status())
	sync.Start()
	for i := 0; i < 5 && sync.Status() == IDLE; i++ {
		time.Sleep(1 * time.Second)
	}
	assert.True(t, sync.Status() == RUNNING, "status was idle")
}

func TestSyncer_Close(t *testing.T) {
	sync := NewSync(nil, nil, nil, Configuration{1, 1, 100 * time.Millisecond, 1})
	sync.Start()
	sync.Close()
	s := sync
	_, ok := <-s.forceSync
	assert.True(t, !ok, "channel 'forceSync' still open")
	_, ok = <-s.exit
	assert.True(t, !ok, "channel 'exit' still open")
}

func TestSyncer_ForceSync(t *testing.T) {
	layers := NewMockLayers()
	sync := NewSync(&PeersMocks{}, layers, nil, Configuration{1, 1, 60 * time.Minute, 1})
	sync.Start()

	for i := 0; i < 5 && sync.Status() == RUNNING; i++ {
		time.Sleep(1 * time.Second)
	}

	layers.SetLatestKnownLayer(200)
	sync.ForceSync()
	time.Sleep(5 * time.Second)
	assert.True(t, sync.Status() == RUNNING, "status was idle")
}
