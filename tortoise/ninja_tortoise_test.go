package tortoise

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"math"
	"os"
	"runtime"
	"testing"
	"time"
)

const Path = "../tmp/tortoise/"

const inmem = 1
const disK = 2

const memType = inmem

func init() {
	persistenceTeardown()
}

func getPersistentMash() *mesh.MeshDB {
	db, _ := mesh.NewPersistentMeshDB(fmt.Sprintf(Path+"ninje_tortoise/"), log.New("ninje_tortoise", "", ""))
	return db
}

func persistenceTeardown() {
	os.RemoveAll(Path)
}

func getInMemMesh() *mesh.MeshDB {
	return mesh.NewMemMeshDB(log.New("", "", ""))
}

func getMeshForBench() *mesh.MeshDB {
	switch memType {
	case inmem:
		return getInMemMesh()
	case disK:
		return getPersistentMash()
	}
	return nil
}

func TestAlgorithm_HandleLateBlock(t *testing.T) {
	mdb := getMeshForBench()
	alg := NewNinjaTortoise(8, mdb, 5, log.New("", "", ""))
	a := Algorithm{Tortoise: alg}
	blk := types.NewExistingBlock(5, []byte("asdfasdfdgadsgdgr"))
	a.HandleLateBlock(blk)
	assert.True(t, blk.Layer() == types.LayerID(5))
}

func TestVec_Add(t *testing.T) {
	v := vec{0, 0}
	v = v.Add(vec{1, 0})
	assert.True(t, v == vec{1, 0}, "vec was wrong %d", v)
	v2 := vec{0, 0}
	v2 = v2.Add(vec{0, 1})
	assert.True(t, v2 == vec{0, 1}, "vec was wrong %d", v2)
}

func TestVec_Negate(t *testing.T) {
	v := vec{1, 0}
	v = v.Negate()
	assert.True(t, v == vec{-1, 0}, "vec was wrong %d", v)
	v2 := vec{0, 1}
	v2 = v2.Negate()
	assert.True(t, v2 == vec{0, -1}, "vec was wrong %d", v2)
}

func TestVec_Multiply(t *testing.T) {
	v := vec{1, 0}
	v = v.Multiply(5)
	assert.True(t, v == vec{5, 0}, "vec was wrong %d", v)
	v2 := vec{2, 1}
	v2 = v2.Multiply(5)
	assert.True(t, v2 == vec{10, 5}, "vec was wrong %d", v2)
}

func TestNinjaTortoise_GlobalOpinion(t *testing.T) {
	glo := globalOpinion(vec{2, 0}, 2, 1)
	assert.True(t, glo == Support, "vec was wrong %d", glo)
	glo = globalOpinion(vec{1, 0}, 2, 1)
	assert.True(t, glo == Abstain, "vec was wrong %d", glo)
	glo = globalOpinion(vec{0, 2}, 2, 1)
	assert.True(t, glo == Against, "vec was wrong %d", glo)
}

