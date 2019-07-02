package sync

import (
	"errors"
	"fmt"
	"github.com/google/uuid"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"math/big"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var conf = Configuration{1, 300, 150 * time.Millisecond}

func init() {
	rand.Seed(time.Now().UnixNano())
}

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

var (
	tx1 = tx()
	tx2 = tx()
	tx3 = tx()
	tx4 = tx()
	tx5 = tx()
	tx6 = tx()
	tx7 = tx()
	tx8 = tx()
)

type poetDbMock struct{}

func (poetDbMock) GetProofMessage(proofRef []byte) ([]byte, error) { return proofRef, nil }

func (poetDbMock) HasProof(proofRef []byte) bool { return true }

func (poetDbMock) ValidateAndStore(proofMessage types.PoetProofMessage) error { return nil }

func newMemPoetDb() PoetDb {
	return activation.NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetDb"))
}

func newMockPoetDb() PoetDb {
	return &poetDbMock{}
}

func SyncMockFactory(number int, conf Configuration, name string, dbType string, poetDb func() PoetDb) (syncs []*Syncer, p2ps []*service.Node) {
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
		sync := NewSync(net, getMesh(dbType, Path+name+"_"+time.Now().String()),
			miner.NewMemPool(reflect.TypeOf(types.SerializableTransaction{})),
			miner.NewMemPool(reflect.TypeOf(types.ActivationTx{})),
			BlockValidatorMock{}, TxValidatorMock{}, poetDb(), conf, tk, 0, l)
		ts.Start()
		nodes = append(nodes, sync)
		p2ps = append(p2ps, net)
	}
	return nodes, p2ps
}

type stateMock struct{}

func (stateMock) ValidateSignature(signed types.Signed) (address.Address, error) {
	return address.Address{}, nil
}

func (s *stateMock) ApplyRewards(layer types.LayerID, miners []address.Address, underQuota map[address.Address]int, bonusReward, diminishedReward *big.Int) {

}

func (s *stateMock) ApplyTransactions(id types.LayerID, tx mesh.Transactions) (uint32, error) {
	return 0, nil
}

func (s *stateMock) ValidateTransactionSignature(tx types.SerializableSignedTransaction) (address.Address, error) {
	return address.Address{}, nil
}

var rewardConf = mesh.Config{
	big.NewInt(10),
	big.NewInt(5000),
	big.NewInt(15),
	15,
	5,
}

func getMeshWithLevelDB(id string) *mesh.Mesh {
	lg := log.New(id, "", "")
	mshdb := mesh.NewPersistentMeshDB(id, lg)
	acdbStore, _ := database.NewLDBDatabase(id+"activation", 0, 0, lg)
	atxdbStore, _ := database.NewLDBDatabase(id+"atx", 0, 0, lg)
	atxdb := activation.NewActivationDb(atxdbStore, acdbStore, &MockIStore{}, mshdb, 10, &ValidatorMock{}, lg.WithName("atxDB"))
	return mesh.NewMesh(mshdb, atxdb, rewardConf, &MeshValidatorMock{}, &MemPoolMock{}, &MemPoolMock{}, &stateMock{}, lg)
}

func persistenceTeardown() {
	os.RemoveAll(Path)
}

func getMeshWithMemoryDB(id string) *mesh.Mesh {
	lg := log.New(id, "", "")
	mshdb := mesh.NewMemMeshDB(lg)
	atxdb := activation.NewActivationDb(database.NewMemDatabase(), database.NewMemDatabase(), &MockIStore{}, mshdb, 10, &ValidatorMock{}, lg.WithName("atxDB"))
	return mesh.NewMesh(mshdb, atxdb, rewardConf, &MeshValidatorMock{}, &MemPoolMock{}, &MemPoolMock{}, &stateMock{}, lg)
}

func getMesh(dbType, id string) *mesh.Mesh {
	if dbType == levelDB {
		return getMeshWithLevelDB(id)
	}

	return getMeshWithMemoryDB(id)
}

