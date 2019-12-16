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
type PatternId uint32 //this hash dose not include the layer id

const ( //Threshold
	Window          = 10
	LocalThreshold  = 0.8 //ThetaL
	GlobalThreshold = 0.6 //ThetaG
	Genesis         = 0
)

var ( //correction vectors type
	//Opinion
	Support     = vec{1, 0}
	Against     = vec{0, 1}
	Abstain     = vec{0, 0}
	ZeroPattern = votingPattern{}
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

type Mesh interface {
	GetBlock(id types.BlockID) (*types.Block, error)
	LayerBlockIds(id types.LayerID) ([]types.BlockID, error)
	ForBlockInView(view map[types.BlockID]struct{}, layer types.LayerID, foo func(block *types.Block) (bool, error)) error
	SaveContextualValidity(id types.BlockID, valid bool) error
	Persist(key []byte, v interface{}) error
	Retrieve(key []byte, v interface{}) (interface{}, error)
}

//todo memory optimizations
type NinjaTortoise struct {
	log.Log
	Mesh           //block cache
	*ninjaTortoise //contains all  fields needed for serialization
}

type ninjaTortoise struct {
	Last         types.LayerID
	Hdist        types.LayerID
	Evict        types.LayerID
	AvgLayerSize int
	PBase        votingPattern
	Patterns     map[types.LayerID][]votingPattern                 //map Patterns by layer for eviction purposes
	TEffective   map[types.BlockID]votingPattern                   //Explicit voting pattern of latest layer for a block
	TCorrect     map[types.BlockID]map[types.BlockID]vec           //correction vectors
	TExplicit    map[types.BlockID]map[types.LayerID]votingPattern //explict votes from block to layer pattern

	TSupport           map[votingPattern]int             //for pattern p the number of blocks that support p
	TComplete          map[votingPattern]struct{}        //complete voting Patterns
	TEffectiveToBlocks map[votingPattern][]types.BlockID //inverse blocks effective pattern

	TTally   map[votingPattern]map[types.BlockID]vec      //for pattern p and block b count votes for b according to p
	TPattern map[votingPattern]map[types.BlockID]struct{} //set of blocks that comprise pattern p

	TPatSupport map[votingPattern]map[types.LayerID]votingPattern //pattern support count

	TGood map[types.LayerID]votingPattern //good pattern for layer i

	TVote map[votingPattern]map[types.BlockID]vec //global opinion
}

func NewNinjaTortoise(layerSize int, blocks Mesh, hdist int, log log.Log) *NinjaTortoise {

	trtl := &NinjaTortoise{Log: log, Mesh: blocks}

	trtl.ninjaTortoise = &ninjaTortoise{
		Hdist:        types.LayerID(hdist),
		AvgLayerSize: layerSize,
		PBase:        ZeroPattern,

		Patterns:   map[types.LayerID][]votingPattern{},
		TGood:      map[types.LayerID]votingPattern{},
		TEffective: map[types.BlockID]votingPattern{},
		TCorrect:   map[types.BlockID]map[types.BlockID]vec{},
		TExplicit:  map[types.BlockID]map[types.LayerID]votingPattern{},
		TSupport:   map[votingPattern]int{},
		TPattern:   map[votingPattern]map[types.BlockID]struct{}{},

		TVote:              map[votingPattern]map[types.BlockID]vec{},
		TTally:             map[votingPattern]map[types.BlockID]vec{},
		TComplete:          map[votingPattern]struct{}{},
		TEffectiveToBlocks: map[votingPattern][]types.BlockID{},
		TPatSupport:        map[votingPattern]map[types.LayerID]votingPattern{},
	}

	return trtl
}

func (ni *NinjaTortoise) saveOpinion() error {
	for bid, vec := range ni.TVote[ni.PBase] {
		valid := vec == Support
		if err := ni.SaveContextualValidity(bid, valid); err != nil {
			return err
		}

		if !valid {
			ni.With().Warning("block is contextually invalid", log.BlockId(bid.String()))
		}
		events.Publish(events.ValidBlock{Id: bid.String(), Valid: valid})
	}
	return nil
}

func (ni *NinjaTortoise) PersistTortoise() error {
	if err := ni.saveOpinion(); err != nil {
		return err
	}
	return ni.Persist(mesh.TORTOISE, ni.ninjaTortoise)
}

func (ni *NinjaTortoise) RecoverTortoise() (interface{}, error) {
	return ni.Retrieve(mesh.TORTOISE, &ninjaTortoise{})
}

func (ni *NinjaTortoise) evictOutOfPbase() {
	wg := sync.WaitGroup{}
	if ni.PBase == ZeroPattern || ni.PBase.Layer() <= ni.Hdist {
		return
	}

	window := ni.PBase.Layer() - ni.Hdist
	for lyr := ni.Evict; lyr < window; lyr++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, p := range ni.Patterns[lyr] {
				delete(ni.TSupport, p)
				delete(ni.TComplete, p)
				delete(ni.TEffectiveToBlocks, p)
				delete(ni.TVote, p)
				delete(ni.TTally, p)
				delete(ni.TPattern, p)
				delete(ni.TPatSupport, p)
				delete(ni.TSupport, p)
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
				delete(ni.TEffective, id)
				delete(ni.TCorrect, id)
				delete(ni.TExplicit, id)
				ni.Debug("evict block %v from maps ", id)
			}
		}()
		wg.Wait()
	}
	ni.Evict = window
}

