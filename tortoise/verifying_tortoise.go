package tortoise

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
)

type blockDataProvider interface {
	GetBlock(id types.BlockID) (*types.Block, error)
	LayerBlockIds(l types.LayerID) (ids []types.BlockID, err error)

	GetLayerInputVector(lyrid types.LayerID) ([]types.BlockID, error)
	SaveLayerInputVector(lyrid types.LayerID, vector []types.BlockID) error

	SaveContextualValidity(id types.BlockID, valid bool) error

	Persist(key []byte, v interface{}) error
	Retrieve(key []byte, v interface{}) (interface{}, error)
}

func blockMapToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, len(m))
	i := 0
	for b := range m {
		arr[i] = b
		i++
	}
	return types.SortBlockIDs(arr)
}

type turtle struct {
	logger log.Log

	bdp blockDataProvider

	Last  types.LayerID
	Hdist types.LayerID
	Evict types.LayerID

	avgLayerSize  int
	maxExceptions int

	GoodBlocksIndex map[types.BlockID]struct{}

	Verified types.LayerID

	// using the array to be able to iterate from latest elements easily.
	BlocksToBlocks []opinion // records hdist, for each block, its votes about every previous block
	// Use this to be able to lookup blocks opinion without iterating the array.
	BlocksToBlocksIndex map[types.BlockID]int

	// TODO: Tal says - We keep a vector containing our vote totals (positive and negative) for every previous block
	//	that's not needed here, probably for self healing?
}

func (t *turtle) SetLogger(log2 log.Log) {
	t.logger = log2
}

// newTurtle creates a new verifying tortoise algorithm instance. XXX: maybe rename?
func newTurtle(bdp blockDataProvider, hdist, avgLayerSize int) *turtle {
	t := &turtle{
		logger:              log.NewDefault("trtl"),
		Hdist:               types.LayerID(hdist),
		bdp:                 bdp,
		Last:                0,
		avgLayerSize:        avgLayerSize,
		GoodBlocksIndex:     make(map[types.BlockID]struct{}),
		BlocksToBlocks:      make([]opinion, 0, 1),
		BlocksToBlocksIndex: make(map[types.BlockID]int),
		maxExceptions:       hdist * avgLayerSize,
	}
	return t
}

func (t *turtle) init(genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	t.logger.With().Debug("Initializing genesis layer for verifying tortoise.", genesisLayer.Index(), genesisLayer.Hash().Field("hash"))
	for i, blk := range genesisLayer.Blocks() {
		id := blk.ID()
		t.BlocksToBlocks = append(t.BlocksToBlocks, opinion{
			blockIDLayerTuple: blockIDLayerTuple{
				id,
				genesisLayer.Index(),
			},
			blocksOpinion: make(map[types.BlockID]vec),
		})
		t.BlocksToBlocksIndex[blk.ID()] = i
		t.GoodBlocksIndex[id] = struct{}{}
	}
	t.Last = genesisLayer.Index()
	t.Evict = genesisLayer.Index()
	t.Verified = genesisLayer.Index()
}

func (t *turtle) evict() {
	// Don't evict before we've Verified more than hdist
	// TODO: fix potential leak when we can't verify but keep receiving layers

	if t.Verified <= types.GetEffectiveGenesis()+t.Hdist {
		return
	}
	// The window is the last [Verified - hdist] layers.
	window := t.Verified - t.Hdist
	t.logger.Info("Window starts %v", window)
	// evict from last evicted to the beginning of our window.
	for lyr := t.Evict; lyr < window; lyr++ {
		t.logger.Info("removing lyr %v", lyr)
		ids, err := t.bdp.LayerBlockIds(lyr)
		if err != nil {
			t.logger.With().Error("could not get layer ids for layer ", lyr, log.Err(err))
			continue
		}
		for _, id := range ids {
			idx, ok := t.BlocksToBlocksIndex[id]
			if !ok {
				continue
			}
			delete(t.BlocksToBlocksIndex, id)
			t.BlocksToBlocks = removeOpinion(t.BlocksToBlocks, idx)
			// FIX indexes
			for i, op := range t.BlocksToBlocks {
				t.BlocksToBlocksIndex[op.BlockID] = i
			}
			delete(t.GoodBlocksIndex, id)
			t.logger.Debug("evict block %v from maps ", id)
		}
	}
	t.Evict = window
}

