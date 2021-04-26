package tortoise

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/syndtr/goleveldb/leveldb"
)

type blockDataProvider interface {
	GetBlock(id types.BlockID) (*types.Block, error)
	LayerBlockIds(l types.LayerID) (ids []types.BlockID, err error)

	GetLayerInputVectorByID(lyrid types.LayerID) ([]types.BlockID, error)
	SaveLayerInputVectorByID(lyrid types.LayerID, vector []types.BlockID) error

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

	AvgLayerSize  int
	MaxExceptions int

	GoodBlocksIndex map[types.BlockID]struct{}

	Verified types.LayerID

	// using the array to be able to iterate from latest elements easily.
	BlocksToBlocks map[types.LayerID]map[types.BlockID]Opinion // records hdist, for each block, its votes about every previous block

	// TODO: Tal says - We keep a vector containing our vote totals (positive and negative) for every previous block
	//	that's not needed here, probably for self healing?
}

// SetLogger sets the Log instance for this turtle
func (t *turtle) SetLogger(log2 log.Log) {
	t.logger = log2
}

// newTurtle creates a new verifying tortoise algorithm instance. XXX: maybe rename?
func newTurtle(bdp blockDataProvider, hdist, avgLayerSize int) *turtle {
	t := &turtle{
		logger:          log.NewDefault("trtl"),
		Hdist:           types.LayerID(hdist),
		bdp:             bdp,
		Last:            0,
		AvgLayerSize:    avgLayerSize,
		GoodBlocksIndex: make(map[types.BlockID]struct{}),
		BlocksToBlocks:  make(map[types.LayerID]map[types.BlockID]Opinion, hdist),
		MaxExceptions:   hdist * avgLayerSize * 100,
	}
	return t
}

func (t *turtle) init(genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	t.logger.With().Debug("Initializing genesis layer for verifying tortoise.", genesisLayer.Index(), genesisLayer.Hash().Field())
	t.BlocksToBlocks[genesisLayer.Index()] = make(map[types.BlockID]Opinion)
	for _, blk := range genesisLayer.Blocks() {
		id := blk.ID()
		t.BlocksToBlocks[genesisLayer.Index()][blk.ID()] = Opinion{
			BlocksOpinion: make(map[types.BlockID]vec),
		}
		t.GoodBlocksIndex[id] = struct{}{}
	}
	t.Last = genesisLayer.Index()
	t.Evict = genesisLayer.Index()
	t.Verified = genesisLayer.Index()
}

// evict makes sure we only keep a window of the last hdist layers.
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
		for blk := range t.BlocksToBlocks[lyr] {
			delete(t.GoodBlocksIndex, blk)
		}
		delete(t.BlocksToBlocks, lyr)
		t.logger.Debug("evict block %v from maps ", lyr)

	}
	t.Evict = window
}

func blockIDsToString(input []types.BlockID) string {
	str := "["
	for i, b := range input {
		str += b.String()
		if i != len(input)-1 {
			str += ","
		}
	}
	str += "]"
	return str
}

// TODO: cache but somehow check for changes (hare that didn't finish finishes..) maybe check hash?
func (t *turtle) getSingleInputVectorFromDB(lyrid types.LayerID, blockid types.BlockID) (vec, error) {
	if lyrid <= types.GetEffectiveGenesis() {
		return support, nil
	}

	input, err := t.bdp.GetLayerInputVectorByID(lyrid)
	if err != nil {
		return abstain, err
	}

	t.logger.With().Debug("got input vector from db", lyrid, log.FieldNamed("query_block", blockid), log.String("input", blockIDsToString(input)))

	for _, bl := range input {
		if bl == blockid {
			return support, nil
		}
	}

	return against, nil
}

