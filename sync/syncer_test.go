package sync

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"
	"math/big"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

var conf = Configuration{2 * time.Second, 1, 300, 100 * time.Millisecond}

const (
	levelDB  = "LevelDB"
	memoryDB = "MemoryDB"
	Path     = "../tmp/sync/"
)

type MockTimer struct {
}

func (MockTimer) Now() time.Time {
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	return start
}

func SyncMockFactory(number int, conf Configuration, name string, dbType string) (syncs []*Syncer, p2ps []*service.Node) {
	nodes := make([]*Syncer, 0, number)
	p2ps = make([]*service.Node, 0, number)
	sim := service.NewSimulator()
	tick := 200 * time.Millisecond
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	ts := timesync.NewTicker(MockTimer{}, tick, start)
	tk := ts.Subscribe()
	for i := 0; i < number; i++ {
		net := sim.NewNode()
		name := fmt.Sprintf(name+"_%d", i)
		l := log.New(name, "", "")
		sync := NewSync(net, getMesh(dbType, name+"_"+time.Now().String()), BlockValidatorMock{}, conf, tk, l)
		ts.Start()
		nodes = append(nodes, sync)
		p2ps = append(p2ps, net)
	}
	return nodes, p2ps
}

type BlockValidatorMock struct {
}

func (BlockValidatorMock) BlockEligible(layerID mesh.LayerID, nodeID mesh.NodeId, proof mesh.BlockEligibilityProof) (bool, error) {
	return true, nil
}

type MeshValidatorMock struct{}

func (m *MeshValidatorMock) HandleIncomingLayer(layer *mesh.Layer) (mesh.LayerID, mesh.LayerID) {
	return layer.Index() - 1, layer.Index()
}
func (m *MeshValidatorMock) HandleLateBlock(bl *mesh.Block)              {}
func (m *MeshValidatorMock) RegisterLayerCallback(func(id mesh.LayerID)) {}
func (mlg *MeshValidatorMock) ContextualValidity(id mesh.BlockID) bool   { return true }

type stateMock struct{}

func (s *stateMock) ApplyRewards(layer mesh.LayerID, miners map[string]struct{}, underQuota map[string]struct{}, bonusReward, diminishedReward *big.Int) {

}

func (s *stateMock) ApplyTransactions(id mesh.LayerID, tx mesh.Transactions) (uint32, error) {
	return 0, nil
}

var rewardConf = mesh.RewardConfig{
	big.NewInt(10),
	big.NewInt(5000),
	big.NewInt(15),
	15,
	5,
}

func getMeshWithLevelDB(id string) *mesh.Mesh {
	return mesh.NewPersistentMesh(fmt.Sprintf(Path+"%v/", id), rewardConf, &MeshValidatorMock{}, &stateMock{}, log.New(id, "", ""))
}

func persistenceTeardown() {
	os.RemoveAll(Path)
}

func getMeshWithMemoryDB(id string) *mesh.Mesh {
	return mesh.NewMemMesh(rewardConf, &MeshValidatorMock{}, &stateMock{}, log.New(id, "", ""))
}

func getMesh(dbType, id string) *mesh.Mesh {
	if dbType == levelDB {
		return getMeshWithLevelDB(id)
	}

	return getMeshWithMemoryDB(id)
}