func removeOpinion(o []opinion, i int) []opinion {
	return append(o[:i], o[i+1:]...)
}

func (t *turtle) singleInputVectorFromDB(lyrid types.LayerID, blockid types.BlockID) (vec, error) {
	if lyrid <= types.GetEffectiveGenesis() {
		return support, nil
	}

	input, err := t.bdp.GetLayerInputVector(lyrid)
	if err != nil {
		return abstain, err
	}

	t.logger.Debug("layer input vector returned from db for lyr %v - %v. querying for %v", lyrid, input, blockid)

	for _, bl := range input {
		if bl == blockid {
			return support, nil
		}
	}

	return against, nil
}

func (t *turtle) inputVectorForLayer(lyrBlocks []types.BlockID, inputvector *[]types.BlockID) map[types.BlockID]vec {
	lyrResult := make(map[types.BlockID]vec, len(lyrBlocks))
	//XXX : input vector must be pointer so we can differentiate
	// no support votes to no results at all.
	if inputvector == nil {
		// hare didn't finish hence we don't have opinion
		// TODO: get hare opinion when hare finishes
		for _, b := range lyrBlocks {
			lyrResult[b] = abstain
		}
		return lyrResult
	}

	input := *inputvector
	for _, b := range input {
		lyrResult[b] = support
	}

	for _, b := range lyrBlocks {
		if _, ok := lyrResult[b]; !ok {
			lyrResult[b] = against
		}
	}
	return lyrResult
}

func (t *turtle) BaseBlock(getres func(id types.LayerID) ([]types.BlockID, error)) (types.BlockID, [][]types.BlockID, error) {
	// Try to find a block counting only good blocks
	for i := len(t.BlocksToBlocks) - 1; i >= 0; i-- {
		if _, ok := t.GoodBlocksIndex[t.BlocksToBlocks[i].BlockID]; !ok {
			continue
		}
		afn, err := t.opinionMatches(t.BlocksToBlocks[i].LayerID, t.BlocksToBlocks[i], getres)
		if err != nil {
			continue
		}
		t.logger.Info("Chose baseblock %v against: %v, for: %v, neutral: %v", t.BlocksToBlocks[i].BlockID, len(afn[0]), len(afn[1]), len(afn[2]))
		return t.BlocksToBlocks[i].BlockID, [][]types.BlockID{blockMapToArray(afn[0]), blockMapToArray(afn[1]), blockMapToArray(afn[2])}, nil
	}
	// TODO: special error encoding when exceeding excpetion list size
	return types.BlockID{0}, nil, errors.New("no base block that fits the limit")
}

