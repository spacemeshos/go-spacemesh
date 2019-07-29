package tortoise

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"hash/fnv"
	"math"
	"sort"
	"sync"
)

type vec [2]int
type PatternId uint32 //this hash dose not include the layer id

const ( //Threshold
	Window          = 10
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

func Max(i types.LayerID, j types.LayerID) types.LayerID {
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
	types.LayerID
}

func (vp votingPattern) Layer() types.LayerID {
	return vp.LayerID
}

type BlockCache interface {
	GetBlock(id types.BlockID) (*types.Block, error)
	LayerBlockIds(id types.LayerID) ([]types.BlockID, error)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, foo func(block *types.BlockHeader) error) error
}

//todo memory optimizations
type ninjaTortoise struct {
	log.Log
	BlockCache   //block cache
	hdist        types.LayerID
	evict        types.LayerID
	avgLayerSize int
	pBase        votingPattern
	patterns     map[types.LayerID][]votingPattern                 //map patterns by layer for eviction purposes
	tEffective   map[types.BlockID]votingPattern                   //Explicit voting pattern of latest layer for a block
	tCorrect     map[types.BlockID]map[types.BlockID]vec           //correction vectors
	tExplicit    map[types.BlockID]map[types.LayerID]votingPattern //explict votes from block to layer pattern
	tGood        map[types.LayerID]votingPattern                   //good pattern for layer i

	tGoodLock          sync.RWMutex                                 // sync access to tGood map
	tSupport           map[votingPattern]int                        //for pattern p the number of blocks that support p
	tComplete          map[votingPattern]struct{}                   //complete voting patterns
	tEffectiveToBlocks map[votingPattern][]types.BlockID            //inverse blocks effective pattern
	tVote              map[votingPattern]map[types.BlockID]vec      //global opinion
	tTally             map[votingPattern]map[types.BlockID]vec      //for pattern p and block b count votes for b according to p
	tPattern           map[votingPattern]map[types.BlockID]struct{} //set of blocks that comprise pattern p

	tPatternLock sync.RWMutex                                      //lock for tPattern
	tPatSupport  map[votingPattern]map[types.LayerID]votingPattern //pattern support count
}

func NewNinjaTortoise(layerSize int, blocks BlockCache, hdist int, log log.Log) *ninjaTortoise {
	return &ninjaTortoise{
		Log:                log,
		BlockCache:         blocks,
		hdist:              types.LayerID(hdist),
		avgLayerSize:       layerSize,
		pBase:              votingPattern{},
		patterns:           map[types.LayerID][]votingPattern{},
		tGood:              map[types.LayerID]votingPattern{},
		tGoodLock:          sync.RWMutex{},
		tEffective:         map[types.BlockID]votingPattern{},
		tCorrect:           map[types.BlockID]map[types.BlockID]vec{},
		tExplicit:          map[types.BlockID]map[types.LayerID]votingPattern{},
		tSupport:           map[votingPattern]int{},
		tPattern:           map[votingPattern]map[types.BlockID]struct{}{},
		tPatternLock:       sync.RWMutex{},
		tVote:              map[votingPattern]map[types.BlockID]vec{},
		tTally:             map[votingPattern]map[types.BlockID]vec{},
		tComplete:          map[votingPattern]struct{}{},
		tEffectiveToBlocks: map[votingPattern][]types.BlockID{},
		tPatSupport:        map[votingPattern]map[types.LayerID]votingPattern{},
	}
}

