package miner

import (
	"bytes"
	"errors"
	"fmt"
	"testing"
	"time"

	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const selectCount = 100
const layersPerEpoch = 10

type MockCoin struct{}

func (m MockCoin) GetResult() bool {
	return rand.Int()%2 == 0
}

type MockHare struct {
	res map[types.LayerID][]types.BlockID
}

func (m MockHare) GetResult(id types.LayerID) ([]types.BlockID, error) {
	blks, ok := m.res[id]
	if !ok {
		return nil, fmt.Errorf("hare result for layer %v was not in map", id)
	}
	return blks, nil
}

type mockBlockOracle struct {
	calls int
	err   error
	J     uint32
}

func (mbo *mockBlockOracle) BlockEligible(types.LayerID) (types.ATXID, []types.BlockEligibilityProof, error) {
	mbo.calls++
	return types.ATXID(types.Hash32{1, 2, 3}), []types.BlockEligibilityProof{{J: mbo.J, Sig: []byte{1}}}, mbo.err
}

type mockAtxValidator struct{}

func (mockAtxValidator) GetIdentity(string) (types.NodeID, error) {
	return types.NodeID{}, nil
}

func (mockAtxValidator) SyntacticallyValidateAtx(*types.ActivationTx) error { return nil }

type mockTxProcessor struct {
	notValid bool
}

func (m mockTxProcessor) ValidateNonceAndBalance(*types.Transaction) error {
	return nil
}

func (m mockTxProcessor) AddressExists(types.Address) bool {
	return !m.notValid
}

type mockSyncer struct {
	notSynced bool
}

func (mockSyncer) ListenToGossip() bool {
	return true
}

func (mockSyncer) FetchPoetProof([]byte) error { return nil }

func (m mockSyncer) IsSynced() bool { return !m.notSynced }

type mockSyncerP struct {
	synced bool
}

func (m mockSyncerP) ListenToGossip() bool {
	return m.synced
}

func (mockSyncerP) FetchPoetProof([]byte) error { return nil }

func (m mockSyncerP) IsSynced() bool { return m.synced }

type atxDbMock struct {
}

func (atxDbMock) GetEpochAtxs(epochID types.EpochID) []types.ATXID {
	return []types.ATXID{atx1, atx2, atx3, atx4, atx5}
}

func (atxDbMock) GetAtxs(epochID types.EpochID) []types.ATXID {
	return []types.ATXID{atx1, atx2, atx3, atx4, atx5}
}

type MockProjector struct {
}

func (p *MockProjector) GetProjection(types.Address) (nonce uint64, balance uint64, err error) {
	return 1, 1000, nil
}

func init() {
	database.SwitchToMemCreationContext()
	types.SetLayersPerEpoch(3)
}

var mockProjector = &MockProjector{}

func TestBlockBuilder_StartStop(t *testing.T) {
	rand.Seed(0)
	net := service.NewSimulator()
	n := net.NewNode()

	block1 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.String(8)))
	hareRes := []types.BlockID{block1.ID(), block2.ID(), block3.ID(), block4.ID()}

	txMempool := state.NewTxMemPool()

	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	bs := []*types.Block{block1, block2, block3, block4}
	builder := createBlockBuilder("a", n, bs)
	builder.hareResult = hare
	builder.TransactionPool = txMempool
	//builder := NewBlockBuilder(types.NodeID{}, signing.NewEdSigner(), n, beginRound, 5, MockCoin{}, orphans, hare, &mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, selectCount, layersPerEpoch, mockProjector, log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	err = builder.Start()
	assert.Error(t, err)

	err = builder.Close()
	assert.NoError(t, err)

	err = builder.Close()
	assert.Error(t, err)

	tx1 := NewTx(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner())
	txMempool.Put(tx1.ID(), tx1)
}

