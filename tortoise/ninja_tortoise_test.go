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
const disc = 2

const memType = inmem

func init() {
	persistenceTeardown()
	types.SetLayersPerEpoch(3)
	types.EffectiveGenesis = 0
}

func getPersistentMash() *mesh.DB {
	db, _ := mesh.NewPersistentMeshDB(fmt.Sprintf(Path+"ninje_tortoise/"), 10, log.New("ninje_tortoise", "", "").WithOptions(log.Nop))
	return db
}

func persistenceTeardown() {
	os.RemoveAll(Path)
}

func getInMemMesh() *mesh.DB {
	return mesh.NewMemMeshDB(log.New("", "", ""))
}

func getMeshForBench() *mesh.DB {
	switch memType {
	case inmem:
		return getInMemMesh()
	case disc:
		return getPersistentMash()
	}
	return nil
}

func TestAlgorithm_HandleLateBlock(t *testing.T) {
	mdb := getMeshForBench()
	a := tortoise{ninjaTortoise: newNinjaTortoise(8, mdb, 5, log.New("", "", ""))}
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
	assert.True(t, glo == support, "vec was wrong %d", glo)
	glo = globalOpinion(vec{1, 0}, 2, 1)
	assert.True(t, glo == abstain, "vec was wrong %d", glo)
	glo = globalOpinion(vec{0, 2}, 2, 1)
	assert.True(t, glo == against, "vec was wrong %d", glo)
}

