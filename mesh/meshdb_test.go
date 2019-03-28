package mesh

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"math"
	"os"
	"testing"
	"time"
)

const (
	Path = "../tmp/sync/"
)

func teardown() {
	os.RemoveAll(Path)
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

func createLayerWithRandVoting(index LayerID, prev []*Layer, blocksInLayer int, patternSize int) *Layer {
	l := NewLayer(index)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize)))))
	}
	layerBlocks := make([]BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := NewExistingBlock(BlockID(uuid.New().ID()), 0, []byte("data data data"))
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(BlockID(b.Id))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(BlockID(prevBloc.Id))
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
	blocks := make(map[BlockID]*Block)
	l := GenesisLayer()
	gen := l.blocks[0]
	blocks[gen.ID()] = gen

	if err := mdb.AddBlock(gen); err != nil {
		t.Fail()
	}

	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*Layer{l}, 2, 2)
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
			mdb.AddBlock(b)
		}
		l = lyr
	}
	mp := map[BlockID]struct{}{}
	foo := func(nb *Block) {
		fmt.Println("process block", "layer", nb.ID(), nb.Layer())
		mp[nb.ID()] = struct{}{}
	}
	errHandler := func(err error) {
		log.Error("error while traversing view ", err)
	}
	ids := map[BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.Id] = struct{}{}
	}
	mdb.ForBlockInView(ids, 0, foo, errHandler)
	for _, bl := range blocks {
		_, found := mp[bl.ID()]
		assert.True(t, found, "did not process block  ", bl)
	}
}
