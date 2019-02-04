package consensus

import (
	"container/list"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"hash/fnv"
	"math"
	"sort"
)

type vec [2]int

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

func (a vec) Add(v vec) vec {
	return vec{a[0] + v[0], a[1] + v[1]}
}

func (a vec) Negate() vec {
	a[0] = a[0] * -1
	a[1] = a[1] * -1
	return a
}

func (a vec) Multiplay(x int) vec {
	a[0] = a[0] * x
	a[1] = a[1] * x
	return a
}

type ninjaBlock struct {
	mesh.Block
	BlockVoteMap map[mesh.LayerID]*votingPattern //Explicit voting , Implicit votes is derived from the view of the latest Explicit voting pattern
}

type votingPattern struct {
	id uint32 //cant put a slice here wont work well with maps, we need to hash the blockids
	mesh.LayerID
}

type correction struct {
	effectiveCount int
	vector         vec
}

func (c *correction) add(v vec) {
	c.vector = c.vector.Add(v)
	c.effectiveCount++
}

func (vp votingPattern) Layer() mesh.LayerID {
	return vp.LayerID
}

//todo memory optimizations
type ninjaTortoise struct {
	log.Log
	LayerSize  uint32
	pBase      votingPattern
	blocks     map[mesh.BlockID]*ninjaBlock
	tEffective map[mesh.BlockID]*votingPattern        //Explicit voting pattern of latest layer for a block
	tCorrect   map[mesh.BlockID]map[votingPattern]vec //correction vectors

	layerBlocks map[mesh.LayerID][]mesh.BlockID
	tExplicit   map[mesh.BlockID]map[mesh.LayerID]*votingPattern // explict votes from block to layer pattern
	tGood       map[mesh.LayerID]votingPattern                   // good pattern for layer i

	tSupport           map[votingPattern]int                  //for pattern p the number of blocks that support p
	tPattern           map[votingPattern][]mesh.BlockID       // set of blocks that comprise pattern p
	tVote              map[votingPattern]map[mesh.BlockID]vec // global opinion
	tTally             map[votingPattern]map[mesh.BlockID]vec //for pattern p and block b count votes for b according to p
	tComplete          map[votingPattern]struct{}
	tEffectiveToBlocks map[votingPattern][]mesh.BlockID
	tPatSupport        map[votingPattern]map[mesh.LayerID]*votingPattern
}

func (ni *ninjaTortoise) processBlock(b *mesh.Block) *ninjaBlock {

	ni.Debug("process block: %d layer: %d  ", b.Id, b.Layer())

	patterns := make(map[mesh.LayerID][]mesh.BlockID)
	for _, bid := range b.BlockVotes {
		ni.Debug("block votes %d", bid)
		bl, found := ni.blocks[bid]
		if !found {
			panic("unknown block!, something went wrong ")
		}
		if _, found := patterns[bl.Layer()]; !found {
			patterns[bl.Layer()] = make([]mesh.BlockID, 0, ni.LayerSize)
		}
		patterns[bl.Layer()] = append(patterns[bl.Layer()], bl.ID())
	}

	nb := &ninjaBlock{Block: *b}

	if b.Layer() == Genesis {
		return nb
	}

	var effective *votingPattern
	nb.BlockVoteMap = make(map[mesh.LayerID]*votingPattern, K)
	for layerId, v := range patterns {
		vp := votingPattern{id: getId(v), LayerID: layerId}
		ni.tPattern[vp] = v
		nb.BlockVoteMap[layerId] = &vp
		if effective == nil || layerId >= effective.Layer() {
			effective = &vp
		}
	}

	ni.tEffective[b.ID()] = effective

	v, found := ni.tEffectiveToBlocks[*effective]
	if !found {
		v = make([]mesh.BlockID, 0, ni.LayerSize)
	}
	var pattern []mesh.BlockID = nil
	pattern = append(v, b.ID())
	ni.tEffectiveToBlocks[*effective] = pattern
	ni.Debug("effective pattern to blocks %d %d", *effective, pattern)

	return nb
}

func getId(bids []mesh.BlockID) uint32 {
	sort.Slice(bids, func(i, j int) bool { return bids[i] < bids[j] })
	// calc
	h := fnv.New32()
	for i := 0; i < len(bids); i++ {
		h.Write(common.Uint32ToBytes(uint32(bids[i])))
	}
	// update
	sum := h.Sum32()
	return sum
}