func (ni *ninjaTortoise) evictOutOfPbase() {
	wg := sync.WaitGroup{}
	if ni.pBase.Layer() <= ni.hdist {
		return
	}

	window := ni.pBase.Layer() - ni.hdist
	for lyr := ni.evict; lyr < window; lyr++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, p := range ni.patterns[lyr] {
				delete(ni.tSupport, p)
				delete(ni.tComplete, p)
				delete(ni.tEffectiveToBlocks, p)
				delete(ni.tVote, p)
				delete(ni.tTally, p)
				ni.tPatternLock.Lock()
				delete(ni.tPattern, p)
				ni.tPatternLock.Unlock()
				delete(ni.tPatSupport, p)
				delete(ni.tSupport, p)
				ni.Debug("evict pattern %v from maps ", p)
			}
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids, err := ni.LayerBlockIds(lyr)
			if err != nil {
				ni.Error("could not get layer ids for layer %v %v", lyr, err)
			}
			for _, id := range ids {
				delete(ni.tEffective, id)
				delete(ni.tCorrect, id)
				delete(ni.tExplicit, id)
				ni.Debug("evict block %v from maps ", id)
			}
		}()
		wg.Wait()
	}
	ni.evict = window
}

func (ni *ninjaTortoise) processBlock(b *types.Block) {

	ni.Debug("process block: %d layer: %d  ", b.ID(), b.Layer())
	if b.Layer() == Genesis {
		return
	}

	patternMap := make(map[types.LayerID]map[types.BlockID]struct{})
	for _, bid := range b.BlockVotes {
		ni.Debug("block votes %d", bid)
		bl, err := ni.GetBlock(bid)
		if err != nil || bl == nil {
			ni.Error(fmt.Sprintf("error block not found ID %d , %v!!!!!", bid, err))
			return
		}
		if _, found := patternMap[bl.Layer()]; !found {
			patternMap[bl.Layer()] = map[types.BlockID]struct{}{}
		}
		patternMap[bl.Layer()][bl.ID()] = struct{}{}
	}

	var effective votingPattern
	ni.tExplicit[b.ID()] = make(map[types.LayerID]votingPattern, ni.hdist)
	for layerId, v := range patternMap {
		vp := votingPattern{id: getIdsFromSet(v), LayerID: layerId}
		ni.tPatternLock.Lock()
		ni.tPattern[vp] = v
		ni.tPatternLock.Unlock()
		arr, _ := ni.patterns[vp.Layer()]
		ni.patterns[vp.Layer()] = append(arr, vp)
		ni.tExplicit[b.ID()][layerId] = vp
		if layerId >= effective.Layer() {
			effective = vp
		}
	}

	ni.tEffective[b.ID()] = effective

	v, found := ni.tEffectiveToBlocks[effective]
	if !found {
		v = make([]types.BlockID, 0, ni.avgLayerSize)
	}
	var pattern []types.BlockID = nil
	pattern = append(v, b.ID())
	ni.tEffectiveToBlocks[effective] = pattern
	ni.Debug("effective pattern to blocks %d %d", effective, pattern)

	return
}

