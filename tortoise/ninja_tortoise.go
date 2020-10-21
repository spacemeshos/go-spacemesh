package tortoise

import (
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/events"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"hash/fnv"
	"math"
	"sync"
	"time"
)

type vec [2]int
type patternID uint32 // this hash does not include the layer id

const ( // Threshold
	window          = 10
	globalThreshold = 0.6
)

var ( // correction vectors type
	// Opinion
	support     = vec{1, 0}
	against     = vec{0, 1}
	abstain     = vec{0, 0}
	zeroPattern = votingPattern{}
)

func max(i types.LayerID, j types.LayerID) types.LayerID {
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

type blockIDLayerTuple struct {
	types.BlockID
	types.LayerID
}

func (blt blockIDLayerTuple) layer() types.LayerID {
	return blt.LayerID
}

func (blt blockIDLayerTuple) id() types.BlockID {
	return blt.BlockID
}

type votingPattern struct {
	id patternID // cant put a slice here wont work well with maps, we need to hash the blockids
	types.LayerID
}

func (vp votingPattern) Layer() types.LayerID {
	return vp.LayerID
}

type database interface {
	GetBlock(id types.BlockID) (*types.Block, error)
	LayerBlockIds(id types.LayerID) ([]types.BlockID, error)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, foo func(block *types.Block) (bool, error)) error
	SaveContextualValidity(id types.BlockID, valid bool) error
	Persist(key []byte, v interface{}) error
	Retrieve(key []byte, v interface{}) (interface{}, error)
}

type ninjaTortoise struct {
	db           database // block cache
	logger       log.Log
	mutex        sync.Mutex
	Last         types.LayerID
	Hdist        types.LayerID
	Evict        types.LayerID
	AvgLayerSize int
	PBase        votingPattern
	Patterns     map[types.LayerID]map[votingPattern]struct{}      // map Patterns by layer for eviction purposes
	TEffective   map[types.BlockID]votingPattern                   // Explicit voting pattern of latest layer for a block
	TCorrect     map[types.BlockID]map[types.BlockID]vec           // correction vectors
	TExplicit    map[types.BlockID]map[types.LayerID]votingPattern // explict votes from block to layer pattern

	TSupport           map[votingPattern]int                 // for pattern p the number of blocks that support p
	TComplete          map[votingPattern]struct{}            // complete voting Patterns
	TEffectiveToBlocks map[votingPattern][]blockIDLayerTuple // inverse blocks effective pattern

	TTally   map[votingPattern]map[blockIDLayerTuple]vec  // for pattern p and block b count votes for b according to p
	TPattern map[votingPattern]map[types.BlockID]struct{} // set of blocks that comprise pattern p

	TPatSupport map[votingPattern]map[types.LayerID]votingPattern // pattern support count

	TGood map[types.LayerID]votingPattern // good pattern for layer i

	TVote map[votingPattern]map[blockIDLayerTuple]vec // global opinion
}

// NewNinjaTortoise create a new ninja tortoise instance
func newNinjaTortoise(layerSize int, blocks database, hdist int, log log.Log) *ninjaTortoise {

	trtl := &ninjaTortoise{logger: log, db: blocks,
		Hdist:        types.LayerID(hdist),
		AvgLayerSize: layerSize,
		PBase:        zeroPattern,

		Patterns:   map[types.LayerID]map[votingPattern]struct{}{},
		TGood:      map[types.LayerID]votingPattern{},
		TEffective: map[types.BlockID]votingPattern{},
		TCorrect:   map[types.BlockID]map[types.BlockID]vec{},
		TExplicit:  map[types.BlockID]map[types.LayerID]votingPattern{},
		TSupport:   map[votingPattern]int{},
		TPattern:   map[votingPattern]map[types.BlockID]struct{}{},

		TVote:              map[votingPattern]map[blockIDLayerTuple]vec{},
		TTally:             map[votingPattern]map[blockIDLayerTuple]vec{},
		TComplete:          map[votingPattern]struct{}{},
		TEffectiveToBlocks: map[votingPattern][]blockIDLayerTuple{},
		TPatSupport:        map[votingPattern]map[types.LayerID]votingPattern{},
	}

	return trtl
}

func (ni *ninjaTortoise) saveOpinion() error {
	for b, vec := range ni.TVote[ni.PBase] {
		valid := vec == support
		if err := ni.db.SaveContextualValidity(b.id(), valid); err != nil {
			return err
		}

		if !valid {
			ni.logger.With().Warning("block is contextually invalid", b.id())
		}
		events.ReportValidBlock(b.id(), valid)
	}
	return nil
}

