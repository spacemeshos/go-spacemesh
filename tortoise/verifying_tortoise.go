package tortoise

import (
	"context"
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

	// last layer processed
	Last types.LayerID

	// last evicted layer
	Evict types.LayerID

	// hare lookback (distance): up to Hdist layers back, we only consider hare results/input vector
	Hdist types.LayerID

	// hare result wait (distance): we wait up to Zdist layers for hare results/input vector, before invalidating
	// a layer
	Zdist types.LayerID

	AvgLayerSize  int
	MaxExceptions int

	GoodBlocksIndex map[types.BlockID]struct{}

	Verified types.LayerID

	// this matrix stores the opinion of each block about other blocks
	BlockOpinionsByLayer map[types.LayerID]map[types.BlockID]Opinion
}

// SetLogger sets the Log instance for this turtle
func (t *turtle) SetLogger(logger log.Log) {
	t.logger = logger
}

// newTurtle creates a new verifying tortoise algorithm instance. XXX: maybe rename?
func newTurtle(bdp blockDataProvider, hdist, zdist, avgLayerSize int) *turtle {
	t := &turtle{
		logger:               log.NewDefault("trtl"),
		Hdist:                types.LayerID(hdist),
		Zdist:                types.LayerID(zdist),
		bdp:                  bdp,
		Last:                 0,
		AvgLayerSize:         avgLayerSize,
		GoodBlocksIndex:      make(map[types.BlockID]struct{}),
		BlockOpinionsByLayer: make(map[types.LayerID]map[types.BlockID]Opinion, hdist),
		MaxExceptions:        hdist * avgLayerSize * 100,
	}
	return t
}

func (t *turtle) init(ctx context.Context, genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	t.logger.WithContext(ctx).With().Debug("initializing genesis layer for verifying tortoise",
		genesisLayer.Index(),
		genesisLayer.Hash().Field())
	t.BlockOpinionsByLayer[genesisLayer.Index()] = make(map[types.BlockID]Opinion)
	for _, blk := range genesisLayer.Blocks() {
		id := blk.ID()
		t.BlockOpinionsByLayer[genesisLayer.Index()][blk.ID()] = Opinion{
			BlockOpinions: make(map[types.BlockID]vec),
		}
		t.GoodBlocksIndex[id] = struct{}{}
	}
	t.Last = genesisLayer.Index()
	t.Evict = genesisLayer.Index()
	t.Verified = genesisLayer.Index()
}