func TestSyncer_Start(t *testing.T) {
	syncs, _ := SyncMockFactory(2, conf, "TestSyncer_Start_", memoryDB)
	sync := syncs[0]
	defer sync.Close()
	sync.SetLatestLayer(5)
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
	syncs, _ := SyncMockFactory(2, conf, "TestSyncer_Close_", memoryDB)
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
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_BlockRequest_", memoryDB)
	syncObj := syncs[0]
	syncObj2 := syncs[1]
	defer syncObj.Close()
	lid := mesh.LayerID(1)
	block := mesh.NewExistingBlock(mesh.BlockID(uuid.New().ID()), lid, []byte("data data data"))
	syncObj.AddBlock(block)
	ch, err := sendBlockRequest(syncObj2.MessageServer, nodes[0].Node.PublicKey(), block.ID(), syncObj.Log)
	timeout := time.NewTimer(2 * time.Second)

	select {
	case a := <-ch:
		assert.NoError(t, err, "Should not return error")
		assert.Equal(t, a.ID(), block.ID(), "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestSyncProtocol_LayerHashRequest(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_LayerHashRequest_", memoryDB)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	lid := mesh.LayerID(1)
	syncObj1.AddBlock(mesh.NewExistingBlock(mesh.BlockID(123), lid, nil))
	//syncObj1.ValidateLayer(l) //this is to simulate the approval of the tortoise...
	timeout := time.NewTimer(2 * time.Second)
	ch, err := syncObj2.sendLayerHashRequest(nodes[0].Node.PublicKey(), lid)
	select {
	case hash := <-ch:
		assert.NoError(t, err, "Should not return error")
		assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestSyncProtocol_LayerIdsRequest(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_LayerIdsRequest_", memoryDB)
	syncObj := syncs[0]
	defer syncObj.Close()
	syncObj1 := syncs[1]
	defer syncObj1.Close()
	lid := mesh.LayerID(1)
	layer := mesh.NewExistingLayer(lid, make([]*mesh.Block, 0, 10))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(123), lid, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(132), lid, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(111), lid, nil))
	layer.AddBlock(mesh.NewExistingBlock(mesh.BlockID(222), lid, nil))

	syncObj1.AddBlock(mesh.NewExistingBlock(mesh.BlockID(123), lid, nil))
	syncObj1.AddBlock(mesh.NewExistingBlock(mesh.BlockID(132), lid, nil))
	syncObj1.AddBlock(mesh.NewExistingBlock(mesh.BlockID(111), lid, nil))
	syncObj1.AddBlock(mesh.NewExistingBlock(mesh.BlockID(222), lid, nil))

	ch, err := syncObj.sendLayerBlockIDsRequest(nodes[1].Node.PublicKey(), lid)
	timeout := time.NewTimer(2 * time.Second)

	select {
	case ids := <-ch:
		assert.NoError(t, err, "Should not return error")
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
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestSyncProtocol_FetchBlocks(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_FetchBlocks_", memoryDB)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	n1 := nodes[0]
	syncObj1.Log.Info("started fetch_blocks")

	block1 := mesh.NewExistingBlock(mesh.BlockID(123), 0, nil)
	block2 := mesh.NewExistingBlock(mesh.BlockID(321), 1, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(222), 2, nil)

	syncObj1.AddBlock(block1)
	syncObj1.AddBlock(block2)
	syncObj1.AddBlock(block3)

	ch, err := syncObj2.sendLayerHashRequest(n1.PublicKey(), 0)
	timeout := time.NewTimer(3 * time.Second)
	var hash *peerHashPair
	select {

	case <-timeout.C:
		t.Error("timed out ")
	case hash = <-ch:
		assert.NoError(t, err, "Should not return error")
		assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")
	}
	ch2, err2 := sendBlockRequest(syncObj2.MessageServer, n1.PublicKey(), block1.ID(), syncObj2.Log)
	assert.NoError(t, err2, "Should not return error")
	timeout = time.NewTimer(3 * time.Second)
	select {

	case <-timeout.C:
		t.Error("timed out ")
	case msg2 := <-ch2:
		assert.Equal(t, msg2.ID(), block1.ID(), "wrong block")
	}
	ch, err = syncObj2.sendLayerHashRequest(n1.PublicKey(), 1)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")
	select {

	case <-timeout.C:
		t.Error("timed out ")
	case <-ch:
	}

	ch2, err2 = sendBlockRequest(syncObj2.MessageServer, n1.PublicKey(), block2.ID(), syncObj2.Log)
	assert.NoError(t, err2, "Should not return error")
	timeout = time.NewTimer(3 * time.Second)
	select {

	case <-timeout.C:
		t.Error("timed out ")
	case msg2 := <-ch2:
		assert.Equal(t, msg2.ID(), block2.ID(), "wrong block")
	}
	ch, err = syncObj2.sendLayerHashRequest(n1.PublicKey(), 2)
	assert.NoError(t, err, "Should not return error")
	assert.Equal(t, "some hash representing the layer", string(hash.hash), "wrong block")
	select {

	case <-timeout.C:
		t.Error("timed out ")
	case <-ch:
	}

	ch2, err2 = sendBlockRequest(syncObj2.MessageServer, n1.PublicKey(), block3.ID(), syncObj2.Log)
	assert.NoError(t, err2, "Should not return error")
	timeout = time.NewTimer(5 * time.Second)
	select {
	case <-timeout.C:
		t.Error("timed out ")
	case msg2 := <-ch2:
		assert.Equal(t, msg2.ID(), block3.ID(), "wrong block")
	}
}

func TestSyncProtocol_SyncTwoNodes(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncer_Start_", memoryDB)
	pm1 := getPeersMock([]p2p.Peer{nodes[1].PublicKey()})
	pm2 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj1 := syncs[0]
	syncObj1.Peers = pm1 //override peers with mock
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.Peers = pm2 //override peers with mock
	defer syncObj2.Close()

	block3 := mesh.NewExistingBlock(mesh.BlockID(333), 1, nil)
	block4 := mesh.NewExistingBlock(mesh.BlockID(444), 1, nil)
	block5 := mesh.NewExistingBlock(mesh.BlockID(555), 2, nil)
	block6 := mesh.NewExistingBlock(mesh.BlockID(666), 2, nil)
	block7 := mesh.NewExistingBlock(mesh.BlockID(777), 3, nil)
	block8 := mesh.NewExistingBlock(mesh.BlockID(888), 3, nil)
	block9 := mesh.NewExistingBlock(mesh.BlockID(999), 4, nil)
	block10 := mesh.NewExistingBlock(mesh.BlockID(101), 5, nil)
	syncObj1.AddBlock(block3)
	syncObj1.AddBlock(block4)
	syncObj1.AddBlock(block5)
	syncObj1.AddBlock(block6)
	syncObj1.AddBlock(block7)
	syncObj1.AddBlock(block8)
	syncObj1.AddBlock(block9)
	syncObj1.AddBlock(block10)
	timeout := time.After(12 * time.Second)
	//syncObj1.Start()
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
			if syncObj2.VerifiedLayer() == 5 {
				t.Log("done!")
				break loop
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func getPeersMock(peers []p2p.Peer) p2p.PeersImpl {
	value := atomic.Value{}
	value.Store(peers)
	pm1 := p2p.NewPeersImpl(&value, make(chan struct{}))
	return *pm1
}

func syncTest(dpType string, t *testing.T) {

	syncs, nodes := SyncMockFactory(4, conf, "SyncMultipleNodes_", dpType)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	syncObj3 := syncs[2]
	defer syncObj3.Close()
	syncObj4 := syncs[3]
	defer syncObj4.Close()
	n1 := nodes[0]
	n2 := nodes[1]
	n4 := nodes[3]

	syncObj1.Peers = getPeersMock([]p2p.Peer{n2.PublicKey()})
	syncObj2.Peers = getPeersMock([]p2p.Peer{n1.PublicKey()})
	syncObj3.Peers = getPeersMock([]p2p.Peer{n1.PublicKey(), n2.PublicKey(), n4.PublicKey()})
	syncObj4.Peers = getPeersMock([]p2p.Peer{n1.PublicKey(), n2.PublicKey()})

	block3 := mesh.NewExistingBlock(mesh.BlockID(333), 1, nil)
	block4 := mesh.NewExistingBlock(mesh.BlockID(444), 1, nil)
	block5 := mesh.NewExistingBlock(mesh.BlockID(555), 2, nil)
	block6 := mesh.NewExistingBlock(mesh.BlockID(666), 2, nil)
	block7 := mesh.NewExistingBlock(mesh.BlockID(777), 3, nil)
	block8 := mesh.NewExistingBlock(mesh.BlockID(888), 3, nil)
	block9 := mesh.NewExistingBlock(mesh.BlockID(999), 4, nil)
	block10 := mesh.NewExistingBlock(mesh.BlockID(101), 4, nil)

	syncObj1.Mesh.ValidateLayer(mesh.GenesisLayer())
	syncObj2.Mesh.ValidateLayer(mesh.GenesisLayer())
	syncObj3.Mesh.ValidateLayer(mesh.GenesisLayer())
	syncObj4.Mesh.ValidateLayer(mesh.GenesisLayer())
	syncObj1.AddBlock(block3)
	syncObj1.AddBlock(block4)
	syncObj1.AddBlock(block5)
	syncObj1.AddBlock(block6)
	syncObj1.AddBlock(block7)
	syncObj1.AddBlock(block8)
	syncObj1.AddBlock(block9)
	syncObj1.AddBlock(block10)

	syncObj2.Start()
	syncObj3.Start()
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
			if syncObj2.VerifiedLayer() == 4 && syncObj3.VerifiedLayer() == 4 {
				t.Log("done!")
				t.Log(syncObj2.VerifiedLayer(), " ", syncObj3.VerifiedLayer())
				break loop
			}
		}
	}
}

func TestSyncProtocol_PersistenceIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	syncTest(levelDB, t)
}

func TestSyncProtocol_SyncMultipleNodes(t *testing.T) {
	syncTest(memoryDB, t)
}

// Integration

type SyncIntegrationSuite struct {
	p2p.IntegrationTestSuite
	syncers []*Syncer
	name    string
	// add more params you need
}

type syncIntegrationTwoNodes struct {
	SyncIntegrationSuite
}

func Test_TwoNodes_SyncIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sis := &syncIntegrationTwoNodes{}
	sis.BootstrappedNodeCount = 2
	sis.BootstrapNodesCount = 1
	sis.NeighborsCount = 2
	sis.name = t.Name()
	i := uint32(1)
	tick := 200 * time.Millisecond
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	ts := timesync.NewTicker(MockTimer{}, tick, start)
	tk := ts.Subscribe()
	sis.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		l := log.New(fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)), "", "")
		sync := NewSync(s, getMesh(memoryDB, fmt.Sprintf("%s_%s", sis.name, time.Now())), BlockValidatorMock{}, conf, tk, l)
		sis.syncers = append(sis.syncers, sync)
		ts.Start()
		atomic.AddUint32(&i, 1)
	}
	suite.Run(t, sis)
}