func forBlockInView(blocks []mesh.BlockID, blockCache map[mesh.BlockID]*ninjaBlock, layer mesh.LayerID, foo func(*ninjaBlock)) map[mesh.LayerID]int {
	stack := list.New()
	for _, b := range blocks {
		stack.PushFront(b)
	}
	layerCounter := make(map[mesh.LayerID]int)
	set := make(map[mesh.BlockID]struct{})
	for b := stack.Front(); b != nil; b = stack.Front() {
		a := stack.Remove(stack.Front()).(mesh.BlockID)
		block, found := blockCache[a]
		if !found {
			panic("error block not found ")
		}
		layerCounter[block.Layer()]++
		foo(block)
		//push children to bfs queue
		for _, bChild := range block.ViewEdges {
			if blockCache[bChild].Layer() >= layer { //dont traverse too deep
				if _, found := set[bChild]; !found {
					set[bChild] = struct{}{}
					stack.PushFront(bChild)
				}
			}
		}
	}
	return layerCounter
}

func globalOpinion(v vec, layerSize uint32, delta float64) (vec, error) {
	threshold := float64(GlobalThreshold*delta) * float64(layerSize)
	if float64(v[0]) > threshold {
		return Support, nil
	} else if float64(v[1]) > threshold {
		return Against, nil
	} else {
		return Abstain, nil
	}
}

func (ni *ninjaTortoise) updateCorrectionVectors(p votingPattern) {
	foo := func(x *ninjaBlock) {
		for _, bid := range ni.tEffectiveToBlocks[p] { //for all b who's effective vote is p
			b := ni.blocks[bid]
			if _, found := ni.tExplicit[b.Id][x.Layer()]; found { //if Texplicit[b][x]!=0 check correctness of x.layer and found
				ni.Debug(" blocks pattern %d block %d layer %d", p, b.ID(), b.Layer())
				if _, found := ni.tCorrect[b.Id]; !found {
					ni.tCorrect[b.Id] = make(map[votingPattern]vec)
				}
				var vo vec
				vo = ni.tVote[p][x.ID()]
				ni.Debug("vote from pattern %d to block %d layer %d vote %d ", p, x.ID(), x.Layer(), vo)
				ni.tCorrect[b.Id][p] = ni.tCorrect[b.Id][p].Add(vo.Negate()) //Tcorrect[b][x] = -Tvote[p][x]
				ni.Debug("update correction vector for block %d layer %d , pattern %d vote %d for block %d ", b.ID(), b.Layer(), p, ni.tCorrect[b.Id][p], x.ID())
			} else {
				ni.Debug("block %d from layer %d dose'nt explicitly vote for layer %d", b.ID(), b.Layer(), x.Layer())
			}
		}
	}

	forBlockInView(ni.tPattern[p], ni.blocks, ni.pBase.Layer(), foo)
}

func (ni *ninjaTortoise) updatePatternTally(newMinGood votingPattern, bootomOfWindow mesh.LayerID) {
	ni.Debug("update tally pbase id:%d layer:%d p id:%d layer:%d", ni.pBase.id, ni.pBase.Layer(), newMinGood.id, newMinGood.Layer())
	// bfs this sucker to get all blocks who's effective vote pattern is g and layer id i s.t pBase<i<p
	//init p's tally to pBase tally
	stack := list.New()
	//include p
	for _, b := range ni.tPattern[newMinGood] {
		stack.PushBack(ni.blocks[b])
	}

	m := make(map[votingPattern]*correction)

	foo := func(b *ninjaBlock) {
		if eff, found := ni.tEffective[b.ID()]; found {
			if g, found := ni.tGood[eff.Layer()]; found && *eff == g {
				if _, f := m[g]; !f {
					m[g] = &correction{}
				}
				m[g].add(ni.tCorrect[b.ID()][g])
			}
		}
	}

	forBlockInView(ni.tPattern[newMinGood], ni.blocks, ni.pBase.Layer()+1, foo) //ni.pBase.LayerID + 1 because we already copied pbase's votes

	for idx := bootomOfWindow; idx < newMinGood.Layer(); idx++ {
		g := ni.tGood[idx]
		if corr, found := m[g]; found {
			for b, v := range ni.tVote[g] {
				tally := ni.tTally[newMinGood][b]
				ni.Debug("correction vectors for g %d vector %d effective %d tally %d", g, corr.vector, corr.effectiveCount, tally)
				tally = tally.Add(v.Multiplay(corr.effectiveCount).Add(corr.vector))
				ni.Debug("tally for pattern %d  and block %d is %d", newMinGood.id, b, tally)
				ni.tTally[newMinGood][b] = tally //in g's view -> in p's view
			}
		} else {
			ni.Debug("no correction vectors for %", g)
		}
	}
}