func (t *turtle) opinionMatches(layerid types.LayerID, opinion2 opinion, getres func(id types.LayerID) ([]types.BlockID, error)) ([]map[types.BlockID]struct{}, error) {
	// using maps makes it easy to not add duplicates
	a := make(map[types.BlockID]struct{})
	f := make(map[types.BlockID]struct{})
	n := make(map[types.BlockID]struct{})

	// handle genesis
	if layerid == types.GetEffectiveGenesis() {
		for _, i := range types.BlockIDs(mesh.GenesisLayer().Blocks()) {
			f[i] = struct{}{}
		}
		return []map[types.BlockID]struct{}{a, f, n}, nil
	}

	// Check how much of this block's opinions corresponds with the input
	//   vote vector and add the differences if there are and they not exceed the diff list limit.

	for b, o := range opinion2.blocksOpinion {
		bl, err := t.bdp.GetBlock(b)
		if err != nil {
			return nil, err
		}

		inputVote, err := t.singleInputVectorFromDB(bl.LayerIndex, b)
		if err != nil {
			return nil, err
		}
		t.logger.Debug("looking on %v vote for block %v in layer %v", opinion2.BlockID, b, layerid)

		if inputVote == simplifyVote(o) {
			t.logger.Debug("no old diff to %v", b)
			continue
		}

		if inputVote == against && simplifyVote(o) != against {
			t.logger.Debug("added diff %v to against", b)
			a[b] = struct{}{}
			continue
		}

		if inputVote == support && simplifyVote(o) != support {
			t.logger.Debug("added diff %v to support", b)
			f[b] = struct{}{}
			continue
		}

		if inputVote == abstain && simplifyVote(o) != abstain {
			t.logger.Debug("added diff %v to neutral", b)
			n[b] = struct{}{}
			continue
		}
	}

	// return now if already exceed explist
	if len(a)+len(f)+len(n) > t.maxExceptions {
		return nil, errors.New(" matches too much exceptions")
	}

	// TODO: maybe we should vote back hdist but drill down check the base blocks ?

	// Add latest layers input vector results to the diff.
	for i := layerid; i <= t.Last; i++ {
		t.logger.Debug("checking input vector results on lyr %v", i)

		blks, err := t.bdp.LayerBlockIds(i)
		if err != nil {
			panic(fmt.Sprintf(" database err or layer not exist. %v", i))
		}

		res, err := getres(i)
		if err != nil {
			for _, b := range blks {
				if v, ok := opinion2.blocksOpinion[b]; !ok || v != abstain {
					t.logger.Debug("added diff %v to neutral", b)
					n[b] = struct{}{}
				}
			}

			if len(a)+len(f)+len(n) > t.maxExceptions {
				return nil, errors.New(" matches too much exceptions")
			}
			continue
		}

		inRes := make(map[types.BlockID]struct{})

		for _, b := range res {
			inRes[b] = struct{}{}
			if v, ok := opinion2.blocksOpinion[b]; !ok || v != support {
				t.logger.Debug("added diff %v to support", b)
				f[b] = struct{}{}
			}
		}

		for _, b := range blks {
			if _, ok := inRes[b]; ok {
				continue
			}
			// TODO: maybe we don't need this if it is not included.
			if v, ok := opinion2.blocksOpinion[b]; !ok || v != against {
				t.logger.Debug("added diff %v to against", b)

				a[b] = struct{}{}
			}
		}
	}

	if len(a)+len(f)+len(n) > t.maxExceptions {
		return nil, errors.New(" matches too much exceptions")
	}

	return []map[types.BlockID]struct{}{a, f, n}, nil
}

func (t *turtle) BlockWeight(voting, voted types.BlockID) int {
	return 1
}

//func (t *turtle) saveOpinion() error {
//	for i := t.Verified - t.Hdist; i < t.Verified; i++ {
//
//			if err := t.bdp.SaveContextualValidity(blk, voteFromVec(v).Bytes()); err != nil {
//				return err
//			}
//			if simplifyVote(v) == support {
//				events.Publish(events.ValidBlock{ID: blk.String(), Valid: true})
//			} else {
//				t.logger.With().Warning("block is contextually invalid", blk.Field(), log.String("vote", v.String()))
//			}
//		}
//	}
//	return nil
//}

//Persist saves the current tortoise state to the database
func (t *turtle) persist() error {
	//if err := t.saveOpinion(); err != nil {
	//	return err
	//}
	return t.bdp.Persist(mesh.TORTOISE, t)
}

// RecoverVerifyingTortoise retrieve latest saved tortoise from the database
func RecoverVerifyingTortoise(mdb retriever) (interface{}, error) {
	return mdb.Retrieve(mesh.TORTOISE, &turtle{})
}

