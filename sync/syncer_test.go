package sync

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/sha256-simd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p"
	p2ppeers "github.com/spacemeshos/go-spacemesh/p2p/peers"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/timesync"
)

var conf = Configuration{1000, 1, 300, 500 * time.Millisecond, 200 * time.Millisecond, 10 * time.Hour, 100, 5, false}

func init() {
	rand.Seed(time.Now().UnixNano())
}

type alwaysOkAtxDb struct {
}

func (alwaysOkAtxDb) ProcessAtx(atx *types.ActivationTx) error {
	return nil
}

func (alwaysOkAtxDb) GetFullAtx(id types.ATXID) (*types.ActivationTx, error) {
	return &types.ActivationTx{}, nil
}

func (alwaysOkAtxDb) GetEpochAtxs(epochID types.EpochID) (atxs []types.ATXID) {
	return []types.ATXID{*types.EmptyATXID}
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

func newMemPoetDb() poetDb {
	return activation.NewPoetDb(database.NewMemDatabase(), log.NewDefault("poetDb"))
}

func newMockPoetDb() poetDb {
	return &poetDbMock{}
}

func SyncMockFactory(number int, conf Configuration, name string, dbType string, poetDb func() poetDb) (syncs []*Syncer, p2ps []*service.Node, ticker *timesync.TimeClock) {
	tick := 200 * time.Millisecond
	timer := timesync.RealClock{}
	ticker = timesync.NewClock(timer, tick, timer.Now().Add(tick*-4), log.NewDefault("clock"))
	syncs, p2ps = SyncMockFactoryManClock(number, conf, name, dbType, poetDb, ticker)
	ticker.StartNotifying()
	return syncs, p2ps, ticker
}

func SyncMockFactoryManClock(number int, conf Configuration, name string, dbType string, poetDb func() poetDb, ticker ticker) ([]*Syncer, []*service.Node) {
	nodes := make([]*Syncer, 0, number)
	p2ps := make([]*service.Node, 0, number)
	sim := service.NewSimulator()
	for i := 0; i < number; i++ {
		net := sim.NewNode()
		name := fmt.Sprintf(name+"_%d", i)
		l := log.New(name, "", "")
		blockValidator := blockEligibilityValidatorMock{}
		txpool := state.NewTxMemPool()
		atxpool := activation.NewAtxMemPool()
		sync := NewSync(net, getMesh(dbType, Path+name+"_"+time.Now().String()), txpool, atxpool, blockValidator, poetDb(), conf, ticker, l)
		nodes = append(nodes, sync)
		p2ps = append(p2ps, net)
	}
	return nodes, p2ps
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
	mshdb, _ := mesh.NewPersistentMeshDB(id, 5, lg)
	atxdbStore, _ := database.NewLDBDatabase(id+"atx", 0, 0, lg.WithOptions(log.Nop))
	atxdb := activation.NewDB(atxdbStore, &mockIStore{}, mshdb, 10, &validatorMock{}, lg.WithOptions(log.Nop))
	return mesh.NewMesh(mshdb, atxdb, rewardConf, &meshValidatorMock{}, &mockTxMemPool{}, &mockState{}, lg.WithOptions(log.Nop))
}

func persistenceTeardown() {
	os.RemoveAll(Path)
}

func getMeshWithMemoryDB(id string) *mesh.Mesh {
	lg := log.New(id, "", "")
	mshdb := mesh.NewMemMeshDB(lg)
	atxdb := activation.NewDB(database.NewMemDatabase(), &mockIStore{}, mshdb, 10, &validatorMock{}, lg.WithName("atxDB"))
	return mesh.NewMesh(mshdb, atxdb, rewardConf, &meshValidatorMock{}, &mockTxMemPool{}, &mockState{}, lg)
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
			if !syn.startLock.TryLock() {
				return
			}
		}
	}
}

func TestSyncer_Close(t *testing.T) {

	syncs, _, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)

	sync := syncs[0]
	sync1 := syncs[1]

	block := types.NewExistingBlock(1, []byte(rand.String(8)))
	block.TxIDs = append(block.TxIDs, txid1)
	//block.ATXIDs = append(block.ATXIDs, atx1)

	sync1.AddBlockWithTxs(block, nil, nil)
	sync1.Close()
	sync.Start()
	sync.Close()
	_, ok := <-sync.forceSync
	assert.True(t, !ok, "channel 'forceSync' still open")
	_, ok = <-sync.exit
	assert.True(t, !ok, "channel 'exit' still open")

}

func TestSyncProtocol_BlockRequest(t *testing.T) {
	signer := signing.NewEdSigner()
	atx1 := atx(signer.PublicKey().String())
	err := activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
	syncObj := syncs[0]
	syncObj2 := syncs[1]
	defer syncObj.Close()
	block := types.NewExistingBlock(1, []byte(rand.String(8)))

	syncObj.AddBlockWithTxs(block, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	syncObj2.peers = getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})

	ch := make(chan []types.Hash32, 1)
	ch <- []types.Hash32{block.Hash32()}

	output := fetchWithFactory(newFetchWorker(syncObj2, 1, newFetchReqFactory(blockMsg, blocksAsItems), ch, ""))

	timeout := time.NewTimer(2 * time.Second)
	emptyID := types.BlockID{}
	select {
	case a := <-output:
		assert.NotEqual(t, a.(fetchJob).items[0].(*types.Block).ID(), emptyID, "id not set")
		assert.Equal(t, a.(fetchJob).ids[0], block.Hash32(), "wrong block")
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}

}

func TestSyncProtocol_LayerHashRequest(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
	signer := signing.NewEdSigner()
	atx1 := atx(signer.PublicKey().String())
	err := activation.SignAtx(signer, atx1)
	assert.NoError(t, err)
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	lid := types.LayerID(1)
	block := types.NewExistingBlock(1, []byte(rand.String(8)))
	syncObj1.AddBlockWithTxs(block, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})
	timeout := time.NewTimer(2 * time.Second)

	wrk := newPeersWorker(syncObj2, []p2ppeers.Peer{nodes[0].PublicKey()}, &sync.Once{}, hashReqFactory(lid))
	go wrk.Work()

	select {
	case <-wrk.output:
		return
	case <-timeout.C:
		assert.Fail(t, "no message received on channel")
	}
}