func (t *turtle) inputVectorForLayer(lyrBlocks []types.BlockID, input []types.BlockID) map[types.BlockID]vec {
	lyrResult := make(map[types.BlockID]vec, len(lyrBlocks))
	//XXX : input vector must be pointer so we can differentiate
	// no support votes to no results at all.
	if input == nil {
		// hare didn't finish hence we don't have Opinion
		// TODO: get hare Opinion when hare finishes
		for _, b := range lyrBlocks {
			lyrResult[b] = abstain
		}
		return lyrResult
	}

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

func (t *turtle) BaseBlock() (types.BlockID, [][]types.BlockID, error) {
	// Try to find a block counting only good blocks
	for i := t.Last; i > t.Last-t.Hdist; i-- {
		for blk, op := range t.BlocksToBlocks[i] {
			if _, ok := t.GoodBlocksIndex[blk]; !ok {
				t.logger.With().Debug("can't use a not good block", log.FieldNamed("last_layer", t.Last), i, blk)
				continue
			}
			afn, err := t.opinionMatches(i, blk, op)
			if err != nil {
				t.logger.With().Debug("can't use block error %v", log.FieldNamed("last_layer", t.Last), i, blk, log.Err(err))
				continue
			}

			t.logger.With().Info("choose baseblock", log.FieldNamed("last_layer", t.Last), log.FieldNamed("base_block_layer", i), blk, log.Int("against_count", len(afn[0])), log.Int("support_count", len(afn[1])), log.Int("neutral_count", len(afn[2])))

			return blk, [][]types.BlockID{blockMapToArray(afn[0]), blockMapToArray(afn[1]), blockMapToArray(afn[2])}, nil
		}
	}
	// TODO: special error encoding when exceeding excpetion list size
	return types.BlockID{0}, nil, errors.New("no good base block that fits the limit")
}

func (t *turtle) opinionMatches(layerid types.LayerID, blockid types.BlockID, opinion2 Opinion) ([]map[types.BlockID]struct{}, error) {
	// using maps makes it easy to not add duplicates
	againstDiff := make(map[types.BlockID]struct{})
	forDiff := make(map[types.BlockID]struct{})
	neutralDiff := make(map[types.BlockID]struct{})

	// handle genesis
	if layerid == types.GetEffectiveGenesis() {
		for _, i := range types.BlockIDs(mesh.GenesisLayer().Blocks()) {
			forDiff[i] = struct{}{}
		}
		return []map[types.BlockID]struct{}{againstDiff, forDiff, neutralDiff}, nil
	}

	// TODO: maybe we should vote back hdist but drill down check the base blocks ?

	// Add latest layers input vector results to the diff.
	for i := layerid; i <= t.Last; i++ {
		t.logger.With().Debug("checking input vector diffs", i)

		blks, err := t.bdp.LayerBlockIds(i)
		if err != nil {
			if err != leveldb.ErrClosed {
				// todo: empty layer? maybe skip verify differently
				t.logger.Panic("can't find old layer %v", i)
			}
			return nil, err
		}

		res, err := t.bdp.GetLayerInputVectorByID(i)
		if err != nil {
			t.logger.With().Debug("input vector is empty adding neutral diffs", i)
			for _, b := range blks {
				if v, ok := opinion2.BlocksOpinion[b]; !ok || v != abstain {
					t.logger.With().Debug("added diff", log.FieldNamed("base_block_candidate", blockid), log.FieldNamed("diff_block", b), log.String("diff", "neutral"))
					neutralDiff[b] = struct{}{}
				}
			}

			if len(againstDiff)+len(forDiff)+len(neutralDiff) > t.MaxExceptions {
				return nil, errors.New(" matches too much exceptions")
			}
			continue
		}

		inRes := make(map[types.BlockID]struct{})

		for _, b := range res {
			inRes[b] = struct{}{}
			if v, ok := opinion2.BlocksOpinion[b]; !ok || v != support {
				t.logger.With().Debug("added diff", log.FieldNamed("base_block_candidate", blockid), log.FieldNamed("diff_block", b), log.String("diff", "support"))
				forDiff[b] = struct{}{}
			}
		}

		for _, b := range blks {
			if _, ok := inRes[b]; ok {
				continue
			}
			// TODO: maybe we don't need this if it is not included.
			if v, ok := opinion2.BlocksOpinion[b]; !ok || v != against {
				t.logger.With().Debug("added diff", log.FieldNamed("base_block_candidate", blockid), log.FieldNamed("diff_block", b), log.String("diff", "against"))
				againstDiff[b] = struct{}{}
			}
		}
	}

	// return now if already exceed explist
	explen := len(againstDiff) + len(forDiff) + len(neutralDiff)
	if explen > t.MaxExceptions {
		return nil, fmt.Errorf(" matches too much exceptions (%v)", explen)
	}

	return []map[types.BlockID]struct{}{againstDiff, forDiff, neutralDiff}, nil
}

func (t *turtle) BlockWeight(voting, voted types.BlockID) int {
	return 1
}

// Persist saves the current tortoise state to the database
func (t *turtle) persist() error {
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

	t.logger.With().Debug("Processing block", block.Fields()...)
	t.logger.With().Debug("Getting baseblock", block.BaseBlock)

	base, err := t.bdp.GetBlock(block.BaseBlock)
	if err != nil {
		return fmt.Errorf("can't find baseblock")
	}

	t.logger.With().Debug("block supports", types.BlockIdsField(block.BlockHeader.ForDiff))
	t.logger.With().Debug("Checking baseblock", base.Fields()...)

	lyr, ok := t.BlocksToBlocks[base.LayerIndex]
	if !ok {
		return fmt.Errorf("baseblock layer not found %v, %v", block.BaseBlock, base.LayerIndex)
	}

	baseBlockOpinion, ok := lyr[base.ID()]
	if !ok {
		return fmt.Errorf("baseblock not found in layer %v, %v", block.BaseBlock, base.LayerIndex)
	}

	// TODO: save and vote negative on blocks that exceed the max exception list size.
	blockid := block.ID()

	thisBlockOpinions := make(map[types.BlockID]vec)
	for blk, vote := range baseBlockOpinion.BlocksOpinion {
		thisBlockOpinions[blk] = vote
	}

	for _, b := range block.ForDiff {
		thisBlockOpinions[b] = thisBlockOpinions[b].Add(support.Multiply(t.BlockWeight(blockid, b)))
	}
	for _, b := range block.AgainstDiff {
		thisBlockOpinions[b] = thisBlockOpinions[b].Add(against.Multiply(t.BlockWeight(blockid, b)))
	}

	//TODO: neutral ?
	t.logger.With().Debug("Adding block to blocks table", block.Fields()...)
	t.BlocksToBlocks[block.LayerIndex][blockid] = Opinion{BlocksOpinion: thisBlockOpinions}
	return nil
}

// HandleIncomingLayer processes all layer block votes
// returns the old pbase and new pbase after taking into account the blocks votes
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

	t.logger.With().Info("start handling layer", newlyr.Index(), log.Int("blocks", len(newlyr.Blocks())))

	// update tables with blocks
	for _, b := range newlyr.Blocks() {
		_, ok := t.BlocksToBlocks[b.LayerIndex]
		if !ok {
			t.BlocksToBlocks[b.LayerIndex] = make(map[types.BlockID]Opinion, t.AvgLayerSize)
		}
		err := t.processBlock(b)
		if err != nil {
			log.Panic(fmt.Sprintf("something is wrong err:%v", err))
		}
	}

	// Go over all blocks, in order. For block i , mark it “good” if
	blockscount := len(newlyr.Blocks())
	goodblocks := len(t.GoodBlocksIndex)
markingLoop:
	for _, b := range newlyr.Blocks() {
		// (1) the base block is marked as good.
		if _, good := t.GoodBlocksIndex[b.BaseBlock]; !good {
			t.logger.With().Debug("skip marking block as good because baseblock is not good", newlyr.Index(), b.ID(), log.FieldNamed("base_block", b.BaseBlock))
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
				t.logger.Panic("can't find %v from block's %v diff ", exfor, b.ID())
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				t.logger.With().Debug("not adding block to good blocks because it points to block older than base block", b.ID(), log.FieldNamed("older_block", b.ForDiff[exfor]), log.FieldNamed("older_layer", exblk.LayerIndex), log.FieldNamed("base_block_lyr", baseBlock.LayerIndex))
				continue markingLoop
			}
			if v, err := t.getSingleInputVectorFromDB(exblk.LayerIndex, exblk.ID()); err != nil || v != support {
				t.logger.With().Debug("not adding block to good blocks because it votes different from our input vector", b.ID(), log.FieldNamed("voted_block", b.ForDiff[exfor]), log.FieldNamed("older_layer", exblk.LayerIndex), log.String("input_vote", v.String()), log.String("block_vote", "support"), log.Err(err))
				continue markingLoop
			}
		}

		for exag := range b.AgainstDiff {
			exblk, err := t.bdp.GetBlock(b.AgainstDiff[exag])
			if err != nil {
				//todo: dangling pointers?
				t.logger.Panic("can't find %v from block's %v diff ", exag, b.ID())
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				t.logger.With().Debug("not adding block to good blocks because it points to block older than base block", b.ID(), log.FieldNamed("older_block", b.AgainstDiff[exag]), log.FieldNamed("older_layer", exblk.LayerIndex), log.FieldNamed("base_block_lyr", baseBlock.LayerIndex))
				continue markingLoop
			}
			if v, err := t.getSingleInputVectorFromDB(exblk.LayerIndex, exblk.ID()); err != nil || v != against {
				t.logger.With().Debug("not adding block to good blocks because it votes different from our input vector", b.ID(), log.FieldNamed("voted_block", b.AgainstDiff[exag]), log.FieldNamed("older_layer", exblk.LayerIndex), log.String("input_vote", v.String()), log.String("block_vote", "against"), log.Err(err))
				continue markingLoop
			}
		}

		for exneu := range b.NeutralDiff {
			exblk, err := t.bdp.GetBlock(b.NeutralDiff[exneu])
			if err != nil {
				t.logger.Panic("can't find %v from block's %v diff ", exneu, b.ID())
			}
			if exblk.LayerIndex < baseBlock.LayerIndex {
				t.logger.With().Debug("not adding block to good blocks because it points to block older than base block", b.ID(), log.FieldNamed("older_block", b.NeutralDiff[exneu]), log.FieldNamed("older_layer", exblk.LayerIndex), log.FieldNamed("base_block_lyr", baseBlock.LayerIndex))
				continue markingLoop
			}
			if v, err := t.getSingleInputVectorFromDB(exblk.LayerIndex, exblk.ID()); err != nil || v != abstain {
				t.logger.With().Debug("not adding block to good blocks because it votes different from our input vector", b.ID(), log.FieldNamed("voted_block", b.NeutralDiff[exneu]), log.FieldNamed("older_layer", exblk.LayerIndex), log.String("input_vote", v.String()), log.String("block_vote", "neutral"), log.Err(err))
				continue markingLoop
			}
		}
		t.logger.With().Debug("marking good block", b.ID(), b.LayerIndex)
		t.GoodBlocksIndex[b.ID()] = struct{}{}
	}

	t.logger.With().Info("finished marking good blocks", log.Int("total_blocks", blockscount), log.Int("good_blocks", len(t.GoodBlocksIndex)-goodblocks))

	idx := newlyr.Index()
	wasVerified := t.Verified
	t.logger.With().Info("starting layer verification", log.FieldNamed("was_verified", wasVerified), log.FieldNamed("target_verification", newlyr.Index()))
	i := wasVerified + 1
