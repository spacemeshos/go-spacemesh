package miner

import (
	"bytes"
	"errors"
	"fmt"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/activation"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

const selectCount = 100

type MockSigning struct {
}

func (ms *MockSigning) Sign(m []byte) []byte {
	return []byte("123456")
}

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
		return nil, errors.New(fmt.Sprintf("hare result for layer%v was not in map", id))
	}
	return blks, nil
}

type mockBlockOracle struct {
}

func (mbo mockBlockOracle) BlockEligible(layerID types.LayerID) (types.AtxId, []types.BlockEligibilityProof, error) {
	return types.AtxId{}, []types.BlockEligibilityProof{{J: 0, Sig: []byte{1}}}, nil
}

type mockMsg struct {
	msg []byte
	c   chan service.MessageValidation
}

func (m *mockMsg) Bytes() []byte {
	return m.msg
}

func (m *mockMsg) ValidationCompletedChan() chan service.MessageValidation {
	return m.c
}

func (m *mockMsg) ReportValidation(protocol string, isValid bool) {
	//m.c <- service.NewMessageValidation(m.msg, protocol, isValid)
}

type mockAtxValidator struct{}

func (mockAtxValidator) GetIdentity(id string) (types.NodeId, error) {
	return types.NodeId{}, nil
}

func (mockAtxValidator) ValidateSignedAtx(signedAtx *types.ActivationTx) error {
	return nil
}

func (mockAtxValidator) SyntacticallyValidateAtx(atx *types.ActivationTx) error { return nil }

type mockTxProcessor struct {
	notValid bool
}

func (m mockTxProcessor) ValidateNonceAndBalance(transaction *types.Transaction) error {
	return nil
}

func (m mockTxProcessor) AddressExists(addr types.Address) bool {
	return !m.notValid
}

type mockSyncer struct {
}

func (mockSyncer) ListenToGossip() bool {
	return true
}

func (mockSyncer) FetchPoetProof(poetProofRef []byte) error { return nil }

func (mockSyncer) IsSynced() bool { return true }

type mockSyncerP struct {
	synced bool
}

func (m mockSyncerP) ListenToGossip() bool {
	return m.synced
}

func (mockSyncerP) FetchPoetProof(poetProofRef []byte) error { return nil }

func (m mockSyncerP) IsSynced() bool { return m.synced }

type MockProjector struct {
}

func (p *MockProjector) GetProjection(addr types.Address) (nonce, balance uint64, err error) {
	return 1, 1000, nil
}

var projector = &MockProjector{}

func TestBlockBuilder_StartStop(t *testing.T) {
	rand.Seed(0)
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	//receiver := net.NewNode()

	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	hareRes := []types.BlockID{block1.Id(), block2.Id(), block3.Id(), block4.Id()}

	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	bs := []*types.Block{block1, block2, block3, block4}
	//st := []types.BlockID{block2.Id(), block3.Id(), block4.Id()}
	orphans := &mockMesh{b: bs}
	builder := NewBlockBuilder(types.NodeId{}, &MockSigning{}, n, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, orphans, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	err = builder.Start()
	assert.Error(t, err)

	err = builder.Close()
	assert.NoError(t, err)

	err = builder.Close()
	assert.Error(t, err)

	err = builder.AddTransaction(NewTx(t, 1, types.BytesToAddress([]byte{0x01}), signing.NewEdSigner()))
	assert.Error(t, err)
}

func TestBlockBuilder_BlockIdGeneration(t *testing.T) {

	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()
	n2 := net.NewNode()

	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	hareRes := []types.BlockID{block1.Id(), block2.Id(), block3.Id(), block4.Id()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	st := []*types.Block{block2, block3, block4}
	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n1.NodeInfo.ID.String(), "", ""))
	builder2 := NewBlockBuilder(types.NodeId{Key: "b"}, &MockSigning{}, n2, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n2.NodeInfo.ID.String(), "", ""))

	b1, _ := builder1.createBlock(1, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)

	b2, _ := builder2.createBlock(1, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)

	assert.True(t, b1.Id() != b2.Id(), "ids are identical")
}