func TestSyncer_FetchPoetProofAvailableAndValid(t *testing.T) {
	r := require.New(t)

	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMemPoetDb)
	s0 := syncs[0]
	s1 := syncs[1]
	s1.peers = getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})

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
	s1.peers = getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})

	// Store a poet proof and a respective atx in s0.

	proofMessage := makePoetProofMessage(t)

	err := s0.poetDb.ValidateAndStore(&proofMessage)
	r.NoError(err)

	poetProofBytes, err := types.InterfaceToBytes(&proofMessage.PoetProof)
	r.NoError(err)
	poetRef := sha256.Sum256(poetProofBytes)

	atx1 := atx(signer.PublicKey().String())
	atx1.Nipst.PostProof.Challenge = poetRef[:]
	err = activation.SignAtx(signer, atx1)
	r.NoError(err)
	err = s0.ProcessAtxs([]*types.ActivationTx{atx1})
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
	poetID := []byte("poet_id_12345")
	roundID := "1337"

	return types.PoetProofMessage{
		PoetProof:     poetProof,
		PoetServiceID: poetID,
		RoundID:       roundID,
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
	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	syncObj1.AddBlockWithTxs(block1, []*types.Transaction{tx1}, []*types.ActivationTx{atx1})

	block2 := types.NewExistingBlock(1, []byte(rand.String(8)))
	syncObj1.AddBlockWithTxs(block2, []*types.Transaction{tx2}, []*types.ActivationTx{atx2})

	block3 := types.NewExistingBlock(1, []byte(rand.String(8)))
	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx3}, []*types.ActivationTx{atx3})

	block4 := types.NewExistingBlock(1, []byte(rand.String(8)))

	syncObj1.AddBlockWithTxs(block4, []*types.Transaction{tx4}, []*types.ActivationTx{atx4})

	timeout := time.NewTimer(2 * time.Second)

	wrk := newPeersWorker(syncObj, []p2ppeers.Peer{nodes[1].PublicKey()}, &sync.Once{}, layerIdsReqFactory(lid))
	go wrk.Work()

	select {
	case intr := <-wrk.output:
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
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMockPoetDb)
	signer := signing.NewEdSigner()
	atx1 := atx(signer.PublicKey().String())

	atx2 := atx(signer.PublicKey().String())
	atx3 := atx(signer.PublicKey().String())
	syncObj1 := syncs[0]
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	defer syncObj2.Close()
	pm1 := getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})
	syncObj1.Log.Info("started fetch_blocks")
	syncObj2.peers = pm1 //override peers with

	err := syncObj1.ProcessAtxs([]*types.ActivationTx{atx3})
	assert.NoError(t, err)
	block1 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block1.ATXID = atx3.ID()
	block2 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block2.ATXID = atx3.ID()
	block3 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block3.ATXID = atx3.ID()
	block1.Initialize()
	block2.Initialize()
	block3.Initialize()

	syncObj1.AddBlockWithTxs(block1, []*types.Transaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx1})
	syncObj1.AddBlockWithTxs(block2, []*types.Transaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx2})
	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3, tx4, tx5, tx6, tx7, tx8}, []*types.ActivationTx{atx3})

	ch := make(chan []types.Hash32, 3)
	ch <- []types.Hash32{block1.Hash32()}
	ch <- []types.Hash32{block2.Hash32()}
	ch <- []types.Hash32{block3.Hash32()}
	close(ch)
	output := fetchWithFactory(newFetchWorker(syncObj2, 1, newFetchReqFactory(blockMsg, blocksAsItems), ch, ""))

	for out := range output {
		block := out.(fetchJob).items[0].(*types.Block)
		_, err := syncObj2.txQueue.HandleTxs(block.TxIDs)
		if err != nil {
			t.Error("could not fetch all txs", err)
		}
		syncObj2.Debug("add block to layer %v", block)

		//syncObj2.AddBlockWithTxs(block, txs, atxs)
	}
}

func TestSyncProtocol_SyncNodes(t *testing.T) {
	syncs, nodes, ts := SyncMockFactory(3, conf, t.Name(), memoryDB, newMockPoetDb)
	defer ts.Close()
	pm1 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm2 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm3 := getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey(), nodes[1].PublicKey()})
	syncObj1 := syncs[0]
	syncObj1.peers = pm1 //override peers with mock
	defer syncObj1.Close()

	syncObj2 := syncs[1]
	syncObj2.peers = pm2 //override peers with mock
	defer syncObj2.Close()

	syncObj3 := syncs[2]
	syncObj3.peers = pm3 //override peers with mock
	defer syncObj3.Close()
	defer ts.Close()

	signer := signing.NewEdSigner()

	block3 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block3.Signature = signer.Sign(block3.Bytes())
	block4 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block4.Signature = signer.Sign(block4.Bytes())
	block5 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block5.Signature = signer.Sign(block5.Bytes())
	block6 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block6.Signature = signer.Sign(block6.Bytes())
	block7 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block7.Signature = signer.Sign(block7.Bytes())
	block8 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block8.Signature = signer.Sign(block8.Bytes())
	block9 := types.NewExistingBlock(4, []byte(rand.String(8)))
	block9.Signature = signer.Sign(block9.Bytes())
	block10 := types.NewExistingBlock(5, []byte(rand.String(8)))
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

	syncObj1.getAndValidateLayer(1)
	syncObj1.getAndValidateLayer(2)
	syncObj1.getAndValidateLayer(3)
	syncObj1.getAndValidateLayer(4)
	syncObj1.getAndValidateLayer(5)

	syncObj2.getAndValidateLayer(1)
	syncObj2.getAndValidateLayer(2)
	syncObj2.getAndValidateLayer(3)
	syncObj2.getAndValidateLayer(4)
	syncObj2.getAndValidateLayer(5)

	syncObj3.SyncInterval = time.Millisecond * 20
	syncObj3.Start()

	timeout := time.After(5 * time.Second)

	//Keep trying until we're timed out or got a result or got an error