func TestBlockBuilder_BlockIdGeneration(t *testing.T) {

	net := service.NewSimulator()
	n1 := net.NewNode()
	n2 := net.NewNode()

	block1 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.String(8)))
	hareRes := []types.BlockID{block1.ID(), block2.ID(), block3.ID(), block4.ID()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	st := []*types.Block{block2, block3, block4}
	builder1 := createBlockBuilder("a", n1, st)
	builder2 := createBlockBuilder("b", n2, st)
	builder1.hareResult = hare
	builder2.hareResult = hare
	builder2.AtxDb = atxDbMock{}
	builder1.AtxDb = atxDbMock{}

	b1, err := builder1.createBlock(types.GetEffectiveGenesis()+2, types.ATXID{}, types.BlockEligibilityProof{}, nil)
	assert.NoError(t, err)
	b2, err := builder2.createBlock(types.GetEffectiveGenesis()+2, types.ATXID{}, types.BlockEligibilityProof{}, nil)
	assert.NoError(t, err)

	assert.True(t, b1.ID() != b2.ID(), "ids are identical")
}

var (
	block1  = types.NewExistingBlock(0, []byte(rand.String(8)))
	block2  = types.NewExistingBlock(0, []byte(rand.String(8)))
	block3  = types.NewExistingBlock(0, []byte(rand.String(8)))
	block4  = types.NewExistingBlock(0, []byte(rand.String(8)))
	hareRes = []types.BlockID{block1.ID(), block2.ID(), block3.ID(), block4.ID()}

	coinbase = types.HexToAddress("aaaa")

	poetRef = []byte{0xba, 0x38}
	atxs    = []*types.ActivationTx{
		newActivationTx(types.NodeID{Key: "aaaa", VRFPublicKey: []byte("bbb")}, 1, types.ATXID(types.Hash32{1}), 5, 1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
		newActivationTx(types.NodeID{Key: "bbbb", VRFPublicKey: []byte("bbb")}, 1, types.ATXID(types.Hash32{2}), 5, 1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
		newActivationTx(types.NodeID{Key: "cccc", VRFPublicKey: []byte("bbb")}, 1, types.ATXID(types.Hash32{3}), 5, 1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
	}

	atxIDs = []types.ATXID{atxs[0].ID(), atxs[1].ID(), atxs[2].ID()}
)

func TestBlockBuilder_CreateBlockFlow(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	receiver := net.NewNode()

	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[1] = hareRes

	txPool := state.NewTxMemPool()

	atxPool := activation.NewAtxMemPool()

	st := []*types.Block{block1, block2, block3}
	builder := createBlockBuilder("a", n, st)
	builder.TransactionPool = txPool
	builder.beginRoundEvent = beginRound
	//builder := NewBlockBuilder(types.NodeID{Key: "anton", VRFPublicKey: []byte("anton")}, signing.NewEdSigner(), n, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, &mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, selectCount, layersPerEpoch, mockProjector, log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	recipient := types.BytesToAddress([]byte{0x01})
	signer := signing.NewEdSigner()

	trans := []*types.Transaction{
		NewTx(t, 1, recipient, signer),
		NewTx(t, 2, recipient, signer),
		NewTx(t, 3, recipient, signer),
	}

	transids := []types.TransactionID{trans[0].ID(), trans[1].ID(), trans[2].ID()}

	txPool.Put(trans[0].ID(), trans[0])
	txPool.Put(trans[1].ID(), trans[1])
	txPool.Put(trans[2].ID(), trans[2])

	atxPool.Put(atxs[0])
	atxPool.Put(atxs[1])
	atxPool.Put(atxs[2])

	go func() { beginRound <- types.GetEffectiveGenesis() + 1 }()
	select {
	case output := <-receiver.RegisterGossipProtocol(config.NewBlockProtocol, priorityq.High):
		b := types.MiniBlock{}
		_, _ = xdr.Unmarshal(bytes.NewBuffer(output.Bytes()), &b)

		assert.NotEqual(t, hareRes, b.BlockVotes)
		assert.Equal(t, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, b.ViewEdges)

		assert.True(t, ContainsTx(b.TxIDs, transids[0]))
		assert.True(t, ContainsTx(b.TxIDs, transids[1]))
		assert.True(t, ContainsTx(b.TxIDs, transids[2]))

		/*assert.True(t, ContainsAtx(b.ATXIDs, atxs[0].ID()))
		assert.True(t, ContainsAtx(b.ATXIDs, atxs[1].ID()))
		assert.True(t, ContainsAtx(b.ATXIDs, atxs[2].ID()))*/

		assert.Equal(t, []types.ATXID{atx1, atx2, atx3, atx4, atx5}, *b.ActiveSet)
	case <-time.After(1 * time.Second):
		assert.Fail(t, "timeout on receiving block")
	}

}

func TestBlockBuilder_CreateBlockWithRef(t *testing.T) {
	net := service.NewSimulator()
	n := net.NewNode()

	hareRes := []types.BlockID{block1.ID(), block2.ID(), block3.ID(), block4.ID()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[1] = hareRes

	st := []*types.Block{block1, block2, block3}
	builder := createBlockBuilder("a", n, st)

	recipient := types.BytesToAddress([]byte{0x01})
	signer := signing.NewEdSigner()

	trans := []*types.Transaction{
		NewTx(t, 1, recipient, signer),
		NewTx(t, 2, recipient, signer),
		NewTx(t, 3, recipient, signer),
	}

	transids := []types.TransactionID{trans[0].ID(), trans[1].ID(), trans[2].ID()}

	b, err := builder.createBlock(types.GetEffectiveGenesis()+1, types.ATXID(types.Hash32{1, 2, 3}), types.BlockEligibilityProof{J: 0, Sig: []byte{1}}, transids)
	assert.NoError(t, err)

	assert.NotEqual(t, hareRes, b.BlockVotes)
	assert.Equal(t, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, b.ViewEdges)

	assert.True(t, ContainsTx(b.TxIDs, transids[0]))
	assert.True(t, ContainsTx(b.TxIDs, transids[1]))
	assert.True(t, ContainsTx(b.TxIDs, transids[2]))

	/*assert.True(t, ContainsAtx(b.ATXIDs, atxs[0].ID()))
	assert.True(t, ContainsAtx(b.ATXIDs, atxs[1].ID()))
	assert.True(t, ContainsAtx(b.ATXIDs, atxs[2].ID()))*/

	assert.Equal(t, []types.ATXID{atx1, atx2, atx3, atx4, atx5}, *b.ActiveSet)

	//test create second block
	bl, err := builder.createBlock(types.GetEffectiveGenesis()+2, types.ATXID(types.Hash32{1, 2, 3}), types.BlockEligibilityProof{J: 1, Sig: []byte{1}}, transids)
	assert.NoError(t, err)

	assert.NotEqual(t, hareRes, bl.BlockVotes)
	assert.Equal(t, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, bl.ViewEdges)

	assert.True(t, ContainsTx(bl.TxIDs, transids[0]))
	assert.True(t, ContainsTx(bl.TxIDs, transids[1]))
	assert.True(t, ContainsTx(bl.TxIDs, transids[2]))

	/*assert.True(t, ContainsAtx(bl.ATXIDs, atxs[0].ID()))
	assert.True(t, ContainsAtx(bl.ATXIDs, atxs[1].ID()))
	assert.True(t, ContainsAtx(bl.ATXIDs, atxs[2].ID()))*/

	assert.Equal(t, *bl.RefBlock, b.ID())
}

func NewTx(t *testing.T, nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tx, err := types.NewSignedTx(nonce, recipient, 1, defaultGasLimit, defaultFee, signer)
	assert.NoError(t, err)
	return tx
}

func TestBlockBuilder_SerializeTrans(t *testing.T) {
	tx := NewTx(t, 1, types.BytesToAddress([]byte{0x02}), signing.NewEdSigner())
	buf, err := types.InterfaceToBytes(tx)
	assert.NoError(t, err)

	ntx, err := types.BytesToTransaction(buf)
	assert.NoError(t, err)
	err = ntx.CalcAndSetOrigin()
	assert.NoError(t, err)

	assert.Equal(t, *tx, *ntx)
}

func ContainsTx(a []types.TransactionID, x types.TransactionID) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

func ContainsAtx(a []types.ATXID, x types.ATXID) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

/*
func TestBlockBuilder_Validation(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()

	block1 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.String(8)))
	hareRes := []types.BlockID{block1.ID(), block2.ID(), block3.ID(), block4.ID()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	st := []*types.Block{block1, block2, block3}
	builder1 := NewBlockBuilder(types.NodeID{Key: "a"}, signing.NewEdSigner(), n1, beginRound, 5, state.NewTxMemPool(), activation.NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, &mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, selectCount, selectCount, layersPerEpoch, mockProjector, log.New(n1.Info.ID.String(), "", ""))
	assert.NoError(t, builder1.Start())
	tx := NewTx(t, 5, types.HexToAddress("0xFF"), signing.NewEdSigner())
	b, e := types.InterfaceToBytes(tx)
	assert.Nil(t, e)
	assert.NoError(t, n1.Broadcast(IncomingTxProtocol, b))
	time.Sleep(300 * time.Millisecond)
	ids, err := builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Empty(t, ids)
	builder1.txValidator = mockTxProcessor{false}
	assert.NoError(t, n1.Broadcast(IncomingTxProtocol, b))
	time.Sleep(300 * time.Millisecond)
	ids, err = builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Len(t, ids, 1)

	poetRef := []byte{0xba, 0x38}
	coinbase := types.HexToAddress("aaaa")
	atx := newActivationTx(types.NodeID{Key: "aaaa", VRFPublicKey: []byte("bbb")}, 0, types.ATXID(types.Hash32{1}), 5, 1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef))

	atxBytes, err := types.InterfaceToBytes(&atx)
	assert.NoError(t, err)
	err = n1.Broadcast(activation.AtxProtocol, atxBytes)
	assert.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	ids, err = builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Len(t, ids, 1)
}*/

/*
func TestBlockBuilder_Gossip_NotSynced(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()

	block1 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.String(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.String(8)))
	hareRes := []types.BlockID{block1.ID(), block2.ID(), block3.ID(), block4.ID()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	st := []*types.Block{block2, block3, block4}
	builder1 := NewBlockBuilder(types.NodeID{Key: "a"}, signing.NewEdSigner(), n1, beginRound, 5, state.NewTxMemPool(), activation.NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, &mockBlockOracle{}, mockTxProcessor{false}, &mockAtxValidator{}, &mockSyncerP{false}, selectCount, selectCount, layersPerEpoch, mockProjector, log.New(n1.Info.ID.String(),
		"",
		""))
	assert.NoError(t, builder1.Start())
	tx := NewTx(t, 5, types.HexToAddress("0xFF"), signing.NewEdSigner())
	b, e := types.InterfaceToBytes(tx)
	assert.Nil(t, e)
	err := n1.Broadcast(IncomingTxProtocol, b)
	assert.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	ids, err := builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Empty(t, ids)

	poetRef := []byte{0xba, 0x38}
	coinbase := types.HexToAddress("aaaa")
	atx := newActivationTx(types.NodeID{Key: "aaaa", VRFPublicKey: []byte("bbb")}, 1, types.ATXID(types.Hash32{1}), 5, 1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef))

	atxBytes, err := types.InterfaceToBytes(&atx)
	assert.NoError(t, err)
	err = n1.Broadcast(activation.AtxProtocol, atxBytes)
	assert.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	ids, err = builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Empty(t, ids)
}*/

func Test_calcHdistRange(t *testing.T) {
	r := require.New(t)
	// will set effective genesis to 5
	types.SetLayersPerEpoch(int32(3))

	// id > hdist
	from, to := calcHdistRange(10, 3)
	r.Equal(types.LayerID(7), from)
	r.Equal(types.LayerID(9), to)

	// id < hdist + effectiveGenesis
	from, to = calcHdistRange(6, 5)
	r.Equal(types.LayerID(types.GetEffectiveGenesis()), from)
	r.Equal(types.LayerID(5), to)

	// id = hdist
	from, to = calcHdistRange(5, 5)
	r.Equal(types.LayerID(types.GetEffectiveGenesis()), from)
	r.Equal(types.LayerID(4), to)

	// hdist = 1
	from, to = calcHdistRange(6, 1)
	r.Equal(types.LayerID(5), from)
	r.Equal(types.LayerID(5), to)

	// hdist = 0
	defer func() {
		err := recover()
		require.Equal(t, err, "hdist cannot be zero")
	}()
	from, to = calcHdistRange(5, 0)
}

var (
	one   = types.CalcHash32([]byte("1"))
	two   = types.CalcHash32([]byte("2"))
	three = types.CalcHash32([]byte("3"))
	four  = types.CalcHash32([]byte("4"))
	five  = types.CalcHash32([]byte("5"))
)

var (
	atx1 = types.ATXID(one)
	atx2 = types.ATXID(two)
	atx3 = types.ATXID(three)
	atx4 = types.ATXID(four)
	atx5 = types.ATXID(five)
)

func Test_selectAtxs(t *testing.T) {
	r := require.New(t)

	atxs := []types.ATXID{atx1, atx2, atx3, atx4, atx5}
	selected := selectAtxs(atxs, 2)
	r.Equal(2, len(selected))

	selected = selectAtxs(atxs, 5)
	r.Equal(5, len(selected))

	selected = selectAtxs(atxs, 10)
	r.Equal(5, len(selected))

	// check uniformity
	rand.Seed(1000)
	origin := []types.ATXID{atx1, atx2, atx3, atx4, atx5}
	mp := make(map[types.ATXID]struct{}, 0)
	for i := 0; i < 100; i++ {
		atxs = []types.ATXID{atx1, atx2, atx3, atx4, atx5}
		selected = selectAtxs(atxs, 2)

		for _, i := range selected {
			mp[i] = struct{}{}
		}
	}

	for _, x := range origin {
		f := false
		for y := range mp {
			if bytes.Equal(x.Bytes(), y.Bytes()) {
				f = true
			}
		}

		if !f {
			r.FailNow("Couldn't find %v", x)
		}
	}

	r.Equal(5, len(mp))
}

var (
	b1 = types.NewExistingBlock(1, []byte{1})
	b2 = types.NewExistingBlock(1, []byte{2})
	b3 = types.NewExistingBlock(1, []byte{3})
	b4 = types.NewExistingBlock(1, []byte{4})
	b5 = types.NewExistingBlock(1, []byte{5})
	b6 = types.NewExistingBlock(1, []byte{6})
	b7 = types.NewExistingBlock(1, []byte{7})
)

func genBlockIds() []types.BlockID {
	bids := []types.BlockID{b1.ID(), b2.ID(), b3.ID(), b4.ID(), b5.ID(), b6.ID(), b7.ID()}
	for i := 0; i < len(bids)*2; i++ {
		l := rand.Int() % len(bids)
		j := rand.Int() % len(bids)
		bids[l], bids[j] = bids[j], bids[l]
	}

	return bids
}

type mockResult struct {
	err error
	ids map[types.LayerID][]types.BlockID
}

func newMockResult() *mockResult {
	m := &mockResult{}
	m.ids = make(map[types.LayerID][]types.BlockID)
	return m
}

func (m *mockResult) set(id types.LayerID) []types.BlockID {
	bids := genBlockIds()
	m.ids[id] = bids

	return bids
}

func (m *mockResult) GetResult(id types.LayerID) ([]types.BlockID, error) {
	bl, ok := m.ids[id]
	if ok {
		return bl, nil
	}
	return nil, m.err
}

var errExample = errors.New("example errExample")

type mockMesh struct {
	b   []*types.Block
	err error
}

func (m *mockMesh) AddBlockWithTxs(blk *types.Block, txs []*types.Transaction, atxs []*types.ActivationTx) error {
	return nil
}

func (m *mockMesh) GetRefBlock(id types.EpochID) types.BlockID {
	return types.BlockID{}
}

func (m *mockMesh) GetBlock(id types.BlockID) (*types.Block, error) {
	for _, blk := range m.b {
		if blk.ID() == id {
			return blk, nil
		}
	}

	return nil, errors.New("not exist")
}

func (m *mockMesh) LayerBlockIds(index types.LayerID) ([]types.BlockID, error) {
	if m.err != nil {
		return nil, m.err
	}
	l := types.NewLayer(index)
	var ids []types.BlockID
	for _, e := range m.b {
		e.LayerIndex = index
		l.AddBlock(e)
		ids = append(ids, e.ID())
	}

	return ids, nil
}

func (m *mockMesh) GetOrphanBlocksBefore(types.LayerID) ([]types.BlockID, error) {
	r := make([]types.BlockID, 0)
	for _, e := range m.b {
		r = append(r, e.ID())
	}
	return r, nil
}

func TestBlockBuilder_getVotes(t *testing.T) {
	rand.Seed(0)

	r := require.New(t)
	n1 := service.NewSimulator().NewNode()
	allblocks := []*types.Block{b1, b2, b3, b4, b5, b6, b7}
	bb := createBlockBuilder("a", n1, allblocks)
	//bb := NewBlockBuilder(types.NodeID{Key: "a"}, signing.NewEdSigner(), n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: allblocks}, &mockResult{}, &mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, selectCount, selectCount, layersPerEpoch, mockProjector, log.NewDefault(t.Name()))
	b, err := bb.getVotes(types.GetEffectiveGenesis())
	r.EqualError(err, "cannot create blockBytes in genesis layer")
	r.Nil(b)

	b, err = bb.getVotes(types.GetEffectiveGenesis() + 1)
	r.Nil(err)
	r.Equal(1, len(b))

	id := types.LayerID(100)
	bb.hdist = 5
	bottom, top := calcHdistRange(id, bb.hdist)

	// has bottom
	mh := newMockResult()
	barr := mh.set(bottom)
	tarr := mh.set(top)
	bb.hareResult = mh
	b, err = bb.getVotes(id)
	r.Nil(err)
	r.Equal(append(barr, tarr...), b)

	// no bottom
	bb.meshProvider = &mockMesh{b: allblocks} // assume all blocks exist in DB --> no filtering applied
	allids := []types.BlockID{b1.ID(), b2.ID(), b3.ID(), b4.ID(), b5.ID(), b6.ID(), b7.ID()}
	mh = newMockResult()
	mh.err = errors.New("no result")
	b1arr := mh.set(bottom + 1)
	tarr = mh.set(top)
	bb.hareResult = mh
	b, err = bb.getVotes(id)
	r.Nil(err)
	var exp []types.BlockID
	exp = append(exp, allids...)
	exp = append(exp, b1arr...)
	exp = append(exp, tarr...)
	r.Equal(exp, b)

	// errExample on layer request
	bb.meshProvider = &mockMesh{b: nil, err: errExample}
	b, err = bb.getVotes(id)
	r.Equal(errExample, err)
}

func TestBlockBuilder_createBlock(t *testing.T) {
	r := require.New(t)
	n1 := service.NewSimulator().NewNode()
	types.SetLayersPerEpoch(int32(3))
	block1 := types.NewExistingBlock(6, []byte(rand.String(8)))
	block2 := types.NewExistingBlock(6, []byte(rand.String(8)))
	block3 := types.NewExistingBlock(6, []byte(rand.String(8)))
	bs := []*types.Block{block1, block2, block3}
	st := []types.BlockID{block1.ID(), block2.ID(), block3.ID()}
	builder1 := createBlockBuilder("a", n1, bs)

	builder1.hareResult = &mockResult{err: errExample, ids: nil}
	builder1.AtxDb = atxDbMock{}
	b, err := builder1.createBlock(7, types.ATXID{}, types.BlockEligibilityProof{}, nil)
	r.Nil(err)
	r.Equal(st, b.BlockVotes)

	builder1.hareResult = &mockResult{err: nil, ids: nil}
	b, err = builder1.createBlock(7, types.ATXID{}, types.BlockEligibilityProof{}, nil)
	r.Nil(err)
	r.Equal([]types.BlockID(nil), b.BlockVotes)
	emptyID := types.BlockID{}
	r.NotEqual(b.ID(), emptyID)

	b, err = builder1.createBlock(5, types.ATXID{}, types.BlockEligibilityProof{}, nil)
	r.EqualError(err, "cannot create blockBytes in genesis layer")
}

func TestBlockBuilder_notSynced(t *testing.T) {
	r := require.New(t)
	beginRound := make(chan types.LayerID)
	n1 := service.NewSimulator().NewNode()
	var bs []*types.Block
	ms := &mockSyncer{}
	ms.notSynced = true
	mbo := &mockBlockOracle{}
	mbo.err = errors.New("err")

	builder := createBlockBuilder("a", n1, bs)
	builder.syncer = ms
	builder.blockOracle = mbo
	builder.beginRoundEvent = beginRound
	//builder := NewBlockBuilder(types.NodeID{Key: "a"}, signing.NewEdSigner(), n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: bs}, hare, mbo, mockTxProcessor{true}, &mockAtxValidator{}, ms, selectCount, selectCount, layersPerEpoch, mockProjector, log.NewDefault(t.Name()))
	go builder.createBlockLoop()
	beginRound <- 1
	beginRound <- 2
	r.Equal(0, mbo.calls)
}

var (
	block1ID = types.NewExistingBlock(1, []byte{1}).ID()
	block2ID = types.NewExistingBlock(1, []byte{2}).ID()
	block3ID = types.NewExistingBlock(1, []byte{3}).ID()
	block4ID = types.NewExistingBlock(1, []byte{4}).ID()
)

func Test_filter(t *testing.T) {
	r := require.New(t)
	f := func(id types.BlockID) (*types.Block, error) {
		if id == block1ID || id == block2ID {
			return nil, errors.New("not exist")
		}

		return nil, nil
	}

	blocks := []types.BlockID{block1ID, block2ID, block3ID, block2ID, block4ID}
	filtered := filterUnknownBlocks(blocks, f)
	for _, b := range filtered {
		r.NotEqual(block1ID, b)
		r.NotEqual(block2ID, b)

		if b != block3ID && b != block4ID {
			r.FailNow("unknown block encountered")
		}
	}
}

func createBlockBuilder(ID string, n *service.Node, meshBlocks []*types.Block) *BlockBuilder {
	beginRound := make(chan types.LayerID)
	cfg := Config{
		Hdist:          5,
		MinerID:        types.NodeID{Key: ID},
		AtxsPerBlock:   selectCount,
		LayersPerEpoch: 3,
		TxsPerBlock:    selectCount,
	}
	bb := NewBlockBuilder(cfg, signing.NewEdSigner(), n, beginRound, MockCoin{}, &mockMesh{b: meshBlocks}, &mockResult{}, &mockBlockOracle{}, &mockSyncer{}, mockProjector, nil, atxDbMock{}, log.NewDefault("mock_builder_"+"a"))
	return bb
}

func Test_getVotesFiltered(t *testing.T) {
	// check scenario where some of the votes are filtered

	r := require.New(t)
	n1 := service.NewSimulator().NewNode()
	types.SetLayersPerEpoch(1)
	allblocks := []*types.Block{b5}
	bb := createBlockBuilder("a", n1, allblocks)
	// has bottom
	mh := newMockResult()
	mh.set(4)
	mh.set(5)
	bb.hareResult = mh
	bb.hdist = 2
	b, err := bb.getVotes(5)
	r.Nil(err)
	r.Equal(1, len(b))
	r.Equal(b5.ID(), b[0])
}

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, activeSetSize uint32, view []types.BlockID,
	nipst *types.NIPST) *types.ActivationTx {

	nipstChallenge := types.NIPSTChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipstChallenge, coinbase, nipst, nil)
}
