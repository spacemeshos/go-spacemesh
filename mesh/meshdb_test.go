package mesh

import (
	"bytes"
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/nipst"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/types"
	"github.com/stretchr/testify/assert"
	"math"
	"os"
	"testing"
	"time"
)

const (
	Path = "../tmp/mdb"
)

func teardown() {
	os.RemoveAll(Path)
}

func getMeshdb() *MeshDB {
	return NewMemMeshDB(log.New("mdb", "", ""))
}

func TestNewMeshDB(t *testing.T) {
	mdb := getMeshdb()
	id := types.BlockID(123)
	mdb.AddBlock(types.NewExistingBlock(123, 1, nil))
	block, err := mdb.GetBlock(123)
	assert.NoError(t, err)
	assert.True(t, id == block.Id)
}

func TestMeshDb_Block(t *testing.T) {
	mdb := getMeshdb()
	blk := types.NewExistingBlock(123, 1, nil)
	addTransactionsToBlock(blk, 5)
	blk.AddAtx(types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, 1, types.AtxId{}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}), true))
	mdb.AddBlock(blk)
	block, err := mdb.GetBlock(123)

	assert.NoError(t, err)
	assert.True(t, 123 == block.Id)

	assert.True(t, block.Txs[0].Origin == blk.Txs[0].Origin)
	assert.True(t, bytes.Equal(block.Txs[0].Recipient.Bytes(), blk.Txs[0].Recipient.Bytes()))
	assert.True(t, bytes.Equal(block.Txs[0].Price, blk.Txs[0].Price))
	assert.True(t, len(block.ATXs) == len(blk.ATXs))
}

func TestMeshDB_AddBlock(t *testing.T) {

	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	defer mdb.Close()

	block1 := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 1, []byte("data1"))

	addTransactionsToBlock(block1, 4)

	block1.AddAtx(types.NewActivationTx(types.NodeId{"aaaa", []byte("bbb")}, 1, types.AtxId{}, 5, 1, types.AtxId{}, 5, []types.BlockID{1, 2, 3}, nipst.NewNIPSTWithChallenge(&common.Hash{}), true))
	err := mdb.AddBlock(block1)
	assert.NoError(t, err)

	rBlock1, err := mdb.GetBlock(block1.Id)
	assert.NoError(t, err)

	assert.True(t, len(rBlock1.Txs) == len(block1.Txs), "block content was wrong")
	assert.True(t, len(rBlock1.ATXs) == len(block1.ATXs), "block content was wrong")
	//assert.True(t, bytes.Compare(rBlock2.Data, []byte("data2")) == 0, "block content was wrong")
}

func chooseRandomPattern(blocksInLayer int, patternSize int) []int {
	rand.Seed(time.Now().UnixNano())
	p := rand.Perm(blocksInLayer)
	indexes := make([]int, 0, patternSize)
	for _, r := range p[:patternSize] {
		indexes = append(indexes, r)
	}
	return indexes
}

func createLayerWithRandVoting(index types.LayerID, prev []*types.Layer, blocksInLayer int, patternSize int) *types.Layer {
	l := types.NewLayer(index)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize)))))
	}
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := types.NewExistingBlock(types.BlockID(uuid.New().ID()), 0, []byte("data data data"))
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.Id))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(types.BlockID(prevBloc.Id))
		}
		l.AddBlock(bl)
	}
	log.Info("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func TestForEachInView_Persistent(t *testing.T) {
	mdb := NewPersistentMeshDB(Path+"/mesh_db/", log.New("TestForEachInView", "", ""))
	defer mdb.Close()
	defer teardown()
	testForeachInView(mdb, t)
}

func TestForEachInView_InMem(t *testing.T) {
	mdb := NewMemMeshDB(log.New("TestForEachInView", "", ""))
	testForeachInView(mdb, t)
}

func testForeachInView(mdb *MeshDB, t *testing.T) {
	blocks := make(map[types.BlockID]*types.Block)
	l := GenesisLayer()
	gen := l.Blocks()[0]
	blocks[gen.ID()] = gen

	if err := mdb.AddBlock(gen); err != nil {
		t.Fail()
	}

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*types.Layer{l}, 2, 2)
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			mdb.AddBlock(b)
		}
		l = lyr
	}
	mp := map[types.BlockID]struct{}{}
	foo := func(nb *types.BlockHeader) error {
		fmt.Println("process block", "layer", nb.Id, nb.LayerIndex)
		mp[nb.Id] = struct{}{}
		return nil
	}
	errHandler := func(err error) {
		log.Error("error while traversing view ", err)
	}
	ids := map[types.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.Id] = struct{}{}
	}
	mdb.ForBlockInView(ids, 0, foo, errHandler)
	for _, bl := range blocks {
		_, found := mp[bl.ID()]
		assert.True(t, found, "did not process block  ", bl)
	}
}
