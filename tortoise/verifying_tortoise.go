package tortoise

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
)

const MaxExcpetionList = 100

//
//We keep a table that records, for each block, its votes about every previous block
//We keep a vector containing our vote totals (positive and negative) for every previous block
//When a new block arrives, we look up the block it points to in our table,
// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
//We then add the vote difference vector and the explicit vote vector to our vote-totals vector.

type opinion struct {
	id            types.BlockID
	blocksOpinion map[types.BlockID]vec
}

type blockDataProvider interface {
	GetBlock(id types.BlockID) *types.Block
}

type turtle struct {
	bdp          blockDataProvider
	Last         types.LayerID
	avgLayerSize int

	goodBlocks  map[types.BlockID]struct{}
	inputVector map[types.BlockID]vec

	blocksToBlocks      []opinion
	blocksToBlocksIndex map[types.BlockID]int

	totalVotes map[types.BlockID]vec
}

func (t *turtle) init(genesisBlock types.BlockID) {
	t.goodBlocks[genesisBlock] = struct{}{}
}

func convertToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, len(m))
	i := 0
	for b := range m {
		arr[i] = b
		i++
	}
	return types.SortBlockIDs(arr)
}

func (t *turtle) BaseBlock() (types.BlockID, [][]types.BlockID, error) {
	for i := len(t.blocksToBlocks); i < 0; i-- {
		afn, err := t.opinionMatches(t.blocksToBlocks[i])
		if err != nil {
			continue
		}
		return t.blocksToBlocks[i].id, [][]types.BlockID{convertToArray(afn[0]), convertToArray(afn[1]), convertToArray(afn[2])}, nil
	}
	// TODO: special error encoding when exceeding excpetion list size
	return types.BlockID{0}, nil, errors.New("no base block that fits the limit")
}

func (t *turtle) opinionMatches(opinion2 opinion) ([]map[types.BlockID]struct{}, error) {
	a := make(map[types.BlockID]struct{})
	f := make(map[types.BlockID]struct{})
	n := make(map[types.BlockID]struct{})

	for b, o := range opinion2.blocksOpinion {
		totalOpinion, ok := t.totalVotes[b]
		if !ok {
			a[b] = struct{}{}
			continue
		}

		if totalOpinion[0] > totalOpinion[1] && !(o[0] > o[1]) {
			f[b] = struct{}{}
			continue
		}

		if totalOpinion[1] > totalOpinion[0] && !(o[1] > o[0]) {
			a[b] = struct{}{}
		}

		if totalOpinion[0] == totalOpinion[1] && !(o[0] == o[1]) {
			n[b] = struct{}{}
		}

		//TODO - Add orphan blocks as neutral as well, or after hare results add them as for/against.

	}

	if len(a)+len(f)+len(n) > MaxExcpetionList {
		return nil, errors.New(" matches too much exceptions")
	}

	return []map[types.BlockID]struct{}{a, f, n}, nil
}

func (t *turtle) BlockWeight(voting, voted types.BlockID) int {
	return 1
}

func (t *turtle) processBlock(block *types.Block) error {
	baseidx, ok := t.blocksToBlocksIndex[block.BaseBlock]
	if !ok {
		panic("base block not foudn")
	}

	if len(t.blocksToBlocks) < baseidx {
		panic("base block not in array")
	}

	baseBlock := t.blocksToBlocks[baseidx]

	blockid := block.ID()

	for b, v := range baseBlock.blocksOpinion {
		t.totalVotes[b] = t.totalVotes[b].Add(v.Multiply(t.BlockWeight(blockid, b)))
	}

	for _, b := range block.ForDiff {
		t.totalVotes[b] = t.totalVotes[b].Add(support.Multiply(t.BlockWeight(blockid, b)))
	}
	for _, b := range block.AgainstDiff {
		t.totalVotes[b] = t.totalVotes[b].Add(against.Multiply(t.BlockWeight(blockid, b)))
	}

	//TODO: neutral ?

	return nil
}

