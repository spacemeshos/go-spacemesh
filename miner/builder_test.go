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
	"github.com/spacemeshos/go-spacemesh/nipst"
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

func (m MockHare) GetResult(lower types.LayerID, upper types.LayerID) ([]types.BlockID, error) {
	var results []types.BlockID
	for id := lower; id <= upper; id++ {
		blks, ok := m.res[id]
		if !ok {
			return nil, errors.New(fmt.Sprintf("hare result for layer%v was not in map", id))
		}
		results = append(results, blks...)
	}
	return results, nil
}

type MockOrphans struct {
	st []types.BlockID
}

func (m MockOrphans) GetOrphanBlocksBefore(l types.LayerID) ([]types.BlockID, error) {
	return m.st, nil
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

func (mockSyncer) WeaklySynced() bool {
	return true
}

func (mockSyncer) FetchPoetProof(poetProofRef []byte) error { return nil }

func (mockSyncer) IsSynced() bool { return true }

type mockSyncerP struct {
	synced bool
}

func (m mockSyncerP) WeaklySynced() bool {
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

	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	//receiver := net.NewNode()

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	orphans := MockOrphans{st: []types.BlockID{1, 2, 3}}
	builder := NewBlockBuilder(types.NodeId{}, &MockSigning{}, n, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, orphans, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	err = builder.Start()
	assert.Error(t, err)

	err = builder.Close()
	assert.NoError(t, err)

	err = builder.Close()
	assert.Error(t, err)

	addr1 := types.BytesToAddress([]byte{0x02})
	addr2 := types.BytesToAddress([]byte{0x01})
	err = builder.AddTransaction(types.NewTxWithOrigin(1, addr1, addr2, 1, DefaultGasLimit, DefaultGas))
	assert.Error(t, err)
}

func TestBlockBuilder_BlockIdGeneration(t *testing.T) {

	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()
	n2 := net.NewNode()

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n1.NodeInfo.ID.String(), "", ""))
	builder2 := NewBlockBuilder(types.NodeId{Key: "b"}, &MockSigning{}, n2, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n2.NodeInfo.ID.String(), "", ""))

	b1, _ := builder1.createBlock(1, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)

	b2, _ := builder2.createBlock(1, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)

	assert.True(t, b1.ID() != b2.ID(), "ids are identical")
}

func TestBlockBuilder_CreateBlock(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	receiver := net.NewNode()
	coinbase := types.HexToAddress("aaaa")

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[1] = hareRes

	builder := NewBlockBuilder(types.NodeId{"anton", []byte("anton")}, &MockSigning{}, n, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	addr1 := types.BytesToAddress([]byte{0x02})
	addr2 := types.BytesToAddress([]byte{0x01})

	trans := []*types.Transaction{
		types.NewTxWithOrigin(1, addr1, addr2, 1, DefaultGasLimit, DefaultGas),
		types.NewTxWithOrigin(2, addr1, addr2, 1, DefaultGasLimit, DefaultGas),
		types.NewTxWithOrigin(3, addr1, addr2, 1, DefaultGasLimit, DefaultGas),
	}

	transids := []types.TransactionId{trans[0].Id(), trans[1].Id(), trans[2].Id()}

	builder.AddTransaction(trans[0])
	builder.AddTransaction(trans[1])
	builder.AddTransaction(trans[2])

	poetRef := []byte{0xba, 0x38}
	atxs := []*types.ActivationTx{
		types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, coinbase, 1, types.AtxId(types.Hash32{1}), 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
		types.NewActivationTx(types.NodeId{"bbbb", []byte("bbb")}, coinbase, 1, types.AtxId(types.Hash32{2}), 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
		types.NewActivationTx(types.NodeId{"cccc", []byte("bbb")}, coinbase, 1, types.AtxId(types.Hash32{3}), 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&types.Hash32{}, poetRef)),
	}

	builder.AtxPool.Put(atxs[0].Id(), atxs[0])
	builder.AtxPool.Put(atxs[1].Id(), atxs[1])
	builder.AtxPool.Put(atxs[2].Id(), atxs[2])

	go func() { beginRound <- 2 }()
	select {
	case output := <-receiver.RegisterGossipProtocol(config.NewBlockProtocol):
		b := types.MiniBlock{}
		xdr.Unmarshal(bytes.NewBuffer(output.Bytes()), &b)

		assert.Equal(t, hareRes, b.BlockVotes)
		assert.Equal(t, []types.BlockID{1, 2, 3}, b.ViewEdges)

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

func TestBlockBuilder_SerializeTrans(t *testing.T) {
	tx, err := types.NewSignedTx(0, types.BytesToAddress([]byte{0x02}), 10, 10, 10, signing.NewEdSigner())
	assert.NoError(t, err)
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

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.New(n1.NodeInfo.ID.String(), "", ""))
	builder1.Start()
	ed := signing.NewEdSigner()
	tx, e := types.NewSignedTx(5, types.HexToAddress("0xFF"), 2, 3, 4, ed)
	assert.Nil(t, e)
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

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: map[types.LayerID][]types.BlockID{}}
	hare.res[0] = hareRes

	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{false}, &mockAtxValidator{}, &mockSyncerP{false}, selectCount, projector, log.New(n1.NodeInfo.ID.String(),
		"",
		""))
	builder1.Start()
	ed := signing.NewEdSigner()
	tx, e := types.NewSignedTx(0, types.HexToAddress("0xFF"), 2, 3, 4, ed)
	assert.Nil(t, e)
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
		[]types.BlockID{1, 2, 3},
		nipst.NewNIPSTWithChallenge(&types.Hash32{}, poetRef))

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

type mockHare struct {
	err error
	ids []types.BlockID
}

func (m mockHare) GetResult(lower types.LayerID, upper types.LayerID) ([]types.BlockID, error) {
	return m.ids, m.err
}

var err = errors.New("example err")

func TestBlockBuilder_createBlock(t *testing.T) {
	r := require.New(t)
	beginRound := make(chan types.LayerID)
	n1 := service.NewSimulator().NewNode()
	hare := mockHare{}
	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, 5, NewTxMemPool(), NewAtxMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, selectCount, projector, log.NewDefault(t.Name()))

	builder1.hareResult = mockHare{err: err, ids: nil}
	b, err := builder1.createBlock(5, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)
	r.Nil(err)
	r.Equal(b.BlockVotes, []types.BlockID{})

	builder1.hareResult = mockHare{err: nil, ids: nil}
	b, err = builder1.createBlock(5, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)
	r.Nil(err)
	r.Equal(b.BlockVotes, []types.BlockID{})

	builder1.hareResult = mockHare{err: nil, ids: []types.BlockID{}}
	b, err = builder1.createBlock(5, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)
	r.Nil(err)
	r.Equal(b.BlockVotes, []types.BlockID{})
}