func getId(bids []types.BlockID) PatternId {
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

func getIdsFromSet(bids map[types.BlockID]struct{}) PatternId {
	keys := make([]types.BlockID, 0, len(bids))
	for k := range bids {
		keys = append(keys, k)
	}
	return getId(keys)
}

func globalOpinion(v vec, layerSize int, delta float64) vec {
	threshold := float64(GlobalThreshold*delta) * float64(layerSize)
	if float64(v[0]) > threshold {
		return Support
	} else if float64(v[1]) > threshold {
		return Against
	} else {
		return Abstain
	}
}

func (ni *ninjaTortoise) updateCorrectionVectors(p votingPattern, bottomOfWindow types.LayerID) {
	foo := func(x *types.BlockHeader) error {
		for _, bid := range ni.tEffectiveToBlocks[p] { //for all b who's effective vote is p
			b, err := ni.GetBlock(bid)
			if err != nil {
				ni.Panic(fmt.Sprintf("error block not found ID %d", bid))
			}

			if _, found := ni.tExplicit[b.ID()][x.Layer()]; found { //if Texplicit[b][x.layer]!=0 check correctness of x.layer and found
				ni.Debug(" blocks pattern %d block %d layer %d", p, b.ID(), b.Layer())
				if _, found := ni.tCorrect[b.ID()]; !found {
					ni.tCorrect[b.ID()] = make(map[types.BlockID]vec)
				}
				vo := ni.tVote[p][x.ID()]
				ni.Debug("vote from pattern %d to block %d layer %d vote %d ", p, x.ID(), x.Layer(), vo)
				ni.tCorrect[b.ID()][x.ID()] = vo.Negate() //Tcorrect[b][x] = -Tvote[p][x]
				ni.Debug("update correction vector for block %d layer %d , pattern %d vote %d for block %d ", b.ID(), b.Layer(), p, ni.tCorrect[b.ID()][x.ID()], x.ID())
			} else {
				ni.Debug("block %d from layer %d dose'nt explicitly vote for layer %d", b.ID(), b.Layer(), x.Layer())
			}
		}
		return nil
	}

	ni.tPatternLock.RLock()
	tp := ni.tPattern[p]
	ni.tPatternLock.RUnlock()
	ni.ForBlockInView(tp, bottomOfWindow, foo)
}

func (ni *ninjaTortoise) updatePatternTally(newMinGood votingPattern, correctionMap map[types.BlockID]vec, effCountMap map[types.LayerID]int) {
	ni.Debug("update tally pbase id:%d layer:%d p id:%d layer:%d", ni.pBase.id, ni.pBase.Layer(), newMinGood.id, newMinGood.Layer())
	for idx, effc := range effCountMap {
		ni.tGoodLock.RLock()
		g := ni.tGood[idx]
		ni.tGoodLock.RUnlock()
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

func (ni *ninjaTortoise) getCorrEffCounter() (map[types.BlockID]vec, map[types.LayerID]int, func(b *types.BlockHeader)) {
	correctionMap := make(map[types.BlockID]vec)
	effCountMap := make(map[types.LayerID]int)
	foo := func(b *types.BlockHeader) {
		if b.Layer() > ni.pBase.Layer() { //because we already copied pbase's votes
			if eff, found := ni.tEffective[b.ID()]; found {
				ni.tGoodLock.RLock()
				p, found := ni.tGood[eff.Layer()]
				ni.tGoodLock.RUnlock()
				if found && eff == p {
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
func (ni *ninjaTortoise) findMinimalNewlyGoodLayer(lyr *types.Layer) types.LayerID {
	minGood := types.LayerID(math.MaxUint64)

	var j types.LayerID
	if Window > lyr.Index() {
		j = ni.pBase.Layer() + 1
	} else {
		j = Max(ni.pBase.Layer()+1, lyr.Index()-Window+1)
	}

	ni.tGoodLock.Lock()
	for ; j < lyr.Index(); j++ {
		// update block votes on all patterns in blocks view
		sUpdated := ni.updateBlocksSupport(lyr.Blocks(), j)
		//todo do this as part of previous for if possible
		//for each p that was updated and not the good layer of j check if it is the good layer
		for p := range sUpdated {
			//if a majority supports p (p is good)
			//according to tal we dont have to know the exact amount, we can multiply layer size by number of layers
			jGood, found := ni.tGood[j]
			threshold := 0.5 * float64(types.LayerID(ni.avgLayerSize)*(lyr.Index()-p.Layer()))

			if (jGood != p || !found) && float64(ni.tSupport[p]) > threshold {
				ni.tGood[p.Layer()] = p
				//if p is the new minimal good layer
				if p.Layer() < minGood {
					minGood = p.Layer()
				}
			}
		}
	}
	ni.tGoodLock.Unlock()
	ni.Debug("found minimal good layer %d", minGood)
	return minGood
}

//update block support for pattern in layer j
func (ni *ninjaTortoise) updateBlocksSupport(b []*types.Block, j types.LayerID) map[votingPattern]struct{} {
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

func (ni *ninjaTortoise) addPatternVote(p votingPattern, view map[types.BlockID]struct{}) func(b types.BlockID) {
	addPatternVote := func(b types.BlockID) {
		var vp map[types.LayerID]votingPattern
		var found bool
		bl, err := ni.GetBlock(b)
		if err != nil {
			ni.Panic(fmt.Sprintf("error block not found ID %d %v", b, err))
		}
		if bl.Layer() <= ni.pBase.Layer() {
			return
		}

		if vp, found = ni.tExplicit[b]; !found {
			ni.Panic(fmt.Sprintf("block %d from layer %v has no explicit voting, something went wrong ", b, bl.Layer()))
		}

		for _, ex := range vp {
			blocks, err := ni.LayerBlockIds(ex.Layer()) //todo handle error
			if err != nil {
				ni.Panic("could not retrieve layer block ids")
			}
			for _, bl := range blocks {
				ni.tPatternLock.RLock()
				_, found := ni.tPattern[ex][bl]
				ni.tPatternLock.RUnlock()
				if found {
					ni.tTally[p][bl] = ni.tTally[p][bl].Add(Support)
				} else if _, inSet := view[bl]; inSet { //in view but not in pattern
					ni.tTally[p][bl] = ni.tTally[p][bl].Add(Against)
				}
			}
		}
	}
	return addPatternVote
}

func sumNodesInView(layerBlockCounter map[types.LayerID]int, layer types.LayerID, pLayer types.LayerID) vec {
	var sum int
	for sum = 0; layer <= pLayer; layer++ {
		sum = sum + layerBlockCounter[layer]
	}
	return Against.Multiply(sum)
}

func (ni *ninjaTortoise) processBlocks(layer *types.Layer) {
	for _, block := range layer.Blocks() {
		ni.processBlock(block)
	}
}

func (ni *ninjaTortoise) handleGenesis(genesis *types.Layer) {
	blkIds := make([]types.BlockID, 0, len(genesis.Blocks()))
	for _, blk := range genesis.Blocks() {
		blkIds = append(blkIds, blk.ID())
	}
	vp := votingPattern{id: getId(blkIds), LayerID: Genesis}
	ni.pBase = vp
	ni.tGoodLock.Lock()
	ni.tGood[Genesis] = vp
	ni.tGoodLock.Unlock()
	ni.tExplicit[genesis.Blocks()[0].ID()] = make(map[types.LayerID]votingPattern, int(ni.hdist)*ni.avgLayerSize)
}

//todo send map instead of ni
func updatePatSupport(ni *ninjaTortoise, p votingPattern, bids []types.BlockID, idx types.LayerID) {
	if val, found := ni.tPatSupport[p]; !found || val == nil {
		ni.tPatSupport[p] = make(map[types.LayerID]votingPattern)
	}
	pid := getId(bids)
	ni.Debug("update support for %d layer %d supported pattern %d", p, idx, pid)
	ni.tPatSupport[p][idx] = votingPattern{id: pid, LayerID: idx}
}

func initTallyToBase(tally map[votingPattern]map[types.BlockID]vec, base votingPattern, p votingPattern) {
	if _, found := tally[p]; !found {
		tally[p] = make(map[types.BlockID]vec)
	}
	for k, v := range tally[base] {
		tally[p][k] = v
	}
}

func (ni *ninjaTortoise) latestComplete() types.LayerID {
	return ni.pBase.Layer()
}

func (ni *ninjaTortoise) getVotes() map[types.BlockID]vec {
	return ni.tVote[ni.pBase]
}

func (ni *ninjaTortoise) getVote(id types.BlockID) vec {
	block, err := ni.GetBlock(id)
	if err != nil {
		ni.Panic(fmt.Sprintf("error block not found ID %d, %v", id, err))
	}

	if block.Layer() > ni.pBase.Layer() {
		ni.Error("we dont have an opinion on block according to current pbase")
		return Against
	}

	return ni.tVote[ni.pBase][id]
}

func (ni *ninjaTortoise) handleIncomingLayer(newlyr *types.Layer) { //i most recent layer
	ni.Info("update tables layer %d with %d blocks", newlyr.Index(), len(newlyr.Blocks()))
	defer ni.evictOutOfPbase()
	ni.processBlocks(newlyr)

	if newlyr.Index() == Genesis {
		ni.handleGenesis(newlyr)
		return
	}

	l := ni.findMinimalNewlyGoodLayer(newlyr)
	//from minimal newly good pattern to current layer
	//update pattern tally for all good layers
	for j := l; j > 0 && j < newlyr.Index(); j++ {
		ni.tGoodLock.RLock()
		p, gfound := ni.tGood[j]
		ni.tGoodLock.RUnlock()
		if gfound {
			//init p's tally to pBase tally
			initTallyToBase(ni.tTally, ni.pBase, p)

			//find bottom of window
			var windowStart types.LayerID
			if Window > newlyr.Index() {
				windowStart = 0
			} else {
				windowStart = Max(ni.pBase.Layer()+1, newlyr.Index()-Window+1)
			}

			view := make(map[types.BlockID]struct{})
			lCntr := make(map[types.LayerID]int)
			correctionMap, effCountMap, getCrrEffCnt := ni.getCorrEffCounter()
			foo := func(block *types.BlockHeader) error {
				view[block.ID()] = struct{}{} //all blocks in view
				for _, id := range block.BlockVotes {
					view[id] = struct{}{}
				}
				lCntr[block.Layer()]++ //amount of blocks for each layer in view
				getCrrEffCnt(block)    //calc correction and eff count
				return nil
			}

			ni.tPatternLock.RLock()
			tp := ni.tPattern[p]
			ni.tPatternLock.RUnlock()
			ni.ForBlockInView(tp, windowStart, foo)

			//add corrected implicit votes
			ni.updatePatternTally(p, correctionMap, effCountMap)

			//add explicit votes
			addPtrnVt := ni.addPatternVote(p, view)
			for bl := range view {
				addPtrnVt(bl)
			}

			complete := true
			for idx := types.LayerID(0); idx < j; idx++ {
				layer, _ := ni.LayerBlockIds(idx) //todo handle error
				bids := make([]types.BlockID, 0, ni.avgLayerSize)
				for _, bid := range layer {
					//if bid is not in p's view.
					//add negative vote multiplied by the amount of blocks in the view
					//explicit votes against (not in view )
					if _, found := view[bid]; idx >= ni.pBase.Layer() && !found {
						ni.tTally[p][bid] = sumNodesInView(lCntr, idx+1, p.Layer())
					}

					if val, found := ni.tVote[p]; !found || val == nil {
						ni.tVote[p] = make(map[types.BlockID]vec)
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

				if idx > ni.pBase.Layer() {
					updatePatSupport(ni, p, bids, idx)
				}
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

func (ni *ninjaTortoise) GetGoodPattern(layer types.LayerID) (uint32, error) {
	if layer == 0 || layer == 1 {
		v, err := ni.LayerBlockIds(layer)
		if err != nil {
			ni.Error("Could not get layer block ids for layer %v err=%v", layer, err)
			return 0, err
		}
		return uint32(getId(v)), nil
	}

	if layer >= ni.pBase.LayerID {
		return 0, errors.New("pbase is lower than provided layer")
	}

	ni.tGoodLock.RLock()
	val, ok := ni.tGood[layer]
	ni.tGoodLock.RUnlock()

	if !ok {
		return 0, errors.New("no good layer")
	}

	return uint32(val.id), nil
}

func (ni *ninjaTortoise) GetGoodPatternBlocks(layer types.LayerID) (map[types.BlockID]struct{}, error) {
	if layer == 0 || layer == 1 {
		blocksSlice, err := ni.LayerBlockIds(layer)
		if err != nil {
			ni.Error("Could not get layer block ids for layer %v err=%v", layer, err)
			return nil, err
		}
		blocks := make(map[types.BlockID]struct{}, len(blocksSlice))
		for _, b := range blocksSlice {
			blocks[b] = struct{}{}
		}
		return blocks, nil
	}

	if layer >= ni.pBase.LayerID {
		return nil, errors.New("pbase is lower than provided layer")
	}

	ni.tGoodLock.RLock()
	val, ok := ni.tGood[layer]
	ni.tGoodLock.RUnlock()

	if !ok {
		return nil, errors.New("no good layer")
	}

	ni.tPatternLock.RLock()
	blocks, ok := ni.tPattern[val]
	ni.tPatternLock.RUnlock()

	if !ok {
		return nil, errors.New("pattern does not exist")
	}

	return blocks, nil
}
