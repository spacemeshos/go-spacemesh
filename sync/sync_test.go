package sync

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/simulator"
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

type BlockValidatorMock struct {
}

func (BlockValidatorMock) ValidateBlock(block Block) bool {
	fmt.Println("validate block ", block)
	return true
}

func TestSyncer_Start(t *testing.T) {
	fmt.Println("test sync start")
	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	NewSync(PeersImpl{n1, func() []Peer { return []Peer{n2.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 10 * time.Second},
	)

	sync := NewSync(PeersImpl{n2, func() []Peer { return []Peer{n1.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 10 * time.Second},
	)
	sync.layers.SetLatestKnownLayer(5)
	fmt.Println(sync.IsSynced())
	defer sync.Close()
	sync.Start()
	timeout := time.After(10 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if atomic.LoadUint32(&sync.startLock) == 1 {
				return
			}
		}
	}
}

func TestSyncer_Close(t *testing.T) {
	fmt.Println("test sync close")
	sync := NewSync(NewPeers(simulator.New().NewNode()), nil, BlockValidatorMock{}, Configuration{1, 1, 100 * time.Millisecond, 1, 10 * time.Second})
	sync.Start()
	sync.Close()
	s := sync
	_, ok := <-s.forceSync
	assert.True(t, !ok, "channel 'forceSync' still open")
	_, ok = <-s.exit
	assert.True(t, !ok, "channel 'exit' still open")
}

func TestSyncProtocol_BlockRequest(t *testing.T) {
	fmt.Println("test sync block request")
	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	syncObj := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	defer syncObj.Close()

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

func TestSyncProtocol_LayerHashRequest(t *testing.T) {
	fmt.Println("test sync layer hash request")
	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	syncObj := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	defer syncObj.Close()

	syncObj.layers.AddLayer(mesh.NewExistingLayer(uint32(1), make([]*mesh.Block, 0, 10)))
	fnd2 := p2p.NewProtocol(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(LAYER_HASH, syncObj.LayerHashRequestHandler)

	hash, err := syncObj.SendLayerHashRequest(n2.Node.PublicKey(), 1)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash), "wrong block")
}

func TestSyncProtocol_LayerIdsRequest(t *testing.T) {
	fmt.Println("test sync layer ids request")
	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	syncObj := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	defer syncObj.Close()

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

func TestSyncProtocol_FetchBlocks(t *testing.T) {
	fmt.Println("test sync layer fetch blocks")
	sim := simulator.New()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	syncObj1 := NewSync(NewPeers(n1),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	defer syncObj1.Close()
	syncObj1.Start()

	syncObj2 := NewSync(NewPeers(n2),
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{1, 1, 1 * time.Millisecond, 1, 10 * time.Second},
	)

	defer syncObj2.Close()
	syncObj1.Start()

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

func TestSyncProtocol_SyncTwoNodes(t *testing.T) {

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
	syncObj2.layers.SetLatestKnownLayer(5)
	defer syncObj2.Close()
	syncObj2.Start()
	// Keep trying until we're timed out or got a result or got an error
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
		default:
			if syncObj2.layers.LocalLayerCount() == 3 {
				t.Log("done!")
				break loop
			}
		}
	}
}

func TestSyncProtocol_SyncMultipalNodes(t *testing.T) {

	sim := simulator.New()
	nn1 := sim.NewNode()
	nn2 := sim.NewNode()
	nn3 := sim.NewNode()
	nn4 := sim.NewNode()

	syncObj1 := NewSync(PeersImpl{nn1, func() []Peer { return []Peer{nn2.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 3 * time.Second},
	)

	syncObj2 := NewSync(PeersImpl{nn2, func() []Peer { return []Peer{nn1.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 3 * time.Second},
	)

	syncObj3 := NewSync(PeersImpl{nn3, func() []Peer { return []Peer{nn1.PublicKey(), nn2.PublicKey(), nn4.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 3 * time.Second},
	)

	syncObj4 := NewSync(PeersImpl{nn4, func() []Peer { return []Peer{nn1.PublicKey(), nn2.PublicKey(), nn3.PublicKey()} }},
		mesh.NewLayers(nil, nil),
		BlockValidatorMock{},
		Configuration{2, 1, 1 * time.Second, 1, 3 * time.Second},
	)

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

	defer syncObj1.Close()
	syncObj1.layers.SetLatestKnownLayer(5)

	defer syncObj2.Close()
	syncObj2.layers.SetLatestKnownLayer(5)
	syncObj2.Start()

	defer syncObj3.Close()
	syncObj3.layers.SetLatestKnownLayer(5)
	syncObj3.Start()

	defer syncObj4.Close()
	syncObj4.layers.SetLatestKnownLayer(5)
	syncObj4.Start()

	// Keep trying until we're timed out or got a result or got an error
	timeout := time.After(20 * time.Second)

loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
		default:
			if syncObj2.layers.LocalLayerCount() == 3 && syncObj3.layers.LocalLayerCount() == 3 {
				t.Log("done!")
				break loop
			}
		}
	}

	fmt.Println("")
}
