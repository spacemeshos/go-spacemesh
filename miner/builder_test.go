package miner

import (
	"bytes"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/address"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/stretchr/testify/assert"
	"math/big"
	"testing"
	"time"
)

type MockCoin struct{}

func (m MockCoin) GetResult() bool {
	return rand.Int()%2 == 0
}

type MockHare struct {
	res []mesh.BlockID
}

func (m MockHare) GetResult(id mesh.LayerID) ([]mesh.BlockID, error) {
	return m.res, nil
}

type MockOrphans struct {
	st []mesh.BlockID
}

func (m MockOrphans) GetOrphanBlocksBefore(l mesh.LayerID) ([]mesh.BlockID, error) {
	return m.st, nil
}

type mockBlockOracle struct {
}

func (mbo mockBlockOracle) BlockEligible(id mesh.LayerID, pubkey string) bool {
	return true
}

func TestBlockBuilder_StartStop(t *testing.T) {

	net := service.NewSimulator()
	beginRound := make(chan mesh.LayerID)
	n := net.NewNode()
	//receiver := net.NewNode()

	hareRes := []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2), mesh.BlockID(3)}
	hare := MockHare{res: hareRes}

	builder := NewBlockBuilder(n.Node.String(), n, beginRound, MockCoin{}, MockOrphans{st: []mesh.BlockID{1, 2, 3}}, hare, mockBlockOracle{},
		log.New(n.Node.String(), "", ""))

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
	err = builder.AddTransaction(1, addr1, addr2, big.NewInt(1))
	assert.Error(t, err)
}

func TestBlockBuilder_BlockIdGeneration(t *testing.T) {

	net := service.NewSimulator()
	beginRound := make(chan mesh.LayerID)
	n1 := net.NewNode()
	n2 := net.NewNode()

	hareRes := []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2), mesh.BlockID(3)}
	hare := MockHare{res: hareRes}

	builder1 := NewBlockBuilder(n1.Node.String(), n1, beginRound, MockCoin{}, MockOrphans{st: []mesh.BlockID{1, 2, 3}}, hare,
		mockBlockOracle{}, log.New(n1.Node.String(), "", ""))
	builder2 := NewBlockBuilder(n2.Node.String(), n2, beginRound, MockCoin{}, MockOrphans{st: []mesh.BlockID{1, 2, 3}}, hare,
		mockBlockOracle{}, log.New(n2.Node.String(), "", ""))

	b1, _ := builder1.createBlock(1, nil)

	b2, _ := builder2.createBlock(1, nil)

	assert.True(t, b1.ID() != b2.ID(), "ids are identical")
}

func TestBlockBuilder_CreateBlock(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan mesh.LayerID)
	n := net.NewNode()
	receiver := net.NewNode()

	hareRes := []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2), mesh.BlockID(3)}
	hare := MockHare{res: hareRes}

	builder := NewBlockBuilder(n.Node.String(), n, beginRound, MockCoin{}, MockOrphans{st: []mesh.BlockID{1, 2, 3}}, hare,
		mockBlockOracle{}, log.New(n.Node.String(), "", ""))

	err := builder.Start()
	assert.NoError(t, err)

	addr1 := address.BytesToAddress([]byte{0x02})
	addr2 := address.BytesToAddress([]byte{0x01})

	trans := []*mesh.SerializableTransaction{
		Transaction2SerializableTransaction(mesh.NewTransaction(1, addr1, addr2, big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
		Transaction2SerializableTransaction(mesh.NewTransaction(1, addr1, addr2, big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
		Transaction2SerializableTransaction(mesh.NewTransaction(2, addr1, addr2, big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
	}

	builder.AddTransaction(trans[0].AccountNonce, trans[0].Origin, *trans[0].Recipient, big.NewInt(0).SetBytes(trans[0].Price))
	builder.AddTransaction(trans[1].AccountNonce, trans[1].Origin, *trans[1].Recipient, big.NewInt(0).SetBytes(trans[1].Price))
	builder.AddTransaction(trans[2].AccountNonce, trans[2].Origin, *trans[2].Recipient, big.NewInt(0).SetBytes(trans[2].Price))

	go func() { beginRound <- 2 }()

	select {
	case output := <-receiver.RegisterGossipProtocol(sync.NewBlockProtocol):
		b := mesh.Block{}
		xdr.Unmarshal(bytes.NewBuffer(output.Bytes()), &b)
		assert.Equal(t, hareRes, b.BlockVotes)
		assert.Equal(t, trans, b.Txs)
		assert.Equal(t, []mesh.BlockID{1, 2, 3}, b.ViewEdges)

	case <-time.After(1 * time.Second):
		assert.Fail(t, "timeout on receiving block")

	}

}

func TestBlockBuilder_SerializeTrans(t *testing.T) {
	tx := mesh.NewSerializableTransaction(0, address.BytesToAddress([]byte{0x01}), address.BytesToAddress([]byte{0x02}), big.NewInt(10), big.NewInt(10), 10)
	buf, err := mesh.TransactionAsBytes(tx)
	assert.NoError(t, err)

	ntx, err := mesh.BytesAsTransaction(buf)
	assert.NoError(t, err)

	assert.Equal(t, *tx, *ntx)
}
