package consensus

import (
	"github.com/spacemeshos/go-spacemesh/crypto"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/stretchr/testify/assert"
	"math"
	"math/rand"
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
	v = v.Multiplay(5)
	assert.True(t, v == vec{5, 0}, "vec was wrong %d", v)
	v2 := vec{2, 1}
	v2 = v2.Multiplay(5)
	assert.True(t, v2 == vec{10, 5}, "vec was wrong %d", v2)
}

func TestNinjaTortoise_GlobalOpinion(t *testing.T) {
	glo, _ := globalOpinion(vec{2, 0}, 2, 1)
	assert.True(t, glo == Support, "vec was wrong %d", glo)
	glo, _ = globalOpinion(vec{1, 0}, 2, 1)
	assert.True(t, glo == Abstain, "vec was wrong %d", glo)
	glo, _ = globalOpinion(vec{0, 2}, 2, 1)
	assert.True(t, glo == Against, "vec was wrong %d", glo)
}

func TestForEachInView(t *testing.T) {
	blocks := make(map[mesh.BlockID]*mesh.Block)
	alg := NewNinjaTortoise(2)
	l := createGenesisLayer()
	for _, b := range l.Blocks() {
		blocks[b.ID()] = b
	}
	for i := 0; i < 3; i++ {
		lyr := createMulExplicitLayer(l.Index()+1, []*mesh.Layer{l}, 2, 2)
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
		log.Debug("process block %d", nb.ID())
		mp[nb.ID()] = struct{}{}
	}

	ids := make([]mesh.BlockID, 0, 2)
	for _, b := range l.Blocks() {
		ids = append(ids, b.ID())
	}

	forBlockInView(ids, blocks, 0, foo)

	for _, bl := range blocks {
		_, found := mp[bl.ID()]
		assert.True(t, found, "did not process block  ", bl)
	}

}

func TestNinjaTortoise_UpdatePatternTally(t *testing.T) {
}

func NewNinjaTortoise(layerSize uint32) *ninjaTortoise {
	return &ninjaTortoise{
		Log:                log.New("optimized tortoise ", "", ""),
		LayerSize:          layerSize,
		pBase:              votingPattern{},
		BlockVoteMap:       map[mesh.BlockID]map[mesh.LayerID]*votingPattern{},
		blocks:             map[mesh.BlockID]*mesh.Block{},
		tEffective:         map[mesh.BlockID]*votingPattern{},
		tCorrect:           map[mesh.BlockID]map[votingPattern]vec{},
		layerBlocks:        map[mesh.LayerID][]mesh.BlockID{},
		tExplicit:          map[mesh.BlockID]map[mesh.LayerID]*votingPattern{},
		tGood:              map[mesh.LayerID]votingPattern{},
		tSupport:           map[votingPattern]int{},
		tPattern:           map[votingPattern][]mesh.BlockID{},
		tVote:              map[votingPattern]map[mesh.BlockID]vec{},
		tTally:             map[votingPattern]map[mesh.BlockID]vec{},
		tComplete:          map[votingPattern]struct{}{},
		tEffectiveToBlocks: map[votingPattern][]mesh.BlockID{},
		tPatSupport:        map[votingPattern]map[mesh.LayerID]*votingPattern{},
	}
}

//vote explicitly only for previous layer
//correction vectors have no affect here
func TestNinjaTortoise_Sanity1(t *testing.T) {
	layerSize := 2
	patternSize := layerSize
	alg := NewNinjaTortoise(2)
	l1 := createGenesisLayer()
	genesisId := l1.Blocks()[0].ID()
	//alg.initGenesis(l1.Blocks(), Genesis)
	l := createMulExplicitLayer(l1.Index()+1, []*mesh.Layer{l1}, 2, 1)
	//alg.initGenPlus1(l.Blocks(), Genesis+1)
	alg.init(l1, l)
	for i := 0; i < 30; i++ {
		lyr := createMulExplicitLayer(l.Index()+1, []*mesh.Layer{l}, 2, 2)
		start := time.Now()
		alg.UpdateTables(lyr.Blocks(), lyr.Index())
		alg.Info("Time to process layer: %v ", time.Since(start))
		l = lyr
		for b, vec := range alg.tTally[alg.pBase] {
			alg.Debug("------> tally for block %d according to complete pattern %d are %d", b, alg.pBase, vec)
		}
		assert.True(t, alg.tTally[alg.pBase][genesisId] == vec{patternSize + patternSize*i, 0},
			"lyr %d tally was %d insted of %d", lyr.Index(), alg.tTally[alg.pBase][genesisId], vec{patternSize + patternSize*i, 0})
	}
}

//vote explicitly for two previous layers
//correction vectors compensate for double count
func TestNinjaTortoise_Sanity2(t *testing.T) {
	layerSize := 200
	patternSize := layerSize - 1
	alg := NewNinjaTortoise(uint32(layerSize))
	l := createGenesisLayer()
	genesisId := l.Blocks()[0].ID()
	lPrev := l
	l = createMulExplicitLayer(l.Index()+1, []*mesh.Layer{l}, layerSize, 1)
	alg.init(lPrev, l)
	for i := 0; i < 30; i++ {
		lyr := createMulExplicitLayer(l.Index()+1, []*mesh.Layer{l, lPrev}, layerSize, layerSize-1)
		start := time.Now()
		alg.UpdateTables(lyr.Blocks(), lyr.Index())
		alg.Info("Time to process layer: %v ", time.Since(start))
		lPrev = l
		l = lyr
		for b, vec := range alg.tTally[alg.pBase] {
			alg.Debug("------> tally for block %d according to complete pattern %d are %d", b, alg.pBase, vec)
		}
		assert.True(t, alg.tTally[alg.pBase][genesisId] == vec{patternSize + patternSize*i, 0},
			"lyr %d tally was %d insted of %d", lyr.Index(), alg.tTally[alg.pBase][genesisId], vec{patternSize + patternSize*i, 0})
	}
}

func createMulExplicitLayer(index mesh.LayerID, prev []*mesh.Layer, blocksInLayer int, patternSize int) *mesh.Layer {
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

	for i := 0; i < blocksInLayer; i++ {
		bl := mesh.NewBlock(coin, data, ts, 1)
		for idx, pat := range patterns {
			for _, id := range pat {
				b := prev[idx].Blocks()[id]
				bl.AddVote(mesh.BlockID(b.Id))
			}
		}
		for _, prevBloc := range prev[len(prev)-1].Blocks() {
			bl.AddView(mesh.BlockID(prevBloc.Id))
		}
		l.AddBlock(bl)
	}
	log.Info("Created layer Id %v", l.Index())
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
