package tortoise

import (
	"fmt"
	"github.com/google/uuid"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/spacemeshos/go-spacemesh/types"
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
	return mesh.NewPersistentMeshDB(fmt.Sprintf(Path+"ninje_tortoise/"), log.New("ninje_tortoise", "", ""))
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
	ni := sanity(getMeshForBench(), 150, 10, 100, badblocks)

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

func TestNinjaTortoise_S10P9(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	mdb := getMeshForBench()
	sanity(mdb, 100, 10, 10, badblocks)
}
func TestNinjaTortoise_S50P49(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	sanity(getMeshForBench(), 110, 50, 50, badblocks)
}
func TestNinjaTortoise_S100P99(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	sanity(getMeshForBench(), 100, 100, 100, badblocks)
}
func TestNinjaTortoise_S10P7(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	sanity(getMeshForBench(), 100, 10, 7, badblocks)
}
func TestNinjaTortoise_S50P35(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	sanity(getMeshForBench(), 100, 50, 35, badblocks)
}
func TestNinjaTortoise_S100P70(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	sanity(getMeshForBench(), 100, 100, 70, badblocks)
}

func TestNinjaTortoise_S200P199(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	sanity(getMeshForBench(), 100, 200, 200, badblocks)
}

func TestNinjaTortoise_S200P140(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	defer persistenceTeardown()
	sanity(getMeshForBench(), 100, 200, 140, badblocks)
}

//vote explicitly only for previous layer
//correction vectors have no affect here
func TestNinjaTortoise_Sanity1(t *testing.T) {
	layerSize := 200
	patternSize := 200
	layers := 20
	mdb := getInMemMesh()
	alg := sanity(mdb, layers, layerSize, patternSize, 0.2)
	res := vec{patternSize * (layers - 1), 0}
	assert.True(t, alg.tTally[alg.pBase][config.GenesisId] == res, "lyr %d tally was %d insted of %d", layers, alg.tTally[alg.pBase][config.GenesisId], res)
}

func sanity(mdb *mesh.MeshDB, layers int, layerSize int, patternSize int, badBlks float64) *ninjaTortoise {
	lg := log.New("tortoise_test", "", "")
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
		fmt.Println(fmt.Sprintf("lyr %d tally was %d", lyr, alg.tTally[alg.pBase][config.GenesisId]))
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
	alg := NewNinjaTortoise(3, mdb, 5, log.New("TestNinjaTortoise_Sanity2", "", ""))
	l := createMulExplicitLayer(0, map[types.LayerID]*types.Layer{}, nil, 1)
	l1 := createMulExplicitLayer(1, map[types.LayerID]*types.Layer{l.Index(): l}, map[types.LayerID][]int{0: {0}}, 3)
	l2 := createMulExplicitLayer(2, map[types.LayerID]*types.Layer{l1.Index(): l1}, map[types.LayerID][]int{1: {0, 1, 2}}, 3)
	l3 := createMulExplicitLayer(3, map[types.LayerID]*types.Layer{l2.Index(): l2}, map[types.LayerID][]int{l2.Index(): {0}}, 3)
	l4 := createMulExplicitLayer(4, map[types.LayerID]*types.Layer{l2.Index(): l2, l3.Index(): l3}, map[types.LayerID][]int{l2.Index(): {1, 2}, l3.Index(): {1, 2}}, 4)

	AddLayer(mdb, l)
	AddLayer(mdb, l1)
	AddLayer(mdb, l2)
	AddLayer(mdb, l3)
	AddLayer(mdb, l4)

	alg.handleIncomingLayer(l)
	alg.handleIncomingLayer(l1)
	alg.handleIncomingLayer(l2)
	alg.handleIncomingLayer(l3)
	alg.handleIncomingLayer(l4)
	for b, vec := range alg.tTally[alg.pBase] {
		alg.Debug("------> tally for block %d according to complete pattern %d are %d", b, alg.pBase, vec)
	}
	assert.True(t, alg.tTally[alg.pBase][l.Blocks()[0].ID()] == vec{5, 0}, "lyr %d tally was %d insted of %d", 0, alg.tTally[alg.pBase][l.Blocks()[0].ID()], vec{5, 0})
}

func createMulExplicitLayer(index types.LayerID, prev map[types.LayerID]*types.Layer, patterns map[types.LayerID][]int, blocksInLayer int) *types.Layer {
	l := types.NewLayer(index)
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := types.NewExistingBlock(types.BlockID(uuid.New().ID()), index, []byte("data data data"))
		layerBlocks = append(layerBlocks, bl.ID())

		for lyrId, pat := range patterns {
			for _, id := range pat {
				b := prev[lyrId].Blocks()[id]
				bl.AddVote(types.BlockID(b.ID()))
			}
		}
		if index > 0 {
			for _, prevBloc := range prev[index-1].Blocks() {
				bl.AddView(types.BlockID(prevBloc.ID()))
			}
		}
		l.AddBlock(bl)
	}
	log.Debug("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)

	return l
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
		bl := addPattern(types.NewExistingBlock(types.BlockID(uuid.New().ID()), index, []byte("data data data")), goodPattern, prev)
		layerBlocks = append(layerBlocks, bl.ID())
		l.AddBlock(bl)
	}
	for i := 0; i < blocksInLayer-gbs; i++ {
		bl := addPattern(types.NewExistingBlock(types.BlockID(uuid.New().ID()), index, []byte("data data data")), badPattern, prev)
		layerBlocks = append(layerBlocks, bl.ID())
		l.AddBlock(bl)
	}

	log.Debug("Created layer Id %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func addPattern(bl *types.Block, goodPattern []int, prev *types.Layer) *types.Block {
	for _, id := range goodPattern {
		b := prev.Blocks()[id]
		bl.AddVote(types.BlockID(b.ID()))
	}
	for _, prevBloc := range prev.Blocks() {
		bl.AddView(types.BlockID(prevBloc.ID()))
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
		bl := types.NewExistingBlock(types.BlockID(uuid.New().ID()), index, []byte("data data data"))
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.ID()))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(types.BlockID(prevBloc.ID()))
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