func TestNinjaTortoise_evict(t *testing.T) {
	defer persistenceTeardown()
	ni := sanity(t, getMeshForBench(), 150, 10, 100, badblocks)

	for i := 1; i < 140; i++ {
		for _, j := range ni.patterns[types.LayerID(i)] {
			if _, ok := ni.tSupport[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tPattern[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tTally[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tVote[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tEffectiveToBlocks[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tComplete[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tPatSupport[j]; ok {
				t.Fail()
			}
		}
		ids, _ := ni.LayerBlockIds(49)
		for _, j := range ids {
			if _, ok := ni.tEffective[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tCorrect[j]; ok {
				t.Fail()
			}
			if _, ok := ni.tExplicit[j]; ok {
				t.Fail()
			}

		}
	}
}

func bToMb(b uint64) uint64 {
	return b / 1024 / 1024
}

func PrintMemUsage() {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// For info on each, see: https://golang.org/pkg/runtime/#MemStats
	fmt.Printf("Alloc = %v MiB", bToMb(m.Alloc))
	fmt.Printf("\tTotalAlloc = %v MiB", bToMb(m.TotalAlloc))
	fmt.Printf("\tSys = %v MiB", bToMb(m.Sys))
	fmt.Printf("\tNumGC = %v\n", m.NumGC)
}

var badblocks = 0.1

func TestNinjaTortoise_VariableLayerSize(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := NewNinjaTortoise(8, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)
	alg.handleIncomingLayer(l)

	l1 := createLayer(1, []*types.Layer{l}, 8)
	AddLayer(mdb, l1)
	alg.handleIncomingLayer(l1)

	l2 := createLayer(2, []*types.Layer{l1, l}, 8)
	AddLayer(mdb, l2)
	alg.handleIncomingLayer(l2)

	l3 := createLayer(3, []*types.Layer{l2, l1, l}, 8)
	AddLayer(mdb, l3)
	alg.handleIncomingLayer(l3)

	l4 := createLayer(4, []*types.Layer{l3, l2, l1, l}, 8)
	AddLayer(mdb, l4)
	alg.handleIncomingLayer(l4)

	l5 := createLayer(5, []*types.Layer{l4, l3, l2, l1, l}, 4)
	AddLayer(mdb, l5)
	alg.handleIncomingLayer(l5)

	l6 := createLayer(6, []*types.Layer{l5, l4, l3, l2, l1}, 9)
	AddLayer(mdb, l6)
	alg.handleIncomingLayer(l6)
	//
	l7 := createLayer(7, []*types.Layer{l6, l5, l4, l3, l2}, 9)
	AddLayer(mdb, l7)
	alg.handleIncomingLayer(l7)

	l8 := createLayer(8, []*types.Layer{l7, l6, l5, l4, l3}, 9)
	AddLayer(mdb, l8)
	alg.handleIncomingLayer(l8)

	l9 := createLayer(9, []*types.Layer{l8, l7, l6, l5, l4}, 9)
	AddLayer(mdb, l9)
	alg.handleIncomingLayer(l9)

	assert.True(t, alg.pBase.Layer() == 8)

}

func TestNinjaTortoise_Abstain(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := NewNinjaTortoise(3, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)
	alg.handleIncomingLayer(l)

	l1 := createLayer(1, []*types.Layer{l}, 3)
	AddLayer(mdb, l1)
	alg.handleIncomingLayer(l1)

	l2 := createLayer(2, []*types.Layer{l1, l}, 3)
	AddLayer(mdb, l2)
	alg.handleIncomingLayer(l2)

	l3 := createLayer(3, []*types.Layer{l2, l}, 3)
	AddLayer(mdb, l3)
	alg.handleIncomingLayer(l3)

	l4 := createLayer(4, []*types.Layer{l3, l}, 3)
	AddLayer(mdb, l4)
	alg.handleIncomingLayer(l4)
	assert.True(t, alg.pBase.Layer() == 3)
	assert.True(t, alg.tTally[alg.tGood[3]][mesh.GenesisBlock.Id()][0] == 9)
}

func TestNinjaTortoise_BlockByBlock(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := NewNinjaTortoise(8, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)
	handleLayerBlockByBlock(l, alg)

	l1 := createLayer(1, []*types.Layer{l}, 8)
	AddLayer(mdb, l1)
	handleLayerBlockByBlock(l1, alg)

	l2 := createLayer(2, []*types.Layer{l1, l}, 8)
	AddLayer(mdb, l2)
	handleLayerBlockByBlock(l2, alg)

	l3 := createLayer(3, []*types.Layer{l2, l1, l}, 8)
	AddLayer(mdb, l3)
	handleLayerBlockByBlock(l3, alg)

	l4 := createLayer(4, []*types.Layer{l3, l2, l1, l}, 8)
	AddLayer(mdb, l4)
	handleLayerBlockByBlock(l4, alg)

	l5 := createLayer(5, []*types.Layer{l4, l3, l2, l1, l}, 4)
	AddLayer(mdb, l5)
	handleLayerBlockByBlock(l5, alg)

	l6 := createLayer(6, []*types.Layer{l5, l4, l3, l2, l1}, 9)
	AddLayer(mdb, l6)
	handleLayerBlockByBlock(l6, alg)
	//
	l7 := createLayer(7, []*types.Layer{l6, l5, l4, l3, l2}, 9)
	AddLayer(mdb, l7)
	handleLayerBlockByBlock(l7, alg)

	l8 := createLayer(8, []*types.Layer{l7, l6, l5, l4, l3}, 9)
	AddLayer(mdb, l8)
	handleLayerBlockByBlock(l8, alg)

	l9 := createLayer(9, []*types.Layer{l8, l7, l6, l5, l4}, 9)
	AddLayer(mdb, l9)
	handleLayerBlockByBlock(l9, alg)

	assert.True(t, alg.pBase.Layer() == 8)

}

func TestNinjaTortoise_GoodLayerChanges(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := NewNinjaTortoise(4, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)
	handleLayerBlockByBlock(l, alg)

	l1 := createLayer(1, []*types.Layer{l}, 4)
	AddLayer(mdb, l1)
	handleLayerBlockByBlock(l1, alg)

	l21 := createLayer(2, []*types.Layer{l1, l}, 2)
	AddLayer(mdb, l21)
	handleLayerBlockByBlock(l21, alg)

	l3 := createLayer(3, []*types.Layer{l21, l1, l}, 5)
	AddLayer(mdb, l3)
	handleLayerBlockByBlock(l3, alg)

	l22 := createLayer(2, []*types.Layer{l1, l}, 6)
	AddLayer(mdb, l22)
	handleLayerBlockByBlock(l22, alg)

	l4 := createLayer(4, []*types.Layer{l3, l22, l1, l}, 4)
	AddLayer(mdb, l4)
	handleLayerBlockByBlock(l4, alg)

	l5 := createLayer(5, []*types.Layer{l4, l3, l22, l1, l}, 4)
	AddLayer(mdb, l5)
	handleLayerBlockByBlock(l5, alg)

	l6 := createLayer(6, []*types.Layer{l5, l4, l3, l22, l1}, 4)
	AddLayer(mdb, l6)
	handleLayerBlockByBlock(l6, alg)
	//
	l7 := createLayer(7, []*types.Layer{l6, l5, l4, l3, l22}, 4)
	AddLayer(mdb, l7)
	handleLayerBlockByBlock(l7, alg)

	assert.True(t, alg.pBase.Layer() == 6)
}

func TestNinjaTortoise_LateBlocks(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := NewNinjaTortoise(10, mdb, 1, lg)

	l01 := types.NewLayer(0)
	l01.AddBlock(types.NewExistingBlock(0, []byte(rand.RandString(8))))

	l02 := types.NewLayer(0)
	l02.AddBlock(types.NewExistingBlock(0, []byte(rand.RandString(8))))

	AddLayer(mdb, l01)
	AddLayer(mdb, l02)
	alg.handleIncomingLayer(l01)
	alg.handleIncomingLayer(l02)

	l11 := createLayer(1, []*types.Layer{l01}, 6)
	AddLayer(mdb, l11)
	alg.handleIncomingLayer(l11)

	l12 := createLayer(1, []*types.Layer{l02}, 6)
	AddLayer(mdb, l12)
	alg.handleIncomingLayer(l12)

	//l2 votes for first pattern
	l21 := createLayer(2, []*types.Layer{l11}, 6)
	AddLayer(mdb, l21)
	alg.handleIncomingLayer(l21)

	//l3 votes for sec pattern
	l3 := createLayer(3, []*types.Layer{l21}, 6)
	AddLayer(mdb, l3)
	alg.handleIncomingLayer(l3)

	l22 := createLayer(2, []*types.Layer{l12}, 6)
	AddLayer(mdb, l22)
	alg.handleIncomingLayer(l22)

	//l4 votes for first pattern
	l4 := createLayer(4, []*types.Layer{l3}, 6)
	AddLayer(mdb, l4)
	alg.handleIncomingLayer(l4)

	//l5 votes for first pattern
	l5 := createLayer(5, []*types.Layer{l4}, 25)
	AddLayer(mdb, l5)
	alg.handleIncomingLayer(l5)

}

func handleLayerBlockByBlock(lyr *types.Layer, algorithm *ninjaTortoise) {
	idx := lyr.Index()
	for _, blk := range lyr.Blocks() {
		lr := types.NewExistingLayer(idx, []*types.Block{blk})
		algorithm.handleIncomingLayer(lr)
	}
}

func createLayer(index types.LayerID, prev []*types.Layer, blocksInLayer int) *types.Layer {
	l := types.NewLayer(index)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(blocksInPrevLayer)))))
	}
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := types.NewExistingBlock(index, []byte(rand.RandString(8)))
		layerBlocks = append(layerBlocks, bl.Id())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.Id()))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(types.BlockID(prevBloc.Id()))
		}
		l.AddBlock(bl)
	}
	log.Debug("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func TestNinjaTortoise_S10P9(t *testing.T) {
	t.Skip()

	defer persistenceTeardown()
	mdb := getMeshForBench()
	sanity(t, mdb, 100, 10, 10, badblocks)
}
func TestNinjaTortoise_S50P49(t *testing.T) {
	t.Skip()

	defer persistenceTeardown()
	sanity(t, getMeshForBench(), 110, 50, 50, badblocks)
}
func TestNinjaTortoise_S100P99(t *testing.T) {
	t.Skip()

	defer persistenceTeardown()
	sanity(t, getMeshForBench(), 100, 100, 100, badblocks)
}
func TestNinjaTortoise_S10P7(t *testing.T) {
	t.Skip()

	defer persistenceTeardown()
	sanity(t, getMeshForBench(), 100, 10, 7, badblocks)
}
func TestNinjaTortoise_S50P35(t *testing.T) {
	t.Skip()

	defer persistenceTeardown()
	sanity(t, getMeshForBench(), 100, 50, 35, badblocks)
}
func TestNinjaTortoise_S100P70(t *testing.T) {
	t.Skip()

	defer persistenceTeardown()
	sanity(t, getMeshForBench(), 100, 100, 70, badblocks)
}

