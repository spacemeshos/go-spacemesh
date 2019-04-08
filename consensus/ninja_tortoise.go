package consensus

import (
	"container/list"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/block"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"hash/fnv"
	"math"
	"sort"
)

type vec [2]int
type PatternId uint32

const ( //Threshold
	K               = 5 //number of explicit layers to vote for
	Window          = 100
	LocalThreshold  = 0.8 //ThetaL
	GlobalThreshold = 0.6 //ThetaG
	Genesis         = 0
)

var ( //correction vectors type
	//Opinion
	Support = vec{1, 0}
	Against = vec{0, 1}
	Abstain = vec{0, 0}
)

func Max(i block.LayerID, j block.LayerID) block.LayerID {
	if i > j {
		return i
	}
	return j
}

func (a vec) Add(v vec) vec {
	return vec{a[0] + v[0], a[1] + v[1]}
}

func (a vec) Negate() vec {
	a[0] = a[0] * -1
	a[1] = a[1] * -1
	return a
}

func (a vec) Multiply(x int) vec {
	a[0] = a[0] * x
	a[1] = a[1] * x
	return a
}

type votingPattern struct {
	id PatternId //cant put a slice here wont work well with maps, we need to hash the blockids
	block.LayerID
}

func (vp votingPattern) Layer() block.LayerID {
	return vp.LayerID
}

type BlockCache interface {
	GetBlock(id block.BlockID) (*block.Block, error)
	LayerBlockIds(id block.LayerID) ([]block.BlockID, error)
	ForBlockInView(view map[block.BlockID]struct{}, layer block.LayerID, blockHandler func(block *block.BlockHeader) error, errHandler func(err error)) error
}

//todo memory optimizations
type ninjaTortoise struct {
	log.Log
	BlockCache         //block cache
	avgLayerSize       uint64
	pBase              votingPattern
	tEffective         map[block.BlockID]votingPattern                   //Explicit voting pattern of latest layer for a block
	tCorrect           map[block.BlockID]map[block.BlockID]vec            //correction vectors
	tExplicit          map[block.BlockID]map[block.LayerID]votingPattern  //explict votes from block to layer pattern
	tGood              map[block.LayerID]votingPattern                   //good pattern for layer i
	tSupport           map[votingPattern]int                            //for pattern p the number of blocks that support p
	tComplete          map[votingPattern]struct{}                       //complete voting patterns
	tEffectiveToBlocks map[votingPattern][]block.BlockID                 //inverse blocks effective pattern
	tVote              map[votingPattern]map[block.BlockID]vec           //global opinion
	tTally             map[votingPattern]map[block.BlockID]vec           //for pattern p and block b count votes for b according to p
	tPattern           map[votingPattern]map[block.BlockID]struct{}      //set of blocks that comprise pattern p
	tPatSupport        map[votingPattern]map[block.LayerID]votingPattern //pattern support count
}

func NewNinjaTortoise(layerSize int, blocks BlockCache, log log.Log) *ninjaTortoise {
	return &ninjaTortoise{
		Log:                log,
		BlockCache:         blocks,
		avgLayerSize:       uint64(layerSize),
		pBase:              votingPattern{},
		tEffective:         map[block.BlockID]votingPattern{},
		tCorrect:           map[block.BlockID]map[block.BlockID]vec{},
		tExplicit:          map[block.BlockID]map[block.LayerID]votingPattern{},
		tGood:              map[block.LayerID]votingPattern{},
		tSupport:           map[votingPattern]int{},
		tPattern:           map[votingPattern]map[block.BlockID]struct{}{},
		tVote:              map[votingPattern]map[block.BlockID]vec{},
		tTally:             map[votingPattern]map[block.BlockID]vec{},
		tComplete:          map[votingPattern]struct{}{},
		tEffectiveToBlocks: map[votingPattern][]block.BlockID{},
		tPatSupport:        map[votingPattern]map[block.LayerID]votingPattern{},
	}
}