func (ni *NinjaTortoise) processBlock(b *types.Block) {

	ni.Debug("process block: %s layer: %s  ", b.Id(), b.Layer())
	if b.Layer() == Genesis {
		return
	}

	patternMap := make(map[types.LayerID]map[types.BlockID]struct{})
	for _, bid := range b.BlockVotes {
		ni.Debug("block votes %s", bid)
		bl, err := ni.GetBlock(bid)
		if err != nil || bl == nil {
			ni.Panic(fmt.Sprintf("error block not found ID %s , %v!!!!!", bid, err))
		}
		if _, found := patternMap[bl.Layer()]; !found {
			patternMap[bl.Layer()] = map[types.BlockID]struct{}{}
		}
		patternMap[bl.Layer()][bl.Id()] = struct{}{}
	}

	var effective votingPattern
	ni.TExplicit[b.Id()] = make(map[types.LayerID]votingPattern, ni.Hdist)

	var layerId types.LayerID
	if ni.Hdist > b.Layer() {
		layerId = 0
	} else {
		layerId = b.Layer() - ni.Hdist
	}

	for ; layerId < b.Layer(); layerId++ {
		v, found := patternMap[layerId]

		if !found {
			ni.TExplicit[b.Id()][layerId] = ZeroPattern
			continue
		}

		vp := votingPattern{id: getIdsFromSet(v), LayerID: layerId}
		ni.TPattern[vp] = v
		arr, _ := ni.Patterns[vp.Layer()]
		ni.Patterns[vp.Layer()] = append(arr, vp)
		ni.TExplicit[b.Id()][layerId] = vp

		if layerId >= effective.Layer() {
			effective = vp
		}
	}

	ni.TEffective[b.Id()] = effective

	v, found := ni.TEffectiveToBlocks[effective]
	if !found {
		v = make([]types.BlockID, 0, ni.AvgLayerSize)
	}
	var pattern []types.BlockID = nil
	pattern = append(v, b.Id())
	ni.TEffectiveToBlocks[effective] = pattern
	ni.Debug("effective pattern to blocks %s %s", effective, pattern)

	return
}

