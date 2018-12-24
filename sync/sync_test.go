package sync

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/server"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

var conf = Configuration{2, 1 * time.Second, 1, 300, 1 * time.Second}

func SyncFactory(t testing.TB, bootNodes int, networkSize int, randomConnections int, config Configuration, name string) (syncs []*Syncer) {

	fmt.Println("===============================================================================================")
	fmt.Println("Running Integration test with these parameters : ", bootNodes, " ", networkSize, " ", randomConnections) //todo add what test

	nodes := make([]*Syncer, 0, bootNodes)
	i := 1
	beforeHook := func(net server.Service) {
		sync := NewSync(net, getMesh(name+"_"+time.Now().String()), BlockValidatorMock{}, config)
		fmt.Println("create sync obj number ", i)
		i++
		nodes = append(nodes, sync)
	}
	net := p2p.NewP2PSwarm(beforeHook, nil)
	net.Start(t, bootNodes, networkSize, randomConnections)
	return nodes
}

func SyncMockFactory(number int, conf Configuration, name string) (syncs []*Syncer, p2ps []*service.Node) {
	nodes := make([]*Syncer, 0, number)
	p2ps = make([]*service.Node, 0, number)
	sim := service.NewSimulator()
	for i := 0; i < number; i++ {
		net := sim.NewNode()
		sync := NewSync(net, getMesh(name+"_"+time.Now().String()), BlockValidatorMock{}, conf)
		nodes = append(nodes, sync)
		p2ps = append(p2ps, net)
	}
	return nodes, p2ps
}

type BlockValidatorMock struct {
}

func (BlockValidatorMock) ValidateBlock(block *mesh.Block) bool {
	fmt.Println("validate block ", block)
	return true
}

func getMesh(id string) mesh.Mesh {
	bdb := database.NewLevelDbStore("blocks_test_"+id, nil, nil)
	ldb := database.NewLevelDbStore("layers_test_"+id, nil, nil)
	cv := database.NewLevelDbStore("contextually_valid_test_"+id, nil, nil)
	layers := mesh.NewMesh(ldb, bdb, cv)
	return layers
}

func TestSyncer_Start(t *testing.T) {
	fmt.Println("test sync start")
	syncs, _ := SyncMockFactory(2, conf, "TestSyncer_Start_")
	sync := syncs[0]
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
	syncs, _ := SyncMockFactory(2, conf, "TestSyncer_Close_")
	sync := syncs[0]
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

	syncs, p2ps := SyncMockFactory(2, conf, "TestSyncer_Close_")
	n2 := p2ps[1]
	syncObj := syncs[0]
	defer syncObj.Close()
	lid := mesh.LayerID(1)
	block := mesh.NewExistingBlock(mesh.BlockID(uuid.New().ID()), lid, []byte("data data data"))
	syncObj.layers.AddLayer(mesh.NewExistingLayer(lid, []*mesh.Block{block}))

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
	syncObj := NewSync(n1, getMesh("TestSyncProtocol_LayerHashRequest"), BlockValidatorMock{}, conf)
	defer syncObj.Close()
	lid := mesh.LayerID(1)

	syncObj.layers.AddLayer(mesh.NewExistingLayer(lid, make([]*mesh.Block, 0, 10)))
	fnd2 := server.NewMsgServer(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(LAYER_HASH, syncObj.layerHashRequestHandler)
	ch := make(chan peerHashPair)
	_, err := syncObj.sendLayerHashRequest(n2.Node.PublicKey(), lid, ch)
	hash := <-ch
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")
}

func TestSyncProtocol_LayerIdsRequest(t *testing.T) {
	fmt.Println("test sync layer ids request")
	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()

	syncObj := NewSync(n1, getMesh("TestSyncProtocol_LayerIdsRequest"), BlockValidatorMock{}, conf)
	defer syncObj.Close()
	lid := mesh.LayerID(1)
	layer := mesh.NewExistingLayer(lid, make([]*mesh.Block, 0, 10))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(123), lid, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(132), lid, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(111), lid, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(222), lid, nil))
	syncObj.layers.AddLayer(layer)
	fnd2 := server.NewMsgServer(n2, protocol, time.Second*5)
	fnd2.RegisterMsgHandler(LAYER_IDS, syncObj.layerIdsRequestHandler)
	ch := make(chan []uint32)
	_, err := syncObj.sendLayerIDsRequest(n2.Node.PublicKey(), lid, ch)
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

	syncObj1 := NewSync(n1, getMesh("TestSyncProtocol_FetchBlocks_1"), BlockValidatorMock{}, conf)
	defer syncObj1.Close()
	syncObj1.Start()

	syncObj2 := NewSync(n2, getMesh("TestSyncProtocol_FetchBlocks_2"), BlockValidatorMock{}, conf)
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

	syncs, nodes := SyncMockFactory(2, conf, "TestSyncer_Start_")
	pm1 := getPeersMock([]Peer{nodes[1].PublicKey()})
	pm2 := getPeersMock([]Peer{nodes[0].PublicKey()})
	syncObj1 := syncs[0]
	syncObj1.peers = pm1 //override peers with mock
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.peers = pm2 //override peers with mock
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

