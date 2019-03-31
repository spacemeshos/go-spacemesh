package consensus

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/rand"
	"github.com/stretchr/testify/assert"
	"math"
	"runtime"
	"testing"
	"time"
)

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

func TestForEachInView(t *testing.T) {
	blocks := make(map[mesh.BlockID]*mesh.Block)
	alg := NewNinjaTortoise(2, log.New("TestForEachInView", "", ""))
	l := GenesisLayer()
	for _, b := range l.Blocks() {
		blocks[b.ID()] = b
	}
	for i := 0; i < 4; i++ {
		lyr := createLayerWithRandVoting(l.Index()+1, []*mesh.Layer{l}, 2, 2)
		for _, b := range lyr.Blocks() {
			blocks[b.ID()] = b
		}
		l = lyr
		for b, vec := range alg.tTally[alg.pBase] {
			alg.Debug("------> tally for block %d according to complete pattern %d are %d", b, alg.pBase, vec)
		}
	}
	mp := map[mesh.BlockID]struct{}{}

	foo := func(nb *mesh.Block) {
		log.Info("process block %d layer %d", nb.ID(), nb.Layer())
		mp[nb.ID()] = struct{}{}
	}

	ids := map[mesh.BlockID]struct{}{}
	for _, b := range l.Blocks() {
		ids[b.ID()] = struct{}{}
	}

	forBlockInView(ids, blocks, 0, foo)

	for _, bl := range blocks {
		_, found := mp[bl.ID()]
		assert.True(t, found, "did not process block  ", bl)
	}

}

func TestNinjaTortoise_UpdatePatternTally(t *testing.T) {
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
	sanity(100, 10, 10, badblocks)
}
func TestNinjaTortoise_S50P49(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sanity(100, 50, 50, badblocks)
}
func TestNinjaTortoise_S100P99(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sanity(100, 100, 100, badblocks)
}
func TestNinjaTortoise_S10P7(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sanity(100, 10, 7, badblocks)
}
func TestNinjaTortoise_S50P35(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sanity(100, 50, 35, badblocks)
}
func TestNinjaTortoise_S100P70(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sanity(100, 100, 70, badblocks)
}

func TestNinjaTortoise_S200P199(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sanity(100, 200, 200, badblocks)
}

func TestNinjaTortoise_S200P140(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	sanity(100, 200, 140, badblocks)
}

//vote explicitly only for previous layer
//correction vectors have no affect here
func TestNinjaTortoise_Sanity1(t *testing.T) {
	layerSize := 200
	patternSize := 200
	layers := 100
	alg := sanity(layers, layerSize, patternSize, 0.2)
	res := vec{patternSize * (layers - 1), 0}
	assert.True(t, alg.tTally[alg.pBase][config.GenesisId] == res, "lyr %d tally was %d insted of %d", layers, alg.tTally[alg.pBase][config.GenesisId], res)
}

func sanity(layers int, layerSize int, patternSize int, badBlks float64) *ninjaTortoise {
	alg := NewNinjaTortoise(layerSize, log.New("TestNinjaTortoise_Sanity1", "", ""))
	l1 := GenesisLayer()
	alg.handleIncomingLayer(l1)
	l := createLayerWithRandVoting(l1.Index()+1, []*mesh.Layer{l1}, layerSize, 1)
	alg.handleIncomingLayer(l)
	for i := 0; i < layers-1; i++ {
		lyr := createLayerWithCorruptedPattern(l.Index()+1, l, layerSize, patternSize, badBlks)
		start := time.Now()
		alg.handleIncomingLayer(lyr)
		alg.Info("Time to process layer: %v ", time.Since(start))
		l = lyr
	}
	fmt.Println(fmt.Sprintf("number of layers: %d layer size: %d good pattern size %d bad blocks %v", layers, layerSize, patternSize, badBlks))
	PrintMemUsage()
	return alg
}

//vote explicitly for two previous layers
//correction vectors compensate for double count
func TestNinjaTortoise_Sanity2(t *testing.T) {
	alg := NewNinjaTortoise(3, log.New("TestNinjaTortoise_Sanity2", "", ""))
	l := createMulExplicitLayer(0, map[mesh.LayerID]*mesh.Layer{}, nil, 1)
	l1 := createMulExplicitLayer(1, map[mesh.LayerID]*mesh.Layer{l.Index(): l}, map[mesh.LayerID][]int{0: {0}}, 3)
	l2 := createMulExplicitLayer(2, map[mesh.LayerID]*mesh.Layer{l1.Index(): l1}, map[mesh.LayerID][]int{1: {0, 1, 2}}, 3)
	l3 := createMulExplicitLayer(3, map[mesh.LayerID]*mesh.Layer{l2.Index(): l2}, map[mesh.LayerID][]int{l2.Index(): {0}}, 3)
	l4 := createMulExplicitLayer(4, map[mesh.LayerID]*mesh.Layer{l2.Index(): l2, l3.Index(): l3}, map[mesh.LayerID][]int{l2.Index(): {1, 2}, l3.Index(): {1, 2}}, 4)

	alg.handleIncomingLayer(l)
	alg.handleIncomingLayer(l1)
	alg.handleIncomingLayer(l2)
	alg.handleIncomingLayer(l3)
	alg.handleIncomingLayer(l4)
	for b, vec := range alg.tTally[alg.pBase] {
		alg.Info("------> tally for block %d according to complete pattern %d are %d", b, alg.pBase, vec)
	}
	assert.True(t, alg.tTally[alg.pBase][l.Blocks()[0].ID()] == vec{5, 0}, "lyr %d tally was %d insted of %d", 0, alg.tTally[alg.pBase][l.Blocks()[0].ID()], vec{5, 0})
}