func TestNinjaTortoise_S200P199(t *testing.T) {
	t.Skip()
	defer persistenceTeardown()
	sanity(t, getMeshForBench(), 100, 200, 200, badblocks)
}

func TestNinjaTortoise_S200P140(t *testing.T) {
	t.Skip()
	defer persistenceTeardown()
	sanity(t, getMeshForBench(), 100, 200, 140, badblocks)
}

//vote explicitly only for previous layer
//correction vectors have no affect here
func TestNinjaTortoise_Sanity1(t *testing.T) {
	layerSize := 3
	patternSize := 3
	layers := 5
	mdb := getInMemMesh()
	alg := sanity(t, mdb, layers, layerSize, patternSize, 0.2)
	assert.True(t, alg.pBase.Layer() == types.LayerID(layers-1))
}

func sanity(t *testing.T, mdb *mesh.MeshDB, layers int, layerSize int, patternSize int, badBlks float64) *ninjaTortoise {
	lg := log.New(t.Name(), "", "")
	l1 := mesh.GenesisLayer()
	var lyrs []*types.Layer
	AddLayer(mdb, l1)
	lyrs = append(lyrs, l1)
	l := createLayerWithRandVoting(l1.Index()+1, []*types.Layer{l1}, layerSize, 1)
	AddLayer(mdb, l)
	lyrs = append(lyrs, l)
	for i := 0; i < layers-1; i++ {
		lyr := createLayerWithCorruptedPattern(l.Index()+1, l, layerSize, patternSize, badBlks)
		start := time.Now()
		AddLayer(mdb, lyr)
		lyrs = append(lyrs, lyr)
		lg.Debug("Time inserting layer into db: %v ", time.Since(start))
		l = lyr
	}

	alg := NewNinjaTortoise(layerSize, mdb, 5, lg)

	for _, lyr := range lyrs {
		start := time.Now()
		alg.handleIncomingLayer(lyr)
		alg.Debug("Time to process layer: %v ", time.Since(start))
		fmt.Println(fmt.Sprintf("lyr %v tally was %d", lyr.Index()-1, alg.tTally[alg.pBase][mesh.GenesisBlock.Id()]))
		l = lyr
	}

	fmt.Println(fmt.Sprintf("number of layers: %d layer size: %d good pattern size %d bad blocks %v", layers, layerSize, patternSize, badBlks))
	PrintMemUsage()
	return alg
}

