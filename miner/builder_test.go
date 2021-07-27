package miner

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/priorityq"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/state"
)

const selectCount = 100

type mockBlockOracle struct {
	calls int
	err   error
	J     uint32
}

func (mbo *mockBlockOracle) BlockEligible(types.LayerID) (types.ATXID, []types.BlockEligibilityProof, []types.ATXID, error) {
	mbo.calls++
	return types.ATXID(types.Hash32{1, 2, 3}), []types.BlockEligibilityProof{{J: mbo.J, Sig: []byte{1}}}, []types.ATXID{atx1, atx2, atx3, atx4, atx5}, mbo.err
}

type mockSyncer struct {
	notSynced bool
}

func (mockSyncer) ListenToGossip() bool {
	return true
}

func (mockSyncer) GetPoetProof(context.Context, types.Hash32) error { return nil }

func (m mockSyncer) IsSynced(context.Context) bool { return !m.notSynced }

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

	block1 := types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block2 := types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block3 := types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block4 := types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)

	txMempool := state.NewTxMemPool()

	bs := []*types.Block{block1, block2, block3, block4}
	builder := createBlockBuilder("a", n, bs)
	builder.TransactionPool = txMempool

	err := builder.Start(context.TODO())
	assert.NoError(t, err)

	err = builder.Start(context.TODO())
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

	block2 := types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block3 := types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block4 := types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)

	st := []*types.Block{block2, block3, block4}
	builder1 := createBlockBuilder("a", n1, st)
	builder2 := createBlockBuilder("b", n2, st)

	atxID1 := types.ATXID(types.HexToHash32("dead"))
	atxID2 := types.ATXID(types.HexToHash32("beef"))

	b1, err := builder1.createBlock(context.TODO(), types.GetEffectiveGenesis().Add(2), atxID1, types.BlockEligibilityProof{}, nil, nil)
	assert.NoError(t, err)
	b2, err := builder2.createBlock(context.TODO(), types.GetEffectiveGenesis().Add(2), atxID2, types.BlockEligibilityProof{}, nil, nil)
	assert.NoError(t, err)

	assert.NotEqual(t, b1.ID(), b2.ID(), "ids are identical")
}

var (
	block1 = types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block2 = types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block3 = types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)
	block4 = types.NewExistingBlock(types.LayerID{}, []byte(rand.String(8)), nil)

	coinbase = types.HexToAddress("aaaa")

	poetRef = []byte{0xba, 0x38}
	atxs    = []*types.ActivationTx{
		newActivationTx(types.NodeID{Key: "aaaa", VRFPublicKey: []byte("bbb")}, 1, types.ATXID(types.Hash32{1}), types.NewLayerID(5),
			1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPostWithChallenge(&types.Hash32{}, poetRef)),
		newActivationTx(types.NodeID{Key: "bbbb", VRFPublicKey: []byte("bbb")}, 1, types.ATXID(types.Hash32{2}), types.NewLayerID(5),
			1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPostWithChallenge(&types.Hash32{}, poetRef)),
		newActivationTx(types.NodeID{Key: "cccc", VRFPublicKey: []byte("bbb")}, 1, types.ATXID(types.Hash32{3}), types.NewLayerID(5),
			1, types.ATXID{}, coinbase, 5, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, activation.NewNIPostWithChallenge(&types.Hash32{}, poetRef)),
	}
)