func (ni *ninjaTortoise) processBlock(b *block.Block) {

	ni.Debug("process block: %d layer: %d  ", b.Id, b.Layer())

	if b.Layer() == Genesis {
		return
	}

	patternMap := make(map[block.LayerID]map[block.BlockID]struct{})
	for _, bid := range b.BlockVotes {
		ni.Debug("block votes %d", bid)
		bl, err := ni.GetBlock(bid)
		if err != nil || bl == nil {
			ni.Error(fmt.Sprintf("error block not found ID %d !!!!!", bid))
			return
		}
		if _, found := patternMap[bl.Layer()]; !found {
			patternMap[bl.Layer()] = map[block.BlockID]struct{}{}
		}
		patternMap[bl.Layer()][bl.ID()] = struct{}{}
	}

	var effective votingPattern
	ni.tExplicit[b.ID()] = make(map[block.LayerID]votingPattern, K)
	for layerId, v := range patternMap {
		vp := votingPattern{id: getIdsFromSet(v), LayerID: layerId}
		ni.tPattern[vp] = v
		ni.tExplicit[b.ID()][layerId] = vp
		if layerId >= effective.Layer() {
			effective = vp
		}
	}

	ni.tEffective[b.ID()] = effective

	v, found := ni.tEffectiveToBlocks[effective]
	if !found {
		v = make([]block.BlockID, 0, ni.avgLayerSize)
	}
	var pattern []block.BlockID = nil
	pattern = append(v, b.ID())
	ni.tEffectiveToBlocks[effective] = pattern
	ni.Debug("effective pattern to blocks %d %d", effective, pattern)

	return
}

func getId(bids []block.BlockID) PatternId {
	sort.Slice(bids, func(i, j int) bool { return bids[i] < bids[j] })
	// calc
	h := fnv.New32()
	for i := 0; i < len(bids); i++ {
		h.Write(common.Uint32ToBytes(uint32(bids[i])))
	}
	// update
	sum := h.Sum32()
	return PatternId(sum)
}

func getIdsFromSet(bids map[block.BlockID]struct{}) PatternId {
	keys := make([]block.BlockID, 0, len(bids))
	for k := range bids {
		keys = append(keys, k)
	}
	return getId(keys)
}

func forBlockInView(blocks map[block.BlockID]struct{}, blockCache map[block.BlockID]*block.Block, layer block.LayerID, foo func(block *block.Block)) {
	stack := list.New()
	for b := range blocks {
		stack.PushFront(b)
	}
	set := make(map[block.BlockID]struct{})
	for b := stack.Front(); b != nil; b = stack.Front() {
		a := stack.Remove(stack.Front()).(block.BlockID)
		block, found := blockCache[a]
		if !found {
			panic(fmt.Sprintf("error block not found ID %d", block.ID()))
		}
		foo(block)
		//push children to bfs queue
		for _, bChild := range block.ViewEdges {
			if blockCache[bChild].Layer() >= layer { //dont traverse too deep
				if _, found := set[bChild]; !found {
					set[bChild] = struct{}{}
					stack.PushBack(bChild)
				}
			}
		}
	}
	return
}

func globalOpinion(v vec, layerSize uint64, delta float64) vec {
	threshold := float64(GlobalThreshold*delta) * float64(layerSize)
	if float64(v[0]) > threshold {
		return Support
	} else if float64(v[1]) > threshold {
		return Against
	} else {
		return Abstain
	}
}

