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

	// use 2D array to be able to iterate from latest elements easily
	BlockOpinionsByLayer map[types.LayerID]map[types.BlockID]Opinion // records hdist, for each block, its votes about every
	// previous block

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

	input, err := t.bdp.GetLayerInputVectorByID(lyrid)
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

func (t *turtle) checkBlockAndGetInputVector(
	diffList []types.BlockID,
	className string,
	voteVector vec,
	sourceBlockID types.BlockID,
	layerIndex types.LayerID,
) bool {
	for _, exceptionBlockID := range diffList {
		if exceptionBlock, err := t.bdp.GetBlock(exceptionBlockID); err != nil {
			t.logger.Panic("can't find %v from block's %v diff ", exceptionBlockID, sourceBlockID)
		} else if exceptionBlock.LayerIndex < layerIndex {
			t.logger.With().Debug("not marking block good because it points to block older than base block",
				sourceBlockID,
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_lyr", layerIndex))
			return false
		} else if v, err := t.getSingleInputVectorFromDB(exceptionBlock.LayerIndex, exceptionBlockID); err != nil {
			t.logger.With().Error("unable to get single input vector for exception block",
				sourceBlockID,
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_lyr", layerIndex),
				log.Err(err))
			return false
		} else if v != voteVector {
			t.logger.With().Debug("not adding block to good blocks because its vote differs from input vector",
				sourceBlockID,
				log.FieldNamed("older_block", exceptionBlock.ID()),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.String("input_vote", v.String()),
				log.String("block_vote", className))
			return false
		}
	}

	return true
}

func (t *turtle) inputVectorForLayer(layerBlocks []types.BlockID, input []types.BlockID) (layerResult map[types.BlockID]vec) {
	layerResult = make(map[types.BlockID]vec, len(layerBlocks))
	// XXX: input vector must be a pointer so we can differentiate
	// between no support votes and no results at all.
	if input == nil {
		// hare didn't finish, so we have no opinion on blocks in this layer
		// TODO: get hare Opinion when hare finishes
		for _, b := range layerBlocks {
			layerResult[b] = abstain
		}
		return
	}

	// add support for all blocks in input vector
	for _, b := range input {
		layerResult[b] = support
	}

	// vote against all layer blocks not in input vector
	for _, b := range layerBlocks {
		if _, ok := layerResult[b]; !ok {
			layerResult[b] = against
		}
	}
	return
}

func (t *turtle) BaseBlock() (types.BlockID, [][]types.BlockID, error) {
	// Try to find a block counting only good blocks
	// TODO: could we do better than just grabbing the first non-error, good block, as below?
	// e.g., trying to minimize the size of the exception list instead
	window := types.LayerID(0)
	if t.Hdist < t.Last {
		window = t.Last - t.Hdist
	}
	for i := t.Last; i > window; i-- {
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
		layerInputVector, err := t.bdp.GetLayerInputVectorByID(i)
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
	opinion := make(map[types.BlockID]vec)
	for blk, vote := range baseBlockOpinion.BlocksOpinion {
		fblk, err := t.bdp.GetBlock(blk)
		if err != nil {
			return fmt.Errorf("voted block not in db voting_block_id: %v, voting_block_layer_id: %v, voted_block_id: %v", block.ID().String(), block.LayerIndex, blk.String())
		}
		window := types.LayerID(0)
		if block.LayerIndex > t.Hdist {
			window = block.LayerIndex - t.Hdist
		}
		if fblk.LayerIndex < window {
			continue
		}

		opinion[blk] = vote
	}

	for _, b := range block.ForDiff {
		opinion[b] = opinion[b].Add(support.Multiply(t.BlockWeight(block.ID(), b)))
	}
	for _, b := range block.AgainstDiff {
		opinion[b] = opinion[b].Add(against.Multiply(t.BlockWeight(block.ID(), b)))
	}

	// TODO: neutral ?
	t.logger.With().Debug("adding block to blocks table", block.Fields()...)
	t.BlockOpinionsByLayer[block.LayerIndex][block.ID()] = Opinion{BlocksOpinion: opinion}
	return nil
}

// HandleIncomingLayer processes all layer block votes
// returns the old pbase and new pbase after taking into account the blocks votes
// TODO: inputVector is unused here, fix this
func (t *turtle) HandleIncomingLayer(newlyr *types.Layer) (pbaseOld, pbaseNew types.LayerID) {
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

	blockscount := len(newlyr.Blocks())
	goodblocks := len(t.GoodBlocksIndex)
	// Go over all blocks, in order. Mark block i “good” if:
	for _, b := range newlyr.Blocks() {
		// (1) the base block is marked as good
		if _, good := t.GoodBlocksIndex[b.BaseBlock]; !good {
			t.logger.With().Debug("not marking block as good because baseblock is not good",
				newlyr.Index(),
				b.ID(),
				log.FieldNamed("base_block", b.BaseBlock))
		} else if baseBlock, err := t.bdp.GetBlock(b.BaseBlock); err != nil {
			t.logger.Panic(fmt.Sprint("block not found ", b.BaseBlock, "err", err))
		} else if t.checkBlockAndGetInputVector(b.ForDiff, "support", support, b.ID(), baseBlock.LayerIndex) &&
			t.checkBlockAndGetInputVector(b.AgainstDiff, "against", against, b.ID(), baseBlock.LayerIndex) &&
			t.checkBlockAndGetInputVector(b.NeutralDiff, "abstain", abstain, b.ID(), baseBlock.LayerIndex) {
			// (2) all diffs appear after the base block and are consistent with the input vote vector
			t.logger.With().Debug("marking good block", b.ID(), b.LayerIndex)
			t.GoodBlocksIndex[b.ID()] = struct{}{}
		} else {
			t.logger.With().Debug("not marking block good", b.ID(), b.LayerIndex)
		}
	}

	t.logger.With().Info("finished marking good blocks", log.Int("total_blocks", blockscount), log.Int("good_blocks", len(t.GoodBlocksIndex)-goodblocks))

	idx := newlyr.Index()
	pbaseOld = t.Verified
	t.logger.With().Info("starting layer verification",
		log.FieldNamed("was_verified", pbaseOld),
		log.FieldNamed("target_verification", newlyr.Index()))

layerLoop:
	for i := pbaseOld + 1; i < idx; i++ {
		t.logger.With().Info("verifying layer", i)

		layerBlockIds, err := t.bdp.LayerBlockIds(i)
		if err != nil {
			t.logger.With().Warning("can't find layer in db, skipping verification", i)
			continue // Panic? can't get layer
		}

		// TODO: implement handling hare terminating with no valid blocks.
		// 	currently input vector is nil if hare hasn't terminated yet.
		//	 ACT: hare should save something in the db when terminating empty set, sync should check it.
		rawLayerInputVector, err := t.bdp.GetLayerInputVectorByID(i)
		if err != nil {
			// this sets the input to abstain
			t.logger.With().Warning("input vector abstains on all blocks", i)
		}
		layerVotes := t.inputVectorForLayer(layerBlockIds, rawLayerInputVector)
		if len(layerVotes) == 0 {
			t.logger.With().Warning("no blocks in input vector, can't verify layer", i)
			break
		}

		contextualValidity := make(map[types.BlockID]bool, len(layerBlockIds))

		// Count the votes of good blocks. vote is our opinion on this block.
		// Declare the vote vector “verified” up to position k if the total weight exceeds the confidence threshold in
		// all positions up to k
		for blk, vote := range layerVotes {
			// Count the votes for the input vote vector by summing the weight of the good blocks
			sum := abstain

			// Step backwards from the latest received layer to i+1, but only count votes for the interval [i, i+Hdist]
			var startLayer types.LayerID
			if t.Last < i+t.Hdist {
				startLayer = t.Last
			} else {
				startLayer = i + t.Hdist - 1
			}
			for j := startLayer; j > i; j-- {
				// check if the block is good
				for bid, op := range t.BlockOpinionsByLayer[j] {
					if _, isgood := t.GoodBlocksIndex[bid]; !isgood {
						t.logger.With().Debug("not counting vote of block not marked good",
							log.FieldNamed("voting_block", bid),
							log.FieldNamed("voted_block", blk))
						continue
					}

					// check if this block has an opinion on this block (TODO: maybe no opinion means AGAINST?)
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
			globalOpinion := calculateGlobalOpinion(t.logger, sum, t.AvgLayerSize, float64(i-pbaseOld))
			t.logger.With().Debug("calculated global opinion on block",
				log.FieldNamed("voted_block", blk),
				i,
				log.String("global_opinion", globalOpinion.String()),
				log.String("sum", fmt.Sprintf("[%v, %v]", sum[0], sum[1])))

			// If, for any block in this layer, the global opinion (summed votes) disagrees with our vote (what the
			// input vector says), or if the global opinion is abstain, then we do not verify this layer. Otherwise,
			// we do.

			if globalOpinion != vote {
				// TODO: trigger self healing after a while?
				t.logger.With().Warning("global opinion on block differs from our vote, cannot verify layer",
					blk,
					log.String("global_opinion", globalOpinion.String()),
					log.String("vote", vote.String()))
				break layerLoop
			}

			// An abstain vote on a single block in this layer means that we should abstain from voting on the entire
			// layer. This usually means everyone is still waiting for Hare to finish, so we cannot verify this layer.
			// This condition should be temporary, except during a balancing attack. The verifying tortoise is not
			// equipped to handle this scenario, but the full tortoise is.
			// TODO: should we trigger self-healing in this scenario, after a while?
			// TODO: should we give up trying to verify later layers, too?
			if globalOpinion == abstain {
				t.logger.With().Warning("global opinion on block is abstain, cannot verify layer",
					blk,
					log.String("global_opinion", globalOpinion.String()),
					log.String("vote", vote.String()))
				break layerLoop
			}

			contextualValidity[blk] = globalOpinion == support
		}

		// Declare the vote vector “verified” up to position k (up to this layer)
		// and record the contextual validity for all blocks in this layer
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
