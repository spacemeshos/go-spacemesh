package mesh

import (
	"bytes"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/stretchr/testify/assert"
	"testing"
	"time"
)

func getMeshdb() *meshDB {
	blocks := database.NewMemDatabase()
	layers := database.NewMemDatabase()
	transactions := database.NewMemDatabase()
	validty := database.NewMemDatabase()
	mdb := NewMeshDB(layers, blocks, transactions, validty, log.New("mashDb", "", ""))
	return mdb
}

func TestNewMeshDB(t *testing.T) {
	mdb := getMeshdb()
	id := BlockID(123)
	mdb.addBlock(&Block{Id: id})
	block, err := mdb.getBlock(123)
	assert.NoError(t, err)
	assert.True(t, id == block.Id)
}

func TestMeshDB_Close(t *testing.T) {
	defer func() {
		err := recover()
		assert.NotNil(t, err)
	}()
	mdb := getMeshdb()
	mdb.Close()
	mdb.addBlock(&Block{Id: 123})
}

func TestMeshDb_Block(t *testing.T) {
	mdb := getMeshdb()
	id := BlockID(123)
	blk := &Block{Id: id}
	addTransactionsToBlock(blk, 5)
	mdb.addBlock(blk)
	block, err := mdb.getBlock(123)

	assert.NoError(t, err)
	assert.True(t, id == block.Id)

	fmt.Println(block.Txs[0])
	fmt.Println(blk.Txs[0])

	assert.True(t, block.Txs[0].Origin == blk.Txs[0].Origin)
	assert.True(t, bytes.Equal(block.Txs[0].Recipient.Bytes(), blk.Txs[0].Recipient.Bytes()))
	assert.True(t, bytes.Equal(block.Txs[0].Price, blk.Txs[0].Price))
}

func TestMeshDb_Layer(t *testing.T) {
	layers := getMeshdb()
	defer layers.Close()
	id := LayerID(1)
	data := []byte("data")
	block1 := NewBlock(true, data, time.Now(), id)
	block2 := NewBlock(true, data, time.Now(), id)
	block3 := NewBlock(true, data, time.Now(), id)
	l, err := layers.getLayer(id)
	assert.True(t, err != nil, "error: ", err)

	err = layers.addBlock(block1)
	assert.NoError(t, err)
	err = layers.addBlock(block2)
	assert.NoError(t, err)
	err = layers.addBlock(block3)
	assert.NoError(t, err)
	l, err = layers.getLayer(id)
	assert.NoError(t, err)
	//assert.True(t, layers.VerifiedLayer() == 0, "wrong layer count")
	assert.True(t, string(l.blocks[1].Data) == "data", "wrong block data ")
}