func (t *turtle) processBlock(block *types.Block) error {

	//When a new block arrives, we look up the block it points to in our table,
	// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
	//We then add the vote difference vector and the explicit vote vector to our vote-totals vector.
	baseidx, ok := t.BlocksToBlocksIndex[block.BaseBlock]
	if !ok {
		return fmt.Errorf("base block not found %v", block.BaseBlock)
	}

	if len(t.BlocksToBlocks) < baseidx {
		return fmt.Errorf("array is less than baseblock id %v idx:%v", block.BaseBlock, baseidx)
	}

	// TODO: save and vote negative on blocks that exceed the max exception list size.

	baseBlockOpinion := t.BlocksToBlocks[baseidx]
	blockid := block.ID()

	thisBlockOpinions := make(map[types.BlockID]vec)
	for blk, vote := range baseBlockOpinion.blocksOpinion {
		thisBlockOpinions[blk] = vote
	}

	for _, b := range block.ForDiff {
		thisBlockOpinions[b] = thisBlockOpinions[b].Add(support.Multiply(t.BlockWeight(blockid, b)))
	}
	for _, b := range block.AgainstDiff {
		thisBlockOpinions[b] = thisBlockOpinions[b].Add(against.Multiply(t.BlockWeight(blockid, b)))
	}

	//TODO: neutral ?

	t.BlocksToBlocks = append(t.BlocksToBlocks, opinion{blockIDLayerTuple: blockIDLayerTuple{
		BlockID: blockid,
		LayerID: block.LayerIndex,
	}, blocksOpinion: thisBlockOpinions})
	t.BlocksToBlocksIndex[blockid] = len(t.BlocksToBlocks) - 1

	return nil
}

