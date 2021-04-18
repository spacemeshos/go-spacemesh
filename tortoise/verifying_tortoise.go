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

	AvgLayerSize  int
	MaxExceptions int

	GoodBlocksIndex map[types.BlockID]struct{}

	Verified types.LayerID

	// use 2D array to be able to iterate from latest elements easily
	BlockOpinionsByLayer map[types.LayerID]map[types.BlockID]Opinion // records hdist, for each block, its votes about every
	// previous block
	// Use this to be able to lookup blocks Opinion without iterating the array.
	BlocksToBlocksIndex map[types.BlockID]int

	// TODO: Tal says: We keep a vector containing our vote totals (positive and negative) for every previous block
	// that's not needed here, probably for self healing?
}

// SetLogger sets the Log instance for this turtle
func (t *turtle) SetLogger(log2 log.Log) {
	t.logger = log2
}

// newTurtle creates a new verifying tortoise algorithm instance. XXX: maybe rename?
func newTurtle(bdp blockDataProvider, hdist, avgLayerSize int) *turtle {
	t := &turtle{
		logger:               log.NewDefault("trtl"),
		Hdist:                types.LayerID(hdist),
		bdp:                  bdp,
		Last:                 0,
		AvgLayerSize:         avgLayerSize,
		GoodBlocksIndex:      make(map[types.BlockID]struct{}),
		BlockOpinionsByLayer: make(map[types.LayerID]map[types.BlockID]Opinion, hdist),
		BlocksToBlocksIndex:  make(map[types.BlockID]int),
		MaxExceptions:        hdist * avgLayerSize * 100,
	}
	return t
}