func TestNinjaTortoise_evict(t *testing.T) {
	defer persistenceTeardown()
	ni := sanity(t, getMeshForBench(), 150, 10, 100, badblocks)

	for i := mesh.GenesisBlock().LayerIndex; i < mesh.GenesisBlock().LayerIndex+140; i++ {
		for j := range ni.Patterns[types.LayerID(i)] {
			if _, ok := ni.TSupport[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TPattern[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TTally[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TVote[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TEffectiveToBlocks[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TComplete[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TPatSupport[j]; ok {
				t.Fail()
			}
		}

		if _, ok := ni.TGood[types.LayerID(i)]; ok {
			t.Fail()
		}
		if _, ok := ni.Patterns[types.LayerID(i)]; ok {
			t.Fail()
		}

		ids, _ := ni.db.LayerBlockIds(49)
		for _, j := range ids {
			if _, ok := ni.TEffective[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TCorrect[j]; ok {
				t.Fail()
			}
			if _, ok := ni.TExplicit[j]; ok {
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
	ni := newNinjaTortoise(8, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)
	ni.handleIncomingLayer(l)

	l1 := createLayer(1, []*types.Layer{l}, 8)
	AddLayer(mdb, l1)
	ni.handleIncomingLayer(l1)

	l2 := createLayer(2, []*types.Layer{l1, l}, 8)
	AddLayer(mdb, l2)
	ni.handleIncomingLayer(l2)

	l3 := createLayer(3, []*types.Layer{l2, l1, l}, 8)
	AddLayer(mdb, l3)
	ni.handleIncomingLayer(l3)

	l4 := createLayer(4, []*types.Layer{l3, l2, l1, l}, 8)
	AddLayer(mdb, l4)
	ni.handleIncomingLayer(l4)

	l5 := createLayer(5, []*types.Layer{l4, l3, l2, l1, l}, 4)
	AddLayer(mdb, l5)
	ni.handleIncomingLayer(l5)

	l6 := createLayer(6, []*types.Layer{l5, l4, l3, l2, l1}, 9)
	AddLayer(mdb, l6)
	ni.handleIncomingLayer(l6)
	//
	l7 := createLayer(7, []*types.Layer{l6, l5, l4, l3, l2}, 9)
	AddLayer(mdb, l7)
	ni.handleIncomingLayer(l7)

	l8 := createLayer(8, []*types.Layer{l7, l6, l5, l4, l3}, 9)
	AddLayer(mdb, l8)
	ni.handleIncomingLayer(l8)

	l9 := createLayer(9, []*types.Layer{l8, l7, l6, l5, l4}, 9)
	AddLayer(mdb, l9)
	ni.handleIncomingLayer(l9)

	assert.True(t, ni.PBase.Layer() == 8)

}

func TestNinjaTortoise_Abstain(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := newNinjaTortoise(3, mdb, 5, lg)
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
	assert.Equal(t, alg.PBase.Layer(), mesh.GenesisLayer().Index()+3)
	assert.True(t, alg.TTally[alg.TGood[3]][blockIDLayerTuple{BlockID: mesh.GenesisBlock().ID(), LayerID: mesh.GenesisBlock().Layer()}][0] == 9)
}

func TestNinjaTortoise_BlockByBlock(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := newNinjaTortoise(8, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)
	handleLayerBlockByBlock(l, alg)

	l1 := createLayer(mesh.GenesisLayer().Index()+1, []*types.Layer{l}, 8)
	AddLayer(mdb, l1)
	handleLayerBlockByBlock(l1, alg)

	l2 := createLayer(mesh.GenesisLayer().Index()+2, []*types.Layer{l1, l}, 8)
	AddLayer(mdb, l2)
	handleLayerBlockByBlock(l2, alg)

	l3 := createLayer(mesh.GenesisLayer().Index()+3, []*types.Layer{l2, l1, l}, 8)
	AddLayer(mdb, l3)
	handleLayerBlockByBlock(l3, alg)

	l4 := createLayer(mesh.GenesisLayer().Index()+4, []*types.Layer{l3, l2, l1, l}, 8)
	AddLayer(mdb, l4)
	handleLayerBlockByBlock(l4, alg)

	l5 := createLayer(mesh.GenesisLayer().Index()+5, []*types.Layer{l4, l3, l2, l1, l}, 4)
	AddLayer(mdb, l5)
	handleLayerBlockByBlock(l5, alg)

	l6 := createLayer(mesh.GenesisLayer().Index()+6, []*types.Layer{l5, l4, l3, l2, l1}, 9)
	AddLayer(mdb, l6)
	handleLayerBlockByBlock(l6, alg)
	//
	l7 := createLayer(mesh.GenesisLayer().Index()+7, []*types.Layer{l6, l5, l4, l3, l2}, 9)
	AddLayer(mdb, l7)
	handleLayerBlockByBlock(l7, alg)

	l8 := createLayer(mesh.GenesisLayer().Index()+8, []*types.Layer{l7, l6, l5, l4, l3}, 9)
	AddLayer(mdb, l8)
	handleLayerBlockByBlock(l8, alg)

	l9 := createLayer(mesh.GenesisLayer().Index()+9, []*types.Layer{l8, l7, l6, l5, l4}, 9)
	AddLayer(mdb, l9)
	handleLayerBlockByBlock(l9, alg)

	assert.Equal(t, alg.PBase.Layer(), mesh.GenesisLayer().Index()+8)

}

func TestNinjaTortoise_GoodLayerChanges(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := newNinjaTortoise(4, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)
	handleLayerBlockByBlock(l, alg)

	l1 := createLayer(mesh.GenesisLayer().Index()+1, []*types.Layer{l}, 4)
	AddLayer(mdb, l1)
	handleLayerBlockByBlock(l1, alg)

	l21 := createLayer(mesh.GenesisLayer().Index()+2, []*types.Layer{l1, l}, 2)
	AddLayer(mdb, l21)
	handleLayerBlockByBlock(l21, alg)

	l3 := createLayer(mesh.GenesisLayer().Index()+3, []*types.Layer{l21, l1, l}, 5)
	AddLayer(mdb, l3)
	handleLayerBlockByBlock(l3, alg)

	l22 := createLayer(mesh.GenesisLayer().Index()+2, []*types.Layer{l1, l}, 6)
	AddLayer(mdb, l22)
	handleLayerBlockByBlock(l22, alg)

	l4 := createLayer(mesh.GenesisLayer().Index()+4, []*types.Layer{l3, l22, l1, l}, 4)
	AddLayer(mdb, l4)
	handleLayerBlockByBlock(l4, alg)

	l5 := createLayer(mesh.GenesisLayer().Index()+5, []*types.Layer{l4, l3, l22, l1, l}, 4)
	AddLayer(mdb, l5)
	handleLayerBlockByBlock(l5, alg)

	l6 := createLayer(mesh.GenesisLayer().Index()+6, []*types.Layer{l5, l4, l3, l22, l1}, 4)
	AddLayer(mdb, l6)
	handleLayerBlockByBlock(l6, alg)
	//
	l7 := createLayer(mesh.GenesisLayer().Index()+7, []*types.Layer{l6, l5, l4, l3, l22}, 4)
	AddLayer(mdb, l7)
	handleLayerBlockByBlock(l7, alg)

	assert.Equal(t, alg.PBase.Layer(), mesh.GenesisLayer().Index()+6)
}

func createLayer2(index types.LayerID, view *types.Layer, votes []*types.Layer, blocksInLayer int) *types.Layer {
	l := types.NewLayer(index)
	var patterns [][]int
	for _, l := range votes {
		blocks := l.Blocks()
		blocksInPrevLayer := len(blocks)
		patterns = append(patterns, chooseRandomPattern(blocksInPrevLayer, int(math.Min(float64(blocksInPrevLayer), float64(blocksInPrevLayer)))))
	}
	layerBlocks := make([]types.BlockID, 0, blocksInLayer)
	for i := 0; i < blocksInLayer; i++ {
		bl := types.NewExistingBlock(index, []byte(rand.String(8)))
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := votes[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.ID()))
			}
		}
		if view != nil && len(view.Blocks()) > 0 {
			for _, prevBloc := range view.Blocks() {
				bl.AddView(types.BlockID(prevBloc.ID()))
			}
		}

		bl.Initialize()
		l.AddBlock(bl)
	}
	log.Debug("Created mesh.LayerID %d with blocks %d", l.Index(), layerBlocks)
	return l
}

func TestNinjaTortoise_LayerWithNoVotes(t *testing.T) {
	lg := log.New(t.Name(), "", "")

	mdb := getInMemMesh()
	alg := newNinjaTortoise(200, mdb, 5, lg)

	l := createLayer2(0, nil, []*types.Layer{}, 154)
	AddLayer(mdb, l)
	alg.handleIncomingLayer(l)

	l1 := createLayer2(1, l, []*types.Layer{l}, 141)
	AddLayer(mdb, l1)
	alg.handleIncomingLayer(l1)

	l2 := createLayer2(2, l1, []*types.Layer{l1, l}, 129)
	AddLayer(mdb, l2)
	alg.handleIncomingLayer(l2)

	l3 := createLayer2(3, l2, []*types.Layer{l2, l1, l}, 132)
	AddLayer(mdb, l3)
	alg.handleIncomingLayer(l3)

	l4 := createLayer2(4, l3, []*types.Layer{l3, l2, l1, l}, 138)
	AddLayer(mdb, l4)
	alg.handleIncomingLayer(l4)

	l5 := createLayer2(5, l4, []*types.Layer{l4, l3, l2, l1, l}, 158)
	AddLayer(mdb, l5)
	alg.handleIncomingLayer(l5)

	l6 := createLayer2(6, l5, []*types.Layer{l5, l4, l3, l2, l1}, 155)
	AddLayer(mdb, l6)
	alg.handleIncomingLayer(l6)
	//
	l7 := createLayer2(7, l6, []*types.Layer{l6, l5, l4, l3, l2}, 130)
	AddLayer(mdb, l7)
	alg.handleIncomingLayer(l7)

	l8 := createLayer2(8, l7, []*types.Layer{l6, l5, l4, l3}, 150)
	AddLayer(mdb, l8)
	alg.handleIncomingLayer(l8)

	l9 := createLayer2(9, l8, []*types.Layer{l8, l7, l6, l5, l4}, 134)
	AddLayer(mdb, l9)
	alg.handleIncomingLayer(l9)

	l10 := createLayer2(10, l9, []*types.Layer{l9, l8, l7, l6, l5}, 148)
	AddLayer(mdb, l10)
	alg.handleIncomingLayer(l10)

	l11 := createLayer2(11, l10, []*types.Layer{l10, l9, l8, l7, l6}, 147)
	AddLayer(mdb, l11)
	alg.handleIncomingLayer(l11)

	assert.True(t, alg.PBase.Layer() == 7)

	//now l7 one votes to be contextually valid in the eyes of layer 12 good pattern
	l12 := createLayer2(12, l11, []*types.Layer{l11, l10, l9, l8, l7}, 171)
	AddLayer(mdb, l12)
	alg.handleIncomingLayer(l12)

	assert.True(t, alg.PBase.Layer() == 7)

	l13 := createLayer2(13, l12, []*types.Layer{l12, l11, l10, l9, l8}, 126)
	AddLayer(mdb, l13)
	alg.handleIncomingLayer(l13)

	//now l7 has the exact amount of votes to be contextually valid which will make 12 good pattern complete
	l121 := createLayer2(12, l11, []*types.Layer{l11, l10, l9, l8, l7}, 1)
	AddLayer(mdb, l121)
	alg.handleIncomingLayer(l121)

	assert.True(t, alg.PBase.Layer() == 7)

	l14 := createLayer2(14, l13, []*types.Layer{l13, l12, l121, l11, l10, l9}, 148)
	AddLayer(mdb, l14)
	alg.handleIncomingLayer(l14)

	l15 := createLayer2(15, l14, []*types.Layer{l14, l13, l121, l12, l11, l10}, 150)
	AddLayer(mdb, l15)
	alg.handleIncomingLayer(l15)

	assert.True(t, alg.PBase.Layer() == 7)

	l16 := createLayer2(16, l15, []*types.Layer{l15, l14, l121, l13, l12, l11}, 121)
	AddLayer(mdb, l16)
	alg.handleIncomingLayer(l16)
	assert.True(t, alg.PBase.Layer() == 15)
}

func TestNinjaTortoise_LayerWithNoVotes2(t *testing.T) {
	lg := log.New(t.Name(), "", "")

	mdb := getInMemMesh()
	alg := newNinjaTortoise(200, mdb, 5, lg)

	l := createLayer2(0, nil, []*types.Layer{}, 154)
	AddLayer(mdb, l)
	alg.handleIncomingLayer(l)

	l1 := createLayer2(1, l, []*types.Layer{l}, 141)
	AddLayer(mdb, l1)
	alg.handleIncomingLayer(l1)

	l2 := createLayer2(2, l1, []*types.Layer{l1, l}, 129)
	AddLayer(mdb, l2)
	alg.handleIncomingLayer(l2)

	l3 := createLayer2(3, l2, []*types.Layer{l2, l1, l}, 132)
	AddLayer(mdb, l3)
	alg.handleIncomingLayer(l3)

	l4 := createLayer2(4, l3, []*types.Layer{l3, l2, l1, l}, 138)
	AddLayer(mdb, l4)
	alg.handleIncomingLayer(l4)

	l5 := createLayer2(5, l4, []*types.Layer{l4, l3, l2, l1, l}, 158)
	AddLayer(mdb, l5)
	alg.handleIncomingLayer(l5)

	l6 := createLayer2(6, l5, []*types.Layer{l5, l4, l3, l2, l1}, 155)
	AddLayer(mdb, l6)
	alg.handleIncomingLayer(l6)
	//
	l7 := createLayer2(7, l6, []*types.Layer{l6, l5, l4, l3, l2}, 130)
	AddLayer(mdb, l7)
	alg.handleIncomingLayer(l7)

	l8 := createLayer2(8, l7, []*types.Layer{l6, l5, l4, l3}, 150)
	AddLayer(mdb, l8)
	alg.handleIncomingLayer(l8)

	l9 := createLayer2(9, l8, []*types.Layer{l8, l7, l6, l5, l4}, 134)
	AddLayer(mdb, l9)
	alg.handleIncomingLayer(l9)

	l10 := createLayer2(10, l9, []*types.Layer{l9, l8, l7, l6, l5}, 148)
	AddLayer(mdb, l10)
	alg.handleIncomingLayer(l10)

	l11 := createLayer2(11, l10, []*types.Layer{l10, l9, l8, l7, l6}, 147)
	AddLayer(mdb, l11)
	alg.handleIncomingLayer(l11)
	assert.True(t, alg.PBase.Layer() == 7)

	// l7 is missing exactly 1 vote to be contextually valid which will make 12 complete
	l12 := createLayer2(12, l11, []*types.Layer{l11, l10, l9, l8, l7}, 171)
	AddLayer(mdb, l12)
	alg.handleIncomingLayer(l12)

	l13 := createLayer2(13, l12, []*types.Layer{l12, l11, l10, l9, l8}, 126)
	AddLayer(mdb, l13)
	alg.handleIncomingLayer(l13)

	l14 := createLayer2(14, l13, []*types.Layer{l13, l12, l11, l10, l9}, 148)
	AddLayer(mdb, l14)
	alg.handleIncomingLayer(l14)

	l15 := createLayer2(15, l14, []*types.Layer{l14, l13, l12, l11, l10}, 150)
	AddLayer(mdb, l15)
	alg.handleIncomingLayer(l15)

	l16 := createLayer2(16, l15, []*types.Layer{l15, l14, l13, l12, l11}, 121)
	AddLayer(mdb, l16)
	alg.handleIncomingLayer(l16)
	assert.True(t, alg.PBase.Layer() == 7)
}

func TestNinjaTortoise_OneMoreLayerWithNoVotes(t *testing.T) {
	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := newNinjaTortoise(200, mdb, 5, lg)

	l := createLayer2(0, nil, []*types.Layer{}, 147)
	AddLayer(mdb, l)
	alg.handleIncomingLayer(l)

	l1 := createLayer2(1, l, []*types.Layer{l}, 150)
	AddLayer(mdb, l1)
	alg.handleIncomingLayer(l1)

	l2 := createLayer2(2, l1, []*types.Layer{l1, l}, 140)
	AddLayer(mdb, l2)
	alg.handleIncomingLayer(l2)

	l3 := createLayer2(3, l2, []*types.Layer{l2, l1, l}, 117)
	AddLayer(mdb, l3)
	alg.handleIncomingLayer(l3)

	l4 := createLayer2(4, l3, []*types.Layer{l3, l2, l1, l}, 142)
	AddLayer(mdb, l4)
	alg.handleIncomingLayer(l4)

	l5 := createLayer2(5, l4, []*types.Layer{l4, l3, l2, l1, l}, 147)
	AddLayer(mdb, l5)
	alg.handleIncomingLayer(l5)

	l6 := createLayer2(6, l5, []*types.Layer{l4, l3, l2, l1}, 128)
	AddLayer(mdb, l6)
	alg.handleIncomingLayer(l6)
	//
	l7 := createLayer2(7, l6, []*types.Layer{l6, l5, l4, l3, l2}, 144)
	AddLayer(mdb, l7)
	alg.handleIncomingLayer(l7)

	l8 := createLayer2(8, l7, []*types.Layer{l7, l6, l5, l4, l3}, 167)
	AddLayer(mdb, l8)
	alg.handleIncomingLayer(l8)

	l9 := createLayer2(9, l8, []*types.Layer{l8, l7, l6, l5, l4}, 128)
	AddLayer(mdb, l9)
	alg.handleIncomingLayer(l9)

	l10 := createLayer2(10, l9, []*types.Layer{l9, l8, l7, l6, l5}, 142)
	AddLayer(mdb, l10)
	alg.handleIncomingLayer(l10)

	l11 := createLayer2(11, l10, []*types.Layer{l10, l9, l8, l7, l6}, 157)
	AddLayer(mdb, l11)
	alg.handleIncomingLayer(l11)

	l12 := createLayer2(12, l11, []*types.Layer{l11, l10, l9, l8, l7}, 138)
	AddLayer(mdb, l12)
	alg.handleIncomingLayer(l12)

	l13 := createLayer2(13, l12, []*types.Layer{l12, l11, l10, l9, l8}, 139)
	AddLayer(mdb, l13)
	alg.handleIncomingLayer(l13)

	l14 := createLayer2(14, l13, []*types.Layer{l13, l12, l11, l10, l9}, 128)
	AddLayer(mdb, l14)
	alg.handleIncomingLayer(l14)

	l15 := createLayer2(15, l14, []*types.Layer{l14, l13, l12, l11, l10}, 134)
	AddLayer(mdb, l15)
	alg.handleIncomingLayer(l15)
}

func TestNinjaTortoise_LateBlocks(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getMeshForBench()
	alg := newNinjaTortoise(10, mdb, 1, lg)

	l01 := types.NewLayer(0)
	l01.AddBlock(types.NewExistingBlock(0, []byte(rand.String(8))))

	l02 := types.NewLayer(0)
	l02.AddBlock(types.NewExistingBlock(0, []byte(rand.String(8))))

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
		bl := types.NewExistingBlock(index, []byte(rand.String(8)))
		layerBlocks = append(layerBlocks, bl.ID())
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(types.BlockID(b.ID()))
			}
		}
		if len(prev) > 0 {
			for _, prevBloc := range prev[0].Blocks() {
				bl.AddView(types.BlockID(prevBloc.ID()))
			}
		}
		bl.Initialize()
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

func TestNinjaTortoise_Increasing_Memory(t *testing.T) {
	t.Skip()
	layers := 1000

	//these where set during benchmarking
	memThreshold := uint64(300)
	handleThreshold := 1000 * time.Millisecond //300ms on mac

	defer persistenceTeardown()
	msh := getPersistentMash()
	layerSize := 100
	lg := log.New(t.Name(), "", "")
	alg := newNinjaTortoise(layerSize, msh, 5, lg.WithOptions(log.Nop))
	l1 := mesh.GenesisLayer()

	AddLayer(msh, l1)
	alg.handleIncomingLayer(l1)

	l := createLayerWithRandVoting(l1.Index()+1, []*types.Layer{l1}, layerSize, 1)
	AddLayer(msh, l)
	alg.handleIncomingLayer(l)
	var allocMb uint64
	type stats struct {
		mem      uint64
		duration time.Duration
	}

	statsMap := map[int]stats{}

	var maxDuration = time.Duration(0)
	var maxDurationLayer = types.LayerID(0)
	var maxMem = uint64(0)
	var maxMemLayer = types.LayerID(0)

	for i := 2; i <= layers; i++ {
		lg.Info("handle layer %v of %v", i, layers)
		lyr := createLayerWithCorruptedPattern(l.Index()+1, l, layerSize, layerSize, 0)
		AddLayer(msh, lyr)
		start := time.Now()
		alg.handleIncomingLayer(lyr)
		duration := time.Since(start)
		l = lyr

		runtime.GC()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		if duration > handleThreshold {
			t.Error("process time performance degradation")
		}
		allocMb = bToMb(m.Alloc)
		if allocMb > memThreshold {
			t.Error("memory performance degradation")
		}

		if duration > maxDuration {
			maxDuration = duration
			maxDurationLayer = lyr.Index()
		}

		if allocMb > maxMem {
			maxMem = allocMb
			maxMemLayer = lyr.Index()
		}

		if i >= 100 && i%100 == 0 {
			statsMap[i] = stats{mem: allocMb, duration: duration}
		}
	}

	lg.Info("-----------------------Stats Report: -----------------------")

	for i := 100; i <= 1000; i += 100 {
		stat := statsMap[i]
		lg.Info("Alloc in layer %v = %v MiB", i, stat.mem)
		lg.Info("%v to process layer", stat.duration)
	}

	lg.Info("max Alloc in layer %v = %v MiB", maxMemLayer, maxMem)
	lg.Info("max time to process %v to process layer %v", maxDuration, maxDurationLayer)

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
	assert.Equal(t, alg.PBase.Layer(), types.LayerID(layers-1))
}

func sanity(t *testing.T, mdb *mesh.DB, layers int, layerSize int, patternSize int, badBlks float64) *ninjaTortoise {
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

	alg := newNinjaTortoise(layerSize, mdb, 5, lg)

	for _, lyr := range lyrs {
		alg.handleIncomingLayer(lyr)
		fmt.Println(fmt.Sprintf("lyr %v tally was %d", lyr.Index()-1, alg.TTally[alg.PBase][blockIDLayerTuple{BlockID: mesh.GenesisBlock().ID(), LayerID: mesh.GenesisBlock().LayerIndex}]))
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
	alg := newNinjaTortoise(3, mdb, 5, log.New(t.Name(), "", ""))
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
	for b, vec := range alg.TTally[alg.PBase] {
		alg.logger.Debug("------> tally for block %d according to complete pattern %d are %d", b, alg.PBase, vec)
	}
	assert.True(t, alg.TTally[alg.PBase][blockIDLayerTuple{BlockID: l.Blocks()[0].ID(), LayerID: l.Blocks()[0].Layer()}] == vec{12, 0}, "lyr %d tally was %d insted of %d", 0, alg.TTally[alg.PBase][blockIDLayerTuple{BlockID: l.Blocks()[0].ID(), LayerID: l.Blocks()[0].Layer()}], vec{12, 0})
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
		bl := addPattern(types.NewExistingBlock(index, []byte(rand.String(8))), goodPattern, prev)
		bl.Initialize()
		layerBlocks = append(layerBlocks, bl.ID())
		l.AddBlock(bl)
	}
	for i := 0; i < blocksInLayer-gbs; i++ {
		bl := addPattern(types.NewExistingBlock(index, []byte(rand.String(8))), badPattern, prev)
		bl.Initialize()
		layerBlocks = append(layerBlocks, bl.ID())
		l.AddBlock(bl)
	}

	log.Debug("Created layer ID %d with blocks %s", l.Index(), layerBlocks)
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
		bl := types.NewExistingBlock(index, []byte(rand.String(8)))
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
		bl.Initialize()
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

func AddLayer(m *mesh.DB, layer *types.Layer) error {
	//add blocks to mDB
	for _, bl := range layer.Blocks() {
		if err := m.AddBlock(bl); err != nil {
			return err
		}
	}
	return nil
}

func TestNinjaTortoise_Recovery(t *testing.T) {

	lg := log.New(t.Name(), "", "")

	mdb := getPersistentMash()
	alg := NewTortoise(3, mdb, 5, lg)
	l := mesh.GenesisLayer()
	AddLayer(mdb, l)

	alg.HandleIncomingLayer(l)
	alg.Persist()

	l1 := createLayer(1, []*types.Layer{l}, 3)
	AddLayer(mdb, l1)

	alg.HandleIncomingLayer(l1)
	alg.Persist()

	l2 := createLayer(2, []*types.Layer{l1, l}, 3)
	AddLayer(mdb, l2)
	alg.HandleIncomingLayer(l2)
	alg.Persist()

	l31 := createLayer(3, []*types.Layer{l1, l}, 4)
	l32 := createLayer(3, []*types.Layer{l31}, 5)

	defer func() {
		if r := recover(); r != nil {
			t.Log("Recovered from", r)
		}
		alg := NewRecoveredTortoise(mdb, lg)

		alg.HandleIncomingLayer(l2)

		l3 := createLayer(3, []*types.Layer{l2, l}, 3)
		AddLayer(mdb, l3)
		alg.HandleIncomingLayer(l3)
		alg.Persist()

		l4 := createLayer(4, []*types.Layer{l3, l2}, 3)
		AddLayer(mdb, l4)
		alg.HandleIncomingLayer(l4)
		alg.Persist()

		assert.True(t, alg.LatestComplete() == 3)
		return
	}()

	alg.HandleIncomingLayer(l32) //crash
}