// evict makes sure we only keep a window of the last hdist layers.
func (t *turtle) evict(ctx context.Context) {
	logger := t.logger.WithContext(ctx)

	// Don't evict before we've verified more than hdist
	// TODO: fix potential leak when we can't verify but keep receiving layers

	if t.Verified <= types.GetEffectiveGenesis()+t.Hdist {
		return
	}
	// The window is the last [Verified - hdist] layers.
	window := t.Verified - t.Hdist
	logger.With().Info("window starts", window)
	// evict from last evicted to the beginning of our window.
	for lyr := t.Evict; lyr < window; lyr++ {
		logger.With().Info("removing layer", lyr)
		for blk := range t.BlockOpinionsByLayer[lyr] {
			delete(t.GoodBlocksIndex, blk)
		}
		delete(t.BlockOpinionsByLayer, lyr)
		logger.With().Debug("evict block from maps", lyr)
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

// returns the binary local opinion on the validity of a block in a layer (support or against)
// TODO: cache but somehow check for changes (e.g., late-finishing Hare), maybe check hash?
func (t *turtle) getSingleInputVectorFromDB(ctx context.Context, lyrid types.LayerID, blockid types.BlockID) (vec, error) {
	if lyrid <= types.GetEffectiveGenesis() {
		return support, nil
	}

	input, err := t.bdp.GetLayerInputVector(lyrid)
	if err != nil {
		return abstain, err
	}

	t.logger.WithContext(ctx).With().Debug("got input vector from db",
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
	ctx context.Context,
	diffList []types.BlockID,
	className string,
	voteVector vec,
	sourceBlockID types.BlockID,
	layerIndex types.LayerID,
) bool {
	logger := t.logger.WithContext(ctx).WithFields(log.FieldNamed("source_block_id", sourceBlockID))
	for _, exceptionBlockID := range diffList {
		if exceptionBlock, err := t.bdp.GetBlock(exceptionBlockID); err != nil {
			logger.With().Error("inconsistent state: can't find block from block's diff",
				log.FieldNamed("exception_block_id", exceptionBlockID))
			return false
		} else if exceptionBlock.LayerIndex < layerIndex {
			// TODO: can a base block exception list point to blocks in the same layer, or must they all be newer?
			logger.With().Error("base block candidate points to older block",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_lyr", layerIndex))
			return false
		} else if v, err := t.getSingleInputVectorFromDB(ctx, exceptionBlock.LayerIndex, exceptionBlockID); err != nil {
			logger.With().Error("unable to get single input vector for exception block",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_lyr", layerIndex),
				log.Err(err))
			return false
		} else if v != voteVector {
			logger.With().Debug("not adding block to good blocks because its vote differs from input vector",
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
	// LANE TODO: test nil input vs. empty map input here
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

// BaseBlock finds and returns a good block from a recent layer that's suitable to serve as a base block for opinions
// for newly-constructed blocks. Also includes vectors of exceptions, where the current local opinion differs from
// (or is newer than) that of the base block.
func (t *turtle) BaseBlock(ctx context.Context) (types.BlockID, [][]types.BlockID, error) {
	logger := t.logger.WithContext(ctx)

	// look at good blocks backwards from most recent processed layer to find a suitable base block
	// TODO: optimize by, e.g., trying to minimize the size of the exception list (rather than first match)
	for layerID := t.Last; layerID > t.Last-t.Hdist; layerID-- {
		for block, opinion := range t.BlockOpinionsByLayer[layerID] {
			if _, ok := t.GoodBlocksIndex[block]; !ok {
				logger.With().Debug("skipping block not marked good",
					log.FieldNamed("last_layer", t.Last),
					layerID,
					block)
				continue
			}

			// Calculate the set of exceptions between the base block opinion and latest local opinion
			exceptionVectorMap, err := t.calculateExceptions(ctx, layerID, block, opinion)
			if err != nil {
				logger.With().Warning("error calculating vote exceptions for block",
					log.FieldNamed("last_layer", t.Last),
					layerID,
					block,
					log.Err(err))
				continue
			}

			logger.With().Info("chose baseblock",
				log.FieldNamed("last_layer", t.Last),
				log.FieldNamed("base_block_layer", layerID),
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

// calculate and return a list of exceptions, i.e., differences between the opinions of a base block and the local
// opinion
func (t *turtle) calculateExceptions(
	ctx context.Context,
	blockLayerID types.LayerID,
	blockID types.BlockID,
	baseBlockOpinion Opinion, // the block's opinion vector
) ([]map[types.BlockID]struct{}, error) {
	logger := t.logger.WithContext(ctx).WithFields(
		log.FieldNamed("base_block_layer_id", blockLayerID),
		log.FieldNamed("base_block_id", blockID))

	// using maps prevents duplicates
	againstDiff := make(map[types.BlockID]struct{})
	forDiff := make(map[types.BlockID]struct{})
	neutralDiff := make(map[types.BlockID]struct{})

	// we support all genesis blocks by default
	if blockLayerID == types.GetEffectiveGenesis() {
		for _, i := range types.BlockIDs(mesh.GenesisLayer().Blocks()) {
			forDiff[i] = struct{}{}
		}
		return []map[types.BlockID]struct{}{againstDiff, forDiff, neutralDiff}, nil
	}

	// Add latest layers input vector results to the diff
	// Note: a block may only be selected as a candidate base block if it's marked "good", and it may only be marked
	// "good" if its own base block is marked "good" and all exceptions it contains agree with our local opinion.
	// We only look for and store exceptions within the sliding window set of layers as an optimization, but a block
	// can contain exceptions from any layer, back to genesis.
	for layerID := t.Hdist; layerID <= t.Last; layerID++ {
		logger := logger.WithFields(log.FieldNamed("diff_layer_id", layerID))
		logger.Debug("checking input vector diffs")

		layerBlockIds, err := t.bdp.LayerBlockIds(layerID)
		if err != nil {
			if err != leveldb.ErrClosed {
				// this is expected, in cases where, e.g., Hare failed for a layer
				logger.Warning("no block ids for layer in database")
			}
			return nil, err
		}

		// helper function for adding diffs
		addDiffs := func(blockID types.BlockID, voteClass string, voteVec vec, diffMap map[types.BlockID]struct{}) {
			if v, ok := baseBlockOpinion.BlockOpinions[blockID]; !ok || v != voteVec {
				logger.With().Debug("added vote diff",
					log.FieldNamed("base_block_candidate", blockID),
					log.FieldNamed("diff_block", blockID),
					log.String("diff_class", voteClass))
				diffMap[blockID] = struct{}{}
			}
		}

		// attempt to read Hare results for layer
		layerInputVector, err := t.bdp.GetLayerInputVector(layerID)
		if err != nil {
			if errors.Is(err, mesh.ErrInvalidLayer) {
				// Hare failed for this layer, vote against all blocks
				logger.With().Debug("voting against all blocks in invalid layer after hare failure", log.Err(err))

				// leaving the input vector empty will have the desired result, below
			} else if layerID < t.Last-t.Zdist {
				// TODO: should this be currentLayer - zdist?
				// this layer cannot be older than hdist before last, since that's where we start looking for a base
				// block, but it can be older than zdist before last. after zdist layers, we give up waiting for hare
				// results/input vector and mark the whole layer invalid.
				logger.With().Debug("voting against all blocks in invalid layer older than zdist", log.Err(err))

				// leaving the input vector empty will have the desired result, below
			} else {
				// Still waiting for Hare results, vote neutral and move on
				logger.With().Debug("input vector is empty, adding neutral diffs", log.Err(err))
				for _, b := range layerBlockIds {
					addDiffs(b, "neutral", abstain, neutralDiff)
				}
				continue
			}
		}

		inInputVector := make(map[types.BlockID]struct{})

		for _, b := range layerInputVector {
			inInputVector[b] = struct{}{}
			// Block is in input vector but no base block vote or base block doesn't support it:
			// add diff FOR this block
			addDiffs(b, "support", support, forDiff)
		}

		for _, b := range layerBlockIds {
			// Matches input vector, no diff (i.e., both support)
			if _, ok := inInputVector[b]; ok {
				continue
			}

			// Layer block has no base block vote or base block supports, but not in input vector AND we are no longer
			// waiting for Hare results for this layer (see above): add diff AGAINST this block
			addDiffs(b, "against", against, againstDiff)
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

func (t *turtle) processBlock(ctx context.Context, block *types.Block) error {
	logger := t.logger.WithContext(ctx).WithFields(block.ID())

	// When a new block arrives, we look up the block it points to in our table,
	// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
	// We then add the vote difference vector and the explicit vote vector to our vote-totals vector.
	logger.With().Debug("processing block", block.Fields()...)
	logger.With().Debug("getting baseblock", log.FieldNamed("base_block_id", block.BaseBlock))

	base, err := t.bdp.GetBlock(block.BaseBlock)
	if err != nil {
		return fmt.Errorf("inconsistent state: can't find baseblock in database")
	}

	logger.With().Debug("block supports", types.BlockIdsField(block.BlockHeader.ForDiff))
	logger.With().Debug("checking baseblock", base.Fields()...)

	layerOpinions, ok := t.BlockOpinionsByLayer[base.LayerIndex]
	if !ok {
		return fmt.Errorf("baseblock layer not found %v, %v", block.BaseBlock, base.LayerIndex)
	}

	baseBlockOpinion, ok := layerOpinions[base.ID()]
	if !ok {
		return fmt.Errorf("baseblock not found in layer %v, %v", block.BaseBlock, base.LayerIndex)
	}

	// TODO: this logic would be simpler if For and Against were a single list
	// TODO: save and vote against blocks that exceed the max exception list size (DoS prevention)
	opinion := make(map[types.BlockID]vec)
	for _, b := range block.ForDiff {
		opinion[b] = support.Multiply(t.BlockWeight(block.ID(), b))
	}
	for _, b := range block.AgainstDiff {
		opinion[b] = against.Multiply(t.BlockWeight(block.ID(), b))
	}
	for _, b := range block.NeutralDiff {
		opinion[b] = abstain
	}
	for blk, vote := range baseBlockOpinion.BlockOpinions {
		// add original vote only if there were no exceptions
		if _, exists := opinion[blk]; !exists {
			opinion[blk] = vote
		}
	}

	logger.With().Debug("adding block to blocks table")
	t.BlockOpinionsByLayer[block.LayerIndex][block.ID()] = Opinion{BlockOpinions: opinion}
	return nil
}

// HandleIncomingLayer processes all layer block votes
// returns the old pbase and new pbase after taking into account the blocks votes
func (t *turtle) HandleIncomingLayer(ctx context.Context, newlyr *types.Layer) (pbaseOld, pbaseNew types.LayerID) {
	logger := t.logger.WithContext(ctx).WithFields(newlyr)

	// These are our starting values
	pbaseOld = t.Verified
	pbaseNew = t.Verified

	if t.Last < newlyr.Index() {
		t.Last = newlyr.Index()
	}

	if newlyr.Index() <= types.GetEffectiveGenesis() {
		logger.Debug("not attempting to handle genesis layer")
		return
	}

	if len(newlyr.Blocks()) == 0 {
		logger.Warning("attempted to handle empty layer")
		return
		// todo: something else to do on empty layer?
	}

	// TODO: handle late blocks

	defer t.evict(ctx)

	logger.With().Info("start handling incoming layer", log.Int("num_blocks", len(newlyr.Blocks())))

	// process the votes in all layer blocks and update tables
	for _, b := range newlyr.Blocks() {
		if _, ok := t.BlockOpinionsByLayer[b.LayerIndex]; !ok {
			t.BlockOpinionsByLayer[b.LayerIndex] = make(map[types.BlockID]Opinion, t.AvgLayerSize)
		}
		if err := t.processBlock(ctx, b); err != nil {
			logger.With().Error("error processing block", b.ID(), log.Err(err))
		}
	}

	// Go over all blocks, in order. Mark block i “good” if:
	for _, b := range newlyr.Blocks() {
		logger := logger.WithFields(b.ID(), log.FieldNamed("base_block_id", b.BaseBlock))
		// (1) the base block is marked as good
		if _, good := t.GoodBlocksIndex[b.BaseBlock]; !good {
			logger.Debug("not marking block as good because baseblock is not good")
		} else if baseBlock, err := t.bdp.GetBlock(b.BaseBlock); err != nil {
			logger.With().Error("inconsistent state: base block not found", log.Err(err))
		} else if true &&
			// (2) all diffs appear after the base block and are consistent with the input vote vector
			t.checkBlockAndGetInputVector(ctx, b.ForDiff, "support", support, b.ID(), baseBlock.LayerIndex) &&
			t.checkBlockAndGetInputVector(ctx, b.AgainstDiff, "against", against, b.ID(), baseBlock.LayerIndex) &&
			t.checkBlockAndGetInputVector(ctx, b.NeutralDiff, "abstain", abstain, b.ID(), baseBlock.LayerIndex) {
			logger.Debug("marking block good")
			t.GoodBlocksIndex[b.ID()] = struct{}{}
		} else {
			logger.Debug("not marking block good")
		}
	}

	logger.With().Info("starting layer verification",
		log.FieldNamed("prev_verified", pbaseOld),
		log.FieldNamed("verification_target", newlyr.Index()))

layerLoop:
	// attempt to verify each layer from the last verified up to one prior to the newly-arrived layer.
	// this is the full range of unverified layers that we might possibly be able to verify at this point.
	for candidateLayer := pbaseOld + 1; candidateLayer < newlyr.Index(); candidateLayer++ {
		logger.With().Info("verifying layer", candidateLayer)

		layerBlockIds, err := t.bdp.LayerBlockIds(candidateLayer)
		if err != nil {
			logger.With().Error("inconsistent state: can't find layer in database, skipping verification", candidateLayer)
			continue // Panic?
		}

		// get the local opinion (input vector) for this layer. below, we calculate the global opinion on each block in
		// the layer and check if it agrees with this local opinion.
		rawLayerInputVector, err := t.bdp.GetLayerInputVector(candidateLayer)
		if err != nil {
			if errors.Is(err, mesh.ErrInvalidLayer) {
				// Hare already failed for this layer, so we want to vote against all blocks in the layer: an empty
				// map will do the trick
				rawLayerInputVector = make([]types.BlockID, 0)
			} else {
				// otherwise, we want to abstain: leaving the map as nil does this
				logger.With().Warning("input vector abstains on all blocks", candidateLayer)
			}
		}
		localLayerOpinionVec := t.inputVectorForLayer(layerBlockIds, rawLayerInputVector)
		if len(localLayerOpinionVec) == 0 {
			logger.With().Warning("no blocks in input vector, can't verify layer", candidateLayer)
			break
		}

		contextualValidity := make(map[types.BlockID]bool, len(layerBlockIds))

		// Count the votes of good blocks. localOpinionOnBlock is our opinion on this block.
		// Declare the vote vector “verified” up to position k if the total weight exceeds the confidence threshold in
		// all positions up to k: in other words, we can verify a layer k if the total weight of the global opinion
		// exceeds the confidence threshold, and agrees with local opinion.
		for blockID, localOpinionOnBlock := range localLayerOpinionVec {
			// Count the votes for the input vote vector by summing the weight of the good blocks
			sum := abstain

			// Count votes for the hDist layers following the candidateLayer, up to the most recently verified layer.
			lastVotingLayer := t.Last
			if candidateLayer+t.Hdist < lastVotingLayer {
				lastVotingLayer = candidateLayer + t.Hdist
			}
			for voteLayer := lastVotingLayer; voteLayer > candidateLayer; voteLayer-- {
				// sum the opinion of blocks we agree with, i.e., blocks marked "good"
				for bid, op := range t.BlockOpinionsByLayer[voteLayer] {
					if _, isgood := t.GoodBlocksIndex[bid]; !isgood {
						logger.With().Debug("not counting vote of block not marked good",
							log.FieldNamed("voting_block", bid),
							log.FieldNamed("block_voted_on", blockID))
						continue
					}

					// check if this block has an opinion on this block
					// no opinion (on a block in an older layer) counts as an explicit vote against the block
					if opinionVote, exists := op.BlockOpinions[blockID]; exists {
						logger.With().Debug("adding block opinion to vote sum",
							log.FieldNamed("voting_block", bid),
							log.FieldNamed("block_voted_on", blockID),
							log.String("vote", opinionVote.String()),
							log.String("sum", fmt.Sprintf("[%v, %v]", sum[0], sum[1])))
						sum = sum.Add(opinionVote.Multiply(t.BlockWeight(bid, blockID)))
					} else {
						logger.With().Debug("no opinion on older block, counting vote against",
							log.FieldNamed("voting_block", bid),
							log.FieldNamed("block_voted_on", blockID))
						sum = sum.Add(against.Multiply(t.BlockWeight(bid, blockID)))
					}
				}
			}

			// check that the total weight exceeds the confidence threshold
			globalOpinionOnBlock := calculateGlobalOpinion(t.logger, sum, t.AvgLayerSize, float64(candidateLayer-pbaseOld))
			logger.With().Debug("calculated global opinion on block",
				log.FieldNamed("block_voted_on", blockID),
				candidateLayer,
				log.String("global_opinion", globalOpinionOnBlock.String()),
				log.String("sum", fmt.Sprintf("[%v, %v]", sum[0], sum[1])))

			// If, for any block in this layer, the global opinion (summed block votes) disagrees with our vote (the
			// input vector), or if the global opinion is abstain, then we do not verify this layer. Otherwise,
			// we do.
			if globalOpinionOnBlock != localOpinionOnBlock {
				// TODO: trigger self healing after a while
				logger.With().Warning("global opinion on block differs from our vote, cannot verify layer",
					blockID,
					log.String("global_opinion", globalOpinionOnBlock.String()),
					log.String("vote", localOpinionOnBlock.String()))
				break layerLoop
			}

			// An abstain vote on a single block in this layer means that we should abstain from voting on the entire
			// layer. This usually means everyone is still waiting for Hare to finish, so we cannot verify this layer.
			// This condition should be temporary. It could theoretically happen for a prolonged period during a
			// balancing attack. The verifying tortoise will continue to fail in this case, but the full tortoise will
			// enter self-healing.
			// TODO: abstain only for entire layer at a time, not for individual blocks (optimization)
			// TODO: trigger self-healing in this scenario, after a while
			if globalOpinionOnBlock == abstain {
				logger.With().Warning("global opinion on block is abstain, cannot verify layer",
					blockID,
					log.String("global_opinion", globalOpinionOnBlock.String()),
					log.String("vote", localOpinionOnBlock.String()))
				break layerLoop
			}

			contextualValidity[blockID] = globalOpinionOnBlock == support
		}

		// Declare the vote vector “verified” up to position k (up to this layer)
		// and record the contextual validity for all blocks in this layer
		for blk, v := range contextualValidity {
			if err := t.bdp.SaveContextualValidity(blk, v); err != nil {
				// panic?
				logger.With().Error("error saving contextual validity on block", blk.Field(), log.Err(err))
			}
		}
		t.Verified = candidateLayer
		pbaseNew = candidateLayer
		logger.With().Info("verified layer", candidateLayer)
	}

	return
}