func TestSyncer_Start(t *testing.T) {
	syncs, _ := SyncMockFactory(2, conf, "TestSyncer_Start_", memoryDB, newMockPoetDb)
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
	syncs, _ := SyncMockFactory(2, conf, "TestSyncer_Close_", memoryDB, newMockPoetDb)
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
	atx1 := atx()
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_BlockRequest_", memoryDB, newMockPoetDb)
	syncObj := syncs[0]
	syncObj2 := syncs[1]
	defer syncObj.Close()
	lid := types.LayerID(1)
	block := types.NewExistingBlock(types.BlockID(uuid.New().ID()), lid, []byte("data data data"))

	syncObj.AddBlockWithTxs(block, []*types.SerializableTransaction{tx1}, []*types.ActivationTx{atx1})
	syncObj2.Peers = getPeersMock([]p2p.Peer{nodes[0].PublicKey()})

	output := syncObj2.fetchWithFactory(BlockReqFactory([]types.BlockID{block.ID()}), 1)
	timeout := time.NewTimer(2 * time.Second)

	select {
	case a := <-output:
		assert.Equal(t, a.(*types.Block).ID(), block.ID(), "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestSyncProtocol_LayerHashRequest(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_LayerHashRequest_", memoryDB, newMockPoetDb)
	atx1 := atx()
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	lid := types.LayerID(1)
	block := types.NewExistingBlock(types.BlockID(123), lid, nil)
	syncObj1.AddBlockWithTxs(block, []*types.SerializableTransaction{tx1}, []*types.ActivationTx{atx1})
	//syncObj1.ValidateLayer(l) //this is to simulate the approval of the tortoise...
	timeout := time.NewTimer(2 * time.Second)

	wrk, output := NewPeersWorker(syncObj2, []p2p.Peer{nodes[0].PublicKey()}, &sync.Once{}, HashReqFactory(lid))
	go wrk.Work()

	select {
	case hash := <-output:
		assert.Equal(t, "some hash representing the layer", string(hash.(*peerHashPair).hash), "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestSyncer_EnsurePoetProofAvailableAndValid(t *testing.T) {
	r := require.New(t)

	syncs, nodes := SyncMockFactory(2, conf, "TestSyncer_EnsurePoetProofAvailableAndValid_", memoryDB, newMemPoetDb)
	s0 := syncs[0]
	s1 := syncs[1]
	s1.Peers = getPeersMock([]p2p.Peer{nodes[0].PublicKey()})

	proofMessage := makePoetProofMessage(t)

	err := s0.poetDb.ValidateAndStore(proofMessage)
	r.NoError(err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	ref := sha256.Sum256(poetProofBytes)

	err = s1.SyncPoetProof(ref[:])
	r.NoError(err)
}

func makePoetProofMessage(t *testing.T) types.PoetProofMessage {
	r := require.New(t)
	file, err := os.Open(filepath.Join("..", "activation", "test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)
	r.EqualValues([][]byte{[]byte("1"), []byte("2"), []byte("3")}, poetProof.Members)
	var poetId [types.PoetIdLength]byte
	copy(poetId[:], "poet id")
	roundId := uint64(1337)

	return types.PoetProofMessage{
		PoetProof: poetProof,
		PoetId:    poetId,
		RoundId:   roundId,
		Signature: nil,
	}
}

func TestSyncProtocol_LayerIdsRequest(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_LayerIdsRequest_", memoryDB, newMockPoetDb)
	atx1 := atx()
	atx2 := atx()
	atx3 := atx()
	atx4 := atx()
	syncObj := syncs[0]
	defer syncObj.Close()
	syncObj1 := syncs[1]
	defer syncObj1.Close()
	lid := types.LayerID(1)
	layer := types.NewExistingLayer(lid, make([]*types.Block, 0, 10))
	block1 := types.NewExistingBlock(types.BlockID(123), lid, nil)
	syncObj1.AddBlockWithTxs(block1, []*types.SerializableTransaction{tx1}, []*types.ActivationTx{atx1})

	block2 := types.NewExistingBlock(types.BlockID(132), lid, nil)
	syncObj1.AddBlockWithTxs(block2, []*types.SerializableTransaction{tx2}, []*types.ActivationTx{atx2})

	block3 := types.NewExistingBlock(types.BlockID(153), lid, nil)

	syncObj1.AddBlockWithTxs(block3, []*types.SerializableTransaction{tx3}, []*types.ActivationTx{atx3})

	block4 := types.NewExistingBlock(types.BlockID(222), lid, nil)

	syncObj1.AddBlockWithTxs(block4, []*types.SerializableTransaction{tx4}, []*types.ActivationTx{atx4})

	timeout := time.NewTimer(2 * time.Second)

	wrk, output := NewPeersWorker(syncObj, []p2p.Peer{nodes[1].PublicKey()}, &sync.Once{}, LayerIdsReqFactory(lid))
	go wrk.Work()

	select {
	case intr := <-output:
		ids := intr.([]types.BlockID)
		for _, a := range layer.Blocks() {
			found := false
			for _, id := range ids {
				if a.ID() == types.BlockID(id) {
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
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncProtocol_FetchBlocks_", memoryDB, newMockPoetDb)
	atx1 := atx()
	atx2 := atx()
	atx3 := atx()
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	pm1 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj1.Log.Info("started fetch_blocks")
	syncObj2.Peers = pm1 //override peers with

	syncObj1.ProcessAtx(atx3)
	block1 := types.NewExistingBlock(types.BlockID(123), 0, nil)
	block1.ATXID = atx3.Id()
	block2 := types.NewExistingBlock(types.BlockID(321), 1, nil)
	block2.ATXID = atx3.Id()
	block3 := types.NewExistingBlock(types.BlockID(222), 2, nil)
	block3.ATXID = atx3.Id()

	syncObj1.AddBlockWithTxs(block1, []*types.SerializableTransaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx1})
	syncObj1.AddBlockWithTxs(block2, []*types.SerializableTransaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx2})
	syncObj1.AddBlockWithTxs(block3, []*types.SerializableTransaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx3})

	output := syncObj2.fetchWithFactory(BlockReqFactory([]types.BlockID{block1.ID(), block2.ID(), block3.ID()}), 1)
	for out := range output {
		block := out.(*types.Block)
		txs, err := syncObj2.Txs(block)
		if err != nil {
			t.Error("could not fetch all txs", err)
		}
		atxs, _, err := syncObj2.ATXs(block)
		if err != nil {
			t.Error("could not fetch all atxs", err)
		}
		syncObj2.Debug("add block to layer %v", block)
		syncObj2.AddBlockWithTxs(block, txs, atxs)
	}
}

func TestSyncProtocol_SyncTwoNodes(t *testing.T) {
	syncs, nodes := SyncMockFactory(2, conf, "TestSyncer_Start_", memoryDB, newMockPoetDb)
	pm1 := getPeersMock([]p2p.Peer{nodes[1].PublicKey()})
	pm2 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj1 := syncs[0]
	syncObj1.Peers = pm1 //override peers with mock
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.Peers = pm2 //override peers with mock
	defer syncObj2.Close()

	block3 := types.NewExistingBlock(types.BlockID(333), 1, nil)
	block4 := types.NewExistingBlock(types.BlockID(444), 1, nil)
	block5 := types.NewExistingBlock(types.BlockID(555), 2, nil)
	block6 := types.NewExistingBlock(types.BlockID(666), 2, nil)
	block7 := types.NewExistingBlock(types.BlockID(777), 3, nil)
	block8 := types.NewExistingBlock(types.BlockID(888), 3, nil)
	block9 := types.NewExistingBlock(types.BlockID(999), 4, nil)
	block10 := types.NewExistingBlock(types.BlockID(101), 5, nil)

	syncObj1.AddBlockWithTxs(block3, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block4, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block5, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block6, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block7, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block8, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block9, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block10, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})

	timeout := time.After(12 * time.Second)
	syncObj2.SetLatestLayer(6)
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
			if syncObj2.ValidatedLayer() == 5 {

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
	pm1 := p2p.NewPeersImpl(&value, make(chan struct{}), log.NewDefault("peers"))
	return *pm1
}

func syncTest(dpType string, t *testing.T) {

	syncs, nodes := SyncMockFactory(4, conf, "SyncMultipleNodes_", dpType, newMockPoetDb)
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

	block2 := types.NewExistingBlock(types.BlockID(222), 0, nil)
	block3 := types.NewExistingBlock(types.BlockID(333), 1, nil)
	block4 := types.NewExistingBlock(types.BlockID(444), 1, nil)
	block5 := types.NewExistingBlock(types.BlockID(555), 2, nil)
	block6 := types.NewExistingBlock(types.BlockID(666), 2, nil)
	block7 := types.NewExistingBlock(types.BlockID(777), 3, nil)
	block8 := types.NewExistingBlock(types.BlockID(888), 3, nil)
	block9 := types.NewExistingBlock(types.BlockID(999), 4, nil)
	block10 := types.NewExistingBlock(types.BlockID(101), 4, nil)

	syncObj1.ValidateLayer(mesh.GenesisLayer())
	syncObj2.ValidateLayer(mesh.GenesisLayer())
	syncObj3.ValidateLayer(mesh.GenesisLayer())
	syncObj4.ValidateLayer(mesh.GenesisLayer())

	syncObj1.AddBlock(block2)
	syncObj1.AddBlockWithTxs(block3, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block4, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block5, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block6, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block7, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block8, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block9, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block10, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})

	syncObj2.Start()
	syncObj2.SetLatestLayer(5)
	syncObj3.Start()
	syncObj3.SetLatestLayer(5)
	syncObj4.Start()
	syncObj4.SetLatestLayer(5)

	// Keep trying until we're timed out or got a result or got an error
	timeout := time.After(30 * time.Second)

loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
		default:
			if syncObj2.ValidatedLayer() == 4 && syncObj3.ValidatedLayer() == 4 {
				t.Log("done!")
				t.Log(syncObj2.ValidatedLayer(), " ", syncObj3.ValidatedLayer())
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
	sis.NeighborsCount = 1
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
		msh := getMesh(memoryDB, fmt.Sprintf("%s_%s", sis.name, time.Now()))
		poetDb := &poetDbMock{}
		sync := NewSync(s, msh, miner.NewMemPool(reflect.TypeOf(types.SerializableTransaction{})), miner.NewMemPool(reflect.TypeOf(types.ActivationTx{})), BlockValidatorMock{}, TxValidatorMock{}, poetDb, conf, tk, 0, l)
		sis.syncers = append(sis.syncers, sync)
		ts.Start()
		atomic.AddUint32(&i, 1)
	}
	suite.Run(t, sis)
}

func (sis *syncIntegrationTwoNodes) TestSyncProtocol_TwoNodes() {
	t := sis.T()
	block1 := types.NewExistingBlock(types.BlockID(111), 1, nil)
	block2 := types.NewExistingBlock(types.BlockID(222), 1, nil)
	block3 := types.NewExistingBlock(types.BlockID(333), 2, nil)
	block4 := types.NewExistingBlock(types.BlockID(444), 2, nil)
	block5 := types.NewExistingBlock(types.BlockID(555), 3, nil)
	block6 := types.NewExistingBlock(types.BlockID(666), 3, nil)
	block7 := types.NewExistingBlock(types.BlockID(777), 4, nil)
	block8 := types.NewExistingBlock(types.BlockID(888), 4, nil)
	block9 := types.NewExistingBlock(types.BlockID(999), 5, nil)
	block10 := types.NewExistingBlock(types.BlockID(101), 5, nil)

	syncObj0 := sis.syncers[0]
	defer syncObj0.Close()
	syncObj1 := sis.syncers[1]
	defer syncObj1.Close()
	syncObj2 := sis.syncers[2]
	defer syncObj2.Close()

	syncObj2.AddBlock(block1)
	syncObj2.AddBlock(block2)

	syncObj1.AddBlockWithTxs(block3, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block4, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block5, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block6, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block7, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block8, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlock(block9)
	syncObj1.AddBlock(block10)

	timeout := time.After(60 * time.Second)
	syncObj1.SetLatestLayer(6)
	syncObj1.Start()

	// Keep trying until we're timed out or got a result or got an error
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if syncObj1.ValidatedLayer() == 5 {
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
	sis.BootstrappedNodeCount = 4
	sis.BootstrapNodesCount = 1
	sis.NeighborsCount = 2
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
		msh := getMesh(memoryDB, fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)))
		poetDb := &poetDbMock{}
		sync := NewSync(s, msh, miner.NewMemPool(reflect.TypeOf(types.SerializableTransaction{})), miner.NewMemPool(reflect.TypeOf(types.ActivationTx{})), BlockValidatorMock{}, TxValidatorMock{}, poetDb, conf, tk, 0, l)
		ts.Start()
		sis.syncers = append(sis.syncers, sync)
		atomic.AddUint32(&i, 1)
	}
	suite.Run(t, sis)
}

func (sis *syncIntegrationMultipleNodes) TestSyncProtocol_MultipleNodes() {
	t := sis.T()

	block2 := types.NewExistingBlock(types.BlockID(222), 0, nil)
	block3 := types.NewExistingBlock(types.BlockID(333), 1, nil)
	block4 := types.NewExistingBlock(types.BlockID(444), 2, nil)
	block5 := types.NewExistingBlock(types.BlockID(555), 3, nil)
	block6 := types.NewExistingBlock(types.BlockID(666), 3, nil)
	block7 := types.NewExistingBlock(types.BlockID(777), 4, nil)
	block8 := types.NewExistingBlock(types.BlockID(888), 4, nil)

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

	syncObj1.AddBlock(block2)
	syncObj1.AddBlockWithTxs(block3, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block4, []*types.SerializableTransaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block5, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block6, []*types.SerializableTransaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block7, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block8, []*types.SerializableTransaction{tx7, tx8}, []*types.ActivationTx{})

	timeout := time.After(30 * time.Second)
	syncObj1.Start()
	syncObj1.SetLatestLayer(5)
	syncObj2.Start()
	syncObj2.SetLatestLayer(5)
	syncObj3.Start()
	syncObj3.SetLatestLayer(5)
	syncObj4.Start()
	syncObj4.SetLatestLayer(5)
	syncObj5.Start()
	syncObj5.SetLatestLayer(5)

	// Keep trying until we're timed out or got a result or got an error
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			goto end
		default:
			if syncObj1.ValidatedLayer() >= 3 || syncObj2.ValidatedLayer() >= 3 || syncObj3.ValidatedLayer() >= 3 || syncObj5.ValidatedLayer() >= 3 {
				t.Log("done!")
				goto end
			}
			time.Sleep(1 * time.Millisecond)
		}
	}
end:
	log.Debug("sync 1 ", syncObj1.ValidatedLayer())
	log.Debug("sync 2 ", syncObj2.ValidatedLayer())
	log.Debug("sync 3 ", syncObj3.ValidatedLayer())
	log.Debug("sync 4 ", syncObj4.ValidatedLayer())
	log.Debug("sync 5 ", syncObj5.ValidatedLayer())
	return
}

func tx() *types.SerializableTransaction {
	gasPrice := rand.Int63n(100)
	addr := rand.Int63n(1000000)
	tx := types.NewSerializableTransaction(1, address.HexToAddress("1"),
		address.HexToAddress(strconv.FormatUint(uint64(addr), 10)),
		big.NewInt(10),
		big.NewInt(gasPrice),
		100)
	return tx
}

var letterRunes = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func RandStringRunes(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letterRunes[rand.Intn(len(letterRunes))]
	}
	return string(b)
}

func atx() *types.ActivationTx {
	coinbase := address.HexToAddress("aaaa")
	return types.NewActivationTx(types.NodeId{Key: RandStringRunes(5), VRFPublicKey: []byte(RandStringRunes(5))}, coinbase, 1, types.AtxId{}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}), true)
}
