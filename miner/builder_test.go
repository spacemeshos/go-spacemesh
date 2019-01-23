package miner

import (
	"bytes"
	"fmt"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/p2p/service"
	"github.com/spacemeshos/go-spacemesh/state"
	"github.com/spacemeshos/go-spacemesh/sync"
	"github.com/stretchr/testify/assert"
	"math/big"
	"math/rand"
	"testing"
	"time"
)

type MockCoin struct {}

func (m MockCoin) GetResult() bool {
	return rand.Int() % 2 == 0
}

type MockHare struct {
	res []mesh.BlockID
}

func (m MockHare) GetResult(id mesh.LayerID) ([]mesh.BlockID, error){
	return m.res, nil
}

type MockOrphans struct {
	st []mesh.BlockID
}

func (m MockOrphans) GetOrphans() []mesh.BlockID{
	return m.st
}

type mockBlockOracle struct {

}

func (mbo mockBlockOracle) Validate(id mesh.LayerID, pubkey fmt.Stringer) bool {
	return true
}

func TestBlockBuilder_StartStop(t *testing.T) {

	net := service.NewSimulator()
	beginRound := make(chan mesh.LayerID)
	n := net.NewNode()
	//receiver := net.NewNode()

	hareRes := []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2), mesh.BlockID(3)}
	hare := MockHare{res:hareRes}

	builder := NewBlockBuilder(n.Node, n,beginRound, MockCoin{}, MockOrphans{st:[]mesh.BlockID{1,2,3}}, hare, mockBlockOracle{})

	err := builder.Start()
	assert.NoError(t, err)

	err = builder.Start()
	assert.Error(t,err)

	err = builder.Stop()
	assert.NoError(t, err)

	err = builder.Stop()
	assert.Error(t, err)

	addr1 := common.BytesToAddress([]byte{0x02})
	addr2 := common.BytesToAddress([]byte{0x01})
	err = builder.AddTransaction(1, addr1,addr2,big.NewInt(1))
	assert.Error(t,err)
}


func TestBlockBuilder_CreateBlock(t *testing.T) {
	net := service.NewSimulator()
	beginRound := make(chan mesh.LayerID)
	n := net.NewNode()
	receiver := net.NewNode()

	hareRes := []mesh.BlockID{mesh.BlockID(0), mesh.BlockID(1), mesh.BlockID(2), mesh.BlockID(3)}
	hare := MockHare{res:hareRes}

	builder := NewBlockBuilder(n.Node, n,beginRound, MockCoin{}, MockOrphans{st:[]mesh.BlockID{1,2,3}}, hare, mockBlockOracle{})

	err := builder.Start()
	assert.NoError(t, err)

	addr1 := common.BytesToAddress([]byte{0x02})
	addr2 := common.BytesToAddress([]byte{0x01})

	trans := []mesh.SerializableTransaction {
		Transaction2SerializableTransaction(state.NewTransaction(1, addr1,addr2,big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
		Transaction2SerializableTransaction(state.NewTransaction(1, addr1,addr2,big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
		Transaction2SerializableTransaction(state.NewTransaction(2, addr1,addr2,big.NewInt(1), DefaultGasLimit, big.NewInt(DefaultGas))),
	}

	builder.AddTransaction(trans[0].AccountNonce, trans[0].Origin, *trans[0].Recipient, big.NewInt(0).SetBytes(trans[0].Price))
	builder.AddTransaction(trans[1].AccountNonce, trans[1].Origin, *trans[1].Recipient, big.NewInt(0).SetBytes(trans[1].Price))
	builder.AddTransaction(trans[2].AccountNonce, trans[2].Origin, *trans[2].Recipient, big.NewInt(0).SetBytes(trans[2].Price))



	go func() {beginRound <- mesh.LayerID(0) }()

	select {
		case output := <- receiver.RegisterProtocol(sync.NewBlock):
			b := mesh.Block{}
			xdr.Unmarshal(bytes.NewBuffer(output.Bytes()), &b)
			assert.Equal(t, hareRes, b.BlockVotes)
			assert.Equal(t, trans,b.Txs)
			assert.Equal(t, []mesh.BlockID{1,2,3}, b.ViewEdges)

		case <-time.After(1 * time.Second):
			assert.Fail(t, "timeout on receiving block")

	}

}

func TestBlockBuilder_SerializeTrans(t *testing.T) {
	tx := mesh.NewSerializableTransaction(0, common.BytesToAddress([]byte{0x01}), common.BytesToAddress([]byte{0x02}), big.NewInt(10), big.NewInt(10), 10)
	buf, err := mesh.TransactionAsBytes(tx)
	assert.NoError(t,err)

	ntx, err := mesh.BytesAsTransaction(bytes.NewReader(buf))
	assert.NoError(t,err)

	assert.Equal(t, *tx ,*ntx)
}