//for all layers from pBase to i add b's votes, mark good layers
// return new minimal good layer
func (ni *ninjaTortoise) findMinimalGoodLayer(i mesh.LayerID, b []*ninjaBlock) mesh.LayerID {
	var minGood mesh.LayerID
	for j := mesh.LayerID(math.Max(float64(ni.pBase.Layer()+1), float64(i)-Window)); j < i || j == 0; j++ {
		// update block votes on all patterns in blocks view
		sUpdated := ni.updateBlocksSupport(b, j)
		//todo do this as part of previous for if possible
		//for each p that was updated and not the good layer of j check if it is the good layer
		for p := range sUpdated {
			//if a majority supports p (p is good)
			//according to tal we dont have to know the exact amount, we can multiply layer size by number of layers
			jGood, found := ni.tGood[j]
			threshold := 0.5 * float64(mesh.LayerID(ni.LayerSize)*(i-p.Layer()))

			if (jGood != p || !found) && float64(ni.tSupport[p]) > threshold {
				ni.tGood[p.Layer()] = p
				//if p is the new minimal good layer
				if p.Layer() < i {
					minGood = p.Layer()
				}
			}
		}
	}
	ni.Debug("found minimal good layer %d", minGood)
	return minGood
}

func (ni *ninjaTortoise) updateBlocksSupport(b []*ninjaBlock, j mesh.LayerID) map[votingPattern]struct{} {
	sUpdated := map[votingPattern]struct{}{}
	for _, block := range b {
		//check if block votes for layer j explicitly or implicitly
		p, found := block.BlockVoteMap[j]
		if found {
			//explicit
			if _, expFound := ni.tExplicit[block.ID()]; !expFound {
				ni.tExplicit[block.ID()] = make(map[mesh.LayerID]*votingPattern, K*ni.LayerSize)
			}
			ni.tExplicit[block.ID()][j] = p
			ni.tSupport[*p]++         //add to supporting patterns
			sUpdated[*p] = struct{}{} //add to updated patterns

			//implicit
		} else if eff, effFound := ni.tEffective[block.ID()]; effFound {
			p, found = ni.tPatSupport[*eff][j]
			if found {
				ni.tSupport[*p]++         //add to supporting patterns
				sUpdated[*p] = struct{}{} //add to updated patterns
			}
		}
	}
	return sUpdated
}

func (ni *ninjaTortoise) addPatternVote(p votingPattern) func(b *ninjaBlock) {
	addPatternVote := func(b *ninjaBlock) {
		var vp map[mesh.LayerID]*votingPattern
		var found bool
		if vp, found = ni.tExplicit[b.ID()]; b.Layer() > 0 && !found {
			panic("block has no explicit voting, something went wrong ")
		}

		for _, ex := range vp {
			for _, block := range ni.tPattern[*ex] {
				ni.Debug("add pattern vote for pattern %d block %d layer %d", p, b.ID(), b.Layer())
				ni.tTally[p][block] = ni.tTally[p][block].Add(vec{1, 0})
			}
		}
	}
	return addPatternVote
}

func sumNodesInView(layerViewCounter map[mesh.LayerID]int, i mesh.LayerID, p mesh.LayerID) vec {
	var sum int
	for sum = 0; i <= p; i++ {
		sum = sum + layerViewCounter[i]
	}
	return Against.Multiplay(sum)
}

func (ni *ninjaTortoise) processBlocks(B []*mesh.Block, i mesh.LayerID) []*ninjaBlock {
	b := make([]*ninjaBlock, 0, len(B))
	for _, block := range B {
		nb := ni.processBlock(block)
		ni.blocks[nb.ID()] = nb
		b = append(b, nb)
		ni.layerBlocks[i] = append(ni.layerBlocks[i], block.ID())
	}
	return b
}