func (ni *ninjaTortoise) updateCorrectionVectors(p votingPattern, bottomOfWindow block.LayerID) {
	traversalFunc := func(blk *block.BlockHeader) error {
		for _, bid := range ni.tEffectiveToBlocks[p] { //for all b who's effective vote is p
			b, err := ni.GetBlock(bid)
			if err != nil {
				panic(fmt.Sprintf("error block not found ID %d", bid))
			}

			if _, found := ni.tExplicit[b.Id][blk.Layer()]; found { //if Texplicit[b][blk]!=0 check correctness of blk.layer and found
				ni.Debug(" blocks pattern %d block %d layer %d", p, b.ID(), b.Layer())
				if _, found := ni.tCorrect[b.Id]; !found {
					ni.tCorrect[b.Id] = make(map[block.BlockID]vec)
				}
				vo := ni.tVote[p][blk.ID()]
				ni.Debug("vote from pattern %d to block %d layer %d vote %d ", p, blk.ID(), blk.Layer(), vo)
				ni.tCorrect[b.Id][blk.ID()] = vo.Negate() //Tcorrect[b][blk] = -Tvote[p][blk]
				ni.Debug("update correction vector for block %d layer %d , pattern %d vote %d for block %d ", b.ID(), b.Layer(), p, ni.tCorrect[b.Id][blk.ID()], blk.ID())
			} else {
				ni.Debug("block %d from layer %d dose'nt explicitly vote for layer %d", b.ID(), b.Layer(), blk.Layer())
			}
		}
		return nil
	}

	ni.ForBlockInView(ni.tPattern[p], bottomOfWindow, traversalFunc, func(err error) {})
}

func (ni *ninjaTortoise) updatePatternTally(newMinGood votingPattern, botomOfWindow block.LayerID, correctionMap map[block.BlockID]vec, effCountMap map[block.LayerID]int) {
	ni.Debug("update tally pbase id:%d layer:%d p id:%d layer:%d", ni.pBase.id, ni.pBase.Layer(), newMinGood.id, newMinGood.Layer())
	for idx, effc := range effCountMap {
		g := ni.tGood[idx]
		for b, v := range ni.tVote[g] {
			tally := ni.tTally[newMinGood][b]
			tally = tally.Add(v.Multiply(effc))
			if count, found := correctionMap[b]; found {
				tally = tally.Add(count)
			} else {
				ni.Debug("no correction vectors for %", g)
			}
			ni.Debug("tally for pattern %d  and block %d is %d", newMinGood.id, b, tally)
			ni.tTally[newMinGood][b] = tally //in g's view -> in p's view
		}
	}
}

func (ni *ninjaTortoise) getCorrEffCounter() (map[block.BlockID]vec, map[block.LayerID]int, func(b *block.BlockHeader)) {
	correctionMap := make(map[block.BlockID]vec)
	effCountMap := make(map[block.LayerID]int)
	foo := func(b *block.BlockHeader) {
		if b.Layer() > ni.pBase.Layer() { //because we already copied pbase's votes
			if eff, found := ni.tEffective[b.ID()]; found {
				if p, found := ni.tGood[eff.Layer()]; found && eff == p {
					effCountMap[eff.Layer()] = effCountMap[eff.Layer()] + 1
					for k, v := range ni.tCorrect[b.ID()] {
						correctionMap[k] = correctionMap[k].Add(v)
					}
				}
			}
		}
	}
	return correctionMap, effCountMap, foo
}

//for all layers from pBase to i add b's votes, mark good layers
// return new minimal good layer
func (ni *ninjaTortoise) findMinimalNewlyGoodLayer(lyr *block.Layer) block.LayerID {
	minGood := block.LayerID(math.MaxUint64)

	var j block.LayerID
	if Window > lyr.Index() {
		j = ni.pBase.Layer() + 1
	} else {
		j = Max(ni.pBase.Layer()+1, lyr.Index()-Window+1)
	}

	for ; j < lyr.Index(); j++ {
		// update block votes on all patterns in blocks view
		sUpdated := ni.updateBlocksSupport(lyr.Blocks(), j)
		//todo do this as part of previous for if possible
		//for each p that was updated and not the good layer of j check if it is the good layer
		for p := range sUpdated {
			//if a majority supports p (p is good)
			//according to tal we dont have to know the exact amount, we can multiply layer size by number of layers
			jGood, found := ni.tGood[j]
			threshold := 0.5 * float64(block.LayerID(ni.avgLayerSize)*(lyr.Index()-p.Layer()))

			if (jGood != p || !found) && float64(ni.tSupport[p]) > threshold {
				ni.tGood[p.Layer()] = p
				//if p is the new minimal good layer
				if p.Layer() < minGood {
					minGood = p.Layer()
				}
			}
		}
	}
	ni.Debug("found minimal good layer %d", minGood)
	return minGood
}