func (sis *syncIntegrationTwoNodes) TestSyncProtocol_TwoNodes() {
	t := sis.T()
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

	syncObj0 := sis.syncers[0]
	defer syncObj0.Close()
	syncObj1 := sis.syncers[1]
	defer syncObj1.Close()
	syncObj2 := sis.syncers[2]
	defer syncObj2.Close()

	syncObj2.AddBlock(block1)
	syncObj2.AddBlock(block2)
	syncObj2.AddBlock(block3)
	syncObj2.AddBlock(block4)
	syncObj2.AddBlock(block5)
	syncObj2.AddBlock(block6)
	syncObj2.AddBlock(block7)
	syncObj2.AddBlock(block8)
	syncObj2.AddBlock(block9)
	syncObj2.AddBlock(block10)
	timeout := time.After(60 * time.Second)
	syncObj1.SetLatestLayer(5)
	syncObj1.Start()

	// Keep trying until we're timed out or got a result or got an error
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if syncObj1.VerifiedLayer() == 5 {
				t.Log("done!")
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

type syncIntegrationMultipleNodes struct {
	SyncIntegrationSuite
}

func Test_Multiple_SyncIntegrationSuite(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sis := &syncIntegrationMultipleNodes{}
	sis.BootstrappedNodeCount = 3
	sis.BootstrapNodesCount = 2
	sis.NeighborsCount = 3
	sis.name = t.Name()
	i := uint32(1)
	tick := 2 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2018-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	ts := timesync.NewTicker(MockTimer{}, tick, start)
	tk := ts.Subscribe()
	sis.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		l := log.New(fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)), "", "")
		sync := NewSync(s, getMesh(memoryDB, fmt.Sprintf("%s_%d_%s", sis.name, atomic.LoadUint32(&i), time.Now())), BlockValidatorMock{}, conf, tk, l)
		ts.Start()
		sis.syncers = append(sis.syncers, sync)
		atomic.AddUint32(&i, 1)
	}
	suite.Run(t, sis)
}