//vote explicitly for two previous layers
//correction vectors compensate for double count
func TestNinjaTortoise_Sanity2(t *testing.T) {
	defer persistenceTeardown()
	mdb := getInMemMesh()
	alg := NewNinjaTortoise(3, mdb, 5, log.New(t.Name(), "", ""))
	l := mesh.GenesisLayer()

	l1 := createLayer(1, []*types.Layer{l}, 3)
	l2 := createLayer(2, []*types.Layer{l1, l}, 3)
	l3 := createLayer(3, []*types.Layer{l2, l1, l}, 3)
	l4 := createLayer(4, []*types.Layer{l3, l1, l}, 3)
	l5 := createLayer(5, []*types.Layer{l4, l3, l2, l1, l}, 3)

	AddLayer(mdb, l)
	AddLayer(mdb, l1)
	AddLayer(mdb, l2)
	AddLayer(mdb, l3)
	AddLayer(mdb, l4)
	AddLayer(mdb, l5)

	alg.handleIncomingLayer(l)
	alg.handleIncomingLayer(l1)
	alg.handleIncomingLayer(l2)
	alg.handleIncomingLayer(l3)
	alg.handleIncomingLayer(l4)
	alg.handleIncomingLayer(l5)
	for b, vec := range alg.tTally[alg.pBase] {
		alg.Debug("------> tally for block %d according to complete pattern %d are %d", b, alg.pBase, vec)
	}
	assert.True(t, alg.tTally[alg.pBase][l.Blocks()[0].Id()] == vec{12, 0}, "lyr %d tally was %d insted of %d", 0, alg.tTally[alg.pBase][l.Blocks()[0].Id()], vec{12, 0})
}

