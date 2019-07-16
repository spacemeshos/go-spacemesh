package miner

import (
	"bytes"
	"errors"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
	"time"
)

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
	res []types.BlockID
}

func (m MockHare) GetResult(id types.LayerID) ([]types.BlockID, error) {
	return m.res, nil
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

func (m mockTxProcessor) ValidateTransactionSignature(tx *types.SerializableSignedTransaction) (address.Address, error) {
	if !m.notValid {

		return address.HexToAddress("0xFFFF"), nil
	}
	return address.Address{}, errors.New("invalid sig for tx")
}

type mockSyncer struct{}

func (mockSyncer) FetchPoetProof(poetProofRef []byte) error { return nil }

func TestBlockBuilder_StartStop(t *testing.T) {

	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	//receiver := net.NewNode()

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: hareRes}

	orphans := MockOrphans{st: []types.BlockID{1, 2, 3}}
	builder := NewBlockBuilder(types.NodeId{}, &MockSigning{},
		n,
		beginRound,
		NewTypesTransactionIdMemPool(),
		NewTypesAtxIdMemPool(),
		MockCoin{},
		orphans,
		hare,
		mockBlockOracle{},
		mockTxProcessor{},
		&mockAtxValidator{},
		&mockSyncer{},
		4,
		log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	err = builder.Start()
	assert.Error(t, err)

	err = builder.Close()
	assert.NoError(t, err)

	err = builder.Close()
	assert.Error(t, err)

	addr1 := address.BytesToAddress([]byte{0x02})
	addr2 := address.BytesToAddress([]byte{0x01})
	err = builder.AddTransaction(types.NewAddressableTx(1, addr1, addr2, 1, DefaultGasLimit, DefaultGas))
	assert.Error(t, err)
}

func TestBlockBuilder_BlockIdGeneration(t *testing.T) {

	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()
	n2 := net.NewNode()

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: hareRes}

	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, NewTypesTransactionIdMemPool(),
		NewTypesAtxIdMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, 4, log.New(n1.NodeInfo.ID.String(), "", ""))
	builder2 := NewBlockBuilder(types.NodeId{Key: "b"}, &MockSigning{}, n2, beginRound, NewTypesTransactionIdMemPool(),
		NewTypesAtxIdMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, 4, log.New(n2.NodeInfo.ID.String(), "", ""))

	b1, _ := builder1.createBlock(1, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)

	b2, _ := builder2.createBlock(1, types.AtxId{}, types.BlockEligibilityProof{}, nil, nil)

	assert.True(t, b1.ID() != b2.ID(), "ids are identical")
}

func TestBlockBuilder_CreateBlock(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n := net.NewNode()
	receiver := net.NewNode()
	coinbase := address.HexToAddress("aaaa")

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: hareRes}

	builder := NewBlockBuilder(types.NodeId{"anton", []byte("anton")}, &MockSigning{}, n, beginRound, NewTypesTransactionIdMemPool(),
		NewTypesAtxIdMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{}, &mockAtxValidator{}, &mockSyncer{}, 4, log.New(n.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	addr1 := address.BytesToAddress([]byte{0x02})
	addr2 := address.BytesToAddress([]byte{0x01})

	trans := []*types.AddressableSignedTransaction{
		Transaction2SerializableTransaction(mesh.NewTransaction(1, addr1, addr2, big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
		Transaction2SerializableTransaction(mesh.NewTransaction(2, addr1, addr2, big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
		Transaction2SerializableTransaction(mesh.NewTransaction(3, addr1, addr2, big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
	}

	transids := []types.TransactionId{types.GetTransactionId(trans[0].SerializableSignedTransaction), types.GetTransactionId(trans[1].SerializableSignedTransaction), types.GetTransactionId(trans[2].SerializableSignedTransaction)}

	builder.AddTransaction(trans[0])
	builder.AddTransaction(trans[1])
	builder.AddTransaction(trans[2])

	poetRef := []byte{0xba, 0x38}
	atxs := []*types.ActivationTx{
		types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, coinbase, 1, types.AtxId{common.Hash{1}}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}, poetRef)),
		types.NewActivationTx(types.NodeId{"bbbb", []byte("bbb")}, coinbase, 1, types.AtxId{common.Hash{2}}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}, poetRef)),
		types.NewActivationTx(types.NodeId{"cccc", []byte("bbb")}, coinbase, 1, types.AtxId{common.Hash{3}}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}, poetRef)),
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
	tx := types.NewAddressableTx(0, address.BytesToAddress([]byte{0x01}), address.BytesToAddress([]byte{0x02}), 10, 10, 10)
	buf, err := types.AddressableTransactionAsBytes(tx)
	assert.NoError(t, err)

	ntx, err := types.BytesAsAddressableTransaction(buf)
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

func TestBlockBuilder_Validation(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan types.LayerID)
	n1 := net.NewNode()

	hareRes := []types.BlockID{types.BlockID(0), types.BlockID(1), types.BlockID(2), types.BlockID(3)}
	hare := MockHare{res: hareRes}

	builder1 := NewBlockBuilder(types.NodeId{Key: "a"}, &MockSigning{}, n1, beginRound, NewTypesTransactionIdMemPool(),
		NewTypesAtxIdMemPool(), MockCoin{}, MockOrphans{st: []types.BlockID{1, 2, 3}}, hare, mockBlockOracle{}, mockTxProcessor{true}, &mockAtxValidator{}, &mockSyncer{}, 4, log.New(n1.NodeInfo.ID.String(), "", ""))
	builder1.Start()
	ed := signing.NewEdSigner()
	tx, e := types.NewSignedTx(0, address.HexToAddress("0xFF"), 2, 3, 4, ed)
	assert.Nil(t, e)
	b, e := types.SignedTransactionAsBytes(tx)
	assert.Nil(t, e)
	n1.Broadcast(IncomingTxProtocol, b)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 0, len(builder1.TransactionPool.txMap))
	builder1.txValidator = mockTxProcessor{false}
	n1.Broadcast(IncomingTxProtocol, b)
	time.Sleep(300 * time.Millisecond)
	assert.Equal(t, 1, len(builder1.TransactionPool.txMap))
}