func (t *turtle) init(genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	t.logger.With().Debug("initializing genesis layer for verifying tortoise",
		genesisLayer.Index(),
		genesisLayer.Hash().Field())
	t.BlockOpinionsByLayer[genesisLayer.Index()] = make(map[types.BlockID]Opinion)
	for _, blk := range genesisLayer.Blocks() {
		id := blk.ID()
		t.BlockOpinionsByLayer[genesisLayer.Index()][blk.ID()] = Opinion{
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
	// Don't evict before we've verified more than hdist
	// TODO: fix potential leak when we can't verify but keep receiving layers

	if t.Verified <= types.GetEffectiveGenesis()+t.Hdist {
		return
	}
	// The window is the last [Verified - hdist] layers.
	window := t.Verified - t.Hdist
	t.logger.Info("window starts %v", window)
	// evict from last evicted to the beginning of our window.
	for lyr := t.Evict; lyr < window; lyr++ {
		t.logger.Info("removing layer %v", lyr)
		for blk := range t.BlockOpinionsByLayer[lyr] {
			delete(t.GoodBlocksIndex, blk)
		}
		delete(t.BlockOpinionsByLayer, lyr)
		t.logger.Debug("evict block %v from maps", lyr)

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

	input, err := t.bdp.GetLayerInputVector(lyrid)
	if err != nil {
		return abstain, err
	}

	t.logger.With().Debug("got input vector from db",
		lyrid,
		log.FieldNamed("query_block", blockid),
		log.String("input", blockIDsToString(input)))

	for _, bl := range input {
		if bl == blockid {
			return support, nil
		}
	}

	return against, nil
}

func (t *turtle) checkBlockAndGetInputVector(exceptionBlockid, sourceBlockid types.BlockID, layerIndex types.LayerID) (vec, *types.Block, bool) {
	if exceptionBlock, err := t.bdp.GetBlock(exceptionBlockid); err != nil {
		t.logger.Panic("can't find %v from block's %v diff ", exceptionBlockid, sourceBlockid)
	} else if exceptionBlock.LayerIndex < layerIndex {
		t.logger.With().Debug("not marking block good because it points to block older than base block",
			sourceBlockid,
			log.FieldNamed("older_block", exceptionBlockid),
			log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
			log.FieldNamed("base_block_lyr", layerIndex))
	} else if v, err := t.getSingleInputVectorFromDB(exceptionBlock.LayerIndex, exceptionBlockid); err != nil {
		t.logger.With().Error("unable to get single input vector for exception block",
			sourceBlockid,
			log.FieldNamed("older_block", exceptionBlockid),
			log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
			log.FieldNamed("base_block_lyr", layerIndex))
	} else {
		return v, exceptionBlock, true
	}
	return abstain, &types.Block{}, false
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
		for block, opinion := range t.BlockOpinionsByLayer[i] {
			if _, ok := t.GoodBlocksIndex[block]; !ok {
				t.logger.With().Debug("skipping block not marked good",
					log.FieldNamed("last_layer", t.Last),
					i,
					block)
				continue
			}

			// Calculate the set of exceptions
			exceptionVectorMap, err := t.calculateExceptions(i, block, opinion)
			if err != nil {
				t.logger.With().Debug("can't use block: error %v",
					log.FieldNamed("last_layer", t.Last),
					i,
					block,
					log.Err(err))
				continue
			}

			t.logger.With().Info("chose baseblock",
				log.FieldNamed("last_layer", t.Last),
				log.FieldNamed("base_block_layer", i),
				block,
				log.Int("against_count", len(exceptionVectorMap[0])),
				log.Int("support_count", len(exceptionVectorMap[1])),
				log.Int("neutral_count", len(exceptionVectorMap[2])))

			return block, [][]types.BlockID{
				blockMapToArray(exceptionVectorMap[0]),
				blockMapToArray(exceptionVectorMap[1]),
				blockMapToArray(exceptionVectorMap[2]),
			}, nil
		}
	}
	// TODO: special error encoding when exceeding exception list size
	return types.BlockID{0}, nil, errors.New("no good base block within exception vector limit")
}

func (t *turtle) calculateExceptions(layerid types.LayerID, blockid types.BlockID, opinion2 Opinion) ([]map[types.BlockID]struct{}, error) {
	// using maps prevents duplicates
	againstDiff := make(map[types.BlockID]struct{})
	forDiff := make(map[types.BlockID]struct{})
	neutralDiff := make(map[types.BlockID]struct{})

	// all genesis blocks get a for vote
	if layerid == types.GetEffectiveGenesis() {
		for _, i := range types.BlockIDs(mesh.GenesisLayer().Blocks()) {
			forDiff[i] = struct{}{}
		}
		return []map[types.BlockID]struct{}{againstDiff, forDiff, neutralDiff}, nil
	}

	// TODO: maybe we should vote back hdist but drill down check the base blocks

	// Add latest layers input vector results to the diff.
	for i := layerid; i <= t.Last; i++ {
		t.logger.With().Debug("checking input vector diffs", i)

		layerBlockIds, err := t.bdp.LayerBlockIds(i)
		if err != nil {
			if err != leveldb.ErrClosed {
				// todo: empty layer? maybe skip verify differently
				t.logger.Panic("can't find old layer %v", i)
			}
			return nil, err
		}

		// TODO: how to differentiate between missing Hare results, and Hare failed/gave up waiting?
		layerInputVector, err := t.bdp.GetLayerInputVector(i)
		if err != nil {
			t.logger.With().Debug("input vector is empty, adding neutral diffs", i, log.Err(err))
			for _, b := range layerBlockIds {
				if v, ok := opinion2.BlocksOpinion[b]; !ok || v != abstain {
					t.logger.With().Debug("added diff",
						log.FieldNamed("base_block_candidate", blockid),
						log.FieldNamed("diff_block", b),
						log.String("diff", "neutral"))
					neutralDiff[b] = struct{}{}
				}
			}
			continue
		}

		inInputVector := make(map[types.BlockID]struct{})

		for _, b := range layerInputVector {
			inInputVector[b] = struct{}{}
			if v, ok := opinion2.BlocksOpinion[b]; !ok || v != support {
				// Block is in input vector but no base block vote or base block doesn't support it:
				// add diff FOR this block
				t.logger.With().Debug("added diff",
					log.FieldNamed("base_block_candidate", blockid),
					log.FieldNamed("diff_block", b),
					log.String("diff", "support"))
				forDiff[b] = struct{}{}
			}
		}

		for _, b := range layerBlockIds {
			// Matches input vector, no diff (i.e., both support)
			if _, ok := inInputVector[b]; ok {
				continue
			}

			// TODO: maybe we don't need this if it is not included
			if v, ok := opinion2.BlocksOpinion[b]; !ok || v != against {
				// Layer block has no base block vote or base block supports, but not in input vector:
				// add diff AGAINST this block
				t.logger.With().Debug("added diff",
					log.FieldNamed("base_block_candidate", blockid),
					log.FieldNamed("diff_block", b),
					log.String("diff", "against"))
				againstDiff[b] = struct{}{}
			}
		}
	}

	// check if exceeded max no. exceptions
	explen := len(againstDiff) + len(forDiff) + len(neutralDiff)
	if explen > t.MaxExceptions {
		return nil, fmt.Errorf("too many exceptions to base block vote (%v)", explen)
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
	// When a new block arrives, we look up the block it points to in our table,
	// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
	// We then add the vote difference vector and the explicit vote vector to our vote-totals vector.
	t.logger.With().Debug("processing block", block.Fields()...)
	t.logger.With().Debug("getting baseblock", block.BaseBlock)

	base, err := t.bdp.GetBlock(block.BaseBlock)
	if err != nil {
		return fmt.Errorf("can't find baseblock")
	}

	t.logger.With().Debug("block supports", types.BlockIdsField(block.BlockHeader.ForDiff))
	t.logger.With().Debug("checking baseblock", base.Fields()...)

	layerOpinions, ok := t.BlockOpinionsByLayer[base.LayerIndex]
	if !ok {
		return fmt.Errorf("baseblock layer not found %v, %v", block.BaseBlock, base.LayerIndex)
	}

	baseBlockOpinion, ok := layerOpinions[base.ID()]
	if !ok {
		return fmt.Errorf("baseblock not found in layer %v, %v", block.BaseBlock, base.LayerIndex)
	}

	// TODO: save and vote against blocks that exceed the max exception list size (DoS prevention)
	blockid := block.ID()

	opinion := make(map[types.BlockID]vec)
	for blk, vote := range baseBlockOpinion.BlocksOpinion {
		opinion[blk] = vote
	}

	for _, b := range block.ForDiff {
		opinion[b] = opinion[b].Add(support.Multiply(t.BlockWeight(blockid, b)))
	}
	for _, b := range block.AgainstDiff {
		opinion[b] = opinion[b].Add(against.Multiply(t.BlockWeight(blockid, b)))
	}

	// TODO: neutral ?
	t.logger.With().Debug("adding block to blocks table", block.Fields()...)
	t.BlockOpinionsByLayer[block.LayerIndex][blockid] = Opinion{BlocksOpinion: opinion}
	return nil
}

// HandleIncomingLayer processes all layer block votes
// returns the old pbase and new pbase after taking into account the blocks votes
func (t *turtle) HandleIncomingLayer(newlyr *types.Layer, inputVector []types.BlockID) (pbaseOld, pbaseNew types.LayerID) {
	// These are our starting values
	pbaseOld = t.Verified
	pbaseNew = t.Verified

	if t.Last < newlyr.Index() {
		t.Last = newlyr.Index()
	}

	if len(newlyr.Blocks()) == 0 {
		t.logger.With().Warning("attempted to handle empty layer", newlyr)
		return
		// todo: something else to do on empty layer?
	}

	if newlyr.Index() <= types.GetEffectiveGenesis() {
		t.logger.With().Debug("not attempting to handle genesis layer", newlyr)
		return
	}

	// TODO: handle late blocks

	defer t.evict()

	t.logger.With().Info("start handling incoming layer",
		newlyr.Index(),
		log.Int("num_blocks", len(newlyr.Blocks())))

	// process the votes in all layer blocks and update tables
	for _, b := range newlyr.Blocks() {
		if _, ok := t.BlockOpinionsByLayer[b.LayerIndex]; !ok {
			t.BlockOpinionsByLayer[b.LayerIndex] = make(map[types.BlockID]Opinion, t.AvgLayerSize)
		}
		if err := t.processBlock(b); err != nil {
			log.Panic(fmt.Sprintf("error processing block %v: %v", b.ID(), err))
		}
	}

	// Go over all blocks, in order. Mark block i “good” if:
markingLoop:
	for _, b := range newlyr.Blocks() {
		// (1) the base block is marked as good
		if _, good := t.GoodBlocksIndex[b.BaseBlock]; !good {
			t.logger.With().Debug("not marking block as good because baseblock is not good",
				newlyr.Index(),
				b.ID(),
				log.FieldNamed("base_block", b.BaseBlock))
			continue markingLoop
		}

		baseBlock, err := t.bdp.GetBlock(b.BaseBlock)
		if err != nil {
			t.logger.Panic(fmt.Sprint("block not found ", b.BaseBlock, "err", err))
		}

		// (2) all diffs appear after the base block and are consistent with the input vote vector
		voteClasses := []struct {
			name     string
			diffList []types.BlockID
			vector   vec
		}{
			{"support", b.ForDiff, support},
			{"against", b.AgainstDiff, against},
			{"neutral", b.NeutralDiff, abstain},
		}
		for _, voteClass := range voteClasses {
			for _, exceptionBlockId := range voteClass.diffList {
				if singleInputVector, exceptionBlock, ok := t.checkBlockAndGetInputVector(exceptionBlockId, b.ID(), baseBlock.LayerIndex); ok {
					if singleInputVector != voteClass.vector {
						t.logger.With().Debug("not adding block to good blocks because its vote differs from input vector",
							b.ID(),
							log.FieldNamed("older_block", exceptionBlock.ID()),
							log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
							log.String("input_vote", singleInputVector.String()),
							log.String("block_vote", voteClass.name))
						continue markingLoop
					}
				} else {
					continue markingLoop
				}
			}
		}
		t.logger.With().Debug("marking good block", b.ID(), b.LayerIndex)
		t.GoodBlocksIndex[b.ID()] = struct{}{}
	}

	idx := newlyr.Index()
	pbaseOld = t.Verified
	t.logger.With().Info("starting layer verification",
		log.FieldNamed("was_verified", pbaseOld),
		log.FieldNamed("target_verification", newlyr.Index()))

loop:
	for i := pbaseOld + 1; i < idx; i++ {
		t.logger.With().Info("verifying layer", i)

		blks, err := t.bdp.LayerBlockIds(i)
		if err != nil {
			t.logger.With().Warning("can't find layer in db, skipping verification", i)
			continue // Panic? can't get layer
		}
		raw, err := t.bdp.GetLayerInputVector(i)
		if err != nil {
			// this sets the input to abstain
			t.logger.With().Warning("input vector abstains on all blocks", i)
		}
		inputVectorForLayer := t.inputVectorForLayer(blks, raw)
		if len(inputVectorForLayer) == 0 {
			t.logger.With().Warning("no blocks in input vector, can't verify layer", i)
			break
		}

		contextualValidity := make(map[types.BlockID]bool, len(blks))

		// Count good blocks votes
		// Declare the vote vector “verified” up to position k if the total weight exceeds the confidence threshold in
		// all positions up to k
		for blk, vote := range inputVectorForLayer {
			// Count the votes for the input vote vector by summing the weight of the good blocks
			sum := abstain

			// Step backwards from the latest received layer to i+1, but only count votes for the interval [i, i+Hdist]
			var startLayer types.LayerID
			if t.Last < i+t.Hdist {
				startLayer = t.Last
			} else {
				startLayer = i+t.Hdist-1
			}
			for j := startLayer; j > i ; j-- {
				// check if the block is good
				for bid, op := range t.BlockOpinionsByLayer[j] {
					if _, isgood := t.GoodBlocksIndex[bid]; !isgood {
						t.logger.With().Debug("not counting vote of block not marked good",
							log.FieldNamed("voting_block", bid),
							log.FieldNamed("voted_block", blk))
						continue
					}

					// check if this block has opinion on this block. (TODO: maybe no opinion means AGAINST?)
					opinionVote, ok := op.BlocksOpinion[blk]
					if !ok {
						t.logger.With().Debug("no opinion on block",
							log.FieldNamed("voting_block", bid),
							log.FieldNamed("voted_block", blk))
						continue
					}

					t.logger.With().Debug("adding block opinion to vote sum",
						log.FieldNamed("voting_block", bid),
						log.FieldNamed("voted_block", blk),
						log.String("vote", opinionVote.String()),
						log.String("sum", fmt.Sprintf("[%v, %v]", sum[0], sum[1])))
					sum = sum.Add(opinionVote.Multiply(t.BlockWeight(bid, blk)))
				}
			}

			// check that the total weight exceeds the confidence threshold in all positions up
			threshold := globalThreshold * float64(i-pbaseOld) * float64(t.AvgLayerSize)
			t.logger.With().Debug("global opinion", sum, log.String("threshold", fmt.Sprint(threshold)))
			glopinion := globalOpinion(sum, t.AvgLayerSize, float64(i-pbaseOld))
			t.logger.With().Debug("calculated global opinion on block",
				log.FieldNamed("voted_block", blk),
				i,
				log.String("global_opinion", glopinion.String()),
				log.String("sum", fmt.Sprintf("[%v, %v]", sum[0], sum[1])))

			if glopinion != vote {
				// TODO: trigger self healing after a while ?
				t.logger.With().Warning("global opinion is different from vote",
					log.String("global_opinion", glopinion.String()),
					log.String("vote", vote.String()))
				break loop
			}

			if glopinion == abstain {
				t.logger.With().Warning("global opinion on a block is abstain hence can't verify layer",
					log.String("global_opinion", glopinion.String()),
					log.String("vote", vote.String()))
				break loop
			}

			contextualValidity[blk] = glopinion == support
		}

		// Declare the vote vector “verified” up to position k
		for blk, v := range contextualValidity {
			if err := t.bdp.SaveContextualValidity(blk, v); err != nil {
				// panic?
				t.logger.With().Error("error saving contextual validity on block", blk.Field(), log.Err(err))
			}
		}
		t.Verified = i
		pbaseNew = i
		t.logger.With().Info("verified layer", i)
	}

	return
}
