package sync

import (
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/timesync"
	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var conf = Configuration{1000, 1, 300, 500 * time.Millisecond, 100, 5}

func init() {
	rand.Seed(time.Now().UnixNano())
}

const (
	levelDB  = "LevelDB"
	memoryDB = "MemoryDB"
	Path     = "../tmp/sync/"
)

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

var commitment = &types.PostProof{
	Challenge:    []byte(nil),
	MerkleRoot:   []byte("1"),
	ProofNodes:   [][]byte(nil),
	ProvenLeaves: [][]byte(nil),
}

func newMemPoetDb() PoetDb {
	return activation.NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetDb"))
}

func newMockPoetDb() PoetDb {
	return &PoetDbMock{}
}

func SyncMockFactory(number int, conf Configuration, name string, dbType string, poetDb func() PoetDb) (syncs []*Syncer, p2ps []*service.Node, ticker *timesync.TimeClock) {
	nodes := make([]*Syncer, 0, number)
	p2ps = make([]*service.Node, 0, number)
	sim := service.NewSimulator()
	tick := 200 * time.Millisecond
	timer := timesync.RealClock{}
	ts := timesync.NewTicker(timer, tick, timer.Now().Add(tick*-4))
	for i := 0; i < number; i++ {
		net := sim.NewNode()
		name := fmt.Sprintf(name+"_%d", i)
		l := log.New(name, "", "")
		blockValidator := BlockEligibilityValidatorMock{}
		txpool := miner.NewTxMemPool()
		atxpool := miner.NewAtxMemPool()
		sync := NewSync(net, getMesh(dbType, Path+name+"_"+time.Now().String()), txpool, atxpool, blockValidator, poetDb(), conf, ts, l)
		nodes = append(nodes, sync)
		p2ps = append(p2ps, net)
	}
	ts.StartNotifying()
	return nodes, p2ps, ts
}

type stateMock struct{}

func (stateMock) ValidateNonceAndBalance(transaction *types.Transaction) error {
	panic("implement me")
}

func (stateMock) GetLayerApplied(txId types.TransactionId) *types.LayerID {
	panic("implement me")
}

func (stateMock) ValidateSignature(signed types.Signed) (types.Address, error) {
	return types.Address{}, nil
}

func (s *stateMock) ApplyRewards(layer types.LayerID, miners []types.Address, reward *big.Int) {

}

func (s *stateMock) ApplyTransactions(layer types.LayerID, txs []*types.Transaction) (int, error) {
	return 0, nil
}

func (s *stateMock) AddressExists(addr types.Address) bool {
	return true
}

type mockBlocksProvider struct {
	mp map[types.BlockID]struct{}
}

func (mbp mockBlocksProvider) GetGoodPatternBlocks(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	return nil, errors.New("not implemented")
}

var rewardConf = mesh.Config{
	BaseReward: big.NewInt(5000),
}

func getMeshWithLevelDB(id string) *mesh.Mesh {
	lg := log.New(id, "", "")
	mshdb, _ := mesh.NewPersistentMeshDB(id, lg)
	atxdbStore, _ := database.NewLDBDatabase(id+"atx", 0, 0, lg.WithOptions(log.Nop))
	atxdb := activation.NewActivationDb(atxdbStore, &MockIStore{}, mshdb, 10, &ValidatorMock{}, lg.WithOptions(log.Nop))
	return mesh.NewMesh(mshdb, atxdb, rewardConf, &MeshValidatorMock{}, &MockTxMemPool{}, &MockAtxMemPool{}, &stateMock{}, lg.WithOptions(log.Nop))
}

func persistenceTeardown() {
	os.RemoveAll(Path)
}

func getMeshWithMemoryDB(id string) *mesh.Mesh {
	lg := log.New(id, "", "")
	mshdb := mesh.NewMemMeshDB(lg)
	atxdb := activation.NewActivationDb(database.NewMemDatabase(), &MockIStore{}, mshdb, 10, &ValidatorMock{}, lg.WithName("atxDB"))
	return mesh.NewMesh(mshdb, atxdb, rewardConf, &MeshValidatorMock{}, &MockTxMemPool{}, &MockAtxMemPool{}, &stateMock{}, lg)
}

func getMesh(dbType, id string) *mesh.Mesh {
	if dbType == levelDB {
		return getMeshWithLevelDB(id)
	}

	return getMeshWithMemoryDB(id)
}

func TestSyncer_Start(t *testing.T) {
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	syn := syncs[0]
	defer syn.Close()
	syn.Start()
	timeout := time.After(10 * time.Second)
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if atomic.LoadUint32(&syn.startLock) == 1 {
				return
			}
		}
	}
}