//HandleIncomingLayer processes all layer block votes
//returns the old pbase and new pbase after taking into account the blocks votes
func (t *turtle) HandleIncomingLayer(newlyr *types.Layer) (types.LayerID, types.LayerID) {

	//start := time.Now()

	if newlyr.Index() > t.Last {
		t.Last = newlyr.Index()
	}

	for _, b := range newlyr.Blocks() {
		err := t.processBlock(b)
		if err != nil {
			panic("something is wrong " + err.Error())
		}
	}

	if newlyr.Index() == genesis {
		// special case genesis ?
	}

markingLoop:
	for _, b := range newlyr.Blocks() {
		if _, good := t.goodBlocks[b.BaseBlock]; !good {
			continue markingLoop
		}
		baseBlock := t.bdp.GetBlock(b.BaseBlock)
		for exfor := range b.ForDiff {
			if t.bdp.GetBlock(b.ForDiff[exfor]).LayerIndex < baseBlock.LayerIndex {
				continue markingLoop
			}
		}

		for exag := range b.AgainstDiff {
			if t.bdp.GetBlock(b.AgainstDiff[exag]).LayerIndex < baseBlock.LayerIndex {
				continue markingLoop
			}
		}

		for exneu := range b.NeutralDiff {
			if t.bdp.GetBlock(b.NeutralDiff[exneu]).LayerIndex < baseBlock.LayerIndex {
				continue markingLoop
			}
		}

		// TODO : Check that it is consistent with the input vector (?)
		vote := globalOpinion(t.totalVotes[b.ID()], t.avgLayerSize, float64(t.Last-b.LayerIndex))

		t.goodBlocks[b.ID()] = struct{}{}
	}

	//		complete := true
	//		for idx := ni.PBase.Layer(); idx < j; idx++ {
	//			layer, _ := ni.db.LayerBlockIds(idx) //todo handle error
	//			bids := make([]types.BlockID, 0, ni.AvgLayerSize)
	//			for _, bid := range layer {
	//				blt := blockIDLayerTuple{BlockID: bid, LayerID: idx}
	//				//if bid is not in p's view.
	//				//add negative vote multiplied by the amount of blocks in the view
	//				//explicit votes against (not in view )
	//				if _, found := view[bid]; idx >= ni.PBase.Layer() && !found {
	//					ni.TTally[p][blt] = sumNodesInView(lCntr, idx+1, p.Layer())
	//				}
	//
	//				if val, found := ni.TVote[p]; !found || val == nil {
	//					ni.TVote[p] = make(map[blockIDLayerTuple]vec)
	//				}
	//
	//				if vote := globalOpinion(ni.TTally[p][blt], ni.AvgLayerSize, float64(p.LayerID-idx)); vote != abstain {
	//					ni.TVote[p][blt] = vote
	//					if vote == support {
	//						bids = append(bids, bid)
	//					}
	//				} else {
	//					ni.TVote[p][blt] = vote
	//					ni.logger.Debug(" %s no opinion on %s %s %s", p, bid, idx, vote, ni.TTally[p][blt])
	//					complete = false //not complete
	//				}
	//			}

	//t.mutex.Lock()
	//defer trtl.mutex.Unlock()
	//oldPbase := trtl.latestComplete()
	//trtl.ninjaTortoise.handleIncomingLayer(ll)
	//newPbase := trtl.latestComplete()
	//updateMetrics(trtl, ll)
	//return oldPbase, newPbase
	//
	//ni.logger.With().Info("tortoise update tables", log.LayerID(uint64(newlyr.Index())), log.Int("n_blocks", len(newlyr.Blocks())))
	//start := time.Now()
	//if newlyr.Index() > ni.Last {
	//	ni.Last = newlyr.Index()
	//}
	//
	//defer ni.evictOutOfPbase()
	//ni.processBlocks(newlyr)
	//
	//if newlyr.Index() == genesis {
	//	ni.handleGenesis(newlyr)
	//	return
	//}
	//
	//l := ni.findMinimalNewlyGoodLayer(newlyr)
	////from minimal newly good pattern to current layer
	////update pattern tally for all good layers
	//for j := l; j > 0 && j < newlyr.Index(); j++ {
	//	p, gfound := ni.TGood[j]
	//	if gfound {
	//
	//		//find bottom of window
	//		windowStart := getBottomOfWindow(newlyr.Index(), ni.PBase.Layer(), ni.Hdist)
	//
	//		//init p's tally to pBase tally
	//		ni.initTallyToBase(ni.PBase, p, windowStart)
	//
	//		view := make(map[types.BlockID]struct{})
	//		lCntr := make(map[types.LayerID]int)
	//		correctionMap, effCountMap, getCrrEffCnt := ni.getCorrEffCounter()
	//		foo := func(block *types.Block) (bool, error) {
	//			view[block.ID()] = struct{}{} //all blocks in view
	//			lCntr[block.Layer()]++        //amount of blocks for each layer in view
	//			getCrrEffCnt(block)           //calc correction and eff count
	//			return false, nil
	//		}
	//
	//		tp := ni.TPattern[p]
	//		ni.db.ForBlockInView(tp, windowStart, foo)
	//
	//		//add corrected implicit votes
	//		ni.updatePatternTally(p, correctionMap, effCountMap)
	//
	//		//add explicit votes
	//		addPtrnVt := ni.addPatternVote(p, view)
	//		for bl := range view {
	//			addPtrnVt(bl)
	//		}
	//
	//		complete := true
	//		for idx := ni.PBase.Layer(); idx < j; idx++ {
	//			layer, _ := ni.db.LayerBlockIds(idx) //todo handle error
	//			bids := make([]types.BlockID, 0, ni.AvgLayerSize)
	//			for _, bid := range layer {
	//				blt := blockIDLayerTuple{BlockID: bid, LayerID: idx}
	//				//if bid is not in p's view.
	//				//add negative vote multiplied by the amount of blocks in the view
	//				//explicit votes against (not in view )
	//				if _, found := view[bid]; idx >= ni.PBase.Layer() && !found {
	//					ni.TTally[p][blt] = sumNodesInView(lCntr, idx+1, p.Layer())
	//				}
	//
	//				if val, found := ni.TVote[p]; !found || val == nil {
	//					ni.TVote[p] = make(map[blockIDLayerTuple]vec)
	//				}
	//
	//				if vote := globalOpinion(ni.TTally[p][blt], ni.AvgLayerSize, float64(p.LayerID-idx)); vote != abstain {
	//					ni.TVote[p][blt] = vote
	//					if vote == support {
	//						bids = append(bids, bid)
	//					}
	//				} else {
	//					ni.TVote[p][blt] = vote
	//					ni.logger.Debug(" %s no opinion on %s %s %s", p, bid, idx, vote, ni.TTally[p][blt])
	//					complete = false //not complete
	//				}
	//			}
	//
	//			if idx > ni.PBase.Layer() {
	//				ni.updatePatSupport(p, bids, idx)
	//			}
	//		}
	//
	//		//update correction vectors after vote count
	//		ni.updateCorrectionVectors(p, windowStart)
	//
	//		// update completeness of p
	//		if _, found := ni.TComplete[p]; complete && !found {
	//			ni.TComplete[p] = struct{}{}
	//			ni.PBase = p
	//			ni.logger.Info("found new complete and good pattern for layer %d pattern %d with %d support ", p.Layer().Uint64(), p.id, ni.TSupport[p])
	//		}
	//	}
	//}
	//ni.logger.With().Info(fmt.Sprintf("tortoise finished layer in %v", time.Since(start)), log.LayerID(uint64(newlyr.Index())), log.Uint64("pbase", uint64(ni.PBase.Layer())))
	//return

}
