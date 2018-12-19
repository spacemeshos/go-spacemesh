package sync

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

type BlockValidatorMock struct {
}

func getInitializedPeers(p server.ServerService, peers []Peer) Peers {
	value := atomic.Value{}
	value.Store(peers)
	pi := &PeersImpl{snapshot: value, exit: make(chan struct{})}
	go pi.listenToPeers(make(chan crypto.PublicKey), make(chan crypto.PublicKey))
	return pi
}

func (BlockValidatorMock) ValidateBlock(block *mesh.Block) bool {
	fmt.Println("validate block ", block)
	return true
}

func getMesh(id string) mesh.Mesh {
	bdb := database.NewLevelDbStore("blocks_test_"+id, nil, nil)
	ldb := database.NewLevelDbStore("layers_test_"+id, nil, nil)
	cv := database.NewLevelDbStore("contextualy_valid_test_"+id, nil, nil)
	layers := mesh.NewMesh(ldb, bdb, cv)
	return layers
}

func TestSyncer_Start(t *testing.T) {

	fmt.Println("test sync start")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	sync1 := NewSync(
		getInitializedPeers(n1, []Peer{n2.PublicKey()}), n1,
		getMesh("TestSyncer_Start_1"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)
	defer sync1.Close()

	sync := NewSync(
		getInitializedPeers(n2, []Peer{n1.PublicKey()}), n2,
		getMesh("TestSyncer_Start_2"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)
	defer sync.Close()

	sync.layers.SetLatestKnownLayer(5)
	fmt.Println(sync.IsSynced())
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
	n2 := service.NewSimulator().NewNode()
	sync := NewSync(
		getInitializedPeers(n2, []Peer{}), n2,
		getMesh("TestSyncer_Close"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)

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

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	syncObj := NewSync(
		getInitializedPeers(n1, []Peer{n2.PublicKey()}), n1,
		getMesh("TestSyncer_BlockRequest"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)

	defer syncObj.Close()

	block := mesh.NewExistingBlock(mesh.BlockID(uuid.New().ID()), 0, []byte("data data data"))

	syncObj.layers.AddLayer(mesh.NewExistingLayer(0, []*mesh.Block{block}))

	fnd2 := server.NewMsgServer(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(BLOCK, syncObj.blockRequestHandler)

	ch, err := syncObj.sendBlockRequest(n2.Node.PublicKey(), block.ID())
	a := <-ch

	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, a.ID(), block.ID(), "wrong block")
}

func TestSyncProtocol_LayerHashRequest(t *testing.T) {
	fmt.Println("test sync layer hash request")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	syncObj := NewSync(
		getInitializedPeers(n1, []Peer{n2.PublicKey()}), n1,
		getMesh("TestSyncProtocol_LayerHashRequest"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)

	defer syncObj.Close()

	syncObj.layers.AddLayer(mesh.NewExistingLayer(0, make([]*mesh.Block, 0, 10)))
	fnd2 := server.NewMsgServer(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(LAYER_HASH, syncObj.layerHashRequestHandler)
	ch := make(chan peerHashPair)
	_, err := syncObj.sendLayerHashRequest(n2.Node.PublicKey(), 0, ch)
	hash := <-ch
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")
}

func TestSyncProtocol_LayerIdsRequest(t *testing.T) {
	fmt.Println("test sync layer ids request")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	syncObj := NewSync(
		getInitializedPeers(n1, []Peer{n2.PublicKey()}), n1,
		getMesh("TestSyncProtocol_LayerIdsRequest"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)

	defer syncObj.Close()

	layer := mesh.NewExistingLayer(0, make([]*mesh.Block, 0, 10))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(123), 10, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(132), 10, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(111), 10, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(222), 10, nil))
	syncObj.layers.AddLayer(layer)
	fnd2 := server.NewMsgServer(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(LAYER_IDS, syncObj.layerIdsRequestHandler)
	ch := make(chan []uint32)
	_, err := syncObj.sendLayerIDsRequest(n2.Node.PublicKey(), 0, ch)
	ids := <-ch
	assert.NoError(t, err, "Should not return error")
	fmt.Println("blocks " + string(len(layer.Blocks())))
	fmt.Println("ids " + string(len(ids)))
	assert.Equal(t, len(layer.Blocks()), len(ids), "wrong block")

	for _, a := range layer.Blocks() {
		found := false
		for _, id := range ids {
			if a.ID() == mesh.BlockID(id) {
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
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	syncObj1 := NewSync(
		getInitializedPeers(n1, []Peer{n2.PublicKey()}), n1,
		getMesh("TestSyncProtocol_FetchBlocks_1"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)

	defer syncObj1.Close()
	syncObj1.Start()

	syncObj2 := NewSync(
		getInitializedPeers(n2, []Peer{n1.PublicKey()}), n2,
		getMesh("TestSyncProtocol_FetchBlocks_2"),
		BlockValidatorMock{},
		Configuration{1, 1 * time.Millisecond, 1, 300, 10 * time.Second},
	)

	defer syncObj2.Close()
	syncObj2.Start()

	block1 := mesh.NewExistingBlock(mesh.BlockID(123), 0, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(321), 1, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(222), 2, nil)

	syncObj1.layers.AddLayer(mesh.NewExistingLayer(0, []*mesh.Block{block1}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(1, []*mesh.Block{block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(2, []*mesh.Block{block3}))

	ch := make(chan peerHashPair)
	_, err := syncObj2.sendLayerHashRequest(n1.PublicKey(), 0, ch)
	hash := <-ch
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")

	ch2, err2 := syncObj2.sendBlockRequest(n1.PublicKey(), block1.ID())
	assert.NoError(t, err2, "Should not return error")
	msg2 := <-ch2
	assert.Equal(t, msg2.ID(), block1.ID(), "wrong block")

	_, err = syncObj2.sendLayerHashRequest(n1.PublicKey(), 1, ch)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")

	ch2, err2 = syncObj2.sendBlockRequest(n1.PublicKey(), block2.ID())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.ID(), block2.ID(), "wrong block")

	_, err = syncObj2.sendLayerHashRequest(n1.PublicKey(), 2, ch)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")

	ch2, err2 = syncObj2.sendBlockRequest(n1.PublicKey(), block3.ID())
	assert.NoError(t, err2, "Should not return error")
	msg2 = <-ch2
	assert.Equal(t, msg2.ID(), block3.ID(), "wrong block")

}

func TestSyncProtocol_SyncTwoNodes(t *testing.T) {

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	syncObj1 := NewSync(
		getInitializedPeers(n1, []Peer{n2.PublicKey()}), n1,
		getMesh("TestSyncProtocol_SyncTwoNodes_1"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)
	defer syncObj1.Close()
	syncObj2 := NewSync(
		getInitializedPeers(n2, []Peer{n1.PublicKey()}), n2,
		getMesh("TestSyncProtocol_SyncTwoNodes_2"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 1, 300, 10 * time.Second},
	)
	defer syncObj2.Close()
	block1 := mesh.NewExistingBlock(mesh.BlockID(111), 0, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(222), 0, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(333), 1, nil)
	block4 := mesh.NewExistingBlock(mesh.BlockID(444), 1, nil)
	block5 := mesh.NewExistingBlock(mesh.BlockID(555), 2, nil)
	block6 := mesh.NewExistingBlock(mesh.BlockID(666), 2, nil)
	block7 := mesh.NewExistingBlock(mesh.BlockID(777), 3, nil)
	block8 := mesh.NewExistingBlock(mesh.BlockID(888), 3, nil)
	block9 := mesh.NewExistingBlock(mesh.BlockID(999), 4, nil)
	block10 := mesh.NewExistingBlock(mesh.BlockID(101), 4, nil)
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(0, []*mesh.Block{block1, block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(1, []*mesh.Block{block3, block4}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(2, []*mesh.Block{block5, block6}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(3, []*mesh.Block{block7, block8}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(4, []*mesh.Block{block9, block10}))

	timeout := time.After(10 * time.Second)
	syncObj2.layers.SetLatestKnownLayer(5)
	syncObj1.Start()
	syncObj2.Start()

	// Keep trying until we're timed out or got a result or got an error
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if syncObj2.layers.LatestIrreversible() == 3 {
				t.Log("done!")
				break loop
			}
		}
	}
}

func TestSyncProtocol_SyncMultipalNodes(t *testing.T) {

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	n3 := sim.NewNode()
	n4 := sim.NewNode()

	syncObj1 := NewSync(
		getInitializedPeers(n1, []Peer{n2.PublicKey()}), n1,
		getMesh("TestSyncProtocol_SyncMultipalNodes_1"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)

	syncObj2 := NewSync(
		getInitializedPeers(n2, []Peer{n1.PublicKey()}), n2,
		getMesh("TestSyncProtocol_SyncMultipalNodes_2"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)

	syncObj3 := NewSync(
		getInitializedPeers(n3, []Peer{n1.PublicKey(), n2.PublicKey(), n4.PublicKey()}), n3,
		getMesh("TestSyncProtocol_SyncMultipalNodes_3"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)

	syncObj4 := NewSync(
		getInitializedPeers(n4, []Peer{n1.PublicKey(), n2.PublicKey(), n4.PublicKey()}), n4,
		getMesh("TestSyncProtocol_SyncMultipalNodes_4"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)

	block1 := mesh.NewExistingBlock(mesh.BlockID(111), 0, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(222), 0, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(333), 1, nil)
	block4 := mesh.NewExistingBlock(mesh.BlockID(444), 1, nil)
	block5 := mesh.NewExistingBlock(mesh.BlockID(555), 2, nil)
	block6 := mesh.NewExistingBlock(mesh.BlockID(666), 2, nil)
	block7 := mesh.NewExistingBlock(mesh.BlockID(777), 3, nil)
	block8 := mesh.NewExistingBlock(mesh.BlockID(888), 3, nil)
	block9 := mesh.NewExistingBlock(mesh.BlockID(999), 4, nil)
	block10 := mesh.NewExistingBlock(mesh.BlockID(101), 4, nil)

	syncObj1.layers.AddLayer(mesh.NewExistingLayer(0, []*mesh.Block{block1, block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(1, []*mesh.Block{block3, block4}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(2, []*mesh.Block{block5, block6}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(3, []*mesh.Block{block7, block8}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(4, []*mesh.Block{block9, block10}))

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
	timeout := time.After(30 * time.Second)

loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
		default:
			if syncObj2.layers.LatestIrreversible() == 3 && syncObj3.layers.LatestIrreversible() == 3 {
				t.Log("done!")
				break loop
			}
		}
	}
}