func TestSyncer_Close(t *testing.T) {
	syncs, _, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
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
	signer := signing.NewEdSigner()
	atx1 := atx(signer.PublicKey().String())
	_, err := activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
	syncObj := syncs[0]
	syncObj2 := syncs[1]
	defer syncObj.Close()
	block := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	syncObj.AddBlockWithTxs(block, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	syncObj2.Peers = getPeersMock([]p2p.Peer{nodes[0].PublicKey()})

	ch := make(chan []types.Hash32, 1)
	ch <- []types.Hash32{block.Hash32()}

	output := fetchWithFactory(NewFetchWorker(syncObj2, 1, newFetchReqFactory(BLOCK, blocksAsItems), ch, ""))

	timeout := time.NewTimer(2 * time.Second)
	emptyId := types.BlockID{}
	select {
	case a := <-output:
		assert.NotEqual(t, a.(fetchJob).items[0].(*types.Block).Id(), emptyId, "id not set")
		assert.Equal(t, a.(fetchJob).ids[0], block.Hash32(), "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestSyncProtocol_LayerHashRequest(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
	signer := signing.NewEdSigner()
	atx1 := atx(signer.PublicKey().String())
	_, err := activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	lid := types.LayerID(1)
	block := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	syncObj1.AddBlockWithTxs(block, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	//syncObj1.ValidateLayer(l) //this is to simulate the approval of the tortoise...
	timeout := time.NewTimer(2 * time.Second)

	wrk, output := NewPeersWorker(syncObj2, []p2p.Peer{nodes[0].PublicKey()}, &sync.Once{}, HashReqFactory(lid))
	go wrk.Work()

	select {
	case <-output:
		return
		//assert.Equal(t, string(orig.Hash()), string(hash.(*peerHashPair).hash), "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}
}

func TestSyncer_FetchPoetProofAvailableAndValid(t *testing.T) {
	r := require.New(t)

	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMemPoetDb)
	s0 := syncs[0]
	s1 := syncs[1]
	s1.Peers = getPeersMock([]p2p.Peer{nodes[0].PublicKey()})

	proofMessage := makePoetProofMessage(t)

	err := s0.poetDb.ValidateAndStore(&proofMessage)
	r.NoError(err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	ref := sha256.Sum256(poetProofBytes)

	err = s1.FetchPoetProof(ref[:])
	r.NoError(err)
}

func TestSyncer_SyncAtxs_FetchPoetProof(t *testing.T) {
	r := require.New(t)
	signer := signing.NewEdSigner()
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMemPoetDb)
	s0 := syncs[0]
	s1 := syncs[1]
	s1.Peers = getPeersMock([]p2p.Peer{nodes[0].PublicKey()})

	// Store a poet proof and a respective atx in s0.

	proofMessage := makePoetProofMessage(t)

	err := s0.poetDb.ValidateAndStore(&proofMessage)
	r.NoError(err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	poetRef := sha256.Sum256(poetProofBytes)

	atx1 := atx(signer.PublicKey().String())
	atx1.Nipst.PostProof.Challenge = poetRef[:]
	_, err = activation.SignAtx(signer, atx1)
	r.NoError(err)
	err = s0.AtxDB.ProcessAtxs([]*types.ActivationTx{atx1})
	r.NoError(err)

	// Make sure that s1 syncAtxs would fetch the missing poet proof.

	r.False(s1.poetDb.HasProof(poetRef[:]))

	atxs, err := s1.atxQueue.handle([]types.Hash32{atx1.Hash32()})
	r.NoError(err)
	r.Equal(1, len(atxs))

	_, found := atxs[atx1.Hash32()]
	r.True(found)

	r.True(s1.poetDb.HasProof(poetRef[:]))
}

func makePoetProofMessage(t *testing.T) types.PoetProofMessage {
	r := require.New(t)
	file, err := os.Open(filepath.Join("..", "activation", "test_resources", "poet.proof"))
	r.NoError(err)

	var poetProof types.PoetProof
	_, err = xdr.Unmarshal(file, &poetProof)
	r.NoError(err)
	r.EqualValues([][]byte{[]byte("1"), []byte("2"), []byte("3")}, poetProof.Members)
	poetId := []byte("poet_id_12345")
	roundId := "1337"

	return types.PoetProofMessage{
		PoetProof:     poetProof,
		PoetServiceId: poetId,
		RoundId:       roundId,
		Signature:     nil,
	}
}

func TestSyncProtocol_LayerIdsRequest(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
	signer := signing.NewEdSigner()
	atx1 := atx(signer.PublicKey().String())
	atx2 := atx(signer.PublicKey().String())
	atx3 := atx(signer.PublicKey().String())
	atx4 := atx(signer.PublicKey().String())
	syncObj := syncs[0]
	defer syncObj.Close()
	syncObj1 := syncs[1]
	defer syncObj1.Close()
	lid := types.LayerID(1)
	layer := types.NewExistingLayer(lid, make([]*types.Block, 0, 10))
	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	syncObj1.AddBlockWithTxs(block1, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})

	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	syncObj1.AddBlockWithTxs(block2, []*types.Transaction{tx2}, []*types.ActivationTx{atx2})

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx3}, []*types.ActivationTx{atx3})

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	syncObj1.AddBlockWithTxs(block4, []*types.Transaction{tx4}, []*types.ActivationTx{atx4})

	timeout := time.NewTimer(2 * time.Second)

	wrk, output := NewPeersWorker(syncObj, []p2p.Peer{nodes[1].PublicKey()}, &sync.Once{}, LayerIdsReqFactory(lid))
	go wrk.Work()

	select {
	case intr := <-output:
		ids := intr.([]types.BlockID)
		for _, a := range layer.Blocks() {
			found := false
			for _, id := range ids {
				if a.Id() == types.BlockID(id) {
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
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
	signer := signing.NewEdSigner()
	atx1 := atx(signer.PublicKey().String())

	atx2 := atx(signer.PublicKey().String())
	atx3 := atx(signer.PublicKey().String())
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	pm1 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj1.Log.Info("started fetch_blocks")
	syncObj2.Peers = pm1 //override peers with

	err := syncObj1.ProcessAtxs([]*types.ActivationTx{atx3})
	assert.NoError(t, err)
	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block1.ATXID = atx3.Id()
	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block2.ATXID = atx3.Id()
	block3 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block3.ATXID = atx3.Id()
	block1.CalcAndSetId()
	block2.CalcAndSetId()
	block3.CalcAndSetId()

	syncObj1.AddBlockWithTxs(block1, []*types.Transaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx1})
	syncObj1.AddBlockWithTxs(block2, []*types.Transaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx2})
	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx3})

	ch := make(chan []types.Hash32, 3)
	ch <- []types.Hash32{block1.Hash32()}
	ch <- []types.Hash32{block2.Hash32()}
	ch <- []types.Hash32{block3.Hash32()}
	close(ch)
	output := fetchWithFactory(NewFetchWorker(syncObj2, 1, newFetchReqFactory(BLOCK, blocksAsItems), ch, ""))

	for out := range output {
		block := out.(fetchJob).items[0].(*types.Block)
		txs, err := syncObj2.txQueue.HandleTxs(block.TxIds)
		if err != nil {
			t.Error("could not fetch all txs", err)
		}
		atxs, err := syncObj2.atxQueue.HandleAtxs(block.AtxIds)
		if err != nil {
			t.Error("could not fetch all atxs", err)
		}
		syncObj2.Debug("add block to layer %v", block)

		syncObj2.AddBlockWithTxs(block, txs, atxs)
	}
}

func TestSyncProtocol_SyncNodes(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(3, conf, t.Name(), memoryDB, newMockPoetDb)
	pm1 := getPeersMock([]p2p.Peer{nodes[2].PublicKey()})
	pm2 := getPeersMock([]p2p.Peer{nodes[2].PublicKey()})
	pm3 := getPeersMock([]p2p.Peer{nodes[0].PublicKey(), nodes[1].PublicKey()})
	syncObj1 := syncs[0]
	syncObj1.Peers = pm1 //override peers with mock
	defer syncObj1.Close()

	syncObj2 := syncs[1]
	syncObj2.Peers = pm2 //override peers with mock
	defer syncObj2.Close()

	syncObj3 := syncs[2]
	syncObj3.Peers = pm3 //override peers with mock
	defer syncObj3.Close()

	signer := signing.NewEdSigner()

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block3.Signature = signer.Sign(block3.Bytes())
	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block4.Signature = signer.Sign(block4.Bytes())
	block5 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block5.Signature = signer.Sign(block5.Bytes())
	block6 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block6.Signature = signer.Sign(block6.Bytes())
	block7 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block7.Signature = signer.Sign(block7.Bytes())
	block8 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block8.Signature = signer.Sign(block8.Bytes())
	block9 := types.NewExistingBlock(4, []byte(rand.RandString(8)))
	block9.Signature = signer.Sign(block9.Bytes())
	block10 := types.NewExistingBlock(5, []byte(rand.RandString(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block4, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block5, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block6, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block7, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block8, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block9, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block10, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})

	syncObj2.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block4, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block5, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block6, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block7, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block8, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block9, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block10, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})

	syncObj1.Unsubscribe(syncObj1.LayerCh)
	syncObj2.Unsubscribe(syncObj2.LayerCh)
	//syncObj1.Start()
	//syncObj2.Start()

	timeout := time.After(3 * 60 * time.Second)
	//syncObj3.currentLayer = 6
	syncObj3.Start()
	// Keep trying until we're timed out or got a result or got an error
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			if syncObj3.ValidatedLayer() == 5 {

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

	syncs, nodes, clock := SyncMockFactory(4, conf, t.Name(), dpType, newMockPoetDb)
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
	//n4 := nodes[3]

	syncObj1.Peers = getPeersMock([]p2p.Peer{n2.PublicKey()})
	syncObj2.Peers = getPeersMock([]p2p.Peer{n1.PublicKey()})
	syncObj3.Peers = getPeersMock([]p2p.Peer{n1.PublicKey()})
	syncObj4.Peers = getPeersMock([]p2p.Peer{n1.PublicKey()})

	signer := signing.NewEdSigner()

	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block6.Signature = signer.Sign(block6.Bytes())

	block7 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block7.Signature = signer.Sign(block7.Bytes())

	block8 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block8.Signature = signer.Sign(block8.Bytes())

	block9 := types.NewExistingBlock(4, []byte(rand.RandString(8)))
	block9.Signature = signer.Sign(block9.Bytes())

	block10 := types.NewExistingBlock(4, []byte(rand.RandString(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	block11 := types.NewExistingBlock(5, []byte(rand.RandString(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	block12 := types.NewExistingBlock(6, []byte(rand.RandString(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	syncObj1.ValidateLayer(mesh.GenesisLayer())
	syncObj2.ValidateLayer(mesh.GenesisLayer())
	syncObj3.ValidateLayer(mesh.GenesisLayer())
	syncObj4.ValidateLayer(mesh.GenesisLayer())

	syncObj1.AddBlock(block2)
	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block4, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block5, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block6, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block7, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block8, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block9, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block10, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block11, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj1.AddBlockWithTxs(block12, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})

	syncObj1.Start()

	syncObj2.Start()
	syncObj3.Start()
	syncObj4.Start()

	// Keep trying until we're timed out or got a result or got an error
	timeout := time.After(30 * time.Second)

	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
		default:
			if syncObj2.ValidatedLayer() >= 4 && syncObj3.ValidatedLayer() >= 4 {
				log.Info("done!", syncObj2.ValidatedLayer(), syncObj3.ValidatedLayer())
				// path for this UT calling sync.Close before we read from channel causes writing to closed channel
				x := clock.Subscribe()
				<-x
				<-x
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func TestSyncProtocol_PersistenceIntegration(t *testing.T) {
	t.Skip("fails on Travis") // TODO
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
	t.Skip("fails on Travis") // TODO

	sis := &syncIntegrationTwoNodes{}
	sis.BootstrappedNodeCount = 2
	sis.BootstrapNodesCount = 1
	sis.NeighborsCount = 1
	sis.name = t.Name()
	i := uint32(1)
	tick := 20 * time.Second
	layout := "2006-01-02T15:04:05.000Z"
	str := "2016-11-12T11:45:26.371Z"
	start, _ := time.Parse(layout, str)
	ts := timesync.NewTicker(timesync.RealClock{}, tick, start)
	sis.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		l := log.New(fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)), "", "")
		msh := getMesh(memoryDB, fmt.Sprintf("%s_%s", sis.name, time.Now()))
		blockValidator := BlockEligibilityValidatorMock{}
		poetDb := activation.NewPoetDb(database.NewMemDatabase(), l.WithName("poetDb"))
		sync := NewSync(s, msh, miner.NewTxMemPool(), miner.NewAtxMemPool(), blockValidator, poetDb, conf, ts, l)
		sis.syncers = append(sis.syncers, sync)
		atomic.AddUint32(&i, 1)
	}
	ts.StartNotifying()
	suite.Run(t, sis)
}

func (sis *syncIntegrationTwoNodes) TestSyncProtocol_TwoNodes() {
	t := sis.T()
	signer := signing.NewEdSigner()

	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block1.Signature = signer.Sign(block1.Bytes())
	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block2.Signature = signer.Sign(block2.Bytes())
	block3 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block3.Signature = signer.Sign(block3.Bytes())
	block4 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block4.Signature = signer.Sign(block4.Bytes())
	block5 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block5.Signature = signer.Sign(block5.Bytes())
	block6 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block6.Signature = signer.Sign(block6.Bytes())
	block7 := types.NewExistingBlock(4, []byte(rand.RandString(8)))
	block7.Signature = signer.Sign(block7.Bytes())
	block8 := types.NewExistingBlock(4, []byte(rand.RandString(8)))
	block8.Signature = signer.Sign(block8.Bytes())
	block9 := types.NewExistingBlock(5, []byte(rand.RandString(8)))
	block9.Signature = signer.Sign(block9.Bytes())
	block10 := types.NewExistingBlock(5, []byte(rand.RandString(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	syncObj0 := sis.syncers[0]
	defer syncObj0.Close()
	syncObj1 := sis.syncers[1]
	defer syncObj1.Close()
	syncObj2 := sis.syncers[2]
	defer syncObj2.Close()

	id1 := tx1.Id()
	id2 := tx2.Id()
	id3 := tx3.Id()
	id4 := tx4.Id()
	id5 := tx5.Id()
	id6 := tx6.Id()
	id7 := tx7.Id()
	id8 := tx8.Id()

	block3.TxIds = []types.TransactionId{id1, id2, id3}
	block4.TxIds = []types.TransactionId{id1, id2, id3}
	block5.TxIds = []types.TransactionId{id4, id5, id6}
	block6.TxIds = []types.TransactionId{id4, id5, id6}
	block7.TxIds = []types.TransactionId{id7, id8}
	block8.TxIds = []types.TransactionId{id7, id8}

	block1.CalcAndSetId()
	block2.CalcAndSetId()
	block3.CalcAndSetId()
	block4.CalcAndSetId()
	block5.CalcAndSetId()
	block6.CalcAndSetId()
	block7.CalcAndSetId()
	block8.CalcAndSetId()
	block9.CalcAndSetId()
	block10.CalcAndSetId()

	syncObj2.AddBlock(block1)
	syncObj2.AddBlock(block2)
	syncObj2.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block4, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block5, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block6, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block7, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj2.AddBlockWithTxs(block8, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	syncObj2.AddBlock(block9)
	syncObj2.AddBlock(block10)

	timeout := time.After(60 * time.Second)
	syncObj1.SetLatestLayer(6)
	syncObj1.Start()

	syncObj0.Start()
	syncObj2.Start()

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
	t.Skip("fails on Travis") // TODO

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
	ts := timesync.NewTicker(timesync.RealClock{}, tick, start)
	sis.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		l := log.New(fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)), "", "")
		msh := getMesh(memoryDB, fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)))
		blockValidator := BlockEligibilityValidatorMock{}
		poetDb := activation.NewPoetDb(database.NewMemDatabase(), l.WithName("poetDb"))
		sync := NewSync(s, msh, miner.NewTxMemPool(), miner.NewAtxMemPool(), blockValidator, poetDb, conf, ts, l)
		ts.StartNotifying()
		sis.syncers = append(sis.syncers, sync)
		atomic.AddUint32(&i, 1)
	}
	suite.Run(t, sis)
}

func (sis *syncIntegrationMultipleNodes) TestSyncProtocol_MultipleNodes() {
	t := sis.T()
	signer := signing.NewEdSigner()

	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(2, []byte(rand.RandString(8)))
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(3, []byte(rand.RandString(8)))
	block6.Signature = signer.Sign(block6.Bytes())

	block7 := types.NewExistingBlock(4, []byte(rand.RandString(8)))
	block7.Signature = signer.Sign(block7.Bytes())

	block8 := types.NewExistingBlock(4, []byte(rand.RandString(8)))
	block8.Signature = signer.Sign(block8.Bytes())

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

	err := syncObj1.AddBlock(block2)
	require.NoError(t, err)
	err = syncObj2.AddBlock(block2)
	require.NoError(t, err)
	err = syncObj3.AddBlock(block2)
	require.NoError(t, err)
	err = syncObj4.AddBlock(block2)
	require.NoError(t, err)
	err = syncObj5.AddBlock(block2)
	require.NoError(t, err)
	err = syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	err = syncObj1.AddBlockWithTxs(block4, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})
	err = syncObj1.AddBlockWithTxs(block5, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	err = syncObj1.AddBlockWithTxs(block6, []*types.Transaction{tx4, tx5, tx6}, []*types.ActivationTx{})
	err = syncObj1.AddBlockWithTxs(block7, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})
	err = syncObj1.AddBlockWithTxs(block8, []*types.Transaction{tx7, tx8}, []*types.ActivationTx{})

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
			time.Sleep(100 * time.Millisecond)
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

func tx() *types.Transaction {
	fee := rand.Uint64()
	addr := rand.Int63n(1000000)
	tx, err := mesh.NewSignedTx(1, types.HexToAddress(strconv.FormatUint(uint64(addr), 10)), 10, 100, fee, signing.NewEdSigner())
	if err != nil {
		log.Panic("failed to create transaction: %v", err)
	}
	return tx
}

func atx(pubkey string) *types.ActivationTx {
	coinbase := types.HexToAddress("aaaa")
	chlng := types.HexToHash32("0x3333")
	poetRef := []byte{0xde, 0xad}
	npst := activation.NewNIPSTWithChallenge(&chlng, poetRef)

	atx := types.NewActivationTx(types.NodeId{Key: pubkey, VRFPublicKey: []byte(rand.RandString(8))}, coinbase, 0, *types.EmptyAtxId, 5, 1, *types.EmptyAtxId, 0, nil, npst)
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = commitment.MerkleRoot
	atx.CalcAndSetId()
	return atx
}

func TestSyncer_Txs(t *testing.T) {
	// check tx validation
	syncs, nodes, _ := SyncMockFactory(3, conf, t.Name(), memoryDB, newMockPoetDb)
	pm1 := getPeersMock([]p2p.Peer{nodes[1].PublicKey()})
	pm2 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	pm3 := getPeersMock([]p2p.Peer{nodes[0].PublicKey()})
	syncObj1 := syncs[0]
	syncObj1.Peers = pm1 //override peers with mock
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.Peers = pm2 //override peers with mock
	defer syncObj2.Close()
	syncObj3 := syncs[2]
	syncObj3.Peers = pm3 //override peers with mock
	defer syncObj3.Close()

	block3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	id1 := tx1.Id()
	id2 := tx2.Id()
	id3 := tx3.Id()
	block3.TxIds = []types.TransactionId{id1, id2, id3}
	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})

	_, err := syncObj2.txQueue.handle([]types.Hash32{id1.Hash32(), id2.Hash32(), id3.Hash32()})
	assert.Nil(t, err)
}

func TestFetchLayerBlockIds(t *testing.T) {
	// check tx validation
	syncs, nodes, _ := SyncMockFactory(3, conf, t.Name(), memoryDB, newMockPoetDb)
	pm1 := getPeersMock([]p2p.Peer{nodes[2].PublicKey()})
	pm2 := getPeersMock([]p2p.Peer{nodes[2].PublicKey()})
	pm3 := getPeersMock([]p2p.Peer{nodes[0].PublicKey(), nodes[1].PublicKey()})

	block1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	block2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	syncObj1 := syncs[0]
	syncObj1.Peers = pm1 //override peers with mock
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.Peers = pm2 //override peers with mock
	defer syncObj2.Close()
	syncObj3 := syncs[2]
	syncObj3.Peers = pm3 //override peers with mock
	defer syncObj3.Close()

	syncObj1.AddBlock(block1)
	syncObj2.AddBlock(block2)

	mp := map[types.Hash32][]p2p.Peer{}
	hash1, _ := types.CalcBlocksHash32([]types.BlockID{block1.Id()})
	mp[hash1] = append(mp[hash1], nodes[0].PublicKey())
	hash2, _ := types.CalcBlocksHash32([]types.BlockID{block2.Id()})
	mp[hash2] = append(mp[hash2], nodes[1].PublicKey())
	ids, _ := syncObj3.fetchLayerBlockIds(mp, 1)

	assert.True(t, len(ids) == 2)

	if ids[0] != block1.Id() && ids[1] != block1.Id() {
		t.Error("did not get ids from all peers")
	}

	if ids[0] != block2.Id() && ids[1] != block2.Id() {
		panic("did not get ids from all peers")
	}

}

type mockLayerValidator struct {
	vl              types.LayerID // the validated layer
	countValidated  int
	countValidate   int
	validatedLayers map[types.LayerID]struct{}
}

func (m *mockLayerValidator) ValidatedLayer() types.LayerID {
	m.countValidated++
	return m.vl
}

func (m *mockLayerValidator) ValidateLayer(lyr *types.Layer) {
	m.countValidate++
	m.vl = lyr.Index()
	if m.validatedLayers == nil {
		m.validatedLayers = make(map[types.LayerID]struct{})
	}

	m.validatedLayers[lyr.Index()] = struct{}{}
}

func TestSyncer_Synchronise(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.lValidator = lv

	sr := sync.getSyncRoutine()
	sr()
	time.Sleep(100 * time.Millisecond) // handle go routine race
	r.Equal(1, lv.countValidated)
	r.Equal(0, lv.countValidate)

	sync.AddBlock(types.NewExistingBlock(1, []byte(rand.RandString(8))))
	sync.AddBlock(types.NewExistingBlock(2, []byte(rand.RandString(8))))
	sync.AddBlock(types.NewExistingBlock(3, []byte(rand.RandString(8))))
	lv = &mockLayerValidator{1, 0, 0, nil}
	sync.lValidator = lv
	sync.currentLayer = 3
	sr()
	time.Sleep(100 * time.Millisecond) // handle go routine race
	r.Equal(1, lv.countValidated)
	r.Equal(1, lv.countValidate) // synced, expect only one call

	lv = &mockLayerValidator{1, 0, 0, nil}
	sync.lValidator = lv
	sync.currentLayer = 4 // simulate not synced
	sr()
	time.Sleep(100 * time.Millisecond) // handle go routine race
	r.Equal(1, lv.countValidate)       // not synced, expect one call
}

func TestSyncer_Synchronise2(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	sync.AddBlockWithTxs(types.NewExistingBlock(1, []byte(rand.RandString(8))), nil, nil)
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.lValidator = lv
	sync.currentLayer = 1
	r.False(sync.gossipSynced == Done)

	// current layer = 0
	sync.currentLayer = 0
	sync.syncRoutineWg.Add(1)
	sync.Synchronise()
	r.Equal(0, lv.countValidate)
	r.True(sync.gossipSynced == Done)

	// current layer = 1
	sync.currentLayer = 1
	sync.syncRoutineWg.Add(1)
	sync.Synchronise()
	r.Equal(0, lv.countValidate)
	r.True(sync.gossipSynced == Done)

	// validated layer = 5 && current layer = 6 -> don't call validate
	lv = &mockLayerValidator{5, 0, 0, nil}
	sync.lValidator = lv
	sync.currentLayer = 6
	sync.syncRoutineWg.Add(1)
	sync.SetLatestLayer(5)
	sync.Synchronise()
	r.Equal(0, lv.countValidate)
	r.True(sync.gossipSynced == Done)

	// current layer != 1 && weakly-synced
	lv = &mockLayerValidator{0, 0, 0, nil}
	sync.lValidator = lv
	sync.currentLayer = 2
	sync.SetLatestLayer(2)
	sync.syncRoutineWg.Add(1)
	sync.Synchronise()
	r.Equal(1, lv.countValidate)
	r.True(sync.gossipSynced == Done)
}

func TestSyncer_ListenToGossip(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	sync.AddBlockWithTxs(types.NewExistingBlock(1, []byte(rand.RandString(8))), nil, nil)
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.lValidator = lv
	sync.currentLayer = 1
	sync.SetLatestLayer(1)
	r.False(sync.gossipSynced == Done)
	assert.False(t, sync.ListenToGossip())

	//run sync
	sync.getSyncRoutine()()

	//check gossip open
	assert.True(t, sync.ListenToGossip())
}

func TestSyncer_handleNotSyncedFlow(t *testing.T) {
	r := require.New(t)
	txpool := miner.NewTxMemPool()
	atxpool := miner.NewAtxMemPool()
	ts := &MockClock{}
	sync := NewSync(service.NewSimulator().NewNode(), getMesh(memoryDB, Path+t.Name()+"_"+time.Now().String()), txpool, atxpool, BlockEligibilityValidatorMock{}, newMockPoetDb(), conf, ts, log.NewDefault(t.Name()))
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.lValidator = lv
	sync.currentLayer = 10
	sync.SetLatestLayer(20)
	go sync.handleNotSynced(10)
	time.Sleep(100 * time.Millisecond)
	r.Equal(2, ts.countSub)
}

func TestSyncer_p2pSyncForTwoLayers(t *testing.T) {
	r := require.New(t)
	timer := &MockClock{Layer: 5}
	sim := service.NewSimulator()
	net := sim.NewNode()
	l := log.New(t.Name(), "", "")
	blockValidator := BlockEligibilityValidatorMock{}
	txpool := miner.NewTxMemPool()
	atxpool := miner.NewAtxMemPool()
	//ch := ts.Subscribe()
	msh := getMesh(memoryDB, Path+t.Name()+"_"+time.Now().String())

	msh.AddBlock(types.NewExistingBlock(1, []byte(rand.RandString(8))))
	msh.AddBlock(types.NewExistingBlock(2, []byte(rand.RandString(8))))
	msh.AddBlock(types.NewExistingBlock(3, []byte(rand.RandString(8))))
	msh.AddBlock(types.NewExistingBlock(4, []byte(rand.RandString(8))))
	msh.AddBlock(types.NewExistingBlock(5, []byte(rand.RandString(8))))
	msh.AddBlock(types.NewExistingBlock(6, []byte(rand.RandString(8))))
	msh.AddBlock(types.NewExistingBlock(7, []byte(rand.RandString(8))))

	sync := NewSync(net, msh, txpool, atxpool, blockValidator, newMockPoetDb(), conf, timer, l)
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.SyncLock = RUNNING
	sync.lValidator = lv
	sync.SetLatestLayer(5)

	sync.Start()
	time.Sleep(250 * time.Millisecond)
	current := sync.currentLayer

	// make sure not validated before the call
	_, ok := lv.validatedLayers[current]
	r.False(ok)
	timer.Tick()
	_, ok = lv.validatedLayers[current+1]
	r.False(ok)

	before := sync.currentLayer
	go func() {
		if err := sync.gossipSyncForOneFullLayer(current); err != nil {
			t.Error(err)
		}
	}()

	time.Sleep(1 * time.Second)

	timer.Layer = timer.Layer + 1
	log.Info("layer %v", timer.GetCurrentLayer())
	timer.Tick()
	timer.Layer = timer.Layer + 1
	log.Info("layer %v", timer.GetCurrentLayer())
	timer.Tick()

	time.Sleep(1 * time.Second)

	after := sync.currentLayer
	_, _ = before, after // TODO: commented out due to flakyness
	//r.Equal(before+2, after)
	r.Equal(2, lv.countValidate)

	// make sure the layers were validated after the call
	_, ok = lv.validatedLayers[current]
	r.True(ok)
	_, ok = lv.validatedLayers[current+1]
	r.True(ok)
}

type mockTimedValidator struct {
	delay time.Duration
	calls int
}

func (m *mockTimedValidator) ValidatedLayer() types.LayerID {
	return 1
}

func (m *mockTimedValidator) ValidateLayer(lyr *types.Layer) {
	m.calls++
	time.Sleep(m.delay)
}

func TestSyncer_ConcurrentSynchronise(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	sync.currentLayer = 3
	lv := &mockTimedValidator{1 * time.Second, 0}
	sync.lValidator = lv
	sync.AddBlock(types.NewExistingBlock(1, []byte(rand.RandString(8))))
	sync.AddBlock(types.NewExistingBlock(2, []byte(rand.RandString(8))))
	sync.AddBlock(types.NewExistingBlock(3, []byte(rand.RandString(8))))
	f := sync.getSyncRoutine()
	go f()
	time.Sleep(100 * time.Millisecond)
	f()
	time.Sleep(100 * time.Millisecond) // handle go routine race
	r.Equal(1, lv.calls)
}

func TestSyncProtocol_NilResponse(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMemPoetDb)
	defer syncs[0].Close()
	defer syncs[1].Close()

	var nonExistingLayerId = types.LayerID(0)
	var nonExistingBlockId = types.BlockID{}
	var nonExistingTxId types.TransactionId
	var nonExistingAtxId types.AtxId
	var nonExistingPoetRef []byte

	timeout := 1 * time.Second
	timeoutErrMsg := "no message received on channel"

	// Layer Hash

	wrk, output := NewPeersWorker(syncs[0], []p2p.Peer{nodes[1].PublicKey()}, &sync.Once{}, HashReqFactory(nonExistingLayerId))
	go wrk.Work()

	select {
	case out := <-output:
		assert.Nil(t, out)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Layer Block Ids

	wrk, output = NewPeersWorker(syncs[0], []p2p.Peer{nodes[1].PublicKey()}, &sync.Once{}, LayerIdsReqFactory(nonExistingLayerId))
	go wrk.Work()

	select {
	case out := <-output:
		assert.Nil(t, out)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Block
	bch := make(chan []types.Hash32, 1)
	bch <- []types.Hash32{nonExistingBlockId.AsHash32()}

	output = fetchWithFactory(NewFetchWorker(syncs[0], 1, newFetchReqFactory(BLOCK, blocksAsItems), bch, ""))

	select {
	case out := <-output:
		assert.True(t, out.(fetchJob).items == nil)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Tx

	ch := syncs[0].txQueue.addToPendingGetCh([]types.Hash32{nonExistingTxId.Hash32()})
	select {
	case out := <-ch:
		assert.False(t, out)
	case <-time.After(timeout):

	}

	// Atx
	ch = syncs[0].atxQueue.addToPendingGetCh([]types.Hash32{nonExistingAtxId.Hash32()})
	// PoET
	select {
	case out := <-ch:
		assert.False(t, out)
	case <-time.After(timeout):

	}

	output = fetchWithFactory(NewNeighborhoodWorker(syncs[0], 1, PoetReqFactory(nonExistingPoetRef)))

	select {
	case out := <-output:
		assert.Nil(t, out)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}
}

func TestSyncProtocol_BadResponse(t *testing.T) {
	syncs, _, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMemPoetDb)
	defer syncs[0].Close()
	defer syncs[1].Close()

	timeout := 1 * time.Second
	timeoutErrMsg := "no message received on channel"

	bl1 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	bl2 := types.NewExistingBlock(1, []byte(rand.RandString(8)))
	bl3 := types.NewExistingBlock(1, []byte(rand.RandString(8)))

	syncs[1].AddBlock(bl1)
	syncs[1].AddBlock(bl2)
	syncs[1].AddBlock(bl3)

	//setup mocks

	layerHashesMock := func([]byte) []byte {
		t.Log("return fake atx")
		return util.Uint32ToBytes(11)
	}

	blockHandlerMock := func([]byte) []byte {
		t.Log("return fake block")
		blk := types.NewExistingBlock(1, []byte(rand.RandString(8)))
		byts, _ := types.InterfaceToBytes([]types.Block{*blk})
		return byts
	}

	txHandlerMock := func([]byte) []byte {
		t.Log("return fake tx")
		byts, _ := types.InterfaceToBytes(tx())
		return byts
	}

	atxHandlerMock := func([]byte) []byte {
		t.Log("return fake atx")
		byts, _ := types.InterfaceToBytes([]types.ActivationTx{*atx("")})
		return byts
	}

	//register mocks
	syncs[1].RegisterBytesMsgHandler(LAYER_HASH, layerHashesMock)
	syncs[1].RegisterBytesMsgHandler(BLOCK, blockHandlerMock)
	syncs[1].RegisterBytesMsgHandler(TX, txHandlerMock)
	syncs[1].RegisterBytesMsgHandler(ATX, atxHandlerMock)

	// layer hash
	_, err1 := syncs[0].getLayerFromNeighbors(types.LayerID(1))

	assert.Error(t, err1)

	// Block
	ch := make(chan []types.Hash32, 1)
	ch <- []types.Hash32{bl1.Id().AsHash32()}
	output := fetchWithFactory(NewFetchWorker(syncs[0], 1, newFetchReqFactory(BLOCK, blocksAsItems), ch, ""))

	select {
	case out := <-output:
		assert.Nil(t, out.(fetchJob).items)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Tx
	ch = make(chan []types.Hash32, 1)
	ch <- []types.Hash32{[32]byte{1}}
	output = fetchWithFactory(NewFetchWorker(syncs[0], 1, newFetchReqFactory(TX, txsAsItems), ch, ""))

	select {
	case out := <-output:
		assert.Nil(t, out.(fetchJob).items)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Atx
	ch = make(chan []types.Hash32, 1)
	ch <- []types.Hash32{[32]byte{1}}
	output = fetchWithFactory(NewFetchWorker(syncs[0], 1, newFetchReqFactory(ATX, atxsAsItems), ch, ""))

	select {
	case out := <-output:
		assert.Nil(t, out.(fetchJob).items)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// PoET

	output = fetchWithFactory(NewNeighborhoodWorker(syncs[0], 1, PoetReqFactory([]byte{1})))

	select {
	case out := <-output:
		assert.Nil(t, out)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

}

func genByte32() [32]byte {
	var x [32]byte
	rand.Read(x[:])

	return x
}

var txid1 = types.TransactionId(genByte32())
var txid2 = types.TransactionId(genByte32())
var txid3 = types.TransactionId(genByte32())

var one = types.CalcHash32([]byte("1"))
var two = types.CalcHash32([]byte("2"))
var three = types.CalcHash32([]byte("3"))

var atx1 = types.AtxId(one)
var atx2 = types.AtxId(two)
var atx3 = types.AtxId(three)

func Test_validateUniqueTxAtx(t *testing.T) {
	r := require.New(t)
	b := &types.Block{}

	// unique
	b.TxIds = []types.TransactionId{txid1, txid2, txid3}
	b.AtxIds = []types.AtxId{atx1, atx2, atx3}
	r.Nil(validateUniqueTxAtx(b))

	// dup txs
	b.TxIds = []types.TransactionId{txid1, txid2, txid1}
	b.AtxIds = []types.AtxId{atx1, atx2, atx3}
	r.EqualError(validateUniqueTxAtx(b), errDupTx.Error())

	// dup atxs
	b.TxIds = []types.TransactionId{txid1, txid2, txid3}
	b.AtxIds = []types.AtxId{atx1, atx2, atx1}
	r.EqualError(validateUniqueTxAtx(b), errDupAtx.Error())
}

func TestSyncer_BlockSyntacticValidation(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(2, conf, "TestSyncProtocol_NilResponse", memoryDB, newMemPoetDb)
	s := syncs[0]
	b := &types.Block{}
	b.TxIds = []types.TransactionId{txid1, txid2, txid1}
	b.AtxIds = []types.AtxId{atx1, atx2, atx3}
	_, _, err := s.blockSyntacticValidation(b)
	r.EqualError(err, errDupTx.Error())

	for i := 0; i <= miner.AtxsPerBlockLimit; i++ {
		b.AtxIds = append(b.AtxIds, atx1)
	}
	_, _, err = s.blockSyntacticValidation(b)
	r.EqualError(err, errTooManyAtxs.Error())

	b.TxIds = []types.TransactionId{}
	b.AtxIds = []types.AtxId{}
	_, _, err = s.blockSyntacticValidation(b)
	r.Nil(err)
}

func TestSyncer_AtxSetID(t *testing.T) {
	a := atx("")
	bbytes, _ := types.InterfaceToBytes(*a)
	var b types.ActivationTx
	types.BytesToInterface(bbytes, &b)
	t.Log(fmt.Sprintf("%+v\n", *a))
	t.Log("---------------------")
	t.Log(fmt.Sprintf("%+v\n", b))
	t.Log("---------------------")
	assert.Equal(t, b.Nipst, a.Nipst)
	assert.Equal(t, b.View, a.View)
	assert.Equal(t, b.Commitment, a.Commitment)

	assert.Equal(t, b.ActivationTxHeader.NodeId, a.ActivationTxHeader.NodeId)
	assert.Equal(t, b.ActivationTxHeader.PrevATXId, a.ActivationTxHeader.PrevATXId)
	assert.Equal(t, b.ActivationTxHeader.ActiveSetSize, a.ActivationTxHeader.ActiveSetSize)
	assert.Equal(t, b.ActivationTxHeader.Coinbase, a.ActivationTxHeader.Coinbase)
	assert.Equal(t, b.ActivationTxHeader.CommitmentMerkleRoot, a.ActivationTxHeader.CommitmentMerkleRoot)
	assert.Equal(t, b.ActivationTxHeader.NIPSTChallenge, a.ActivationTxHeader.NIPSTChallenge)
	b.CalcAndSetId()
	assert.Equal(t, a.ShortString(), b.ShortString())
}
