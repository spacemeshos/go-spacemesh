package sync

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

var (
	sim = simulator.New()
	n1  = sim.NewNode()
	n2  = sim.NewNode()
)

type BlockValidatorMock struct {
}

func (BlockValidatorMock) ValidateBlock(block Block) bool {
	fmt.Println("validate block ", block)
	return true
}

func TestSyncer_Status(t *testing.T) {
	sync := NewSync(NewPeers(simulator.New().NewNode()), nil, BlockValidatorMock{}, Configuration{1, 1, 100 * time.Millisecond, 1, 10 * time.Second})
	assert.True(t, sync.Status() == IDLE, "status was running")
}

func TestSyncer_Start(t *testing.T) {
	layers := mesh.NewLayers(nil, nil)
	sync := NewSync(PeersImpl{n2, func() []Peer { return []Peer{n1.PublicKey()} }}, layers, BlockValidatorMock{}, Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second})
	fmt.Println(sync.Status())
	sync.Start()
	for i := 0; i < 5 && sync.Status() == IDLE; i++ {
		time.Sleep(1 * time.Second)
	}
	assert.True(t, sync.Status() == RUNNING, "status was idle")
}

func TestSyncer_Close(t *testing.T) {
	sync := NewSync(NewPeers(simulator.New().NewNode()), nil, BlockValidatorMock{}, Configuration{1, 1, 100 * time.Millisecond, 1, 10 * time.Second})
	sync.Start()
	sync.Close()
	s := sync
	_, ok := <-s.forceSync
	assert.True(t, !ok, "channel 'forceSync' still open")
	_, ok = <-s.exit
	assert.True(t, !ok, "channel 'exit' still open")
}

func TestSyncProtocol_AddMsgHandlers(t *testing.T) {
	syncObj := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	id := uuid.New().ID()
	block := mesh.NewExistingBlock(id, 1, nil)
	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block}))
	fnd2 := p2p.NewProtocol(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(BLOCK, syncObj.BlockRequestHandler)

	ch, err := syncObj.SendBlockRequest(n2.Node.PublicKey(), block.Id())
	a := <-ch

	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, a.GetId(), block.Id(), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers2(t *testing.T) {
	syncObj := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 0, 10)))
	fnd2 := p2p.NewProtocol(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(LAYER_HASH, syncObj.LayerHashRequestHandler)

	hash, err := syncObj.SendLayerHashRequest(n2.Node.PublicKey(), 1)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers3(t *testing.T) {
	syncObj := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)
	layer := mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 0, 10))
	layer.AddBlock(mesh.NewExistingBlock(uuid.New().ID(), 1, nil))
	layer.AddBlock(mesh.NewExistingBlock(uuid.New().ID(), 1, nil))
	layer.AddBlock(mesh.NewExistingBlock(uuid.New().ID(), 1, nil))
	layer.AddBlock(mesh.NewExistingBlock(uuid.New().ID(), 1, nil))
	syncObj.layers.AddLayer(layer)
	fnd2 := p2p.NewProtocol(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(LAYER_IDS, syncObj.LayerIdsRequestHandler)
	hashCh, err := syncObj.SendLayerIDsRequest(n2.Node.PublicKey(), 1)
	ids := <-hashCh
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, len(layer.Blocks()), len(ids), "wrong block")
	for _, a := range layer.Blocks() {
		found := false
		for _, id := range ids {
			if a.Id() == id {
				found = true
				break
			}
		}
		if !found {
			t.Error(errors.New("id list did not match"))
		}
	}
}

func TestSyncProtocol_AddMsgHandlers4(t *testing.T) {
	syncObj1 := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	syncObj2 := NewSync(NewPeers(n2),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	block1 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block2 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block3 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)

	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block1}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(2), []*mesh.Block{block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(3), []*mesh.Block{block3}))

	hash, err := syncObj2.SendLayerHashRequest(n1.PublicKey(), 1)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash), "wrong block")

	ch2, err2 := syncObj2.SendBlockRequest(n1.PublicKey(), block1.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 := <-ch2
	assert.Equal(t, msg2.GetId(), block1.Id(), "wrong block")

	hash, err = syncObj2.SendLayerHashRequest(n1.PublicKey(), 2)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash), "wrong block")

	ch2, err2 = syncObj2.SendBlockRequest(n1.PublicKey(), block2.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.GetId(), block2.Id(), "wrong block")

	hash, err = syncObj2.SendLayerHashRequest(n1.PublicKey(), 3)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash), "wrong block")

	ch2, err2 = syncObj2.SendBlockRequest(n1.PublicKey(), block3.Id())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.GetId(), block3.Id(), "wrong block")
}

func TestSyncProtocol_AddMsgHandlers5(t *testing.T) {
	sim := simulator.New()
	nn1 := sim.NewNode()
	nn2 := sim.NewNode()
	syncObj1 := NewSync(PeersImpl{nn1, func() []Peer { return []Peer{nn2.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 10 * time.Second},
	)

	syncObj2 := NewSync(PeersImpl{nn2, func() []Peer { return []Peer{nn1.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 10 * time.Second},
	)

	syncObj2.layers.SetLatestKnownLayer(5)
	syncObj2.Start()
	syncObj2.ForceSync()

	block1 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block2 := mesh.NewExistingBlock(uuid.New().ID(), 1, nil)
	block3 := mesh.NewExistingBlock(uuid.New().ID(), 2, nil)
	block4 := mesh.NewExistingBlock(uuid.New().ID(), 2, nil)
	block5 := mesh.NewExistingBlock(uuid.New().ID(), 3, nil)
	block6 := mesh.NewExistingBlock(uuid.New().ID(), 3, nil)
	block7 := mesh.NewExistingBlock(uuid.New().ID(), 4, nil)
	block8 := mesh.NewExistingBlock(uuid.New().ID(), 4, nil)
	block9 := mesh.NewExistingBlock(uuid.New().ID(), 5, nil)
	block10 := mesh.NewExistingBlock(uuid.New().ID(), 5, nil)

	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(1), []*mesh.Block{block1, block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(2), []*mesh.Block{block3, block4}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(3), []*mesh.Block{block5, block6}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(3), []*mesh.Block{block7, block8}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(uint32(3), []*mesh.Block{block9, block10}))

	timeout := time.After(10 * time.Second)
	// Keep trying until we're timed out or got a result or got an error
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if syncObj2.Status() == IDLE {
				assert.Equal(t, syncObj2.layers.LocalLayerCount(), uint32(3), "wrong block")
				return
			}
		}
	}
}