func TestBlockBuilder_CreateBlockFlow(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	receiver := net.NewNode()

	blockset := []types.BlockID{block1.ID(), block2.ID(), block3.ID()}

	txPool := state.NewTxMemPool()

	atxPool := activation.NewAtxMemPool()

	st := []*types.Block{block1, block2, block3}
	builder := createBlockBuilder("a", n, st)
	builder.baseBlockP = &mockBBP{f: func() (types.BlockID, [][]types.BlockID, error) {
		return types.BlockID{0}, [][]types.BlockID{{}, blockset, {}}, nil
	}}
	builder.TransactionPool = txPool
	builder.beginRoundEvent = beginRound

	gossipMessages := receiver.RegisterGossipProtocol(blocks.NewBlockProtocol, priorityq.High)
	err := builder.Start(context.TODO())
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

	go func() { beginRound <- types.GetEffectiveGenesis().Add(1) }()
	select {
	case output := <-gossipMessages:
		b := types.MiniBlock{}
		_, _ = xdr.Unmarshal(bytes.NewBuffer(output.Bytes()), &b)

		assert.Equal(t, []types.BlockID{block1.ID(), block2.ID(), block3.ID()}, b.ForDiff)

		assert.True(t, ContainsTx(b.TxIDs, transids[0]))
		assert.True(t, ContainsTx(b.TxIDs, transids[1]))
		assert.True(t, ContainsTx(b.TxIDs, transids[2]))

		assert.Equal(t, []types.ATXID{atx1, atx2, atx3, atx4, atx5}, *b.ActiveSet)
	case <-time.After(1 * time.Minute):
		assert.Fail(t, "timeout on receiving block")
	}

}