func getPeersMock(peers []Peer) PeersImpl {
	value := atomic.Value{}
	value.Store(peers)
	pm1 := PeersImpl{&value, nil}
	return pm1
}

func TestSyncProtocol_SyncMultipalNodes(t *testing.T) {

	sim := service.NewSimulator()
	n1 := sim.NewNode()
	n2 := sim.NewNode()
	n3 := sim.NewNode()
	n4 := sim.NewNode()

	pm1 := getPeersMock([]Peer{n2.PublicKey()})
	pm2 := getPeersMock([]Peer{n1.PublicKey()})
	pm3 := getPeersMock([]Peer{n1.PublicKey(), n2.PublicKey(), n4.PublicKey()})
	pm4 := getPeersMock([]Peer{n1.PublicKey(), n2.PublicKey()})

	syncObj1 := NewSync(
		n1,
		getMesh("TestSyncProtocol_SyncMultipalNodes_1"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)
	syncObj1.peers = pm1

	syncObj2 := NewSync(
		n2,
		getMesh("TestSyncProtocol_SyncMultipalNodes_2"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)
	syncObj2.peers = pm2

	syncObj3 := NewSync(
		n3,
		getMesh("TestSyncProtocol_SyncMultipalNodes_3"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)
	syncObj3.peers = pm3

	syncObj4 := NewSync(
		n4,
		getMesh("TestSyncProtocol_SyncMultipalNodes_4"),
		BlockValidatorMock{},
		Configuration{2, 1 * time.Second, 3, 300, 1 * time.Second},
	)
	syncObj4.peers = pm4

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

func TestSyncProtocol_p2pIntegrationTwoNodes(t *testing.T) {

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
	syncObjs := SyncFactory(t, 1, 1, 1, conf, "systemtest1")
	syncObj1 := syncObjs[0]
	defer syncObj1.Close()
	syncObj2 := syncObjs[1]
	defer syncObj2.Close()
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(0, []*mesh.Block{block1, block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(1, []*mesh.Block{block3, block4}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(2, []*mesh.Block{block5, block6}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(3, []*mesh.Block{block7, block8}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(4, []*mesh.Block{block9, block10}))

	timeout := time.After(60 * time.Second)
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

func TestSyncProtocol_p2pIntegrationMultipleNodes(t *testing.T) {

	block1 := mesh.NewExistingBlock(mesh.BlockID(111), 1, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(222), 1, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(333), 2, nil)
	block4 := mesh.NewExistingBlock(mesh.BlockID(444), 2, nil)
	block5 := mesh.NewExistingBlock(mesh.BlockID(555), 3, nil)
	block6 := mesh.NewExistingBlock(mesh.BlockID(666), 3, nil)
	block7 := mesh.NewExistingBlock(mesh.BlockID(777), 4, nil)
	block8 := mesh.NewExistingBlock(mesh.BlockID(888), 4, nil)
	block9 := mesh.NewExistingBlock(mesh.BlockID(999), 5, nil)
	block10 := mesh.NewExistingBlock(mesh.BlockID(101), 5, nil)
	syncObjs := SyncFactory(t, 1, 2, 2, conf, "systemtest2")

	syncObj1 := syncObjs[0]
	defer syncObj1.Close()
	syncObj2 := syncObjs[1]
	defer syncObj2.Close()
	syncObj3 := syncObjs[2]
	defer syncObj3.Close()

	syncObj1.layers.AddLayer(mesh.NewExistingLayer(1, []*mesh.Block{block1, block2}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(2, []*mesh.Block{block3, block4}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(3, []*mesh.Block{block5, block6}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(4, []*mesh.Block{block7, block8}))
	syncObj1.layers.AddLayer(mesh.NewExistingLayer(5, []*mesh.Block{block9, block10}))

	timeout := time.After(2 * 60 * time.Second)

	syncObj2.layers.SetLatestKnownLayer(5)
	syncObj3.layers.SetLatestKnownLayer(5)
	syncObj2.Start()
	syncObj3.Start()
	syncObj1.Start()
	// Keep trying until we're timed out or got a result or got an error
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if syncObj2.layers.LatestIrreversible() == 3 && syncObj3.layers.LatestIrreversible() == 3 {
				t.Log("done!")
				break loop
			}
		}
	}
}
