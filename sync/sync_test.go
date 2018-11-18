package sync

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func TestSyncer_Status(t *testing.T) {
	sync := NewSync(NewPeers(simulator.New().NewNode()), nil, nil, Configuration{1, 1, 100 * time.Millisecond, 1})
	assert.True(t, sync.Status() == IDLE, "status was running")
}

func TestSyncer_Start(t *testing.T) {
	layers := mesh.NewLayers(nil, nil)
	sync := NewSync(NewPeers(simulator.New().NewNode()), layers, nil, Configuration{1, 1, 1 * time.Millisecond, 1})
	fmt.Println(sync.Status())
	sync.Start()
	for i := 0; i < 5 && sync.Status() == IDLE; i++ {
		time.Sleep(1 * time.Second)
	}
	assert.True(t, sync.Status() == RUNNING, "status was idle")
}

func TestSyncer_Close(t *testing.T) {
	sync := NewSync(NewPeers(simulator.New().NewNode()), nil, nil, Configuration{1, 1, 100 * time.Millisecond, 1})
	sync.Start()
	sync.Close()
	s := sync
	_, ok := <-s.forceSync
	assert.True(t, !ok, "channel 'forceSync' still open")
	_, ok = <-s.exit
	assert.True(t, !ok, "channel 'exit' still open")
}

func TestSyncer_ForceSync(t *testing.T) {
	layers := mesh.NewLayers(nil, nil)
	sync := NewSync(NewPeers(simulator.New().NewNode()), layers, nil, Configuration{1, 1, 60 * time.Minute, 1})
	sync.Start()

	for i := 0; i < 5 && sync.Status() == RUNNING; i++ {
		time.Sleep(1 * time.Second)
	}

	layers.SetLatestKnownLayer(200)
	sync.ForceSync()
	time.Sleep(5 * time.Second)
	assert.True(t, sync.Status() == RUNNING, "status was idle")
}

func TestSyncProtocol_AddMsgHandlers(t *testing.T) {

	sim := simulator.New()
	syncObj := NewSync(NewPeers(sim.NewNode()),
		mesh.NewLayers(nil, nil),
		nil,
		Configuration{1, 1, 1 * time.Millisecond, 1},
	)

	n2 := sim.NewNode()
	id := uuid.New().ID()
	block := mesh.NewExistingBlock(id, 1, nil)
	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block}))
	fnd2 := p2p.NewProtocol(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(blockMsg, syncObj.BlockRequestHandler)

	ch, err := syncObj.SendBlockRequest(n2.Node.PublicKey(), block.Id())
	a := <-ch

	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, a.GetId(), block.Id(), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers2(t *testing.T) {

	sim := simulator.New()
	syncObj := NewSync(NewPeers(sim.NewNode()),
		mesh.NewLayers(nil, nil),
		nil,
		Configuration{1, 1, 1 * time.Millisecond, 1},
	)

	n2 := sim.NewNode()
	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 0, 10)))
	fnd2 := p2p.NewProtocol(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(layerHash, syncObj.LayerHashRequestHandler)

	ch, err := syncObj.SendLayerHashRequest(n2.Node.PublicKey(), 1)
	a := <-ch
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(a.Hash), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers3(t *testing.T) {

	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	syncObj1 := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		nil,
		Configuration{1, 1, 1 * time.Millisecond, 1},
	)

	syncObj2 := NewSync(NewPeers(n2),
		mesh.NewLayers(nil, nil),
		nil,
		Configuration{1, 1, 1 * time.Millisecond, 1},
	)

	block1 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block2 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block3 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)

	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block1}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(2), []*mesh.Block{block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(3), []*mesh.Block{block3}))

	ch, err := syncObj2.SendLayerHashRequest(n1.PublicKey(), 1)
	assert.NoError(t, err, "Should not return error")
	msg := <-ch
	assert.Equal(t, "some hash representing the layer", string(msg.Hash), "wrong block")

	ch2, err2 := syncObj2.SendBlockRequest(n1.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 := <-ch2
	assert.Equal(t, msg2.GetId(), block1.Id(), "wrong block")

	ch, err = syncObj2.SendLayerHashRequest(n1.PublicKey(), 2)
	assert.NoError(t, err, "Should not return error")
	msg = <-ch
	assert.Equal(t, "some hash representing the layer", string(msg.Hash), "wrong block")

	ch2, err2 = syncObj2.SendBlockRequest(n1.PublicKey(), block2.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.GetId(), block2.Id(), "wrong block")

	ch, err = syncObj2.SendLayerHashRequest(n1.PublicKey(), 3)
	assert.NoError(t, err, "Should not return error")
	msg = <-ch
	assert.Equal(t, "some hash representing the layer", string(msg.Hash), "wrong block")

	ch2, err2 = syncObj2.SendBlockRequest(n1.PublicKey(), block3.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.GetId(), block3.Id(), "wrong block")
}