// Persist saves the current tortoise state to the database
func (ni *ninjaTortoise) persist() error {
	if err := ni.saveOpinion(); err != nil {
		return err
	}
	return ni.db.Persist(mesh.TORTOISE, ni)
}

// RecoverTortoise retrieve latest saved tortoise from the database
func RecoverTortoise(mdb database) (interface{}, error) {
	return mdb.Retrieve(mesh.TORTOISE, &ninjaTortoise{})
}

func (ni *ninjaTortoise) evictOutOfPbase() {
	wg := sync.WaitGroup{}
	if ni.PBase == zeroPattern || ni.PBase.Layer() <= ni.Hdist {
		return
	}

	window := ni.PBase.Layer() - ni.Hdist
	for lyr := ni.Evict; lyr < window; lyr++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ni.Patterns[lyr] {
				delete(ni.TSupport, p)
				delete(ni.TComplete, p)
				delete(ni.TEffectiveToBlocks, p)
				delete(ni.TVote, p)
				delete(ni.TTally, p)
				delete(ni.TPattern, p)
				delete(ni.TPatSupport, p)
				ni.logger.Debug("evict pattern %v from maps ", p)
			}
			delete(ni.TGood, lyr)
			delete(ni.Patterns, lyr)
		}()
		wg.Add(1)
		go func() {
			defer wg.Done()
			ids, err := ni.db.LayerBlockIds(lyr)
			if err != nil {
				ni.logger.With().Error("could not get layer ids for layer ", lyr, log.Err(err))
			}
			for _, id := range ids {
				delete(ni.TEffective, id)
				delete(ni.TCorrect, id)
				delete(ni.TExplicit, id)
				ni.logger.Debug("evict block %v from maps ", id)
			}
		}()
		wg.Wait()
	}
	ni.Evict = window
}

func (ni *ninjaTortoise) processBlock(b *types.Block) {

	ni.logger.Debug("process block: %s layer: %s  ", b.ID(), b.Layer())
	if b.Layer() == types.GetEffectiveGenesis() {
		return
	}

	patternMap := make(map[types.LayerID]map[types.BlockID]struct{})
	for _, bid := range b.BlockVotes {
		ni.logger.Debug("block votes %s", bid)
		bl, err := ni.db.GetBlock(bid)
		if err != nil || bl == nil {
			ni.logger.Panic(fmt.Sprintf("error block not found ID %s , %v!!!!!", bid, err))
		}
		if _, found := patternMap[bl.Layer()]; !found {
			patternMap[bl.Layer()] = map[types.BlockID]struct{}{}
		}
		patternMap[bl.Layer()][bl.ID()] = struct{}{}
	}

	var effective votingPattern
	ni.TExplicit[b.ID()] = make(map[types.LayerID]votingPattern, ni.Hdist)

	var layerID types.LayerID
	if ni.Hdist+types.GetEffectiveGenesis() > b.Layer() {
		layerID = types.GetEffectiveGenesis()
	} else {
		layerID = b.Layer() - ni.Hdist
	}

	for ; layerID < b.Layer(); layerID++ {
		v, found := patternMap[layerID]

		if !found {
			ni.TExplicit[b.ID()][layerID] = zeroPattern
			continue
		}

		vp := votingPattern{id: getIdsFromSet(v), LayerID: layerID}
		ni.TPattern[vp] = v
		if _, ok := ni.Patterns[vp.Layer()]; !ok {
			ni.Patterns[vp.Layer()] = map[votingPattern]struct{}{}
		}
		ni.Patterns[vp.Layer()][vp] = struct{}{}
		ni.TExplicit[b.ID()][layerID] = vp

		if layerID >= effective.Layer() {
			effective = vp
		}
	}

	ni.TEffective[b.ID()] = effective

	v, found := ni.TEffectiveToBlocks[effective]
	if !found {
		v = make([]blockIDLayerTuple, 0, ni.AvgLayerSize)
	}
	var pattern []blockIDLayerTuple
	pattern = append(v, blockIDLayerTuple{b.ID(), b.Layer()})
	ni.TEffectiveToBlocks[effective] = pattern
	ni.logger.Debug("effective pattern to blocks %s %s", effective, pattern)

	return
}

func getID(bids []types.BlockID) patternID {
	bids = types.SortBlockIDs(bids)
	// calc
	h := fnv.New32()
	for i := 0; i < len(bids); i++ {
		h.Write(bids[i].Bytes())
	}
	// update
	sum := h.Sum32()
	return patternID(sum)
}