func getId(bids []types.BlockID) PatternId {
	bids = types.SortBlockIds(bids)
	// calc
	h := fnv.New32()
	for i := 0; i < len(bids); i++ {
		h.Write(bids[i].ToBytes())
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

func (ni *NinjaTortoise) updateCorrectionVectors(p votingPattern, bottomOfWindow types.LayerID) {
	foo := func(x *types.Block) (bool, error) {
		for _, bid := range ni.TEffectiveToBlocks[p] { //for all b whose effective vote is p
			b, err := ni.GetBlock(bid)
			if err != nil {
				ni.Panic(fmt.Sprintf("error block not found ID %s", bid))
			}

			if _, found := ni.TExplicit[b.Id()][x.Layer()]; found { //if Texplicit[b][x.layer]!=0 check correctness of x.layer and found
				ni.Debug(" blocks pattern %s block %s layer %s", p, b.Id(), b.Layer())
				if _, found := ni.TCorrect[b.Id()]; !found {
					ni.TCorrect[b.Id()] = make(map[types.BlockID]vec)
				}
				vo := ni.TVote[p][x.Id()]
				ni.Debug("vote from pattern %s to block %s layer %s vote %s ", p, x.Id(), x.Layer(), vo)
				ni.TCorrect[b.Id()][x.Id()] = vo.Negate() //Tcorrect[b][x] = -Tvote[p][x]
				ni.Debug("update correction vector for block %s layer %s , pattern %s vote %s for block %s ", b.Id(), b.Layer(), p, ni.TCorrect[b.Id()][x.Id()], x.Id())
			} else {
				ni.Debug("block %s from layer %s dose'nt explicitly vote for layer %s", b.Id(), b.Layer(), x.Layer())
			}
		}
		return false, nil
	}

	tp := ni.TPattern[p]
	ni.ForBlockInView(tp, bottomOfWindow, foo)
}

func (ni *NinjaTortoise) updatePatternTally(newMinGood votingPattern, correctionMap map[types.BlockID]vec, effCountMap map[types.LayerID]int) {
	ni.Debug("update tally pbase id:%s layer:%s p id:%s layer:%s", ni.PBase.id, ni.PBase.Layer(), newMinGood.id, newMinGood.Layer())
	for idx, effc := range effCountMap {
		g := ni.TGood[idx]
		for b, v := range ni.TVote[g] {
			tally := ni.TTally[newMinGood][b]
			tally = tally.Add(v.Multiply(effc))
			ni.TTally[newMinGood][b] = tally //in g's view -> in p's view
		}
	}

	for b, tally := range ni.TTally[newMinGood] {
		if count, found := correctionMap[b]; found {
			ni.TTally[newMinGood][b] = tally.Add(count)
		}
	}
}

func (ni *ninjaTortoise) getCorrEffCounter() (map[types.BlockID]vec, map[types.LayerID]int, func(b *types.Block)) {
	correctionMap := make(map[types.BlockID]vec)
	effCountMap := make(map[types.LayerID]int)
	foo := func(b *types.Block) {
		if b.Layer() > ni.PBase.Layer() {
			if eff, found := ni.TEffective[b.Id()]; found {
				p, found := ni.TGood[eff.Layer()]
				if found && eff == p {
					effCountMap[eff.Layer()] = effCountMap[eff.Layer()] + 1
					for k, v := range ni.TCorrect[b.Id()] {
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
func (ni *NinjaTortoise) findMinimalNewlyGoodLayer(lyr *types.Layer) types.LayerID {
	minGood := types.LayerID(math.MaxUint64)

	var j types.LayerID
	if Window > lyr.Index() {
		j = ni.PBase.Layer() + 1
	} else {
		j = Max(ni.PBase.Layer()+1, lyr.Index()-Window+1)
	}

	for ; j < lyr.Index(); j++ {
		// update block votes on all Patterns in blocks view
		sUpdated := ni.updateBlocksSupport(lyr.Blocks(), j)
		//todo do this as part of previous for if possible
		//for each p that was updated and not the good layer of j check if it is the good layer
		for p := range sUpdated {
			//if a majority supports p (p is good)
			//according to tal we dont have to know the exact amount, we can multiply layer size by number of layers
			jGood, found := ni.TGood[j]
			threshold := 0.5 * float64(types.LayerID(ni.AvgLayerSize)*(ni.Last-p.Layer()))
			if (jGood != p || !found) && float64(ni.TSupport[p]) > threshold {
				ni.TGood[p.Layer()] = p
				//if p is the new minimal good layer
				if p.Layer() < minGood {
					minGood = p.Layer()
				}
			}
		}
	}

	ni.Info("found minimal good layer %d, %d", minGood, ni.TGood[minGood].id)
	return minGood
}

//update block support for pattern in layer j
func (ni *NinjaTortoise) updateBlocksSupport(b []*types.Block, j types.LayerID) map[votingPattern]struct{} {
	sUpdated := map[votingPattern]struct{}{}
	for _, block := range b {
		//check if block votes for layer j explicitly or implicitly
		p, found := ni.TExplicit[block.Id()][j]
		if found {
			//explicit
			ni.TSupport[p]++         //add to supporting Patterns
			sUpdated[p] = struct{}{} //add to updated Patterns

			//implicit
		} else if eff, effFound := ni.TEffective[block.Id()]; effFound {
			p, found = ni.TPatSupport[eff][j]
			if found {
				ni.TSupport[p]++         //add to supporting Patterns
				sUpdated[p] = struct{}{} //add to updated Patterns
			}
		}
	}
	return sUpdated
}

func (ni *NinjaTortoise) addPatternVote(p votingPattern, view map[types.BlockID]struct{}) func(b types.BlockID) {
	addPatternVote := func(b types.BlockID) {
		var vp map[types.LayerID]votingPattern
		var found bool
		blk, err := ni.GetBlock(b)
		if err != nil {
			ni.Panic(fmt.Sprintf("error block not found ID %s %v", b, err))
		}

		if ni.PBase != ZeroPattern && blk.Layer() <= ni.PBase.Layer() { //ignore under pbase
			return
		}

		if vp, found = ni.TExplicit[b]; !found {
			ni.Panic(fmt.Sprintf("block %s from layer %v has no explicit voting, something went wrong ", b, blk.Layer()))
		}

		for _, ex := range vp {
			blocks, err := ni.LayerBlockIds(ex.Layer()) //todo handle error
			if err != nil {
				ni.Panic("could not retrieve layer block ids")
			}

			//explicitly abstain
			if ex == ZeroPattern {
				continue
			}

			for _, bl := range blocks {
				_, found := ni.TPattern[ex][bl]
				if found {
					ni.TTally[p][bl] = ni.TTally[p][bl].Add(Support)
				} else if _, inSet := view[bl]; inSet { //in view but not in pattern
					ni.TTally[p][bl] = ni.TTally[p][bl].Add(Against)
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

func (ni *NinjaTortoise) processBlocks(layer *types.Layer) {
	for _, block := range layer.Blocks() {
		ni.processBlock(block)
	}
}

func (ni *NinjaTortoise) handleGenesis(genesis *types.Layer) {
	for _, blk := range genesis.Blocks() {
		ni.TExplicit[blk.Id()] = make(map[types.LayerID]votingPattern, int(ni.Hdist)*ni.AvgLayerSize)
	}
}

//todo send map instead of ni
func (ni *NinjaTortoise) updatePatSupport(p votingPattern, bids []types.BlockID, idx types.LayerID) {
	if val, found := ni.TPatSupport[p]; !found || val == nil {
		ni.TPatSupport[p] = make(map[types.LayerID]votingPattern)
	}
	pid := getId(bids)
	ni.Debug("update support for %s layer %s supported pattern %s", p, idx, pid)
	ni.TPatSupport[p][idx] = votingPattern{id: pid, LayerID: idx}
}

func initTallyToBase(tally map[votingPattern]map[types.BlockID]vec, base votingPattern, p votingPattern) {
	tally[p] = make(map[types.BlockID]vec)
	for k, v := range tally[base] {
		tally[p][k] = v
	}
}

func (ni *NinjaTortoise) latestComplete() types.LayerID {
	return ni.PBase.Layer()
}

func (ni *NinjaTortoise) getVotes() map[types.BlockID]vec {
	return ni.TVote[ni.PBase]
}

func (ni *NinjaTortoise) getVote(id types.BlockID) vec {
	block, err := ni.GetBlock(id)
	if err != nil {
		ni.Panic(fmt.Sprintf("error block not found ID %s, %v", id, err))
	}

	if block.Layer() > ni.PBase.Layer() {
		ni.Error("we dont have an opinion on block according to current pbase")
		return Against
	}

	return ni.TVote[ni.PBase][id]
}

func (ni *NinjaTortoise) handleIncomingLayer(newlyr *types.Layer) {
	ni.With().Info("Tortoise update tables", log.LayerId(uint64(newlyr.Index())), log.Int("n_blocks", len(newlyr.Blocks())))
	start := time.Now()
	if newlyr.Index() > ni.Last {
		ni.Last = newlyr.Index()
	}

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
		p, gfound := ni.TGood[j]
		if gfound {
			//init p's tally to pBase tally
			initTallyToBase(ni.TTally, ni.PBase, p)

			//find bottom of window
			var windowStart types.LayerID
			if Window > newlyr.Index() {
				windowStart = 0
			} else {
				windowStart = Max(ni.PBase.Layer(), newlyr.Index()-Window+1)
			}

			view := make(map[types.BlockID]struct{})
			lCntr := make(map[types.LayerID]int)
			correctionMap, effCountMap, getCrrEffCnt := ni.getCorrEffCounter()
			foo := func(block *types.Block) (bool, error) {
				view[block.Id()] = struct{}{} //all blocks in view
				lCntr[block.Layer()]++        //amount of blocks for each layer in view
				getCrrEffCnt(block)           //calc correction and eff count
				return false, nil
			}

			tp := ni.TPattern[p]
			ni.ForBlockInView(tp, windowStart, foo)

			//add corrected implicit votes
			ni.updatePatternTally(p, correctionMap, effCountMap)

			//add explicit votes
			addPtrnVt := ni.addPatternVote(p, view)
			for bl := range view {
				addPtrnVt(bl)
			}

			complete := true
			for idx := ni.PBase.Layer(); idx < j; idx++ {
				layer, _ := ni.LayerBlockIds(idx) //todo handle error
				bids := make([]types.BlockID, 0, ni.AvgLayerSize)
				for _, bid := range layer {
					//if bid is not in p's view.
					//add negative vote multiplied by the amount of blocks in the view
					//explicit votes against (not in view )
					if _, found := view[bid]; idx >= ni.PBase.Layer() && !found {
						ni.TTally[p][bid] = sumNodesInView(lCntr, idx+1, p.Layer())
					}

					if val, found := ni.TVote[p]; !found || val == nil {
						ni.TVote[p] = make(map[types.BlockID]vec)
					}

					if vote := globalOpinion(ni.TTally[p][bid], ni.AvgLayerSize, float64(p.LayerID-idx)); vote != Abstain {
						ni.TVote[p][bid] = vote
						if vote == Support {
							bids = append(bids, bid)
						}
					} else {
						ni.TVote[p][bid] = vote
						ni.Debug(" %s no opinion on %s %s %s", p, bid, idx, vote, ni.TTally[p][bid])
						complete = false //not complete
					}
				}

				if idx > ni.PBase.Layer() {
					ni.updatePatSupport(p, bids, idx)
				}
			}

			//update correction vectors after vote count
			ni.updateCorrectionVectors(p, windowStart)

			// update completeness of p
			if _, found := ni.TComplete[p]; complete && !found {
				ni.TComplete[p] = struct{}{}
				ni.PBase = p
				if err := ni.PersistTortoise(); err != nil {
					ni.Error("Tortoise persistence failed %v", err)
				}
				ni.Info("found new complete and good pattern for layer %d pattern %d with %d support ", p.Layer().Uint64(), p.id, ni.TSupport[p])
			}
		}
	}
	ni.With().Info(fmt.Sprintf("Tortoise finished layer in %v", time.Since(start)), log.LayerId(uint64(newlyr.Index())), log.Uint64("pbase", uint64(ni.PBase.Layer())))
	return
}