//update block support for pattern in layer j
func (ni *ninjaTortoise) updateBlocksSupport(b []*block.Block, j block.LayerID) map[votingPattern]struct{} {
	sUpdated := map[votingPattern]struct{}{}
	for _, block := range b {
		//check if block votes for layer j explicitly or implicitly
		p, found := ni.tExplicit[block.ID()][j]
		if found {
			//explicit
			ni.tSupport[p]++         //add to supporting patterns
			sUpdated[p] = struct{}{} //add to updated patterns

			//implicit
		} else if eff, effFound := ni.tEffective[block.ID()]; effFound {
			p, found = ni.tPatSupport[eff][j]
			if found {
				ni.tSupport[p]++         //add to supporting patterns
				sUpdated[p] = struct{}{} //add to updated patterns
			}
		}
	}
	return sUpdated
}

func (ni *ninjaTortoise) addPatternVote(p votingPattern, view map[block.BlockID]struct{}) func(b block.BlockID) {
	addPatternVote := func(b block.BlockID) {
		var vp map[block.LayerID]votingPattern
		var found bool
		bl, err := ni.GetBlock(b)
		if err != nil {
			panic(fmt.Sprintf("error block not found ID %d", b))
		}
		if bl.Layer() <= ni.pBase.Layer() {
			return
		}

		if vp, found = ni.tExplicit[b]; !found {
			panic(fmt.Sprintf("block %d has no explicit voting, something went wrong ", b))
		}
		for _, ex := range vp {
			blocks, err := ni.LayerBlockIds(ex.Layer()) //todo handle error
			if err != nil {
				panic("could not retrieve layer block ids")
			}
			for _, bl := range blocks {
				if _, found := ni.tPattern[ex][bl]; found {
					ni.tTally[p][bl] = ni.tTally[p][bl].Add(Support)
				} else if _, inSet := view[bl]; inSet { //in view but not in pattern
					ni.tTally[p][bl] = ni.tTally[p][bl].Add(Against)
				}
			}
		}
	}
	return addPatternVote
}

func sumNodesInView(layerBlockCounter map[block.LayerID]int, layer block.LayerID, pLayer block.LayerID) vec {
	var sum int
	for sum = 0; layer <= pLayer; layer++ {
		sum = sum + layerBlockCounter[layer]
	}
	return Against.Multiply(sum)
}

func (ni *ninjaTortoise) processBlocks(layer *block.Layer) {
	for _, block := range layer.Blocks() {
		ni.processBlock(block)
	}

}

func (ni *ninjaTortoise) handleGenesis(genesis *block.Layer) {
	blkIds := make([]block.BlockID, 0, len(genesis.Blocks()))
	for _, blk := range genesis.Blocks() {
		blkIds = append(blkIds, blk.ID())
	}
	vp := votingPattern{id: getId(blkIds), LayerID: Genesis}
	ni.pBase = vp
	ni.tGood[Genesis] = vp
	ni.tExplicit[genesis.Blocks()[0].ID()] = make(map[block.LayerID]votingPattern, K*ni.avgLayerSize)
}

//todo send map instead of ni
func updatePatSupport(ni *ninjaTortoise, p votingPattern, bids []block.BlockID, idx block.LayerID) {
	if val, found := ni.tPatSupport[p]; !found || val == nil {
		ni.tPatSupport[p] = make(map[block.LayerID]votingPattern)
	}
	pid := getId(bids)
	ni.Debug("update support for %d layer %d supported pattern %d", p, idx, pid)
	ni.tPatSupport[p][idx] = votingPattern{id: pid, LayerID: idx}
}

func initTallyToBase(tally map[votingPattern]map[block.BlockID]vec, base votingPattern, p votingPattern) {
	if _, found := tally[p]; !found {
		tally[p] = make(map[block.BlockID]vec)
	}
	for k, v := range tally[base] {
		tally[p][k] = v
	}
}

func (ni *ninjaTortoise) latestComplete() block.LayerID {
	return ni.pBase.Layer()
}