func getIdsFromSet(bids map[types.BlockID]struct{}) patternID {
	keys := make([]types.BlockID, 0, len(bids))
	for k := range bids {
		keys = append(keys, k)
	}
	return getID(keys)
}

func globalOpinion(v vec, layerSize int, delta float64) vec {
	threshold := float64(globalThreshold*delta) * float64(layerSize)
	if float64(v[0]) > threshold {
		return support
	} else if float64(v[1]) > threshold {
		return against
	} else {
		return abstain
	}
}

func (ni *ninjaTortoise) updateCorrectionVectors(p votingPattern, bottomOfWindow types.LayerID) {
	foo := func(x *types.Block) (bool, error) {
		for _, b := range ni.TEffectiveToBlocks[p] { // for all b whose effective vote is p
			if _, found := ni.TExplicit[b.id()][x.Layer()]; found { // if Texplicit[b][x.layer]!=0 check correctness of x.layer and found
				ni.logger.Debug(" blocks pattern %s block %s layer %s", p, b.id(), b.layer())
				if _, found := ni.TCorrect[b.id()]; !found {
					ni.TCorrect[b.id()] = make(map[types.BlockID]vec)
				}
				vo := ni.TVote[p][blockIDLayerTuple{BlockID: x.ID(), LayerID: x.Layer()}]
				ni.logger.Debug("vote from pattern %s to block %s layer %s vote %s ", p, x.ID(), x.Layer(), vo)
				ni.TCorrect[b.id()][x.ID()] = vo.Negate() // Tcorrect[b][x] = -Tvote[p][x]
				ni.logger.Debug("update correction vector for block %s layer %s , pattern %s vote %s for block %s ", b.id(), b.layer(), p, ni.TCorrect[b.id()][x.ID()], x.ID())
			} else {
				ni.logger.Debug("block %s from layer %s doesn't explicitly vote for layer %s", b.id(), b.layer(), x.Layer())
			}
		}
		return false, nil
	}

	tp := ni.TPattern[p]
	ni.db.ForBlockInView(tp, bottomOfWindow, foo)
}

func (ni *ninjaTortoise) updatePatternTally(newMinGood votingPattern, correctionMap map[types.BlockID]vec, effCountMap map[types.LayerID]int) {
	ni.logger.Debug("update tally pbase id:%s layer:%s p id:%s layer:%s", ni.PBase.id, ni.PBase.Layer(), newMinGood.id, newMinGood.Layer())
	for idx, effc := range effCountMap {
		g := ni.TGood[idx]
		for b, v := range ni.TVote[g] {
			tally := ni.TTally[newMinGood][b]
			tally = tally.Add(v.Multiply(effc))
			ni.TTally[newMinGood][b] = tally // in g's opinion -> p's tally
		}
	}

	// add corrections for each block
	for b, tally := range ni.TTally[newMinGood] {
		if count, found := correctionMap[b.id()]; found {
			ni.TTally[newMinGood][b] = tally.Add(count)
		}
	}
}

func (ni *ninjaTortoise) getCorrEffCounter() (map[types.BlockID]vec, map[types.LayerID]int, func(b *types.Block)) {
	correctionMap := make(map[types.BlockID]vec)
	effCountMap := make(map[types.LayerID]int)
	foo := func(b *types.Block) {
		if b.Layer() > ni.PBase.Layer() {
			if eff, found := ni.TEffective[b.ID()]; found {
				p, found := ni.TGood[eff.Layer()]
				if found && eff == p {
					effCountMap[eff.Layer()] = effCountMap[eff.Layer()] + 1
					for k, v := range ni.TCorrect[b.ID()] {
						correctionMap[k] = correctionMap[k].Add(v)
					}
				}
			}
		}
	}
	return correctionMap, effCountMap, foo
}

// for all layers from pBase to i add b's votes, mark good layers
// return new minimal good layer
func (ni *ninjaTortoise) findMinimalNewlyGoodLayer(lyr *types.Layer) types.LayerID {
	minGood := types.LayerID(math.MaxUint64)

	var j types.LayerID
	if window > lyr.Index() {
		j = ni.PBase.Layer() + 1
	} else {
		j = max(ni.PBase.Layer()+1, lyr.Index()-window+1)
	}

	for ; j < lyr.Index(); j++ {
		// update block votes on all Patterns in blocks view
		sUpdated := ni.updateBlocksSupport(lyr.Blocks(), j)
		// todo do this as part of previous for if possible
		// for each p that was updated and not the good layer of j check if it is the good layer
		for p := range sUpdated {
			// if a majority supports p (p is good)
			// according to tal we dont have to know the exact amount, we can multiply layer size by number of layers
			jGood, found := ni.TGood[j]
			threshold := 0.5 * float64(types.LayerID(ni.AvgLayerSize)*(ni.Last-p.Layer()))
			if (jGood != p || !found) && float64(ni.TSupport[p]) > threshold {
				ni.TGood[p.Layer()] = p
				// if p is the new minimal good layer
				if p.Layer() < minGood {
					minGood = p.Layer()
				}
			}
		}
	}

	ni.logger.Info("found minimal good layer %d, %d", minGood, ni.TGood[minGood].id)
	return minGood
}