//HandleIncomingLayer processes all layer block votes
//returns the old pbase and new pbase after taking into account the blocks votes
func (t *turtle) HandleIncomingLayer(newlyr *types.Layer, inputVector []types.BlockID) (types.LayerID, types.LayerID) {

	if t.Last < newlyr.Index() {
		t.Last = newlyr.Index()
	}

	if len(newlyr.Blocks()) == 0 {
		return t.Verified, t.Verified // todo: something else to do on empty layer?
	}

	if newlyr.Index() <= types.GetEffectiveGenesis() {
		return t.Verified, t.Verified
	}
	// TODO: handle late blocks

	defer t.evict()

	// update tables with blocks
	for _, b := range newlyr.Blocks() {
		err := t.processBlock(b)
		if err != nil {
			panic("something is wrong " + err.Error())
		}
	}

	// Go over all blocks, in order. For block i , mark it “good” if
markingLoop:
	for _, b := range newlyr.Blocks() {
		// (1) the base block is marked as good.
		if _, good := t.GoodBlocksIndex[b.BaseBlock]; !good {
			t.logger.Debug("not adding %v to good blocks because baseblock %v is not good", b.ID(), b.BaseBlock)
			continue markingLoop
		}

		baseBlock, err := t.bdp.GetBlock(b.BaseBlock)
		if err != nil {
			panic(fmt.Sprint("block not found ", b.BaseBlock, "err", err))
		}

		// (2) all diffs appear after the block base block and consistent with the input vote vector.

		for exfor := range b.ForDiff {
			exblk, err := t.bdp.GetBlock(b.ForDiff[exfor])
			if err != nil {
				panic(fmt.Sprint("err , ", err))
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				t.logger.Debug("not adding %v to good blocks because it points to block %v in old layer %v baseblock_lyr:%v", b.ID(), b.ForDiff[exfor], exblk.LayerIndex, baseBlock.LayerIndex)
				continue markingLoop
			}
			if v, err := t.singleInputVectorFromDB(exblk.LayerIndex, exblk.ID()); err != nil || v != support {
				t.logger.Debug("not adding %v to good blocks because it votes different from out input on %v, err:%v, blockvote:%v, ourvote: %v ", b.ID(), b.ForDiff[exfor], err, "for", v)
				continue markingLoop
			}
		}

		for exag := range b.AgainstDiff {
			exblk, err := t.bdp.GetBlock(b.AgainstDiff[exag])
			if err != nil {
				panic(fmt.Sprint("err , ", err))
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				t.logger.Debug("not adding %v to good blocks because it points to block %v in old layer %v baseblock_lyr:%v", b.ID(), b.ForDiff[exag], exblk.LayerIndex, baseBlock.LayerIndex)
				continue markingLoop
			}
			if v, err := t.singleInputVectorFromDB(exblk.LayerIndex, exblk.ID()); err != nil || v != against {
				t.logger.Debug("not adding %v to good blocks because it votes different from out input on %v, err:%v, blockvote:%v, ourvote: %v ", b.ID(), b.AgainstDiff[exag], err, "against", v)
				continue markingLoop
			}
		}

		for exneu := range b.NeutralDiff {
			exblk, err := t.bdp.GetBlock(b.NeutralDiff[exneu])
			if err != nil {
				panic(fmt.Sprint("err , ", err))
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				t.logger.Debug("not adding %v to good blocks because it points to block %v in old layer %v baseblock_lyr:%v", b.ID(), b.ForDiff[exneu], exblk.LayerIndex, baseBlock.LayerIndex)
				continue markingLoop
			}
			if v, err := t.singleInputVectorFromDB(exblk.LayerIndex, exblk.ID()); err != nil || v != abstain {
				t.logger.Debug("not adding %v to good blocks because it votes different from out input on %v, err:%v, blockvote:%v, ourvote: %v ", b.ID(), b.NeutralDiff[exneu], err, "neutral", v)
				continue markingLoop
			}
		}
		t.logger.Debug("marking %v of layer %v as good", b.ID(), b.LayerIndex)
		t.GoodBlocksIndex[b.ID()] = struct{}{}
	}

	idx := newlyr.Index()
	wasVerified := t.Verified
	t.logger.Info("Trying to advance from layer %v to %v", wasVerified, newlyr.Index())
	i := wasVerified + 1
loop:
	for ; i < idx; i++ {
		t.logger.Info("Verifying layer %v", i)

		blks, err := t.bdp.LayerBlockIds(i)
		if err != nil {
			continue // Panic? can't get layer
		}

		var input map[types.BlockID]vec

		if i == newlyr.Index() {
			input = t.inputVectorForLayer(blks, &inputVector)
		} else {
			raw, err := t.bdp.GetLayerInputVector(i)
			if err != nil {
				// this sets the input to abstain
				raw = nil
			}
			input = t.inputVectorForLayer(blks, &raw)
		}

		if len(input) == 0 {
			break
		}

		// Count good blocks votes..
		// Declare the vote vector “verified” up to position k if the total weight exceeds the confidence threshold in all positions up to k .
		for blk, vote := range input {
			// Count the votes for the input vote vector by summing the weight of the good blocks
			sum := abstain
			//t.logger.Info("counting votes for block %v", blk)
			for _, vopinion := range t.BlocksToBlocks {

				t.logger.Debug("Checking %v opinion on %v", vopinion.BlockID, blk)

				// blocks older than the voted block are not counted
				if vopinion.LayerID <= i {
					t.logger.Debug("%v is not newer than %v", vopinion.BlockID, blk)
					continue
				}

				// check if this block has opinion on this block. (TODO: maybe doesn't have opinion means AGAINST?)
				opinionVote, ok := vopinion.blocksOpinion[blk]
				if !ok {
					t.logger.Debug("%v has no opinion on %v", vopinion.BlockID, blk)
					continue
				}

				// check if the block is good
				_, isgood := t.GoodBlocksIndex[vopinion.BlockID]
				if !isgood {
					t.logger.Debug("%v is not good hence not counting", vopinion.BlockID)
					continue
				}

				t.logger.Debug("adding %v opinion = %v to the vote sum on %v", vopinion.BlockID, opinionVote, blk)
				sum = sum.Add(opinionVote.Multiply(t.BlockWeight(vopinion.BlockID, blk)))
			}

			// check that the total weight exceeds the confidence threshold in all positions up
			gop := globalOpinion(sum, t.avgLayerSize, float64(i-wasVerified))
			t.logger.Info("Global opinion on blk %v (lyr:%v) is %v (from:%v)", blk, i, gop, sum)
			if gop != vote || gop == abstain {
				// TODO: trigger self healing after a while ?
				t.logger.Warning("The global opinion is different from vote, global: %v, vote: %v", gop, vote)
				break loop
			}

			// Just verified that layer, save contextual validity for hare beacon. (TODO: revisit)
			if err := t.bdp.SaveContextualValidity(blk, gop == support); err != nil {
				// panic?
				t.logger.With().Error("Error saving contextual validity on block", blk.Field(), log.Err(err))
			}
		}

		//Declare the vote vector “verified” up to position k.
		t.Verified = i
		t.logger.Info("Verified layer %v", i)

	}

	return wasVerified, t.Verified
}