func TestBlockBuilder_CreateBlock(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	receiver := net.NewNode()
	coinbase := types.HexToAddress("aaaa")

	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	hareRes := []types.BlockID{block1.Id(), block2.Id(), block3.Id(), block4.Id()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[1] = hareRes

	st := []*types.Block{block1, block2, block3}
	builder := NewBlockBuilder(types.NodeId{"anton", []byte("anton")}, &MockSigning{}, n, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	recipient := types.BytesToAddress([]byte{0x01})
	signer := signing.NewEdSigner()

	trans := []*types.Transaction{
		NewTx(t, 1, recipient, signer),
		NewTx(t, 2, recipient, signer),
		NewTx(t, 3, recipient, signer),
	}

	transids := []types.TransactionId{trans[0].Id(), trans[1].Id(), trans[2].Id()}

	builder.AddTransaction(trans[0])
	builder.AddTransaction(trans[1])
	builder.AddTransaction(trans[2])

	poetRef := []byte{0xba, 0x38}
	atxs := []*types.ActivationTx{
		types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, coinbase, 1, types.AtxId(types.Hash32{1}), 5, 1, types.AtxId{}, 5, []types.BlockID{block1.Id(), block2.Id(), block3.Id()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
		types.NewActivationTx(types.NodeId{"bbbb", []byte("bbb")}, coinbase, 1, types.AtxId(types.Hash32{2}), 5, 1, types.AtxId{}, 5, []types.BlockID{block1.Id(), block2.Id(), block3.Id()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
		types.NewActivationTx(types.NodeId{"cccc", []byte("bbb")}, coinbase, 1, types.AtxId(types.Hash32{3}), 5, 1, types.AtxId{}, 5, []types.BlockID{block1.Id(), block2.Id(), block3.Id()}, activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
	}

	builder.AtxPool.Put(atxs[0])
	builder.AtxPool.Put(atxs[1])
	builder.AtxPool.Put(atxs[2])

	go func() { beginRound <- 2 }()
	select {
	case output := <-receiver.RegisterGossipProtocol(config.NewBlockProtocol):
		b := types.MiniBlock{}
		xdr.Unmarshal(bytes.NewBuffer(output.Bytes()), &b)

		assert.Equal(t, hareRes, b.BlockVotes)
		assert.Equal(t, []types.BlockID{block1.Id(), block2.Id(), block3.Id()}, b.ViewEdges)

		assert.True(t, ContainsTx(b.TxIds, transids[0]))
		assert.True(t, ContainsTx(b.TxIds, transids[1]))
		assert.True(t, ContainsTx(b.TxIds, transids[2]))

		assert.True(t, ContainsAtx(b.AtxIds, atxs[0].Id()))
		assert.True(t, ContainsAtx(b.AtxIds, atxs[1].Id()))
		assert.True(t, ContainsAtx(b.AtxIds, atxs[2].Id()))

	case <-time.After(1 * time.Second):
		assert.Fail(t, "timeout on receiving block")

	}

}

func NewTx(t *testing.T, nonce uint64, recipient types.Address, signer *signing.EdSigner) *types.Transaction {
	tx, err := mesh.NewSignedTx(nonce, recipient, 1, DefaultGasLimit, DefaultFee, signer)
	assert.NoError(t, err)
	return tx
}

func TestBlockBuilder_SerializeTrans(t *testing.T) {
	tx := NewTx(t, 1, types.BytesToAddress([]byte{0x02}), signing.NewEdSigner())
	buf, err := types.InterfaceToBytes(tx)
	assert.NoError(t, err)

	ntx, err := types.BytesAsTransaction(buf)
	assert.NoError(t, err)
	err = ntx.CalcAndSetOrigin()
	assert.NoError(t, err)

	assert.Equal(t, *tx, *ntx)
}

func ContainsTx(a []types.TransactionId, x types.TransactionId) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

func ContainsAtx(a []types.AtxId, x types.AtxId) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
}

func getState(addr types.Address) (uint64, uint64, error) {
	return 5, 1000, nil
}

func TestBlockBuilder_Validation(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()

	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	hareRes := []types.BlockID{block1.Id(), block2.Id(), block3.Id(), block4.Id()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	st := []*types.Block{block1, block2, block3}
	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n1.NodeInfo.ID.String(), "", ""))
	builder1.Start()
	tx := NewTx(t, 5, types.HexToAddress("0xFF"), signing.NewEdSigner())
	b, e := types.InterfaceToBytes(tx)
	assert.Nil(t, e)
	n1.Broadcast(IncomingTxProtocol, b)
	time.Sleep(300 * time.Millisecond)
	ids, err := builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Empty(t, ids)
	builder1.txValidator = mockTxProcessor{false}
	n1.Broadcast(IncomingTxProtocol, b)
	time.Sleep(300 * time.Millisecond)
	ids, err = builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Len(t, ids, 1)
}

func TestBlockBuilder_Gossip_NotSynced(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()
	coinbase := types.HexToAddress("aaaa")

	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block4 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	hareRes := []types.BlockID{block1.Id(), block2.Id(), block3.Id(), block4.Id()}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	st := []*types.Block{block2, block3, block4}
	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: st}, hare, mockBlockOracle{}, mockTxProcessor{false}, &mockAtxValidator{}, &mockSyncerP{false}, selectCount, projector, log.New(n1.NodeInfo.ID.String(),
		"",
		""))
	builder1.Start()
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
	atx := types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")},
		coinbase,
		1,
		types.AtxId(types.Hash32{1}),
		5,
		1,
		types.AtxId{},
		5,
		[]types.BlockID{block1.Id(), block2.Id(), block3.Id()},
		activation.NewNIPSTWithChallenge(&types.Hash32{}, poetRef))

	atxBytes, err := types.InterfaceToBytes(&atx)
	assert.NoError(t, err)
	err = n1.Broadcast(activation.AtxProtocol, atxBytes)
	assert.NoError(t, err)
	time.Sleep(300 * time.Millisecond)
	ids, err = builder1.TransactionPool.GetTxsForBlock(10, getState)
	assert.NoError(t, err)
	assert.Empty(t, ids)
}

func Test_calcHdistRange(t *testing.T) {
	r := require.New(t)

	// id > hdist
	from, to := calcHdistRange(10, 3)
	r.Equal(types.LayerID(7), from)
	r.Equal(types.LayerID(9), to)

	// id < hdist
	from, to = calcHdistRange(3, 5)
	r.Equal(types.LayerID(1), from)
	r.Equal(types.LayerID(2), to)

	// id = hdist
	from, to = calcHdistRange(5, 5)
	r.Equal(types.LayerID(1), from)
	r.Equal(types.LayerID(4), to)

	// hdist = 1
	from, to = calcHdistRange(5, 1)
	r.Equal(types.LayerID(4), from)
	r.Equal(types.LayerID(4), to)

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
	atx1 = types.AtxId(one)
	atx2 = types.AtxId(two)
	atx3 = types.AtxId(three)
	atx4 = types.AtxId(four)
	atx5 = types.AtxId(five)
)

func Test_selectAtxs(t *testing.T) {
	r := require.New(t)

	atxs := []types.AtxId{atx1, atx2, atx3, atx4, atx5}
	selected := selectAtxs(atxs, 2)
	r.Equal(2, len(selected))

	selected = selectAtxs(atxs, 5)
	r.Equal(5, len(selected))

	selected = selectAtxs(atxs, 10)
	r.Equal(5, len(selected))

	// check uniformity
	rand.Seed(1000)
	origin := []types.AtxId{atx1, atx2, atx3, atx4, atx5}
	mp := make(map[types.AtxId]struct{}, 0)
	for i := 0; i < 100; i++ {
		atxs = []types.AtxId{atx1, atx2, atx3, atx4, atx5}
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
	bids := []types.BlockID{b1.Id(), b2.Id(), b3.Id(), b4.Id(), b5.Id(), b6.Id(), b7.Id()}
	for i := 0; i < len(bids)*2; i++ {
		i := rand.Int() % len(bids)
		j := rand.Int() % len(bids)
		bids[i], bids[j] = bids[j], bids[i]
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

var exampleErr = errors.New("example exampleErr")

type mockMesh struct {
	b   []*types.Block
	err error
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
		ids = append(ids, e.Id())
	}

	return ids, nil
}

func (m *mockMesh) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	r := make([]types.BlockID, 0)
	for _, e := range m.b {
		r = append(r, e.Id())
	}
	return r, nil
}

func TestBlockBuilder_getVotes(t *testing.T) {
	rand.Seed(0)

	r := require.New(t)
	beginRound := make(chan types.LayerID)
	n1 := service.NewSimulator().NewNode()
	bs := []*types.Block{b1, b2, b3}
	//st := []types.BlockID{b1.Id(), b2.Id(), b3.Id()}
	bb := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: bs}, &mockResult{}, mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.NewDefault(t.Name()))
	b, err := bb.getVotes(config.Genesis)
	r.EqualError(err, "cannot create blockBytes in genesis layer")
	r.Nil(b)

	b, err = bb.getVotes(config.Genesis + 1)
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
	bs = []*types.Block{b2, b5, b6, b3}
	bis := []types.BlockID{b2.Id(), b5.Id(), b6.Id(), b3.Id()}
	bb.meshProvider = &mockMesh{b: bs}
	mh = newMockResult()
	mh.err = errors.New("no result")
	b1arr := mh.set(bottom + 1)
	tarr = mh.set(top)
	bb.hareResult = mh
	b, err = bb.getVotes(id)
	r.Nil(err)
	exp := append(bis, b1arr...)
	exp = append(exp, tarr...)
	r.Equal(exp, b)

	// exampleErr on layer request
	bb.meshProvider = &mockMesh{b: nil, err: exampleErr}
	b, err = bb.getVotes(id)
	r.Equal(exampleErr, err)

}

func TestBlockBuilder_createBlock(t *testing.T) {
	r := require.New(t)
	beginRound := make(chan types.LayerID)
	n1 := service.NewSimulator().NewNode()
	hare := &mockResult{}
	block1 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block2 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	block3 := types.NewExistingBlock(0, []byte(rand.RandString(8)))
	bs := []*types.Block{block1, block2, block3}
	st := []types.BlockID{block1.Id(), block2.Id(), block3.Id()}
	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, &mockMesh{b: bs}, hare, mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.NewDefault(t.Name()))

	builder1.hareResult = &mockResult{err: exampleErr, ids: nil}
	b, err := builder1.createBlock(5, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)
	r.Nil(err)
	r.Equal(st, b.BlockVotes)

	builder1.hareResult = &mockResult{err: nil, ids: nil}
	b, err = builder1.createBlock(5, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)
	r.Nil(err)
	r.Equal([]types.BlockID(nil), b.BlockVotes)
	emptyId := types.BlockID{}
	r.NotEqual(b.Id(), emptyId)
}