loop:
	for {
		select {
		// Got a timeout! fail with a timeout error
		case <-timeout:
			t.Error("timed out ")
			return
		default:
			log.Info("---------------- %d ---------------- %d", syncObj3.ProcessedLayer(), syncObj3.GetCurrentLayer())
			if syncObj3.ProcessedLayer() >= 5 {

				t.Log("done!")
				break loop
			}
			time.Sleep(1 * time.Second)
		}
	}

}

func getPeersMock(peers []p2ppeers.Peer) *p2ppeers.Peers {
	value := atomic.Value{}
	value.Store(peers)
	return p2ppeers.NewPeersImpl(&value, make(chan struct{}), log.NewDefault("peers"))
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

	syncObj1.peers = getPeersMock([]p2ppeers.Peer{n2.PublicKey()})
	syncObj2.peers = getPeersMock([]p2ppeers.Peer{n1.PublicKey()})
	syncObj3.peers = getPeersMock([]p2ppeers.Peer{n1.PublicKey()})
	syncObj4.peers = getPeersMock([]p2ppeers.Peer{n1.PublicKey()})

	signer := signing.NewEdSigner()

	block2 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block6.Signature = signer.Sign(block6.Bytes())

	block7 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block7.Signature = signer.Sign(block7.Bytes())

	block8 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block8.Signature = signer.Sign(block8.Bytes())

	block9 := types.NewExistingBlock(4, []byte(rand.String(8)))
	block9.Signature = signer.Sign(block9.Bytes())

	block10 := types.NewExistingBlock(4, []byte(rand.String(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	block11 := types.NewExistingBlock(5, []byte(rand.String(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	block12 := types.NewExistingBlock(6, []byte(rand.String(8)))
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
			if syncObj2.ProcessedLayer() >= 4 && syncObj3.ProcessedLayer() >= 4 {
				log.Info("done!", syncObj2.ProcessedLayer(), syncObj3.ProcessedLayer())
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
	ts := timesync.NewClock(timesync.RealClock{}, tick, start, log.NewDefault(t.Name()))
	sis.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		l := log.New(fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)), "", "")
		msh := getMesh(memoryDB, fmt.Sprintf("%s_%s", sis.name, time.Now()))
		blockValidator := blockEligibilityValidatorMock{}
		poetDb := activation.NewPoetDb(database.NewMemDatabase(), l.WithName("poetDb"))
		sync := NewSync(s, msh, state.NewTxMemPool(), activation.NewAtxMemPool(), blockValidator, poetDb, conf, ts, l)
		sis.syncers = append(sis.syncers, sync)
		atomic.AddUint32(&i, 1)
	}
	ts.StartNotifying()
	suite.Run(t, sis)
}

func (sis *syncIntegrationTwoNodes) TestSyncProtocol_TwoNodes() {
	t := sis.T()
	signer := signing.NewEdSigner()

	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block1.Signature = signer.Sign(block1.Bytes())
	block2 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block2.Signature = signer.Sign(block2.Bytes())
	block3 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block3.Signature = signer.Sign(block3.Bytes())
	block4 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block4.Signature = signer.Sign(block4.Bytes())
	block5 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block5.Signature = signer.Sign(block5.Bytes())
	block6 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block6.Signature = signer.Sign(block6.Bytes())
	block7 := types.NewExistingBlock(4, []byte(rand.String(8)))
	block7.Signature = signer.Sign(block7.Bytes())
	block8 := types.NewExistingBlock(4, []byte(rand.String(8)))
	block8.Signature = signer.Sign(block8.Bytes())
	block9 := types.NewExistingBlock(5, []byte(rand.String(8)))
	block9.Signature = signer.Sign(block9.Bytes())
	block10 := types.NewExistingBlock(5, []byte(rand.String(8)))
	block10.Signature = signer.Sign(block10.Bytes())

	syncObj0 := sis.syncers[0]
	defer syncObj0.Close()
	syncObj1 := sis.syncers[1]
	defer syncObj1.Close()
	syncObj2 := sis.syncers[2]
	defer syncObj2.Close()

	id1 := tx1.ID()
	id2 := tx2.ID()
	id3 := tx3.ID()
	id4 := tx4.ID()
	id5 := tx5.ID()
	id6 := tx6.ID()
	id7 := tx7.ID()
	id8 := tx8.ID()

	block3.TxIDs = []types.TransactionID{id1, id2, id3}
	block4.TxIDs = []types.TransactionID{id1, id2, id3}
	block5.TxIDs = []types.TransactionID{id4, id5, id6}
	block6.TxIDs = []types.TransactionID{id4, id5, id6}
	block7.TxIDs = []types.TransactionID{id7, id8}
	block8.TxIDs = []types.TransactionID{id7, id8}

	block1.Initialize()
	block2.Initialize()
	block3.Initialize()
	block4.Initialize()
	block5.Initialize()
	block6.Initialize()
	block7.Initialize()
	block8.Initialize()
	block9.Initialize()
	block10.Initialize()

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
			if syncObj1.ProcessedLayer() == 5 {
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
	ts := timesync.NewClock(timesync.RealClock{}, tick, start, log.NewDefault(t.Name()))
	sis.BeforeHook = func(idx int, s p2p.NodeTestInstance) {
		l := log.New(fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)), "", "")
		msh := getMesh(memoryDB, fmt.Sprintf("%s_%d", sis.name, atomic.LoadUint32(&i)))
		blockValidator := blockEligibilityValidatorMock{}
		poetDb := activation.NewPoetDb(database.NewMemDatabase(), l.WithName("poetDb"))
		sync := NewSync(s, msh, state.NewTxMemPool(), activation.NewAtxMemPool(), blockValidator, poetDb, conf, ts, l)
		ts.StartNotifying()
		sis.syncers = append(sis.syncers, sync)
		atomic.AddUint32(&i, 1)
	}
	suite.Run(t, sis)
}

func (sis *syncIntegrationMultipleNodes) TestSyncProtocol_MultipleNodes() {
	t := sis.T()
	signer := signing.NewEdSigner()

	block2 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block2.Signature = signer.Sign(block2.Bytes())

	block3 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block3.Signature = signer.Sign(block3.Bytes())

	block4 := types.NewExistingBlock(2, []byte(rand.String(8)))
	block4.Signature = signer.Sign(block4.Bytes())

	block5 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block5.Signature = signer.Sign(block5.Bytes())

	block6 := types.NewExistingBlock(3, []byte(rand.String(8)))
	block6.Signature = signer.Sign(block6.Bytes())

	block7 := types.NewExistingBlock(4, []byte(rand.String(8)))
	block7.Signature = signer.Sign(block7.Bytes())

	block8 := types.NewExistingBlock(4, []byte(rand.String(8)))
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

			if syncObj1.ProcessedLayer() >= 3 || syncObj2.ProcessedLayer() >= 3 || syncObj3.ProcessedLayer() >= 3 || syncObj5.ProcessedLayer() >= 3 {
				t.Log("done!")
				goto end
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
end:
	log.Debug("sync 1 ", syncObj1.ProcessedLayer())
	log.Debug("sync 2 ", syncObj2.ProcessedLayer())
	log.Debug("sync 3 ", syncObj3.ProcessedLayer())
	log.Debug("sync 4 ", syncObj4.ProcessedLayer())
	log.Debug("sync 5 ", syncObj5.ProcessedLayer())
	return
}

func tx() *types.Transaction {
	fee := rand.Uint64()
	addr := rand.Int63n(1000000)
	tx, err := types.NewSignedTx(1, types.HexToAddress(strconv.FormatUint(uint64(addr), 10)), 10, 100, fee, signing.NewEdSigner())
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

	atx := newActivationTx(types.NodeID{Key: pubkey, VRFPublicKey: []byte(rand.String(8))}, 0, *types.EmptyATXID, 5, 1, *types.EmptyATXID, coinbase, 0, nil, npst)
	atx.Commitment = commitment
	atx.CommitmentMerkleRoot = commitment.MerkleRoot
	atx.CalcAndSetID()
	return atx
}

func TestSyncer_Txs(t *testing.T) {
	// check tx validation
	syncs, nodes, _ := SyncMockFactory(3, conf, t.Name(), memoryDB, newMockPoetDb)
	pm1 := getPeersMock([]p2ppeers.Peer{nodes[1].PublicKey()})
	pm2 := getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})
	pm3 := getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey()})
	syncObj1 := syncs[0]
	syncObj1.peers = pm1 //override peers with mock
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.peers = pm2 //override peers with mock
	defer syncObj2.Close()
	syncObj3 := syncs[2]
	syncObj3.peers = pm3 //override peers with mock
	defer syncObj3.Close()

	block3 := types.NewExistingBlock(1, []byte(rand.String(8)))
	id1 := tx1.ID()
	id2 := tx2.ID()
	id3 := tx3.ID()
	block3.TxIDs = []types.TransactionID{id1, id2, id3}
	syncObj1.AddBlockWithTxs(block3, []*types.Transaction{tx1, tx2, tx3}, []*types.ActivationTx{})

	_, err := syncObj2.txQueue.handle([]types.Hash32{id1.Hash32(), id2.Hash32(), id3.Hash32()})
	assert.Nil(t, err)
}