func createMulExplicitLayer(index mesh.LayerID, prev map[mesh.LayerID]*mesh.Layer, patterns map[mesh.LayerID][]int, blocksInLayer int) *mesh.Layer {
	ts := time.Now()
	coin := false
	// just some random Data
	data := []byte(crypto.UUIDString())
	l := mesh.NewLayer(index)
	layerBlocks := make([]mesh.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := mesh.NewBlock(coin, data, ts, 1)
		layerBlocks = append(layerBlocks, bl.ID())

		for lyrId, pat := range patterns {
			for _, id := range pat {
				b := prev[lyrId].Blocks()[id]
				bl.AddVote(mesh.BlockID(b.Id))
			}
		}
		if index > 0 {
			for _, prevBloc := range prev[index-1].Blocks() {
				bl.AddView(mesh.BlockID(prevBloc.Id))
			}
		}
		l.AddBlock(bl)
	}
	log.Info("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)

	return l
}

func createLayerWithCorruptedPattern(index mesh.LayerID, prev *mesh.Layer, blocksInLayer int, patternSize int, badBlocks float64) *mesh.Layer {
	ts := time.Now()
	coin := false
	// just some random Data
	data := []byte(crypto.UUIDString())
	l := mesh.NewLayer(index)

	blocks := prev.Blocks()
	blocksInPrevLayer := len(blocks)
	goodPattern := chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize))))
	badPattern := chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize))))

	gbs := int(float64(blocksInLayer) * (1 - badBlocks))
	layerBlocks := make([]mesh.BlockID, 0, blocksInLayer)
	for i := 0; i < gbs; i++ {
		bl := addPattern(mesh.NewBlock(coin, data, ts, 1), goodPattern, prev)
		layerBlocks = append(layerBlocks, bl.ID())
		l.AddBlock(bl)
	}
	for i := 0; i < blocksInLayer-gbs; i++ {
		bl := addPattern(mesh.NewBlock(coin, data, ts, 1), badPattern, prev)
		layerBlocks = append(layerBlocks, bl.ID())
		l.AddBlock(bl)
	}

	log.Info("Created layer Id %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func addPattern(bl *mesh.Block, goodPattern []int, prev *mesh.Layer) *mesh.Block {
	for _, id := range goodPattern {
		b := prev.Blocks()[id]
		bl.AddVote(mesh.BlockID(b.Id))
	}
	for _, prevBloc := range prev.Blocks() {
		bl.AddView(mesh.BlockID(prevBloc.Id))
	}
	return bl
}

func createLayerWithRandVoting(index mesh.LayerID, prev []*mesh.Layer, blocksInLayer int, patternSize int) *mesh.Layer {
	ts := time.Now()
	coin := false
	// just some random Data
	data := []byte(crypto.UUIDString())
	l := mesh.NewLayer(index)
	var patterns [][]int
	for _, l := range prev {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(patternSize)))))
	}
	layerBlocks := make([]mesh.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := mesh.NewBlock(coin, data, ts, 1)
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(mesh.BlockID(b.Id))
			}
		}
		for _, prevBloc := range prev[0].Blocks() {
			bl.AddView(mesh.BlockID(prevBloc.Id))
		}
		l.AddBlock(bl)
	}
	log.Info("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
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

//this is  a util function to manufacture mesh files for the test
func TestSyncer_MakeMeshFile(t *testing.T) {
	//t.Skip()
	db := database.NewLevelDbStore("sync", nil, nil)
	defer db.Close()
	lg := log.New("sync", "", "")
	mdb := mesh.NewMeshDB(db, db, db, db, lg)
	l, _ := mdb.GetLayer(5)
	assert.True(t, len(l.Blocks()) == 200)
	////todo add 10 layers of 200 blocks
	//l1 := GenesisLayer()
	//l := createLayerWithRandVoting(l1.Index()+1, []*mesh.Layer{l1}, 200, 1)
	//for i := 0; i < 100; i++ {
	//	lyr := createLayerWithCorruptedPattern(l.Index()+1, l, 200, 180, 0.1)
	//	mdb.AddLayer(lyr)
	//	l = lyr
	//}
}