loop:
	for ; i < idx; i++ {
		t.logger.With().Info("verifying layer", i)

		blks, err := t.bdp.LayerBlockIds(i)
		if err != nil {
			t.logger.With().Warning("can't find layer in db, skipping verification", i)
			continue // Panic? can't get layer
		}

		var input map[types.BlockID]vec

		if i == idx {
			input = t.inputVectorForLayer(blks, inputVector)
		} else {
			raw, err := t.bdp.GetLayerInputVectorByID(i)
			if err != nil {
				// this sets the input to abstain
				t.logger.With().Warning("input vector abstains on all blocks", i)
			}
			input = t.inputVectorForLayer(blks, raw)
		}

		if len(input) == 0 {
			t.logger.With().Warning("no blocks in input vector can't verify", i)
			break
		}

		contextualValidity := make(map[types.BlockID]bool, len(blks))

		// Count good blocks votes..
		// Declare the vote vector “verified” up to position k if the total weight exceeds the confidence threshold in all positions up to k .
		for blk, vote := range input {
			// Count the votes for the input vote vector by summing the weight of the good blocks
			sum := abstain
			//t.logger.Info("counting votes for block %v", blk)
			for j := t.Last; j > i && j-i < t.Hdist; j-- {
				// check if the block is good
				for bid, op := range t.BlocksToBlocks[j] {
					_, isgood := t.GoodBlocksIndex[bid]
					if !isgood {
						t.logger.With().Debug("block not good hence not counting", log.FieldNamed("voting_block", bid), log.FieldNamed("voted_block", blk))
						continue
					}

					// check if this block has Opinion on this block. (TODO: maybe doesn't have Opinion means AGAINST?)
					opinionVote, ok := op.BlocksOpinion[blk]
					if !ok {
						t.logger.With().Debug("no opinion on block", log.FieldNamed("voting_block", bid), log.FieldNamed("voted_block", blk))
						continue
					}

					t.logger.With().Debug("adding block opinion to vote sum", log.FieldNamed("voting_block", bid), log.FieldNamed("voted_block", blk),
						log.String("vote", opinionVote.String()), log.String("sum", fmt.Sprintf("[%v, %v]", sum[0], sum[1])))
					sum = sum.Add(opinionVote.Multiply(t.BlockWeight(bid, blk)))
				}
			}

			// check that the total weight exceeds the confidence threshold in all positions up
			threshold := float64(globalThreshold*float64(i-wasVerified)) * float64(t.AvgLayerSize)
			t.logger.With().Debug("global opinion", sum, log.String("threshold", fmt.Sprint(threshold)))
			gop := globalOpinion(sum, t.AvgLayerSize, float64(i-wasVerified))
			t.logger.With().Debug("calculated global opinion on block", log.FieldNamed("voted_block", blk), i, log.String("global_opinion", gop.String()), log.String("sum", fmt.Sprintf("[%v, %v]", sum[0], sum[1])))
			if gop != vote {
				// TODO: trigger self healing after a while ?
				t.logger.With().Warning("global opinion is different from vote", log.String("global_opinion", gop.String()), log.String("vote", vote.String()))
				break loop
			}

			if gop == abstain {
				t.logger.With().Warning("global opinion on a block is abstain hence can't verify layer", log.String("global_opinion", gop.String()), log.String("vote", vote.String()))
				break loop
			}

			contextualValidity[blk] = gop == support
		}

		//Declare the vote vector “verified” up to position k.
		for blk, v := range contextualValidity {
			if err := t.bdp.SaveContextualValidity(blk, v); err != nil {
				// panic?
				t.logger.With().Error("error saving contextual validity on block", blk.Field(), log.Err(err))
			}
		}
		t.Verified = i
		t.logger.With().Info("verified layer", i)

	}

	return wasVerified, t.Verified
}