func TestFetchLayerBlockIds(t *testing.T) {
	// check tx validation
	syncs, nodes, _ := SyncMockFactory(3, conf, t.Name(), memoryDB, newMockPoetDb)
	pm1 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm2 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm3 := getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey(), nodes[1].PublicKey()})

	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block2 := types.NewExistingBlock(1, []byte(rand.String(8)))

	syncObj1 := syncs[0]
	syncObj1.peers = pm1 //override peers with mock
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.peers = pm2 //override peers with mock
	defer syncObj2.Close()
	syncObj3 := syncs[2]
	syncObj3.peers = pm3 //override peers with mock
	defer syncObj3.Close()

	syncObj1.AddBlock(block1)
	syncObj2.AddBlock(block2)
	syncObj1.SetZeroBlockLayer(2)
	syncObj2.SetZeroBlockLayer(2)

	mp := map[types.Hash32][]p2ppeers.Peer{}
	hash1 := types.CalcBlocksHash32([]types.BlockID{block1.ID()}, nil)
	mp[hash1] = append(mp[hash1], nodes[0].PublicKey())
	hash2 := types.CalcBlocksHash32([]types.BlockID{block2.ID()}, nil)
	mp[hash2] = append(mp[hash2], nodes[1].PublicKey())
	ids, _ := syncObj3.fetchLayerBlockIds(mp, 1)

	assert.True(t, len(ids) == 2)

	if ids[0] != block1.ID() && ids[1] != block1.ID() {
		t.Error("did not get ids from all peers")
	}

	if ids[0] != block2.ID() && ids[1] != block2.ID() {
		panic("did not get ids from all peers")
	}

	syncObj3.handleNotSynced(2)
	assert.NoError(t, syncObj3.getAndValidateLayer(2))
	l, err := syncObj3.GetLayer(2)
	assert.NoError(t, err)
	assert.True(t, len(l.Blocks()) == 0)

}