func TestBlockBuilder_CreateBlockWithRef(t *testing.T) {
	net := service.NewSimulator()
	n := net.NewNode()

	hareRes := []types.BlockID{block1.ID(), block2.ID(), block3.ID(), block4.ID()}

	st := []*types.Block{block1, block2, block3}
	builder := createBlockBuilder("a", n, st)
	builder.baseBlockP = &mockBBP{f: func() (types.BlockID, [][]types.BlockID, error) {
		return types.BlockID{0}, [][]types.BlockID{{block4.ID()}, hareRes, {}}, nil
	}}

	recipient := types.BytesToAddress([]byte{0x01})
	signer := signing.NewEdSigner()

	trans := []*types.Transaction{
		NewTx(t, 1, recipient, signer),
		NewTx(t, 2, recipient, signer),
		NewTx(t, 3, recipient, signer),
	}

	transids := []types.TransactionID{trans[0].ID(), trans[1].ID(), trans[2].ID()}

	b, err := builder.createBlock(context.TODO(), types.GetEffectiveGenesis().Add(1), types.ATXID(types.Hash32{1, 2, 3}), types.BlockEligibilityProof{J: 0, Sig: []byte{1}}, transids, []types.ATXID{atx1, atx2, atx3, atx4, atx5})
	assert.NoError(t, err)

	assert.Equal(t, hareRes, b.ForDiff)
	assert.Equal(t, []types.BlockID{block4.ID()}, b.AgainstDiff)

	assert.True(t, ContainsTx(b.TxIDs, transids[0]))
	assert.True(t, ContainsTx(b.TxIDs, transids[1]))
	assert.True(t, ContainsTx(b.TxIDs, transids[2]))

	assert.Equal(t, []types.ATXID{atx1, atx2, atx3, atx4, atx5}, *b.ActiveSet)

	//test create second block
	bl, err := builder.createBlock(context.TODO(), types.GetEffectiveGenesis().Add(2), types.ATXID(types.Hash32{1, 2, 3}), types.BlockEligibilityProof{J: 1, Sig: []byte{1}}, transids, nil)
	assert.NoError(t, err)

	assert.Equal(t, hareRes, bl.ForDiff)
	assert.Equal(t, []types.BlockID{block4.ID()}, bl.AgainstDiff)

	assert.True(t, ContainsTx(bl.TxIDs, transids[0]))
	assert.True(t, ContainsTx(bl.TxIDs, transids[1]))
	assert.True(t, ContainsTx(bl.TxIDs, transids[2]))

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

var (
	b1 = types.NewExistingBlock(types.NewLayerID(1), []byte{1}, nil)
	b2 = types.NewExistingBlock(types.NewLayerID(1), []byte{2}, nil)
	b3 = types.NewExistingBlock(types.NewLayerID(1), []byte{3}, nil)
	b4 = types.NewExistingBlock(types.NewLayerID(1), []byte{4}, nil)
	b5 = types.NewExistingBlock(types.NewLayerID(1), []byte{5}, nil)
	b6 = types.NewExistingBlock(types.NewLayerID(1), []byte{6}, nil)
	b7 = types.NewExistingBlock(types.NewLayerID(1), []byte{7}, nil)
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

func (m *mockMesh) AddBlockWithTxs(context.Context, *types.Block) error {
	return nil
}

func (m *mockMesh) GetRefBlock(types.EpochID) types.BlockID {
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

func TestBlockBuilder_createBlock(t *testing.T) {
	r := require.New(t)
	n1 := service.NewSimulator().NewNode()
	types.SetLayersPerEpoch(3)
	block1 := types.NewExistingBlock(types.NewLayerID(6), []byte(rand.String(8)), nil)
	block2 := types.NewExistingBlock(types.NewLayerID(6), []byte(rand.String(8)), nil)
	block3 := types.NewExistingBlock(types.NewLayerID(6), []byte(rand.String(8)), nil)
	bs := []*types.Block{block1, block2, block3}
	st := []types.BlockID{block1.ID(), block2.ID(), block3.ID()}
	builder1 := createBlockBuilder("a", n1, bs)
	builder1.baseBlockP = &mockBBP{f: func() (types.BlockID, [][]types.BlockID, error) {
		return types.BlockID{0}, [][]types.BlockID{[]types.BlockID{}, []types.BlockID{}, st}, nil
	}}

	b, err := builder1.createBlock(context.TODO(), types.NewLayerID(7), types.ATXID{}, types.BlockEligibilityProof{}, nil, nil)
	r.Nil(err)
	r.Equal(st, b.NeutralDiff)

	builder1.baseBlockP = &mockBBP{f: func() (types.BlockID, [][]types.BlockID, error) {
		return types.BlockID{0}, [][]types.BlockID{[]types.BlockID{}, nil, st}, nil
	}}

	b, err = builder1.createBlock(context.TODO(), types.NewLayerID(7), types.ATXID{}, types.BlockEligibilityProof{}, nil, nil)
	r.Nil(err)
	r.Equal([]types.BlockID(nil), b.ForDiff)
	emptyID := types.BlockID{}
	r.NotEqual(b.ID(), emptyID)

	b, err = builder1.createBlock(context.TODO(), types.NewLayerID(5), types.ATXID{}, types.BlockEligibilityProof{}, nil, nil)
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
	go builder.createBlockLoop(context.TODO())
	beginRound <- types.NewLayerID(1)
	beginRound <- types.NewLayerID(2)
	r.Equal(0, mbo.calls)
}

type mockBBP struct {
	f func() (types.BlockID, [][]types.BlockID, error)
}

func (b *mockBBP) BaseBlock(context.Context) (types.BlockID, [][]types.BlockID, error) {
	// XXX: for now try to not break all tests
	if b.f != nil {
		return b.f()
	}
	return types.BlockID{0}, [][]types.BlockID{{}, {}, {}}, nil
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
	bb := NewBlockBuilder(cfg, signing.NewEdSigner(), n, beginRound, &mockMesh{b: meshBlocks}, &mockBBP{f: func() (types.BlockID, [][]types.BlockID, error) {
		return types.BlockID{}, [][]types.BlockID{{}, {}, {}}, nil
	}}, &mockBlockOracle{}, &mockSyncer{}, mockProjector, nil, log.NewDefault("mock_builder_"+"a"))
	return bb
}

func newActivationTx(nodeID types.NodeID, sequence uint64, prevATX types.ATXID, pubLayerID types.LayerID,
	startTick uint64, positioningATX types.ATXID, coinbase types.Address, activeSetSize uint32, view []types.BlockID,
	nipst *types.NIPost) *types.ActivationTx {

	nipstChallenge := types.NIPostChallenge{
		NodeID:         nodeID,
		Sequence:       sequence,
		PrevATXID:      prevATX,
		PubLayerID:     pubLayerID,
		StartTick:      startTick,
		PositioningATX: positioningATX,
	}
	return types.NewActivationTx(nipstChallenge, coinbase, nipst, 0, nil)
}