func (ni *ninjaTortoise) initGenesis(B []*mesh.Block, i mesh.LayerID) {
	ni.processBlocks(B, i)
	vp := &votingPattern{id: getId(ni.layerBlocks[Genesis]), LayerID: Genesis}
	ni.pBase = *vp
	ni.tGood[Genesis] = *vp
	ni.tExplicit[B[0].ID()] = make(map[mesh.LayerID]*votingPattern, K*ni.LayerSize)
}

func (ni *ninjaTortoise) initGenPlus1(B []*mesh.Block, i mesh.LayerID) {
	b := ni.processBlocks(B, i)
	vp := votingPattern{id: getId(ni.layerBlocks[Genesis+1]), LayerID: Genesis + 1}
	ni.tPattern[vp] = ni.layerBlocks[Genesis+1]
	ni.tVote[vp] = make(map[mesh.BlockID]vec)
	ni.tTally[vp] = map[mesh.BlockID]vec{}
	for _, b := range ni.layerBlocks[Genesis] {
		ni.tVote[vp][b] = vec{1, 0}
	}
	ni.updateBlocksSupport(b, 0)
}

func updatePatSupport(ni *ninjaTortoise, p votingPattern, bids []mesh.BlockID, idx mesh.LayerID, i mesh.LayerID) {
	if val, found := ni.tPatSupport[p]; !found || val == nil {
		ni.tPatSupport[p] = make(map[mesh.LayerID]*votingPattern)
	}
	pid := getId(bids)
	ni.Debug("update support for %d layer %d supported pattern %d", p, idx, pid)
	ni.tPatSupport[p][i] = &votingPattern{id: pid, LayerID: idx}
}

func (ni *ninjaTortoise) initToPbase(p votingPattern) {
	if _, found := ni.tTally[p]; !found {
		ni.tTally[p] = make(map[mesh.BlockID]vec)
	}
	for k, v := range ni.tTally[ni.pBase] {
		ni.Debug("add tally pattern %d block %d", p.id, k)
		ni.tTally[p][k] = v
	}
}

func (ni *ninjaTortoise) UpdateTables(B []*mesh.Block, i mesh.LayerID) mesh.LayerID { //i most recent layer
	ni.Debug("update tables layer %d", i)
	//initialize these tables //not in article
	b := ni.processBlocks(B, i)

	l := ni.findMinimalGoodLayer(i, b)

	//from minimal good pattern to current layer
	//update pattern tally for all good layers
	for j := l; j < i; j++ {
		if p, gfound := ni.tGood[j]; gfound {
			//init p's tally to pBase tally
			ni.initToPbase(p)

			//update correction vectors after vote count
			ni.updateCorrectionVectors(p)

			//update pattern tally for each good layer on the way
			ni.updatePatternTally(p, 0)

			// for each block in p's view add the pattern votes
			layerViewCounter := forBlockInView(ni.tPattern[p], ni.blocks, ni.pBase.Layer()+1, ni.addPatternVote(p))
			complete := true
			//update vote for each block between bottom of window  to p
			for idx := mesh.LayerID(math.Max(0, float64(i)-Window)); idx < j; idx++ {
				layer, _ := ni.layerBlocks[idx]
				bids := make([]mesh.BlockID, 0, ni.LayerSize)
				for _, bid := range layer {

					//if bid is not in p's view.
					// add negative vote multiplied by the amount of blocks in the view
					if _, found := ni.tTally[p][bid]; !found {
						ni.tTally[p][bid] = sumNodesInView(layerViewCounter, idx, p.Layer())
					}

					v, found := ni.tTally[p][bid]
					if !found {
						ni.Error(fmt.Sprintf("%d not in %d view ", bid, p))
					}

					if vote, err := globalOpinion(v, ni.LayerSize, float64(p.LayerID-idx)); err == nil {
						if val, found := ni.tVote[p]; !found || val == nil {
							ni.tVote[p] = make(map[mesh.BlockID]vec)
						}
						ni.tVote[p][bid] = vote
						bids = append(bids, bid)
					} else {
						complete = false //not complete
					}
				}
				updatePatSupport(ni, p, bids, idx, i)
			}

			// update completeness of p
			if _, found := ni.tComplete[p]; complete && !found {
				ni.tComplete[p] = struct{}{}
				ni.pBase = p
				ni.Debug("found new complete and good pattern for layer %d pattern %d with %d support ", l, p.id, ni.tSupport[p])
			}
		}
	}
	return ni.pBase.LayerID
}