func TestFetchLayerBlockIdsNoResponse(t *testing.T) {
	// check tx validation
	types.SetLayersPerEpoch(1)
	signer := signing.NewEdSigner()
	atx := atx(signer.PublicKey().String())
	atx.PubLayerID = 1
	atx.CalcAndSetID()
	err := activation.SignAtx(signer, atx)
	assert.NoError(t, err)
	atxDb := activation.NewAtxMemPool()
	atxDb.Put(atx)

	clk := &mockClock{Layer: 6}
	syncs, nodes := SyncMockFactoryManClock(5, conf, t.Name(), memoryDB, newMockPoetDb, clk)
	pm1 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm2 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm3 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm4 := getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey()})
	pm5 := getPeersMock([]p2ppeers.Peer{nodes[0].PublicKey(), nodes[1].PublicKey()})

	block1 := createBlock(*atx, signer)
	block2 := createBlock(*atx, signer)
	block2.LayerIndex = 2
	block2.Initialize()
	block31 := createBlock(*atx, signer)
	block31.LayerIndex = 3
	block31.Initialize()
	block32 := createBlock(*atx, signer)
	block32.LayerIndex = 3
	block32.Initialize()
	block4 := createBlock(*atx, signer)
	block4.LayerIndex = 4
	block4.Initialize()
	block5 := createBlock(*atx, signer)
	block5.LayerIndex = 5
	block5.Initialize()

	syncObj1 := syncs[0]
	syncObj1.peers = pm1 //override peers with mock
	syncObj1.atxDb = atxDb
	defer syncObj1.Close()
	syncObj2 := syncs[1]
	syncObj2.peers = pm2 //override peers with mock
	defer syncObj2.Close()
	syncObj3 := syncs[2]
	syncObj3.peers = pm3 //override peers with mock
	syncObj3.atxDb = atxDb
	defer syncObj3.Close()
	syncObj4 := syncs[3]
	syncObj4.peers = pm4 //override peers with mock
	syncObj4.atxDb = atxDb
	defer syncObj4.Close()

	syncObj5 := syncs[4]
	syncObj5.peers = pm5 //override peers with mock
	defer syncObj5.Close()

	syncObj1.AddBlock(block1)
	syncObj2.AddBlock(block1)
	syncObj3.AddBlock(block1)
	syncObj4.AddBlock(block1)
	syncObj5.AddBlock(block1)

	syncObj1.AddBlock(block2)
	syncObj2.AddBlock(block2)
	syncObj3.AddBlock(block2)
	syncObj4.AddBlock(block2)
	syncObj5.AddBlock(block2)

	syncObj1.SetZeroBlockLayer(3)
	syncObj2.SetZeroBlockLayer(3)

	syncObj3.AddBlock(block31)
	syncObj4.AddBlock(block31)
	syncObj5.AddBlock(block31)

	syncObj3.AddBlock(block32)
	syncObj4.AddBlock(block32)

	syncObj3.AddBlock(block4)
	syncObj4.AddBlock(block4)
	syncObj3.AddBlock(block5)
	syncObj4.AddBlock(block5)
	if err := syncObj5.getAndValidateLayer(2); err != nil {
		t.Fail()
	}

	//sync5 only knows sync1 and sync2 so the response for 3 should be an empty layer
	syncObj5.synchronise()
	lyr, err := syncObj5.GetLayer(3)
	assert.NoError(t, err)

	//check that we did not override layer with empty layer
	assert.True(t, len(lyr.Blocks()) == 1)
	//switch sync5 peers to sync3 and sync4 that know layer 3 and lets see it recovers
	syncObj5.peers = getPeersMock([]p2ppeers.Peer{nodes[2].PublicKey(), nodes[3].PublicKey()})
	go func() { time.Sleep(1 * time.Second); clk.Tick(); time.Sleep(1 * time.Second); clk.Tick() }()
	syncObj5.synchronise()

	//now we should have 2 blocks in layer 3
	lyr, err = syncObj5.GetLayer(3)
	assert.NoError(t, err)
	assert.True(t, len(lyr.Blocks()) == 2)

	err = syncObj5.getAndValidateLayer(3)
	assert.NoError(t, err)

	//check that we got layer 4
	err = syncObj5.getAndValidateLayer(4)
	assert.NoError(t, err)

	//check that we got layer 5
	err = syncObj5.getAndValidateLayer(5)
	assert.NoError(t, err)

}

type mockLayerValidator struct {
	processedLayer  types.LayerID // the validated layer
	countValidated  int
	countValidate   int
	validatedLayers map[types.LayerID]struct{}
}

func (m *mockLayerValidator) ProcessedLayer() types.LayerID {
	m.countValidated++
	return m.processedLayer
}

func (m *mockLayerValidator) SetProcessedLayer(lyr types.LayerID) {
	m.processedLayer = lyr
}

func (m *mockLayerValidator) HandleLateBlock(bl *types.Block) {
	panic("implement me")
}

func (m *mockLayerValidator) ValidateLayer(lyr *types.Layer) {
	log.Info("mock Validate layer %d", lyr.Index())
	m.countValidate++
	m.processedLayer = lyr.Index()
	if m.validatedLayers == nil {
		m.validatedLayers = make(map[types.LayerID]struct{})
	}

	m.validatedLayers[lyr.Index()] = struct{}{}
	log.Info("Validated count %d", m.countValidate)
}

func TestSyncer_Synchronise(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.Mesh.Validator = lv

	sr := sync.synchronise
	sr()
	time.Sleep(100 * time.Millisecond) // handle go routine race
	r.Equal(0, lv.countValidate)

	sync.AddBlock(types.NewExistingBlock(1, []byte(rand.String(8))))
	sync.AddBlock(types.NewExistingBlock(2, []byte(rand.String(8))))
	sync.AddBlock(types.NewExistingBlock(3, []byte(rand.String(8))))
	lv = &mockLayerValidator{1, 0, 0, nil}
	sync.Mesh.Validator = lv
	sync.ticker = &mockClock{Layer: 3}
	sr()
	time.Sleep(100 * time.Millisecond) // handle go routine race
	r.Equal(1, lv.countValidate)       // synced, expect only one call

	lv = &mockLayerValidator{1, 0, 0, nil}
	sync.Mesh.Validator = lv
	sync.ticker = &mockClock{Layer: 4} // simulate not synced
	sr()
	time.Sleep(100 * time.Millisecond) // handle go routine race
}