func (sis *syncIntegrationMultipleNodes) TestSyncProtocol_MultipleNodes() {
	t := sis.T()

	block2 := mesh.NewExistingBlock(mesh.BlockID(222), 1, nil)
	block3 := mesh.NewExistingBlock(mesh.BlockID(333), 2, nil)
	block4 := mesh.NewExistingBlock(mesh.BlockID(444), 2, nil)
	block5 := mesh.NewExistingBlock(mesh.BlockID(555), 3, nil)
	block6 := mesh.NewExistingBlock(mesh.BlockID(666), 3, nil)
	block7 := mesh.NewExistingBlock(mesh.BlockID(777), 4, nil)
	block8 := mesh.NewExistingBlock(mesh.BlockID(888), 4, nil)

	syncObj1 := sis.syncers[0]
	defer syncObj1.Close()
	syncObj2 := sis.syncers[1]
	defer syncObj2.Close()
	syncObj3 := sis.syncers[2]
	defer syncObj3.Close()
	syncObj4 := sis.syncers[3]
	defer syncObj4.Close()
	syncObj5 := sis.syncers[4]
	defer syncObj5.Close()

	syncObj4.AddBlock(block2)
	syncObj4.AddBlock(block3)
	syncObj4.AddBlock(block4)
	syncObj4.AddBlock(block5)
	syncObj4.AddBlock(block6)
	syncObj4.AddBlock(block7)
	syncObj4.AddBlock(block8)

	timeout := time.After(30 * time.Second)
	syncObj1.Start()
	syncObj2.Start()
	syncObj3.Start()
	syncObj4.Start()
	syncObj5.Start()

	// Keep trying until we're timed out or got a result or got an error
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			goto end
		default:
			if syncObj1.VerifiedLayer() >= 3 || syncObj2.VerifiedLayer() >= 3 || syncObj3.VerifiedLayer() >= 3 || syncObj5.VerifiedLayer() >= 3 {
				t.Log("done!")
				goto end
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
end:
	log.Debug("sync 1 ", syncObj1.VerifiedLayer())
	log.Debug("sync 2 ", syncObj2.VerifiedLayer())
	log.Debug("sync 3 ", syncObj3.VerifiedLayer())
	log.Debug("sync 4 ", syncObj4.VerifiedLayer())
	log.Debug("sync 5 ", syncObj5.VerifiedLayer())
	return
}