func createLayerWithCorruptedPattern(index types.LayerID, prev *types.Layer, blocksInLayer int, patternSize int, badBlocks float64) *types.Layer {
	l := types.NewLayer(index)

	blocks := prev.Blocks()
	blocksInPrevLayer := len(blocks)
	goodPattern := chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize))))
	badPattern := chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize))))

	gbs := int(float64(blocksInLayer) * (1 - badBlocks))
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < gbs; i++ {
		bl := addPattern(types.NewExistingBlock(index, []byte(rand.RandString(8))), goodPattern, prev)
		layerBlocks = append(layerBlocks, bl.Id())
		l.AddBlock(bl)
	}
	for i := 0; i < blocksInLayer-gbs; i++ {
		bl := addPattern(types.NewExistingBlock(index, []byte(rand.RandString(8))), badPattern, prev)
		layerBlocks = append(layerBlocks, bl.Id())
		l.AddBlock(bl)
	}

	log.Debug("Created layer Id %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func addPattern(bl *types.Block, goodPattern []int, prev *types.Layer) *types.Block {
	for _, id := range goodPattern {
		b := prev.Blocks()[id]
		bl.AddVote(types.BlockID(b.Id()))
	}
	for _, prevBloc := range prev.Blocks() {
		bl.AddView(types.BlockID(prevBloc.Id()))
	}
	return bl
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
		bl := types.NewExistingBlock(index, []byte(rand.RandString(8)))
		layerBlocks = append(layerBlocks, bl.Id())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.Id()))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(types.BlockID(prevBloc.Id()))
		}
		l.AddBlock(bl)
	}
	log.Debug("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
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

func AddLayer(m *mesh.MeshDB, layer *types.Layer) error {
	//add blocks to mDB
	for _, bl := range layer.Blocks() {
		if err := m.AddBlock(bl); err != nil {
			return err
		}
	}
	return nil
}