func TestSyncer_Synchronise2(t *testing.T) {
	r := require.New(t)
	types.SetLayersPerEpoch(1)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	gen := types.GetEffectiveGenesis()
	sync.AddBlockWithTxs(types.NewExistingBlock(1+gen, []byte(rand.String(8))), nil, nil)
	sync.AddBlockWithTxs(types.NewExistingBlock(2+gen, []byte(rand.String(8))), nil, nil)
	sync.AddBlockWithTxs(types.NewExistingBlock(3+gen, []byte(rand.String(8))), nil, nil)
	sync.AddBlockWithTxs(types.NewExistingBlock(4+gen, []byte(rand.String(8))), nil, nil)
	sync.AddBlockWithTxs(types.NewExistingBlock(5+gen, []byte(rand.String(8))), nil, nil)

	lv := &mockLayerValidator{types.GetEffectiveGenesis(), 0, 0, nil}
	sync.Mesh.Validator = lv
	sync.ticker = &mockClock{Layer: 1 + gen}
	r.False(sync.gossipSynced == done)

	// current layer = 0
	sync.ticker = &mockClock{Layer: 0 + gen}
	sync.synchronise()
	r.Equal(0, lv.countValidate)
	r.True(sync.gossipSynced == done)

	// current layer = 1
	sync.ticker = &mockClock{Layer: 1 + gen}
	sync.synchronise()
	r.Equal(0, lv.countValidate)
	r.True(sync.gossipSynced == done)

	// current layer != 1 && weakly-synced
	lv = &mockLayerValidator{types.GetEffectiveGenesis(), 0, 0, nil}
	sync.Mesh.Validator = lv
	sync.ticker = &mockClock{Layer: 2 + gen}
	sync.SetLatestLayer(2)
	sync.synchronise()
	r.Equal(1, lv.countValidate)
	r.True(sync.gossipSynced == done)

	// validated layer = 5 && current layer = 6 -> don't call validate
	lv = &mockLayerValidator{5, 0, 0, nil}
	sync.Mesh.Validator = lv
	sync.ticker = &mockClock{Layer: 6}
	sync.SetLatestLayer(5)
	log.Info("damn ", sync.ProcessedLayer())
	sync.synchronise()
	log.Info("damn ", sync.ProcessedLayer())
	r.Equal(0, lv.countValidate)
	r.True(sync.gossipSynced == done)
}

func TestSyncer_ListenToGossip(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	sync.AddBlockWithTxs(types.NewExistingBlock(1, []byte(rand.String(8))), nil, nil)
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.Mesh.Validator = lv
	sync.ticker = &mockClock{Layer: 1}
	sync.SetLatestLayer(1)
	r.False(sync.gossipSynced == done)
	assert.False(t, sync.ListenToGossip())

	//run sync
	sync.synchronise()

	//check gossip open
	assert.True(t, sync.ListenToGossip())
}

func TestSyncer_handleNotSyncedFlow(t *testing.T) {
	r := require.New(t)
	txpool := state.NewTxMemPool()
	atxpool := activation.NewAtxMemPool()
	ts := &mockClock{Layer: 10}
	sync := NewSync(service.NewSimulator().NewNode(), getMesh(memoryDB, Path+t.Name()+"_"+time.Now().String()), txpool, atxpool, blockEligibilityValidatorMock{}, newMockPoetDb(), conf, ts, log.NewDefault(t.Name()))
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.Mesh.Validator = lv
	sync.SetLatestLayer(20)
	go sync.handleNotSynced(10)
	time.Sleep(100 * time.Millisecond)
	r.Equal(1, ts.countSub)
}