// update block support for pattern in layer j
func (ni *ninjaTortoise) updateBlocksSupport(b []*types.Block, j types.LayerID) map[votingPattern]struct{} {
	sUpdated := map[votingPattern]struct{}{}
	for _, block := range b {
		// check if block votes for layer j explicitly or implicitly
		p, found := ni.TExplicit[block.ID()][j]
		if found {
			// explicit
			ni.TSupport[p]++         // add to supporting Patterns
			sUpdated[p] = struct{}{} // add to updated Patterns

			// implicit
		} else if eff, effFound := ni.TEffective[block.ID()]; effFound {
			p, found = ni.TPatSupport[eff][j]
			if found {
				ni.TSupport[p]++         // add to supporting Patterns
				sUpdated[p] = struct{}{} // add to updated Patterns
			}
		}
	}
	return sUpdated
}

func (ni *ninjaTortoise) addPatternVote(p votingPattern, view map[types.BlockID]struct{}) func(b types.BlockID) {
	addPatternVote := func(b types.BlockID) {
		var vp map[types.LayerID]votingPattern
		var found bool
		blk, err := ni.db.GetBlock(b)
		if err != nil {
			ni.logger.Panic(fmt.Sprintf("error block not found ID %s %v", b, err))
		}

		if ni.PBase != zeroPattern && blk.Layer() <= ni.PBase.Layer() { // ignore under pbase
			return
		}

		if vp, found = ni.TExplicit[b]; !found {
			ni.logger.Panic(fmt.Sprintf("block %s from layer %v has no explicit voting, something went wrong ", b, blk.Layer()))
		}

		for _, ex := range vp {
			blocks, err := ni.db.LayerBlockIds(ex.Layer())
			if err != nil {
				if ex.Layer() == 0 {
					//todo: fix this so that zero votes are ok
					log.Warning("block %v int layer %v voted on zero layer", blk.ID().String(), blk.Layer())
					continue
				}
				ni.logger.Panic("could not retrieve layer block ids %v error %v", ex.Layer(), err)
			}

			// explicitly abstain
			if ex == zeroPattern {
				continue
			}

			for _, bl := range blocks {
				_, found := ni.TPattern[ex][bl]
				blt := blockIDLayerTuple{bl, ex.Layer()}
				if found {
					ni.TTally[p][blt] = ni.TTally[p][blt].Add(support)
				} else if _, inSet := view[bl]; inSet { // in view but not in pattern
					ni.TTally[p][blt] = ni.TTally[p][blt].Add(against)
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
	return against.Multiply(sum)
}

func (ni *ninjaTortoise) processBlocks(layer *types.Layer) {
	for _, block := range layer.Blocks() {
		ni.processBlock(block)
	}
}

func (ni *ninjaTortoise) handleGenesis(genesis *types.Layer) {
	for _, blk := range genesis.Blocks() {
		ni.TExplicit[blk.ID()] = make(map[types.LayerID]votingPattern, int(ni.Hdist)*ni.AvgLayerSize)
	}
}

// todo send map instead of ni
func (ni *ninjaTortoise) updatePatSupport(p votingPattern, bids []types.BlockID, idx types.LayerID) {
	if val, found := ni.TPatSupport[p]; !found || val == nil {
		ni.TPatSupport[p] = make(map[types.LayerID]votingPattern)
	}
	pid := getID(bids)
	ni.logger.Debug("update support for %s layer %s supported pattern %s", p, idx, pid)
	ni.TPatSupport[p][idx] = votingPattern{id: pid, LayerID: idx}
}

//todo not sure initTallyToBase is even needed due to the fact that we treat layers under pbase as irreversible
func (ni *ninjaTortoise) initTallyToBase(base votingPattern, p votingPattern, windowStart types.LayerID) {
	mp := make(map[blockIDLayerTuple]vec, len(ni.TTally[base]))
	for b, v := range ni.TTally[base] {
		if b.layer() < windowStart {
			continue // dont copy votes for block outside window
		}
		mp[b] = v
	}
	ni.TTally[p] = mp
}

// LatestComplete latest complete layer out of all processed layer
func (ni *ninjaTortoise) latestComplete() types.LayerID {
	return ni.PBase.Layer()
}

func (ni *ninjaTortoise) handleIncomingLayer(newlyr *types.Layer) {
	ni.logger.With().Info("tortoise update tables", newlyr.Index(), log.Int("n_blocks", len(newlyr.Blocks())))
	start := time.Now()
	if newlyr.Index() > ni.Last {
		ni.Last = newlyr.Index()
	}

	defer ni.evictOutOfPbase()
	ni.processBlocks(newlyr)

	if newlyr.Index() == types.GetEffectiveGenesis() {
		ni.handleGenesis(newlyr)
		return
	}

	l := ni.findMinimalNewlyGoodLayer(newlyr)
	// from minimal newly good pattern to current layer
	// update pattern tally for all good layers
	for j := l; j > 0 && j < newlyr.Index(); j++ {
		p, gfound := ni.TGood[j]
		if gfound {

			// find bottom of window
			windowStart := getBottomOfWindow(newlyr.Index(), ni.PBase.Layer(), ni.Hdist)

			// init p's tally to pBase tally
			ni.initTallyToBase(ni.PBase, p, windowStart)

			view := make(map[types.BlockID]struct{})
			lCntr := make(map[types.LayerID]int)
			correctionMap, effCountMap, getCrrEffCnt := ni.getCorrEffCounter()
			foo := func(block *types.Block) (bool, error) {
				view[block.ID()] = struct{}{} // all blocks in view
				lCntr[block.Layer()]++        // amount of blocks for each layer in view
				getCrrEffCnt(block)           // calc correction and eff count
				return false, nil
			}

			tp := ni.TPattern[p]
			ni.db.ForBlockInView(tp, windowStart, foo)

			// add corrected implicit votes
			ni.updatePatternTally(p, correctionMap, effCountMap)

			// add explicit votes
			addPtrnVt := ni.addPatternVote(p, view)
			for bl := range view {
				addPtrnVt(bl)
			}

			complete := true
			for idx := ni.PBase.Layer(); idx < j; idx++ {
				layer, _ := ni.db.LayerBlockIds(idx) // todo handle error
				bids := make([]types.BlockID, 0, ni.AvgLayerSize)
				for _, bid := range layer {
					blt := blockIDLayerTuple{BlockID: bid, LayerID: idx}
					// if bid is not in p's view.
					// add negative vote multiplied by the amount of blocks in the view
					// explicit votes against (not in view )
					if _, found := view[bid]; idx >= ni.PBase.Layer() && !found {
						ni.TTally[p][blt] = sumNodesInView(lCntr, idx+1, p.Layer())
					}

					if val, found := ni.TVote[p]; !found || val == nil {
						ni.TVote[p] = make(map[blockIDLayerTuple]vec)
					}

					if vote := globalOpinion(ni.TTally[p][blt], ni.AvgLayerSize, float64(p.LayerID-idx)); vote != abstain {
						ni.TVote[p][blt] = vote
						if vote == support {
							bids = append(bids, bid)
						}
					} else {
						ni.TVote[p][blt] = vote
						ni.logger.Debug(" %s no opinion on %s %s %s", p, bid, idx, vote, ni.TTally[p][blt])
						complete = false // not complete
					}
				}

				if idx > ni.PBase.Layer() {
					ni.updatePatSupport(p, bids, idx)
				}
			}

			// update correction vectors after vote count
			ni.updateCorrectionVectors(p, windowStart)

			// update completeness of p
			if _, found := ni.TComplete[p]; complete && !found {
				ni.TComplete[p] = struct{}{}
				ni.PBase = p
				ni.logger.Info("found new complete and good pattern for layer %d pattern %d with %d support ", p.Layer().Uint64(), p.id, ni.TSupport[p])
			}
		}
	}
	ni.logger.With().Info(fmt.Sprintf("tortoise finished layer in %v", time.Since(start)), newlyr.Index(), log.FieldNamed("pbase", ni.PBase.Layer()))
	return
}

func getBottomOfWindow(newlyr types.LayerID, pbase types.LayerID, hdist types.LayerID) types.LayerID {
	if window > newlyr {
		return 0
	} else if pbase < hdist {
		return newlyr - window + 1
	}
	return max(pbase-hdist, newlyr-window+1)
}