func (ni *ninjaTortoise) getVotes() map[block.BlockID]vec {
	return ni.tVote[ni.pBase]
}

func (ni *ninjaTortoise) getVote(id block.BlockID) vec {
	block, err := ni.GetBlock(id)
	if err != nil {
		panic(fmt.Sprintf("error block not found ID %d", id))
	}

	if block.Layer() > ni.pBase.Layer() {
		ni.Error("we dont have an opinion on block according to current pbase")
		return Against
	}

	return ni.tVote[ni.pBase][id]
}

func (ni *ninjaTortoise) handleIncomingLayer(newlyr *block.Layer) { //i most recent layer
	ni.Info("update tables layer %d with %d blocks", newlyr.Index(), len(newlyr.Blocks()))

	ni.processBlocks(newlyr)

	if newlyr.Index() == Genesis {
		ni.handleGenesis(newlyr)
		return
	}

	l := ni.findMinimalNewlyGoodLayer(newlyr)

	//from minimal newly good pattern to current layer
	//update pattern tally for all good layers
	for j := l; j > 0 && j < newlyr.Index(); j++ {
		if p, gfound := ni.tGood[j]; gfound {
			//init p's tally to pBase tally
			initTallyToBase(ni.tTally, ni.pBase, p)

			//find bottom of window
			var windowStart block.LayerID
			if Window > newlyr.Index() {
				windowStart = 0
			} else {
				windowStart = newlyr.Index() - Window + 1
			}

			view := make(map[block.BlockID]struct{})
			lCntr := make(map[block.LayerID]int)
			correctionMap, effCountMap, getCrrEffCnt := ni.getCorrEffCounter()
			traversalFunc := func(block *block.BlockHeader) error {
				view[block.ID()] = struct{}{} //all blocks in view
				for _, id := range block.BlockVotes {
					view[id] = struct{}{}
				}
				lCntr[block.Layer()]++ //amount of blocks for each layer in view
				getCrrEffCnt(block)    //calc correction and eff count
				return nil
			}

			ni.ForBlockInView(ni.tPattern[p], ni.pBase.Layer()+1, traversalFunc, func(err error) {})

			//add corrected implicit votes
			ni.updatePatternTally(p, windowStart, correctionMap, effCountMap)

			//add explicit votes
			addPtrnVt := ni.addPatternVote(p, view)
			for bl := range view {
				addPtrnVt(bl)
			}

			complete := true
			for idx := windowStart; idx < j; idx++ {
				layer, _ := ni.LayerBlockIds(idx) //todo handle error
				bids := make([]block.BlockID, 0, ni.avgLayerSize)
				for _, bid := range layer {
					//if bid is not in p's view.
					//add negative vote multiplied by the amount of blocks in the view
					//explicit votes against (not in view )
					if _, found := view[bid]; idx >= ni.pBase.Layer() && !found {
						ni.tTally[p][bid] = sumNodesInView(lCntr, idx+1, p.Layer())
					}

					if val, found := ni.tVote[p]; !found || val == nil {
						ni.tVote[p] = make(map[block.BlockID]vec)
					}

					if vote := globalOpinion(ni.tTally[p][bid], ni.avgLayerSize, float64(p.LayerID-idx)); vote != Abstain {
						ni.tVote[p][bid] = vote
						if vote == Support {
							bids = append(bids, bid)
						}
					} else {
						ni.tVote[p][bid] = vote
						complete = false //not complete
					}
				}
				updatePatSupport(ni, p, bids, idx)
			}

			//update correction vectors after vote count
			ni.updateCorrectionVectors(p, windowStart)

			// update completeness of p
			if _, found := ni.tComplete[p]; complete && !found {
				ni.tComplete[p] = struct{}{}
				ni.pBase = p
				ni.Debug("found new complete and good pattern for layer %d pattern %d with %d support ", l, p.id, ni.tSupport[p])
			}
		}
	}
	ni.Info("finished layer %d pbase is %d", newlyr.Index(), ni.pBase.Layer())
	return
}