func TestSyncer_p2pSyncForTwoLayers(t *testing.T) {
	r := require.New(t)
	timer := &mockClock{Layer: 5}
	sim := service.NewSimulator()
	net := sim.NewNode()
	l := log.New(t.Name(), "", "")
	blockValidator := blockEligibilityValidatorMock{}
	txpool := state.NewTxMemPool()
	atxpool := activation.NewAtxMemPool()
	//ch := ts.Subscribe()
	msh := getMesh(memoryDB, Path+t.Name()+"_"+time.Now().String())

	msh.AddBlock(types.NewExistingBlock(1, []byte(rand.String(8))))
	msh.AddBlock(types.NewExistingBlock(2, []byte(rand.String(8))))
	msh.AddBlock(types.NewExistingBlock(3, []byte(rand.String(8))))
	msh.AddBlock(types.NewExistingBlock(4, []byte(rand.String(8))))
	msh.AddBlock(types.NewExistingBlock(5, []byte(rand.String(8))))
	msh.AddBlock(types.NewExistingBlock(6, []byte(rand.String(8))))
	msh.AddBlock(types.NewExistingBlock(7, []byte(rand.String(8))))

	sync := NewSync(net, msh, txpool, atxpool, blockValidator, newMockPoetDb(), conf, timer, l)
	lv := &mockLayerValidator{0, 0, 0, nil}
	sync.syncLock.Lock()
	sync.Mesh.Validator = lv
	sync.SetLatestLayer(5)

	sync.Start()
	time.Sleep(250 * time.Millisecond)
	current := sync.GetCurrentLayer()

	// make sure not validated before the call
	_, ok := lv.validatedLayers[current]
	r.False(ok)
	timer.Tick()
	_, ok = lv.validatedLayers[current+1]
	r.False(ok)

	before := sync.GetCurrentLayer()
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

	after := sync.GetCurrentLayer()
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

func (m *mockTimedValidator) HandleLateBlock(bl *types.Block) {
	return
}

func (m *mockTimedValidator) ProcessedLayer() types.LayerID {
	return 1
}

func (m *mockTimedValidator) SetProcessedLayer(lyr types.LayerID) {
	panic("implement me")
}

func (m *mockTimedValidator) ValidateLayer(lyr *types.Layer) {
	log.Info("Validate layer %d", lyr.Index())
	m.calls++
	time.Sleep(m.delay)
}

func TestSyncer_ConcurrentSynchronise(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	sync := syncs[0]
	sync.ticker = &mockClock{Layer: 3}
	lv := &mockTimedValidator{1 * time.Second, 0}
	sync.Validator = lv
	sync.AddBlock(types.NewExistingBlock(1, []byte(rand.String(8))))
	sync.AddBlock(types.NewExistingBlock(2, []byte(rand.String(8))))
	sync.AddBlock(types.NewExistingBlock(3, []byte(rand.String(8))))
	go sync.synchronise()
	time.Sleep(100 * time.Millisecond)
	sync.synchronise()
	time.Sleep(100 * time.Millisecond) // handle go routine race
	r.Equal(1, lv.calls)
}

func TestSyncProtocol_NilResponse(t *testing.T) {
	syncs, nodes, _ := SyncMockFactory(2, conf, t.Name(), memoryDB, newMemPoetDb)
	defer syncs[0].Close()
	defer syncs[1].Close()

	var nonExistingLayerID = types.LayerID(0)
	var nonExistingBlockID = types.BlockID{}
	var nonExistingTxID types.TransactionID
	var nonExistingAtxID types.ATXID
	var nonExistingPoetRef []byte

	timeout := 1 * time.Second
	timeoutErrMsg := "no message received on channel"

	for i := 0; i < 10; i++ {
		if len(syncs[0].GetPeers()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	if len(syncs[0].GetPeers()) == 0 {
		t.Error("syncer has no peers ")
		t.Fail()
	}

	// Layer Hash

	wrk := newPeersWorker(syncs[0], []p2ppeers.Peer{nodes[1].PublicKey()}, &sync.Once{}, hashReqFactory(nonExistingLayerID))
	go wrk.Work()

	select {
	case out := <-wrk.output:
		assert.Nil(t, out)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Layer Block Ids

	wrk = newPeersWorker(syncs[0], []p2ppeers.Peer{nodes[1].PublicKey()}, &sync.Once{}, layerIdsReqFactory(nonExistingLayerID))
	go wrk.Work()

	select {
	case out := <-wrk.output:
		assert.Nil(t, out)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Block
	bch := make(chan []types.Hash32, 1)
	bch <- []types.Hash32{nonExistingBlockID.AsHash32()}

	output := fetchWithFactory(newFetchWorker(syncs[0], 1, newFetchReqFactory(blockMsg, blocksAsItems), bch, ""))

	select {
	case out := <-output:
		assert.True(t, out.(fetchJob).items == nil)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Tx

	ch := syncs[0].txQueue.addToPendingGetCh([]types.Hash32{nonExistingTxID.Hash32()})
	select {
	case out := <-ch:
		assert.False(t, out)
	case <-time.After(timeout):

	}

	// Atx
	ch = syncs[0].atxQueue.addToPendingGetCh([]types.Hash32{nonExistingAtxID.Hash32()})
	// PoET
	select {
	case out := <-ch:
		assert.False(t, out)
	case <-time.After(timeout):

	}

	output = fetchWithFactory(newNeighborhoodWorker(syncs[0], 1, poetReqFactory(nonExistingPoetRef)))

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

	bl1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	bl2 := types.NewExistingBlock(1, []byte(rand.String(8)))
	bl3 := types.NewExistingBlock(1, []byte(rand.String(8)))

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
		blk := types.NewExistingBlock(1, []byte(rand.String(8)))
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
	syncs[1].RegisterBytesMsgHandler(layerHashMsg, layerHashesMock)
	syncs[1].RegisterBytesMsgHandler(blockMsg, blockHandlerMock)
	syncs[1].RegisterBytesMsgHandler(txMsg, txHandlerMock)
	syncs[1].RegisterBytesMsgHandler(atxMsg, atxHandlerMock)

	for i := 0; i < 10; i++ {
		if len(syncs[0].GetPeers()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if len(syncs[0].GetPeers()) == 0 {
		t.Error("no peers for syncer")
		t.Fail()
		return
	}

	// ugly hack, just to see if this fixed travis fail
	time.Sleep(3 * time.Second)
	// layer hash
	_, err1 := syncs[0].getLayerFromNeighbors(types.LayerID(1))

	assert.Nil(t, err1)

	// Block
	ch := make(chan []types.Hash32, 1)
	ch <- []types.Hash32{bl1.ID().AsHash32()}
	output := fetchWithFactory(newFetchWorker(syncs[0], 1, newFetchReqFactory(blockMsg, blocksAsItems), ch, ""))

	select {
	case out := <-output:
		assert.Nil(t, out.(fetchJob).items)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Tx
	ch = make(chan []types.Hash32, 1)
	ch <- []types.Hash32{[32]byte{1}}
	output = fetchWithFactory(newFetchWorker(syncs[0], 1, newFetchReqFactory(txMsg, txsAsItems), ch, ""))

	select {
	case out := <-output:
		assert.Nil(t, out.(fetchJob).items)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// Atx
	ch = make(chan []types.Hash32, 1)
	ch <- []types.Hash32{[32]byte{1}}
	output = fetchWithFactory(newFetchWorker(syncs[0], 1, newFetchReqFactory(atxMsg, atxsAsItems), ch, ""))

	select {
	case out := <-output:
		assert.Nil(t, out.(fetchJob).items)
	case <-time.After(timeout):
		assert.Fail(t, timeoutErrMsg)
	}

	// PoET

	output = fetchWithFactory(newNeighborhoodWorker(syncs[0], 1, poetReqFactory([]byte{1})))

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

var txid1 = types.TransactionID(genByte32())
var txid2 = types.TransactionID(genByte32())
var txid3 = types.TransactionID(genByte32())

var one = types.CalcHash32([]byte("1"))
var two = types.CalcHash32([]byte("2"))
var three = types.CalcHash32([]byte("3"))

var atx1 = types.ATXID(one)
var atx2 = types.ATXID(two)
var atx3 = types.ATXID(three)

func Test_validateUniqueTxAtx(t *testing.T) {
	r := require.New(t)
	b := &types.Block{}

	// unique
	b.TxIDs = []types.TransactionID{txid1, txid2, txid3}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	r.Nil(validateUniqueTxAtx(b))

	// dup txs
	b.TxIDs = []types.TransactionID{txid1, txid2, txid1}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	r.EqualError(validateUniqueTxAtx(b), errDupTx.Error())

	// dup atxs
	b.TxIDs = []types.TransactionID{txid1, txid2, txid3}
	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx1}
	r.EqualError(validateUniqueTxAtx(b), errDupAtx.Error())
}

func TestSyncer_BlockSyntacticValidation(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(2, conf, "TestSyncProtocol_NilResponse", memoryDB, newMemPoetDb)
	s := syncs[0]
	s.atxDb = alwaysOkAtxDb{}
	b := &types.Block{}
	b.TxIDs = []types.TransactionID{txid1, txid2, txid1}
	_, _, err := s.blockSyntacticValidation(b)
	r.EqualError(err, errNoActiveSet.Error())

	b.ActiveSet = &[]types.ATXID{}
	_, _, err = s.blockSyntacticValidation(b)
	r.EqualError(err, errZeroActiveSet.Error())

	b.ActiveSet = &[]types.ATXID{atx1, atx2, atx3}
	_, _, err = s.blockSyntacticValidation(b)
	r.EqualError(err, errDupTx.Error())
}

func TestSyncer_BlockSyntacticValidation_syncRefBlock(t *testing.T) {
	r := require.New(t)
	syncs, _, _ := SyncMockFactory(2, conf, "BlockSyntacticValidation_syncRefBlock", memoryDB, newMemPoetDb)
	atxpool := activation.NewAtxMemPool()
	s := syncs[0]
	s.atxDb = atxpool
	a := atx("")
	atxpool.Put(a)
	b := &types.Block{}
	b.TxIDs = []types.TransactionID{}
	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block1.ActiveSet = &[]types.ATXID{a.ID()}
	block1.ATXID = a.ID()
	block1.Initialize()
	block1ID := block1.ID()
	b.RefBlock = &block1ID
	b.ATXID = a.ID()
	_, _, err := s.blockSyntacticValidation(b)
	r.Equal(err, fmt.Errorf("failed to fetch ref block %v", *b.RefBlock))

	err = syncs[1].AddBlock(block1)
	r.NoError(err)
	_, _, err = s.blockSyntacticValidation(b)
	r.NoError(err)
}

func TestSyncer_fetchBlock(t *testing.T) {
	r := require.New(t)
	atxPool := activation.NewAtxMemPool()
	syncs, _, _ := SyncMockFactory(2, conf, "fetchBlock", memoryDB, newMemPoetDb)
	s := syncs[0]
	s.atxDb = atxPool
	atx := atx("")
	atxPool.Put(atx)
	block1 := types.NewExistingBlock(1, []byte(rand.String(8)))
	block1.ActiveSet = &[]types.ATXID{atx.ID()}
	block1.ATXID = atx.ID()
	block1.Initialize()
	block1ID := block1.ID()
	res := s.fetchBlock(block1ID)
	r.False(res)
	err := syncs[1].AddBlock(block1)
	r.NoError(err)
	res = s.fetchBlock(block1ID)
	r.True(res)

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
	assert.Equal(t, b.Commitment, a.Commitment)

	assert.Equal(t, b.ActivationTxHeader.NodeID, a.ActivationTxHeader.NodeID)
	assert.Equal(t, b.ActivationTxHeader.PrevATXID, a.ActivationTxHeader.PrevATXID)
	assert.Equal(t, b.ActivationTxHeader.Coinbase, a.ActivationTxHeader.Coinbase)
	assert.Equal(t, b.ActivationTxHeader.CommitmentMerkleRoot, a.ActivationTxHeader.CommitmentMerkleRoot)
	assert.Equal(t, b.ActivationTxHeader.NIPSTChallenge, a.ActivationTxHeader.NIPSTChallenge)
	b.CalcAndSetID()
	assert.Equal(t, a.ShortString(), b.ShortString())
}

func TestSyncer_Await(t *testing.T) {
	r := require.New(t)

	syncs, _, _ := SyncMockFactory(1, conf, t.Name(), memoryDB, newMockPoetDb)
	syncer := syncs[0]
	err := syncer.AddBlockWithTxs(types.NewExistingBlock(1, []byte(rand.String(8))), nil, nil)
	r.NoError(err)
	lv := &mockLayerValidator{0, 0, 0, nil}
	syncer.Mesh.Validator = lv
	syncer.ticker = &mockClock{Layer: 1}
	syncer.SetLatestLayer(1)

	ch := syncer.Await()
	r.False(closed(ch))

	//run sync
	syncer.synchronise()

	r.True(closed(ch))
}

func TestSyncer_Await_LowLevel(t *testing.T) {
	r := require.New(t)

	syncer := &Syncer{
		gossipSynced: pending,
		awaitCh:      make(chan struct{}),
		Log:          log.New("", "", ""),
	}

	ch := syncer.Await()
	r.False(closed(ch))

	// pending -> inProgress keeps the channel open
	syncer.setGossipBufferingStatus(inProgress)
	r.False(closed(ch))

	// inProgress -> inProgress has no effect
	syncer.setGossipBufferingStatus(inProgress)
	r.False(closed(ch))

	// inProgress -> done closes the channel
	syncer.setGossipBufferingStatus(done)
	r.True(closed(ch))

	// done -> done has no effect
	syncer.setGossipBufferingStatus(done)
	r.True(closed(ch))

	// done -> pending...
	syncer.setGossipBufferingStatus(pending)
	// ...the channel from the previous call to `Await()` should still be closed...
	r.True(closed(ch))
	// ...but a new call to `Await()` should provide a new, open channel
	newCh := syncer.Await()
	r.False(closed(newCh))

	// pending -> done should close the new channel
	syncer.setGossipBufferingStatus(done)
	r.True(closed(newCh))
}

func closed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
