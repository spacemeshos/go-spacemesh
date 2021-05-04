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
	GetBlock(types.BlockID) (*types.Block, error)
	LayerBlockIds(types.LayerID) ([]types.BlockID, error)
	LayerBlocks(types.LayerID) ([]*types.Block, error)

	GetCoinflip(context.Context, types.LayerID) (bool, bool)
	GetLayerInputVectorByID(types.LayerID) ([]types.BlockID, error)
	SaveLayerInputVectorByID(types.LayerID, []types.BlockID) error
	SaveContextualValidity(types.BlockID, bool) error

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

	// the number of layers we wait until we have confidence that w.h.p. all honest nodes have reached consensus on the
	// contents of a layer
	ConfidenceParam types.LayerID

	AvgLayerSize  int
	MaxExceptions int

	GoodBlocksIndex map[types.BlockID]struct{}

	Verified types.LayerID

	// this matrix stores the opinion of each block about other blocks
	// it stores every block, regardless of local opinion (i.e., regardless of whether it's marked as "good" or not)
	BlockOpinionsByLayer map[types.LayerID]map[types.BlockID]Opinion
}

// SetLogger sets the Log instance for this turtle
func (t *turtle) SetLogger(logger log.Log) {
	t.logger = logger
}

// newTurtle creates a new verifying tortoise algorithm instance. XXX: maybe rename?
func newTurtle(bdp blockDataProvider, hdist, zdist, confidenceParam, avgLayerSize int) *turtle {
	t := &turtle{
		logger:               log.NewDefault("trtl"),
		Hdist:                types.LayerID(hdist),
		Zdist:                types.LayerID(zdist),
		ConfidenceParam:      types.LayerID(confidenceParam),
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

	input, err := t.bdp.GetLayerInputVectorByID(lyrid)
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
	baseBlockLayer types.LayerID,
) bool {
	logger := t.logger.WithContext(ctx).WithFields(log.FieldNamed("source_block_id", sourceBlockID))
	for _, exceptionBlockID := range diffList {
		if exceptionBlock, err := t.bdp.GetBlock(exceptionBlockID); err != nil {
			logger.With().Error("inconsistent state: can't find block from block's diff",
				log.FieldNamed("exception_block_id", exceptionBlockID))
			return false
		} else if exceptionBlock.LayerIndex < baseBlockLayer {
			logger.With().Error("base block candidate points to older block",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_lyr", baseBlockLayer))
			return false
		} else if v, err := t.getSingleInputVectorFromDB(ctx, exceptionBlock.LayerIndex, exceptionBlockID); err != nil {
			logger.With().Error("unable to get single input vector for exception block",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_lyr", baseBlockLayer),
				log.Err(err))
			return false
		} else if v != voteVector {
			logger.With().Debug("not adding block to good blocks because its vote differs from input vector",
				log.FieldNamed("older_block", exceptionBlock.ID()),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("input_vote", v),
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
	// see https://github.com/spacemeshos/go-spacemesh/issues/2402
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
		layerInputVector, err := t.bdp.GetLayerInputVectorByID(layerID)
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

// Note: weight depends on more than just the weight of the voting block. It also depends on contextual factors such as whether
// or not the block's ATX was received on time, and on how old the layer is.
// TODO: for now it's probably sufficient to adjust weight based on whether the ATX was received on time, or late, for the
// current epoch. Assign weight of zero to late ATXs for the current epoch?
func (t *turtle) BlockWeight(votingBlock, blockVotedOn types.BlockID) int {
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
func (t *turtle) HandleIncomingLayer(ctx context.Context, newlyr *types.Layer) {
	logger := t.logger.WithContext(ctx).WithFields(newlyr)

	if t.Last < newlyr.Index() {
		t.Last = newlyr.Index()
	}

	if newlyr.Index() <= types.GetEffectiveGenesis() {
		logger.Debug("not attempting to handle genesis layer")
		return
	}

	// Note: we don't compare newlyr and t.Verified, so this method could be called again on an already-verified layer.
	// That would update the stored block opinions but it would not attempt to re-verify an already-verified layer.

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

	blockscount := len(newlyr.Blocks())
	goodblocks := len(t.GoodBlocksIndex)
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

	logger.With().Info("finished marking good blocks",
		log.Int("total_blocks", blockscount),
		log.Int("good_blocks", len(t.GoodBlocksIndex)-goodblocks))

	logger.With().Info("starting layer verification",
		log.FieldNamed("prev_verified", t.Verified),
		log.FieldNamed("verification_target", newlyr.Index()))

layerLoop:
	// attempt to verify each layer from the last verified up to one prior to the newly-arrived layer.
	// this is the full range of unverified layers that we might possibly be able to verify at this point.
	// Note: t.Verified is initialized to the effective genesis layer, so the first candidate layer here necessarily
	// follows and is post-genesis. There's no need for an additional check here.
	for candidateLayerID := t.Verified + 1; candidateLayerID < newlyr.Index(); candidateLayerID++ {
		logger.With().Info("verifying layer", candidateLayerID)

		layerBlockIds, err := t.bdp.LayerBlockIds(candidateLayerID)
		if err != nil {
			// TODO: consider making this a panic, as it means state cannot advance at all
			logger.With().Error("inconsistent state: can't find layer in database, skipping verification",
				candidateLayerID)
			continue
		}

		// get the local opinion (input vector) for this layer. below, we calculate the global opinion on each block in
		// the layer and check if it agrees with this local opinion.
		rawLayerInputVector, err := t.bdp.GetLayerInputVectorByID(candidateLayerID)
		if err != nil {
			if errors.Is(err, mesh.ErrInvalidLayer) {
				// Hare already failed for this layer, so we want to vote against all blocks in the layer: an empty
				// map will do the trick
				rawLayerInputVector = make([]types.BlockID, 0)
			} else {
				// otherwise, we want to abstain: leaving the map as nil does this
				logger.With().Warning("input vector abstains on all blocks", candidateLayerID)
			}
		}
		localLayerOpinionVec := t.inputVectorForLayer(layerBlockIds, rawLayerInputVector)
		if len(localLayerOpinionVec) == 0 {
			logger.With().Warning("no blocks in input vector, can't verify layer", candidateLayerID)
			break
		}

		contextualValidity := make(map[types.BlockID]bool, len(layerBlockIds))

		// Count the votes of good blocks. localOpinionOnBlock is our opinion on this block.
		// Declare the vote vector “verified” up to position k if the total weight exceeds the confidence threshold in
		// all positions up to k: in other words, we can verify a layer k if the total weight of the global opinion
		// exceeds the confidence threshold, and agrees with local opinion.
		for blockID, localOpinionOnBlock := range localLayerOpinionVec {
			// count votes for the hDist layers following the candidateLayerID, up to the most recently verified layer
			lastVotingLayer := t.Verified
			if candidateLayerID+t.Hdist < lastVotingLayer {
				lastVotingLayer = candidateLayerID + t.Hdist
			}

			// count the votes for the input vote vector by summing the voting weight of good blocks
			sum := t.sumVotesForBlock(ctx, blockID, candidateLayerID+1, lastVotingLayer, func(votingBlockID types.BlockID) bool {
				if _, isgood := t.GoodBlocksIndex[votingBlockID]; !isgood {
					logger.With().Debug("not counting vote of block not marked good",
						log.FieldNamed("voting_block", votingBlockID))
					return false
				}
				return true
			})

			// check that the total weight exceeds the confidence threshold
			globalOpinionOnBlock := calculateGlobalOpinion(t.logger, sum, t.AvgLayerSize, float64(candidateLayerID-t.Verified))
			logger.With().Debug("verifying tortoise calculated global opinion on block",
				log.FieldNamed("block_voted_on", blockID),
				candidateLayerID,
				log.FieldNamed("global_opinion", globalOpinionOnBlock),
				sum)

			// At this point, we have all of the data we need to make a decision. There are three possible outcomes:
			// 1. verify the layer (if local and global consensus match, and global consensus is decided)
			// 2. keep waiting to verify the layer (if not, and the layer is relatively recent)
			// 3. trigger self-healing (if not, and the layer is sufficiently old)
			consensusMatches := globalOpinionOnBlock == localOpinionOnBlock
			globalOpinionDecided := globalOpinionOnBlock != abstain

			if consensusMatches && globalOpinionDecided {
				// Opinion on this block is decided, save and keep going
				contextualValidity[blockID] = globalOpinionOnBlock == support
				continue
			}

			// Verifying tortoise will wait `zdist' layers for consensus, then an additional `ConfidenceParam'
			// layers until all other nodes achieve consensus. If it's still stuck after this point, then we
			// trigger self-healing.
			// TODO: allow verifying tortoise to continue to verify later layers, even after failing to verify a
			// layer. See https://github.com/spacemeshos/go-spacemesh/issues/2403.
			needsHealing := candidateLayerID-t.Verified > t.Zdist+t.ConfidenceParam

			// If, for any block in this layer, the global opinion (summed block votes) disagrees with our vote (the
			// input vector), or if the global opinion is abstain, then we do not verify this layer. This could be the
			// result of a reorg (e.g., resolution of a network partition), or a malicious peer during sync, or
			// disagreement about Hare success.
			if !consensusMatches {
				logger.With().Warning("global opinion on block differs from our vote, cannot verify layer",
					blockID,
					log.FieldNamed("global_opinion", globalOpinionOnBlock),
					log.FieldNamed("vote", localOpinionOnBlock))

			}

			// There are only two scenarios that could result in a global opinion of abstain: if everyone is still
			// waiting for Hare to finish for a layer (i.e., it has not yet succeeded or failed), or a balancing attack.
			// The former is temporary and will go away after `zdist' layers. And it should be true of an entire layer,
			// not just of a single block. The latter could cause the global opinion of a single block to permanently
			// be abstain. As long as the contextual validity of any block in a layer is unresolved, we cannot verify
			// the layer (since the effectiveness of each transaction in the layer depends upon the contents of the
			// entire layer and transaction ordering). Therefore we have to enter self-healing in this case.
			// TODO: abstain only for entire layer at a time, not for individual blocks (optimization)
			if !globalOpinionDecided {
				logger.With().Warning("global opinion on block is abstain, cannot verify layer",
					blockID,
					log.FieldNamed("global_opinion", globalOpinionOnBlock),
					log.FieldNamed("vote", localOpinionOnBlock))
			}

			// If the layer is old enough, give up running the verifying tortoise and fall back on self-healing
			if needsHealing {
				t.selfHealing(ctx, newlyr.Index())
				return
			}

			// Otherwise, give up trying to verify layers and keep waiting
			break layerLoop
		}

		// Declare the vote vector “verified” up to position k (up to this layer)
		// and record the contextual validity for all blocks in this layer
		for blk, v := range contextualValidity {
			if err := t.bdp.SaveContextualValidity(blk, v); err != nil {
				// panic?
				logger.With().Error("error saving contextual validity on block", blk, log.Err(err))
			}
		}
		t.Verified = candidateLayerID
		logger.With().Info("verifying tortoise verified layer", candidateLayerID)
	}

	return
}

func (t *turtle) sumVotesForBlock(
	ctx context.Context,
	blockID types.BlockID, // the block we're summing votes for/against
	startLayer, endLayer types.LayerID,
	filter func(types.BlockID) bool,
) (sum vec) {
	sum = abstain
	logger := t.logger.WithContext(ctx).WithFields(
		log.FieldNamed("sum_votes_start_layer", startLayer),
		log.FieldNamed("sum_votes_end_layer", endLayer),
		log.FieldNamed("block_voting_on", blockID))
	for voteLayer := startLayer; voteLayer <= endLayer; voteLayer++ {
		// sum the opinion of blocks we agree with, i.e., blocks marked "good"
		for votingBlockID, votingBlockOpinion := range t.BlockOpinionsByLayer[voteLayer] {
			logger := logger.WithFields(log.FieldNamed("voting_block", votingBlockID))
			if !filter(votingBlockID) {
				logger.Debug("voting block did not pass filter, not counting its vote")
				continue
			}

			// check if this block has an opinion on the block to vote on
			// no opinion (on a block in an older layer) counts as an explicit vote against the block
			if opinionVote, exists := votingBlockOpinion.BlockOpinions[blockID]; exists {
				logger.With().Debug("adding block opinion to vote sum",
					log.FieldNamed("vote", opinionVote),
					sum)
				sum = sum.Add(opinionVote.Multiply(t.BlockWeight(votingBlockID, blockID)))
			} else {
				logger.Debug("no opinion on older block, counting vote against")
				sum = sum.Add(against.Multiply(t.BlockWeight(votingBlockID, blockID)))
			}
		}
	}
	return
}

// Manually count all votes for all layers since the last verified layer, up to the newly-arrived layer (there's no
// point in going further since we have no new information about any newer layers). Self-healing does not take into
// consideration local opinion, it relies solely on global opinion.
func (t *turtle) selfHealing(ctx context.Context, endLayerID types.LayerID) {
	// These are our starting values
	pbaseOld := t.Verified
	pbaseNew := t.Verified

	// TODO: optimize this algorithm using, e.g., a triangular matrix rather than nested loops
	for candidateLayerID := pbaseOld; candidateLayerID < endLayerID; candidateLayerID++ {
		logger := t.logger.WithContext(ctx).WithFields(
			log.FieldNamed("old_verified_layer", pbaseOld),
			log.FieldNamed("new_verified_layer", pbaseNew),
			log.FieldNamed("highest_candidate_layer", endLayerID),
			log.FieldNamed("candidate_layer", candidateLayerID))

		// Calculate the global opinion on all blocks in the layer
		logger.Info("self-healing verifying candidate layer")

		layerBlockIds, err := t.bdp.LayerBlockIds(candidateLayerID)
		if err != nil {
			// TODO: consider making this a panic, as it means state cannot advance at all
			logger.Error("inconsistent state: can't find layer in database, skipping verification")
			continue
		}

		// This map keeps track of the contextual validity of all blocks in this layer
		contextualValidity := make(map[types.BlockID]bool, len(layerBlockIds))
		for _, blockID := range layerBlockIds {
			logger := logger.WithFields(log.FieldNamed("candidate_block_id", blockID))

			// count all votes for or against this block by all blocks in later layers: don't filter out any
			sum := t.sumVotesForBlock(ctx, blockID, candidateLayerID+1, t.Last, func(id types.BlockID) bool { return true })

			// check that the total weight exceeds the confidence threshold
			globalOpinionOnBlock := calculateGlobalOpinion(t.logger, sum, t.AvgLayerSize, float64(candidateLayerID-t.Verified))
			logger.With().Debug("self-healing calculated global opinion on candidate block",
				log.FieldNamed("global_opinion", globalOpinionOnBlock),
				sum)

			// fall back on weak coin: if the global opinion is abstain for a single block in a layer, we instead use
			// the weak coin to decide on the validity of all blocks in the layer
			if globalOpinionOnBlock == abstain {
				layerCoin, exists := t.bdp.GetCoinflip(ctx, candidateLayerID)
				if !exists {
					logger.Error("no weak coin value for candidate layer, self-healing cannot proceed")
					return
				}

				// short-circuit all votes for all blocks in this layer to the weak coin value
				logger.With().Info("re-scoring all blocks in candidate layer using weak coin",
					log.Bool("coinflip", layerCoin))
				for _, blockID := range layerBlockIds {
					contextualValidity[blockID] = layerCoin
				}
				break
			}

			contextualValidity[blockID] = globalOpinionOnBlock == support
		}

		// TODO: do we overwrite the layer input vector in the database here?

		// TODO: reprocess state if we changed the validity of any blocks

		// record the contextual validity for all blocks in this layer
		for blk, v := range contextualValidity {
			if err := t.bdp.SaveContextualValidity(blk, v); err != nil {
				logger.With().Error("error saving contextual validity on block", blk, log.Err(err))
			}
		}
		t.Verified = candidateLayerID
		pbaseNew = candidateLayerID
		logger.With().Info("self healing verified candidate layer")
	}

	return
}
