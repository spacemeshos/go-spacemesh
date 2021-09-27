package tortoise

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/tortoise/metrics"
	"github.com/syndtr/goleveldb/leveldb"
)

type blockDataProvider interface {
	LayerContextuallyValidBlocks(context.Context, types.LayerID) (map[types.BlockID]struct{}, error)
	GetBlock(types.BlockID) (*types.Block, error)
	LayerBlockIds(types.LayerID) ([]types.BlockID, error)
	LayerBlocks(types.LayerID) ([]*types.Block, error)

	GetCoinflip(context.Context, types.LayerID) (bool, bool)
	GetLayerInputVectorByID(types.LayerID) ([]types.BlockID, error)
	SaveContextualValidity(types.BlockID, types.LayerID, bool) error
	ContextualValidity(types.BlockID) (bool, error)
}

type atxDataProvider interface {
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
	GetAtxTimestamp(types.ATXID) (time.Time, error)
}

type layerClock interface {
	LayerToTime(types.LayerID) time.Time
}

var (
	errNoBaseBlockFound                 = errors.New("no good base block within exception vector limit")
	errBaseBlockNotInDatabase           = errors.New("inconsistent state: can't find base block in database")
	errNotSorted                        = errors.New("input blocks are not sorted by layerID")
	errstrNoCoinflip                    = "no weak coin value for layer"
	errstrTooManyExceptions             = "too many exceptions to base block vote"
	errstrBaseBlockLayerMissing         = "base block layer not found"
	errstrBaseBlockNotFoundInLayer      = "base block opinions not found in layer"
	errstrConflictingVotes              = "conflicting votes found in block"
	errstrCantFindLayer                 = "inconsistent state: can't find layer in database"
	errstrUnableToCalculateLocalOpinion = "unable to calculate local opinion for layer"
)

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
	state
	logger log.Log

	atxdb atxDataProvider
	bdp   blockDataProvider
	clock layerClock

	// note: the rest of these are exported for purposes of serialization only

	// hare lookback (distance): up to Hdist layers back, we only consider hare results/input vector
	Hdist uint32

	// hare abort distance: we wait up to Zdist layers for hare results/input vector, before invalidating a layer
	// without hare results.
	Zdist uint32

	// the number of layers we wait until we have confidence that w.h.p. all honest nodes have reached consensus on the
	// contents of a layer
	ConfidenceParam uint32

	// the size of the tortoise sliding window which controls how far back the tortoise stores data
	WindowSize uint32

	// thresholds used for determining finality, and whether to use local or global results, respectively
	GlobalThreshold *big.Rat
	LocalThreshold  *big.Rat

	AvgLayerSize  uint32
	MaxExceptions int

	// how often we want to rerun from genesis
	RerunInterval time.Duration
}

// newTurtle creates a new verifying tortoise algorithm instance
func newTurtle(
	lg log.Log,
	db database.Database,
	bdp blockDataProvider,
	atxdb atxDataProvider,
	clock layerClock,
	hdist,
	zdist,
	confidenceParam,
	windowSize uint32,
	avgLayerSize uint32,
	globalThreshold,
	localThreshold *big.Rat,
	rerun time.Duration,
) *turtle {
	return &turtle{
		state: state{
			diffMode:             true,
			db:                   db,
			log:                  lg,
			GoodBlocksIndex:      map[types.BlockID]bool{},
			BlockOpinionsByLayer: map[types.LayerID]map[types.BlockID]Opinion{},
		},
		logger:          lg.Named("turtle"),
		Hdist:           hdist,
		Zdist:           zdist,
		ConfidenceParam: confidenceParam,
		WindowSize:      windowSize,
		GlobalThreshold: globalThreshold,
		LocalThreshold:  localThreshold,
		bdp:             bdp,
		atxdb:           atxdb,
		clock:           clock,
		AvgLayerSize:    avgLayerSize,
		MaxExceptions:   int(hdist) * int(avgLayerSize) * 100,
		RerunInterval:   rerun,
	}
}

// cloneTurtleParams creates a new verifying tortoise instance using the params of this instance
func (t *turtle) cloneTurtleParams() *turtle {
	return newTurtle(
		t.log,
		t.db,
		t.bdp,
		t.atxdb,
		t.clock,
		t.Hdist,
		t.Zdist,
		t.ConfidenceParam,
		t.WindowSize,
		t.AvgLayerSize,
		t.GlobalThreshold,
		t.LocalThreshold,
		t.RerunInterval,
	)
}

func (t *turtle) init(ctx context.Context, genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	t.logger.WithContext(ctx).With().Info("initializing genesis layer for verifying tortoise",
		genesisLayer.Index(),
		genesisLayer.Hash().Field())
	t.BlockOpinionsByLayer[genesisLayer.Index()] = make(map[types.BlockID]Opinion)
	for _, blk := range genesisLayer.Blocks() {
		id := blk.ID()
		t.BlockOpinionsByLayer[genesisLayer.Index()][id] = Opinion{}
		t.GoodBlocksIndex[id] = false // false means good block, not flushed
	}
	t.Last = genesisLayer.Index()
	t.LastEvicted = genesisLayer.Index().Sub(1)
	t.Verified = genesisLayer.Index()
}

func (t *turtle) lookbackWindowStart() (types.LayerID, bool) {
	// prevent overflow/wraparound
	if t.Verified.Before(types.NewLayerID(t.WindowSize)) {
		return types.NewLayerID(0), false
	}
	return t.Verified.Sub(t.WindowSize), true
}

// evict makes sure we only keep a window of the last hdist layers.
func (t *turtle) evict(ctx context.Context) {
	logger := t.logger.WithContext(ctx)

	// Don't evict before we've verified at least hdist layers
	if !t.Verified.After(types.GetEffectiveGenesis().Add(t.Hdist)) {
		return
	}

	// TODO: fix potential leak when we can't verify but keep receiving layers
	//    see https://github.com/spacemeshos/go-spacemesh/issues/2671

	windowStart, ok := t.lookbackWindowStart()
	if !ok {
		return
	}
	if !windowStart.After(t.LastEvicted) {
		return
	}
	logger.With().Info("attempting eviction",
		log.FieldNamed("effective_genesis", types.GetEffectiveGenesis()),
		log.Uint32("hdist", t.Hdist),
		log.FieldNamed("verified", t.Verified),
		log.Uint32("window_size", t.WindowSize),
		log.FieldNamed("last_evicted", t.LastEvicted),
		log.FieldNamed("window_start", windowStart))

	// evict from last evicted to the beginning of our window
	for layerToEvict := t.LastEvicted.Add(1); layerToEvict.Before(windowStart); layerToEvict = layerToEvict.Add(1) {
		logger.With().Debug("evicting layer", layerToEvict)
		for blk := range t.BlockOpinionsByLayer[layerToEvict] {
			delete(t.GoodBlocksIndex, blk)
		}
		delete(t.BlockOpinionsByLayer, layerToEvict)
	}
	t.LastEvicted = windowStart.Sub(1)
	if err := t.state.Evict(); err != nil {
		logger.With().Panic("can't evict persisted state", log.Err(err))
	}
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

// returns the local opinion on the validity of a block in a layer (support, against, or abstain)
// TODO: cache but somehow check for changes (e.g., late-finishing Hare), maybe check hash?
//    see https://github.com/spacemeshos/go-spacemesh/issues/2672
func (t *turtle) getLocalBlockOpinion(ctx context.Context, layerID types.LayerID, blockid types.BlockID) (vec, error) {
	if !layerID.After(types.GetEffectiveGenesis()) {
		return support, nil
	}

	input, err := t.layerOpinionVector(ctx, layerID)
	// an error here signifies a real database failure
	if err != nil {
		return abstain, err
	}
	// otherwise, nil means we should abstain
	if input == nil {
		return abstain, nil
	}

	t.logger.WithContext(ctx).With().Debug("got layer opinion vector",
		layerID,
		log.FieldNamed("query_block", blockid),
		log.String("input", blockIDsToString(input)))

	for _, bl := range input {
		if bl == blockid {
			// note: we never abstain on individual blocks, only on a whole layer, which would have been captured
			// above.
			return support, nil
		}
	}

	return against, nil
}

func (t *turtle) checkBlockAndGetLocalOpinion(
	ctx context.Context,
	diffList []types.BlockID,
	className string,
	voteVector vec,
	baseBlockLayer types.LayerID,
) bool {
	logger := t.logger.WithContext(ctx)
	for _, exceptionBlockID := range diffList {
		exceptionBlock, err := t.bdp.GetBlock(exceptionBlockID)
		if err != nil {
			logger.With().Error("inconsistent state: can't find block from diff list",
				log.FieldNamed("exception_block_id", exceptionBlockID))
			return false
		}

		if exceptionBlock.LayerIndex.Before(baseBlockLayer) {
			logger.With().Error("good block candidate contains exception for block older than its base block",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_layer", baseBlockLayer))
			return false
		}

		v, err := t.getLocalBlockOpinion(ctx, exceptionBlock.LayerIndex, exceptionBlockID)
		if err != nil {
			logger.With().Error("unable to get single block opinion for block in exception list",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("base_block_layer", baseBlockLayer),
				log.Err(err))
			return false
		}

		if v != voteVector {
			logger.With().Debug("not adding block to good blocks because its vote differs from local opinion",
				log.FieldNamed("older_block", exceptionBlock.ID()),
				log.FieldNamed("older_layer", exceptionBlock.LayerIndex),
				log.FieldNamed("local_opinion", v),
				log.String("block_exception_vote", className))
			return false
		}
	}

	return true
}

// convert two vectors, of (1) raw candidate block IDs for a layer and (2) an opinion vector of blocks we believe belong
// in the layer, into a map of votes for each of these blocks
func (t *turtle) voteVectorForLayer(
	candidateBlocks []types.BlockID, opinionVec []types.BlockID) (voteMap map[types.BlockID]vec) {
	voteMap = make(map[types.BlockID]vec, len(candidateBlocks))
	if opinionVec == nil {
		// nil means abstain, i.e., we have no opinion on blocks in this layer
		for _, b := range candidateBlocks {
			voteMap[b] = abstain
		}
		return
	}

	// add support for all blocks in input vector
	for _, b := range opinionVec {
		voteMap[b] = support
	}

	// vote against all layer blocks not in input vector
	for _, b := range candidateBlocks {
		if _, ok := voteMap[b]; !ok {
			voteMap[b] = against
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
	for layerID := t.Last; layerID.After(t.LastEvicted); layerID = layerID.Sub(1) {
		logger := logger.WithFields(
			log.FieldNamed("last_layer", t.Last),
			log.FieldNamed("last_evicted", t.LastEvicted),
			log.FieldNamed("base_block_candidate_layer", layerID))
		for blockID, opinion := range t.BlockOpinionsByLayer[layerID] {
			if _, ok := t.GoodBlocksIndex[blockID]; !ok {
				logger.With().Info("not considering block not marked good as base block candidate", blockID)
				continue
			}

			// Calculate the set of exceptions between the base block opinion and latest local opinion
			logger.With().Debug("found candidate base block", blockID)
			exceptionVectorMap, err := t.calculateExceptions(ctx, layerID, opinion)
			if err != nil {
				logger.With().Warning("error calculating vote exceptions for block", blockID, log.Err(err))
				continue
			}

			logger.With().Info("chose base block",
				blockID,
				log.Int("against_count", len(exceptionVectorMap[0])),
				log.Int("support_count", len(exceptionVectorMap[1])),
				log.Int("neutral_count", len(exceptionVectorMap[2])))

			metrics.LayerDistanceToBaseBlock.Observe(float64(t.Last.Value - layerID.Value))

			return blockID, [][]types.BlockID{
				blockMapToArray(exceptionVectorMap[0]),
				blockMapToArray(exceptionVectorMap[1]),
				blockMapToArray(exceptionVectorMap[2]),
			}, nil
		}
	}

	// TODO: special error encoding when exceeding exception list size
	return types.BlockID{0}, nil, errNoBaseBlockFound
}

// calculate and return a list of exceptions, i.e., differences between the opinions of a base block and the local
// opinion
func (t *turtle) calculateExceptions(
	ctx context.Context,
	baseBlockLayerID types.LayerID,
	baseBlockOpinion Opinion, // candidate base block's opinion vector
) ([]map[types.BlockID]struct{}, error) {
	logger := t.logger.WithContext(ctx).WithFields(log.FieldNamed("base_block_layer_id", baseBlockLayerID))

	// using maps prevents duplicates
	againstDiff := make(map[types.BlockID]struct{})
	forDiff := make(map[types.BlockID]struct{})
	neutralDiff := make(map[types.BlockID]struct{})

	// we support all genesis blocks by default
	if baseBlockLayerID == types.GetEffectiveGenesis() {
		for _, i := range types.BlockIDs(mesh.GenesisLayer().Blocks()) {
			forDiff[i] = struct{}{}
		}
	}

	// Add latest layers input vector results to the diff
	// Note: a block may only be selected as a candidate base block if it's marked "good", and it may only be marked
	// "good" if its own base block is marked "good" and all exceptions it contains agree with our local opinion.
	// We only look for and store exceptions within the sliding window set of layers as an optimization, but a block
	// can contain exceptions from any layer, back to genesis.
	startLayer := t.LastEvicted.Add(1)
	if startLayer.Before(types.GetEffectiveGenesis()) {
		startLayer = types.GetEffectiveGenesis()
	}
	for layerID := startLayer; !layerID.After(t.Last); layerID = layerID.Add(1) {
		logger := logger.WithFields(log.FieldNamed("diff_layer_id", layerID))
		logger.Debug("checking input vector diffs")

		layerBlockIds, err := t.bdp.LayerBlockIds(layerID)
		if err != nil {
			if err != leveldb.ErrClosed {
				// this should not happen! we only look at layers up to the last processed layer, and we only process
				// layers with valid block data.
				logger.With().Error("no block ids for layer in database", log.Err(err))
			}
			return nil, err
		}

		// helper function for adding diffs
		addDiffs := func(bid types.BlockID, voteClass string, voteVec vec, diffMap map[types.BlockID]struct{}) {
			if v, ok := baseBlockOpinion[bid]; !ok || simplifyVote(v) != voteVec {
				logger.With().Debug("added vote diff",
					log.FieldNamed("diff_block", bid),
					log.String("diff_class", voteClass))
				if layerID.Before(baseBlockLayerID) {
					logger.With().Warning("added exception before base block layer, this block will not be marked good",
						log.FieldNamed("diff_block", bid),
						log.String("diff_class", voteClass))
				}
				diffMap[bid] = struct{}{}
			}
		}

		// get local opinion for layer
		layerInputVector, err := t.layerOpinionVector(ctx, layerID)
		if err != nil {
			// an error here signifies a real database failure
			logger.With().Error(errstrUnableToCalculateLocalOpinion, log.Err(err))
			return nil, err
		}

		// otherwise, nil means we should abstain
		if layerInputVector == nil {
			// still waiting for Hare results, vote neutral and move on
			logger.With().Debug("input vector is empty, adding neutral diffs", log.Err(err))
			for _, b := range layerBlockIds {
				addDiffs(b, "neutral", abstain, neutralDiff)
			}
			continue
		}
		logger.With().Debug("got local opinion vector for layer", log.Int("count", len(layerInputVector)))

		inInputVector := make(map[types.BlockID]struct{})

		// Add diffs FOR blocks that are in the input vector, but where the base block has no opinion or does not
		// explicitly support the block
		for _, b := range layerInputVector {
			inInputVector[b] = struct{}{}
			addDiffs(b, "support", support, forDiff)
		}

		// Finally, we need to consider the case where the base block supports a block in this layer that is not in our
		// input vector (e.g., one we haven't seen), by adding a diff against the block.
		// We do not explicitly add votes against blocks that the base block does _not_ support, since by not voting to
		// support a block in an old layer, we are implicitly voting against it. But if the base block does explicitly
		// support a block and we disagree, we need to add a vote against here.
		// TODO: this is not currently possible since base block opinions aren't indexed by layer. See
		//   https://github.com/spacemeshos/go-spacemesh/issues/2424
		//for b, v := range baseBlockOpinion.BlockOpinions {
		//	if _, ok := inInputVector[b]; !ok && v != against {
		//		addDiffs(b, "against", against, againstDiff)
		//	}
		//}
	}

	// check if exceeded max no. exceptions
	explen := len(againstDiff) + len(forDiff) + len(neutralDiff)
	if explen > t.MaxExceptions {
		return nil, fmt.Errorf("%s (%v)", errstrTooManyExceptions, explen)
	}

	return []map[types.BlockID]struct{}{againstDiff, forDiff, neutralDiff}, nil
}

// voteWeight returns the weight to assign to one block's vote for another.
// Note: weight depends on more than just the weight of the voting block. It also depends on contextual factors such as
// whether or not the block's ATX was received on time, and on how old the layer is.
// TODO: for now it's probably sufficient to adjust weight based on whether the ATX was received on time, or late, for
//   the current epoch. See https://github.com/spacemeshos/go-spacemesh/issues/2540.
func (t *turtle) voteWeight(ctx context.Context, votingBlock *types.Block) (uint64, error) {
	logger := t.logger.WithContext(ctx)

	atxHeader, err := t.atxdb.GetAtxHeader(votingBlock.ATXID)
	if err != nil {
		return 0, err
	}
	atxTimestamp, err := t.atxdb.GetAtxTimestamp(votingBlock.ATXID)
	if err != nil {
		return 0, err
	}
	atxEpoch := atxHeader.PubLayerID.GetEpoch()
	nextEpochStart := t.clock.LayerToTime((atxEpoch + 1).FirstLayer())

	// check if the ATX was received on time
	// TODO: add an exception for sync, when we expect everything to be received late
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2540
	// if atxTimestamp.Before(nextEpochStart) {
	blockWeight := atxHeader.GetWeight()
	logger.With().Debug("voting block atx was timely",
		log.FieldNamed("next_epoch", atxEpoch+1),
		log.Time("next_epoch_start", nextEpochStart),
		log.Time("atx_timestamp", atxTimestamp),
		votingBlock.ID(),
		votingBlock.ATXID,
		log.Uint64("block_weight", blockWeight))
	return blockWeight, nil
	//}
	//logger.With().Warning("voting block atx was untimely, zeroing block vote weight",
	//	log.FieldNamed("next_epoch", atxEpoch+1),
	//	log.Time("next_epoch_start", nextEpochStart),
	//	log.Time("atx_timestamp", atxTimestamp),
	//	votingBlock.ID(),
	//	votingBlock.ATXID)
	//return 0, nil
}

func (t *turtle) voteWeightByID(ctx context.Context, votingBlockID, blockVotedOn types.BlockID) (uint64, error) {
	block, err := t.bdp.GetBlock(votingBlockID)
	if err != nil {
		return 0, err
	}
	return t.voteWeight(ctx, block)
}

// Persist saves the current tortoise state to the database
func (t *turtle) persist() error {
	return t.state.Persist()
}

func (t *turtle) processBlock(ctx context.Context, block *types.Block) error {
	logger := t.logger.WithContext(ctx).WithFields(
		log.FieldNamed("processing_block_id", block.ID()),
		log.FieldNamed("processing_block_layer", block.LayerIndex))

	// When a new block arrives, we look up the block it points to in our table,
	// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
	// We then add the vote difference vector and the explicit vote vector to our vote-totals vector.
	logger.With().Debug("processing block", block.Fields()...)
	logger.With().Debug("getting base block", log.FieldNamed("base_block_id", block.BaseBlock))

	baseBlock, err := t.bdp.GetBlock(block.BaseBlock)
	if err != nil {
		return errBaseBlockNotInDatabase
	}

	logger.With().Debug("block adds support for",
		log.Int("count", len(block.BlockHeader.ForDiff)),
		types.BlockIdsField(block.BlockHeader.ForDiff))
	logger.With().Debug("checking base block", baseBlock.Fields()...)

	layerOpinions, ok := t.BlockOpinionsByLayer[baseBlock.LayerIndex]
	if !ok {
		return fmt.Errorf("%s: %v, %v", errstrBaseBlockLayerMissing, block.BaseBlock, baseBlock.LayerIndex)
	}

	baseBlockOpinion, ok := layerOpinions[baseBlock.ID()]
	if !ok {
		return fmt.Errorf("%s: %v, %v", errstrBaseBlockNotFoundInLayer, block.BaseBlock, baseBlock.LayerIndex)
	}

	voteWeight, err := t.voteWeight(ctx, block)
	if err != nil {
		return fmt.Errorf("error getting vote weight for block %v: %w", block.ID(), err)
	}

	// TODO: this logic would be simpler if For and Against were a single list
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2369
	// TODO: save and vote against blocks that exceed the max exception list size (DoS prevention)
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2673
	opinion := make(map[types.BlockID]vec)

	for _, bid := range block.ForDiff {
		opinion[bid] = support.Multiply(voteWeight)
	}
	for _, bid := range block.AgainstDiff {
		// this could only happen in malicious blocks, and they should not pass a syntax check, but check here just
		// to be extra safe
		if _, alreadyVoted := opinion[bid]; alreadyVoted {
			return fmt.Errorf("%s %v", errstrConflictingVotes, block.ID())
		}
		opinion[bid] = against.Multiply(voteWeight)
	}
	for _, bid := range block.NeutralDiff {
		if _, alreadyVoted := opinion[bid]; alreadyVoted {
			return fmt.Errorf("%s %v", errstrConflictingVotes, block.ID())
		}
		opinion[bid] = abstain
	}
	for blk, vote := range baseBlockOpinion {
		// ignore opinions of very old blocks
		fblk, err := t.bdp.GetBlock(blk)
		if err != nil {
			return fmt.Errorf(
				"block in base block opinion list not in db! "+
					"voting_block_id: %v, voting_block_layer: %v, base_block_id: %v, base_block_layer: %v, block_id: %v",
				block.ID().String(), block.LayerIndex, baseBlock.ID(), baseBlock.LayerIndex, blk.String())
		}
		if fblk.LayerIndex.Before(t.LastEvicted) {
			continue
		}

		// add base block vote only if there weren't already exceptions (support/against/abstain) for this block.
		// and re-weight vote since we want the voting block's weight, not the base block's weight.
		if _, exists := opinion[blk]; !exists {
			opinion[blk] = simplifyVote(vote).Multiply(voteWeight)
		}
	}

	logger.With().Debug("adding or updating block opinion")
	t.BlockOpinionsByLayer[block.LayerIndex][block.ID()] = opinion
	return nil
}

// ProcessNewBlocks processes the votes of a set of blocks, records their opinions, and marks good blocks good.
// The blocks do not all have to be in the same layer, but if they span multiple layers, they must be sorted by LayerID.
func (t *turtle) ProcessNewBlocks(ctx context.Context, blocks []*types.Block) error {
	if len(blocks) == 0 {
		// nothing to do
		t.logger.WithContext(ctx).Warning("cannot process empty block list")
		return nil
	}
	if err := t.processBlocks(ctx, blocks); err != nil {
		return err
	}

	// attempt to verify layers up to the latest one for which we have new block data
	return t.verifyLayers(ctx)
}

func (t *turtle) processBlocks(ctx context.Context, blocks []*types.Block) error {
	logger := t.logger.WithContext(ctx)
	lastLayerID := types.NewLayerID(0)

	logger.With().Info("tortoise handling incoming block data", log.Int("num_blocks", len(blocks)))

	// process the votes in all layer blocks and update tables
	filteredBlocks := make([]*types.Block, 0, len(blocks))
	for _, b := range blocks {
		logger := logger.WithFields(b.ID(), b.LayerIndex)
		// make sure we don't write data on old blocks whose layer has already been evicted
		if b.LayerIndex.Before(t.LastEvicted) {
			logger.With().Warning("not processing block from layer older than last evicted layer",
				log.FieldNamed("last_evicted", t.LastEvicted))
			continue
		}
		if b.LayerIndex.Before(lastLayerID) {
			return errNotSorted
		} else if b.LayerIndex.After(lastLayerID) {
			lastLayerID = b.LayerIndex
		}
		if _, ok := t.BlockOpinionsByLayer[b.LayerIndex]; !ok {
			t.BlockOpinionsByLayer[b.LayerIndex] = make(map[types.BlockID]Opinion, t.AvgLayerSize)
		}
		if err := t.processBlock(ctx, b); err != nil {
			logger.With().Error("error processing block", log.Err(err))
		} else {
			filteredBlocks = append(filteredBlocks, b)
		}
	}

	t.scoreBlocks(ctx, filteredBlocks)

	if t.Last.Before(lastLayerID) {
		logger.With().Warning("got blocks for new layer before receiving layer, updating highest layer seen",
			log.FieldNamed("previous_highest", t.Last),
			log.FieldNamed("new_highest", lastLayerID))
		t.Last = lastLayerID
	}

	return nil
}

func (t *turtle) scoreBlocksByLayerID(ctx context.Context, layerID types.LayerID) error {
	blocks, err := t.bdp.LayerBlocks(layerID)
	if err != nil {
		return err
	}
	t.scoreBlocks(ctx, blocks)
	return nil
}

func (t *turtle) scoreBlocks(ctx context.Context, blocks []*types.Block) {
	logger := t.logger.WithContext(ctx)
	logger.With().Debug("marking good blocks", log.Int("count", len(blocks)))
	numGood := 0
	for _, b := range blocks {
		if t.determineBlockGoodness(ctx, b) {
			// note: we have no way of warning if a block was previously marked as not good
			logger.With().Debug("marking block good", b.ID(), b.LayerIndex)
			t.GoodBlocksIndex[b.ID()] = false // false means good block, not flushed
			numGood++
		} else {
			logger.With().Info("not marking block good", b.ID(), b.LayerIndex)
			if _, isGood := t.GoodBlocksIndex[b.ID()]; isGood {
				logger.With().Warning("marking previously good block as not good", b.ID(), b.LayerIndex)
				delete(t.GoodBlocksIndex, b.ID())
			}
		}
	}

	logger.With().Info("finished marking good blocks",
		log.Int("total_blocks", len(blocks)),
		log.Int("good_blocks", numGood))
}

func (t *turtle) determineBlockGoodness(ctx context.Context, block *types.Block) bool {
	logger := t.logger.WithContext(ctx).WithFields(
		block.ID(),
		block.LayerIndex,
		log.FieldNamed("base_block_id", block.BaseBlock))
	// Go over all blocks, in order. Mark block i "good" if:
	// (1) the base block is marked as good
	if _, good := t.GoodBlocksIndex[block.BaseBlock]; !good {
		logger.Debug("base block is not good")
	} else if baseBlock, err := t.bdp.GetBlock(block.BaseBlock); err != nil {
		logger.With().Error("inconsistent state: base block not found", log.Err(err))
	} else if true &&
		// (2) all diffs appear after the base block and are consistent with the current local opinion
		t.checkBlockAndGetLocalOpinion(ctx, block.ForDiff, "support", support, baseBlock.LayerIndex) &&
		t.checkBlockAndGetLocalOpinion(ctx, block.AgainstDiff, "against", against, baseBlock.LayerIndex) &&
		t.checkBlockAndGetLocalOpinion(ctx, block.NeutralDiff, "abstain", abstain, baseBlock.LayerIndex) {
		logger.Debug("block is good")
		return true
	}
	logger.Debug("block is not good")
	return false
}

// HandleIncomingLayer processes all layer block votes
// returns the old pbase and new pbase after taking into account block votes
func (t *turtle) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) error {
	if err := t.handleLayerBlocks(ctx, layerID); err != nil {
		return err
	}

	// attempt to verify layers up to the latest one for which we have new block data
	return t.verifyLayers(ctx)
}

func (t *turtle) handleLayerBlocks(ctx context.Context, layerID types.LayerID) error {
	logger := t.logger.WithContext(ctx).WithFields(layerID)

	if !layerID.After(types.GetEffectiveGenesis()) {
		logger.Debug("not attempting to handle genesis layer")
		return nil
	}

	// Note: we don't compare newlyr and t.Verified, so this method could be called again on an already-verified layer.
	// That would update the stored block opinions but it would not attempt to re-verify an already-verified layer.

	// read layer blocks
	layerBlocks, err := t.bdp.LayerBlocks(layerID)
	if err != nil {
		return fmt.Errorf("unable to read contents of layer %v: %w", layerID, err)
	}
	if len(layerBlocks) == 0 {
		// nothing to do
		t.logger.WithContext(ctx).Warning("cannot process empty layer block list")
		return nil
	}

	if t.Last.Before(layerID) {
		t.Last = layerID
	}

	logger.With().Info("tortoise handling incoming layer", log.Int("num_blocks", len(layerBlocks)))
	return t.processBlocks(ctx, layerBlocks)
}

// loops over all layers from the last verified up to a new target layer and attempts to verify each in turn
func (t *turtle) verifyLayers(ctx context.Context) error {
	logger := t.logger.WithContext(ctx).WithFields(
		log.FieldNamed("verification_target", t.Last),
		log.FieldNamed("old_verified", t.Verified))
	logger.Info("starting layer verification")

	// we perform eviction here because it should happen after the verified layer advances
	defer t.evict(ctx)

	// attempt to verify each layer from the last verified up to one prior to the newly-arrived layer.
	// this is the full range of unverified layers that we might possibly be able to verify at this point.
	// Note: t.Verified is initialized to the effective genesis layer, so the first candidate layer here necessarily
	// follows and is post-genesis. There's no need for an additional check here.
candidateLayerLoop:
	for candidateLayerID := t.Verified.Add(1); candidateLayerID.Before(t.Last); candidateLayerID = candidateLayerID.Add(1) {
		logger := logger.WithFields(log.FieldNamed("candidate_layer", candidateLayerID))

		// it's possible that self healing already verified a layer
		if !t.Verified.Before(candidateLayerID) {
			logger.Info("self healing already verified this layer")
			continue
		}

		logger.Info("attempting to verify candidate layer")

		// note: if the following checks fail, we just return rather than trying to verify later layers.
		// we don't presently support verifying layer N+1 when layer N hasn't been verified.

		layerBlockIds, err := t.bdp.LayerBlockIds(candidateLayerID)
		if err != nil {
			return fmt.Errorf("%s %v: %w", errstrCantFindLayer, candidateLayerID, err)
		}

		// get the local opinion for this layer. below, we calculate the global opinion on each block in the layer and
		// check if it agrees with this local opinion.
		rawLayerInputVector, err := t.layerOpinionVector(ctx, candidateLayerID)
		if err != nil {
			// an error here signifies a real database failure
			return fmt.Errorf("%s %v: %w", errstrUnableToCalculateLocalOpinion, candidateLayerID, err)
		}

		// otherwise, nil means we should abstain
		if rawLayerInputVector == nil {
			logger.With().Warning("input vector abstains on all blocks in layer", candidateLayerID)
		}
		localLayerOpinionVec := t.voteVectorForLayer(layerBlockIds, rawLayerInputVector)
		if len(localLayerOpinionVec) == 0 {
			// warn about this to be safe, but we do allow empty layers and must be able to verify them
			logger.With().Warning("empty vote vector for layer", candidateLayerID)
		}

		contextualValidity := make(map[types.BlockID]bool, len(layerBlockIds))

		// Count the votes of good blocks. localOpinionOnBlock is our local opinion on this block.
		// Declare the vote vector "verified" up to position k if the total weight exceeds the confidence threshold in
		// all positions up to k: in other words, we can verify a layer k if the total weight of the global opinion
		// exceeds the confidence threshold, and agrees with local opinion.
		for blockID, localOpinionOnBlock := range localLayerOpinionVec {
			// count the votes of the input vote vector by summing the voting weight of good blocks
			logger.With().Debug("summing votes for candidate layer block",
				blockID,
				log.FieldNamed("layer_start", candidateLayerID.Add(1)),
				log.FieldNamed("layer_end", t.Last))
			sum, err := t.sumVotesForBlock(ctx, blockID, candidateLayerID.Add(1), func(votingBlockID types.BlockID) bool {
				if _, isgood := t.GoodBlocksIndex[votingBlockID]; !isgood {
					logger.With().Debug("not counting vote of block not marked good",
						log.FieldNamed("voting_block", votingBlockID))
					return false
				}
				return true
			})
			if err != nil {
				return fmt.Errorf("error summing votes for block %v in candidate layer %v: %w",
					blockID, candidateLayerID, err)
			}

			// check that the total weight exceeds the global threshold
			globalOpinionOnBlock := calculateOpinionWithThreshold(
				t.logger, sum, t.GlobalThreshold, t.AvgLayerSize, t.Last.Difference(candidateLayerID))
			logger.With().Debug("verifying tortoise calculated global opinion on block",
				log.FieldNamed("block_voted_on", blockID),
				candidateLayerID,
				log.FieldNamed("global_vote_sum", sum),
				log.FieldNamed("global_opinion", globalOpinionOnBlock),
				log.FieldNamed("local_opinion", localOpinionOnBlock))

			// At this point, we have all of the data we need to make a decision on this block. There are three possible
			// outcomes:
			// 1. record our opinion on this block and go on evaluating the rest of the blocks in this layer to see if
			//    we can verify the layer (if local and global consensus match, and global consensus is decided)
			// 2. keep waiting to verify the layer (if not, and the layer is relatively recent)
			// 3. trigger self healing (if not, and the layer is sufficiently old)
			consensusMatches := globalOpinionOnBlock == localOpinionOnBlock
			globalOpinionDecided := globalOpinionOnBlock != abstain

			if consensusMatches && globalOpinionDecided {
				// Opinion on this block is decided, save and keep going
				contextualValidity[blockID] = globalOpinionOnBlock == support
				continue
			}

			// If, for any block in this layer, the global opinion (summed block votes) disagrees with our vote (the
			// input vector), or if the global opinion is abstain, then we do not verify this layer. This could be the
			// result of a reorg (e.g., resolution of a network partition), or a malicious peer during sync, or
			// disagreement about Hare success.
			if !consensusMatches {
				logger.With().Warning("global opinion on block differs from our vote, cannot verify layer",
					blockID,
					log.FieldNamed("global_opinion", globalOpinionOnBlock),
					log.FieldNamed("local_opinion", localOpinionOnBlock))
			}

			// There are only two scenarios that could result in a global opinion of abstain: if everyone is still
			// waiting for Hare to finish for a layer (i.e., it has not yet succeeded or failed), or a balancing attack.
			// The former is temporary and will go away after `zdist' layers. And it should be true of an entire layer,
			// not just of a single block. The latter could cause the global opinion of a single block to permanently
			// be abstain. As long as the contextual validity of any block in a layer is unresolved, we cannot verify
			// the layer (since the effectiveness of each transaction in the layer depends upon the contents of the
			// entire layer and transaction ordering). Therefore we have to enter self healing in this case.
			// TODO: abstain only for entire layer at a time, not for individual blocks (optimization)
			//   see https://github.com/spacemeshos/go-spacemesh/issues/2674
			if !globalOpinionDecided {
				logger.With().Warning("global opinion on block is abstain, cannot verify layer",
					blockID,
					log.FieldNamed("global_opinion", globalOpinionOnBlock),
					log.FieldNamed("local_opinion", localOpinionOnBlock))
			}

			// Verifying tortoise will wait `zdist' layers for consensus, then an additional `ConfidenceParam'
			// layers until all other nodes achieve consensus. If it's still stuck after this point, i.e., if the gap
			// between this unverified candidate layer and the latest layer is greater than this distance, then we trigger
			// self healing. But there's no point in trying to heal a layer that's not at least Hdist layers old since
			// we only consider the local opinion for recent layers.
			if candidateLayerID.After(t.Last) {
				logger.With().Panic("candidate layer is higher than last layer received",
					log.FieldNamed("last_layer", t.Last))
			}
			logger.With().Debug("considering attempting to heal layer",
				log.FieldNamed("layer_cutoff", t.layerCutoff()),
				log.Uint32("zdist", t.Zdist),
				log.FieldNamed("last_layer_received", t.Last),
				log.Uint32("confidence_param", t.ConfidenceParam))
			if candidateLayerID.Before(t.layerCutoff()) && t.Last.Difference(candidateLayerID) > t.Zdist+t.ConfidenceParam {
				lastLayer := t.Last
				// don't attempt to heal layers newer than Hdist
				if lastLayer.After(t.layerCutoff()) {
					lastLayer = t.layerCutoff()
				}
				lastVerified := t.Verified
				t.heal(ctx, lastLayer)

				// if self healing made progress, short-circuit processing of this layer, but allow verification of
				// later layers to continue
				if t.Verified.After(lastVerified) {
					// rescore goodness of blocks in all intervening layers on the basis of new information
					// TODO: this is inefficient and we can probably do better
					//   see https://github.com/spacemeshos/go-spacemesh/issues/2522
					for layerID := lastVerified.Add(1); !layerID.After(t.Last); layerID = layerID.Add(1) {
						// TODO LANE: this operation can be very expensive and very slow after a few layers,
						//   since no caching is currently performed. it needs to be optimized and cached.
						//   see https://github.com/spacemeshos/go-spacemesh/issues/2672
						if err := t.scoreBlocksByLayerID(ctx, layerID); err != nil {
							// if we fail to process a layer, there's probably no point in trying to rescore blocks
							// in later layers, so just print an error and bail
							logger.With().Error("error trying to rescore good blocks in healed layers",
								log.FieldNamed("layer_from", lastVerified),
								log.FieldNamed("layer_to", t.Last),
								log.Err(err))
							break
						}
					}

					continue candidateLayerLoop
				}
				// otherwise, if self healing didn't make any progress, there's no point in continuing to attempt
				// verification
			}

			// give up trying to verify layers and keep waiting
			// TODO: continue to verify later layers, even after failing to verify a layer.
			//   See https://github.com/spacemeshos/go-spacemesh/issues/2403
			logger.With().Info("failed to verify candidate layer, will reattempt later")
			return nil
		}

		// Declare the vote vector "verified" up to this layer and record the contextual validity for all blocks in this
		// layer
		for blk, v := range contextualValidity {
			if err := t.bdp.SaveContextualValidity(blk, candidateLayerID, v); err != nil {
				logger.With().Error("error saving contextual validity on block", blk, log.Err(err))
			}
		}
		t.Verified = candidateLayerID
		logger.With().Info("verified candidate layer", log.FieldNamed("new_verified", t.Verified))
	}

	return nil
}

// for layers older than this point, we vote according to global opinion (rather than local opinion)
func (t *turtle) layerCutoff() types.LayerID {
	// if we haven't seen at least Hdist layers yet, we always rely on local opinion
	if t.Last.Before(types.NewLayerID(t.Hdist)) {
		return types.NewLayerID(0)
	}
	return t.Last.Sub(t.Hdist)
}

// return the set of blocks we currently consider valid for the layer. it's based on both local and global opinion,
// depending how old the layer is, and uses weak coin to break ties.
func (t *turtle) layerOpinionVector(ctx context.Context, layerID types.LayerID) ([]types.BlockID, error) {
	logger := t.logger.WithContext(ctx).WithFields(layerID)
	var voteAbstain, voteAgainstAll []types.BlockID // nil slice, by default
	voteAgainstAll = make([]types.BlockID, 0, 0)

	// for layers older than hdist, we vote according to global opinion
	if layerID.Before(t.layerCutoff()) {
		if layerID.After(t.Verified) {
			// this layer has not yet been verified
			// we must have an opinion about older layers at this point. if the layer hasn't been verified yet, count votes
			// and see if they pass the local threshold. if not, use the current weak coin instead to determine our vote for
			// the blocks in the layer.
			// TODO: do we need to/can we somehow cache this?
			//   see https://github.com/spacemeshos/go-spacemesh/issues/2675
			layerBlockIds, err := t.bdp.LayerBlockIds(layerID)
			logger.With().Debug("counting votes for and against blocks in old, unverified layer",
				log.Int("num_blocks", len(layerBlockIds)))
			if err != nil {
				return nil, err
			}
			layerBlocks := make(map[types.BlockID]struct{}, len(layerBlockIds))
			for _, blockID := range layerBlockIds {
				logger := logger.WithFields(log.FieldNamed("candidate_block_id", blockID))
				// TODO: this operation can be expensive, it needs to be cached
				//   see https://github.com/spacemeshos/go-spacemesh/issues/2676
				sum, err := t.sumVotesForBlock(ctx, blockID, layerID.Add(1), func(id types.BlockID) bool { return true })
				if err != nil {
					return nil, fmt.Errorf("error summing votes for block %v in old layer %v: %w",
						blockID, layerID, err)
				}

				// TODO: should delta here represent layer depth, or should it always be 1?
				//   votes are counted for all layers!
				//   see https://github.com/spacemeshos/go-spacemesh/issues/2677
				localOpinionOnBlock := calculateOpinionWithThreshold(t.logger, sum, t.LocalThreshold, t.AvgLayerSize, 1)
				logger.With().Debug("local opinion on block in old layer",
					sum,
					log.FieldNamed("local_opinion", localOpinionOnBlock))
				if localOpinionOnBlock == support {
					layerBlocks[blockID] = struct{}{}
				} else if localOpinionOnBlock == abstain {
					// abstain means the votes for and against this block did not cross the local threshold.
					// if any block in this layer doesn't cross the local threshold, rescore the entire layer using the
					// weak coin.
					// note: we use the weak coin not for the layer of the block being voted on but rather for the
					// layer in which the voting block is created. this is because the weak coin must be generated late
					// enough that any adversarial block that depends on the value of the coin will not be accepted by
					// honest parties.
					// we use the weak coin for the _previous_ layer since we expect to receive blocks for a layer
					// before hare finishes for that layer, i.e., before the weak coin value is ready for the layer.
					// TODO: update this logic per https://github.com/spacemeshos/go-spacemesh/issues/2688

					// TODO: if we rescore old blocks, it's very likely that newly-created blocks will contain
					//   exceptions for those blocks, and their opinion will differ from their base blocks for blocks
					//   older than the base blocks, which will cause those blocks not to be marked good. is there
					//   anything we can do about this? e.g., explicitly pick base blocks that agree with the new
					//   opinion, or pick older base blocks.
					//   see https://github.com/spacemeshos/go-spacemesh/issues/2678
					if coin, exists := t.bdp.GetCoinflip(ctx, t.Last.Sub(1)); exists {
						logger.With().Info("rescoring all blocks in old layer using weak coin",
							log.Int("count", len(layerBlockIds)),
							log.Bool("coinflip", coin),
							log.FieldNamed("coinflip_layer", t.Last.Sub(1)))
						if coin {
							// heads on the weak coin means vote for all blocks in the layer
							return layerBlockIds, nil
						}
						// tails on the weak coin means vote against all blocks in the layer
						return voteAgainstAll, nil
					}
					return nil, fmt.Errorf("%s %v", errstrNoCoinflip, t.Last.Sub(1))
				} // (nothing to do if local opinion is against, just don't include block in output)
			}
			logger.With().Debug("local opinion supports blocks in old, unverified layer",
				log.Int("count", len(layerBlocks)))
			return blockMapToArray(layerBlocks), nil
		}
		// this layer has been verified, so we should be able to read the set of contextual blocks
		logger.Debug("using contextually valid blocks as opinion on old, verified layer")
		layerBlocks, err := t.bdp.LayerContextuallyValidBlocks(ctx, layerID)
		if err != nil {
			return nil, err
		}
		logger.With().Debug("got contextually valid blocks for layer",
			log.Int("count", len(layerBlocks)))
		return blockMapToArray(layerBlocks), nil
	}

	// for newer layers, we vote according to the local opinion (input vector, from hare or sync)
	opinionVec, err := t.bdp.GetLayerInputVectorByID(layerID)
	if err != nil {
		if errors.Is(err, mesh.ErrInvalidLayer) {
			// Hare already failed for this layer, so we want to vote against all blocks in the layer. Just return an
			// empty list.
			logger.Debug("local opinion is against all blocks in layer where hare failed")
			return voteAgainstAll, nil
		} else if t.Last.After(types.NewLayerID(t.Zdist)) && layerID.Before(t.Last.Sub(t.Zdist)) {
			// Layer has passed the Hare abort distance threshold, so we give up waiting for Hare results. At this point
			// our opinion on this layer is that we vote against blocks (i.e., we support an empty layer).
			logger.With().Debug("local opinion on layer beyond hare abort window is against all blocks",
				log.Err(err))
			return voteAgainstAll, nil
		} else {
			// Hare hasn't failed and layer has not passed the Hare abort threshold, so we abstain while we keep waiting
			// for Hare results.
			logger.With().Warning("local opinion abstains on all blocks in layer", log.Err(err))
			return voteAbstain, nil
		}
	}
	logger.With().Debug("got input vector for layer", log.Int("count", len(opinionVec)))
	return opinionVec, nil
}

func (t *turtle) sumVotesForBlock(
	ctx context.Context,
	blockID types.BlockID, // the block we're summing votes for/against
	startLayer types.LayerID,
	filter func(types.BlockID) bool,
) (sum vec, err error) {
	sum = abstain
	logger := t.logger.WithContext(ctx).WithFields(
		log.FieldNamed("start_layer", startLayer),
		log.FieldNamed("end_layer", t.Last),
		log.FieldNamed("block_voting_on", blockID),
		log.FieldNamed("layer_voting_on", startLayer.Sub(1)))
	for voteLayer := startLayer; !voteLayer.After(t.Last); voteLayer = voteLayer.Add(1) {
		logger := logger.WithFields(voteLayer)
		// logger.With().Debug("summing layer votes",
		// 	log.Int("count", len(t.BlockOpinionsByLayer[voteLayer])))
		for votingBlockID, votingBlockOpinion := range t.BlockOpinionsByLayer[voteLayer] {
			if !filter(votingBlockID) {
				logger.Debug("voting block did not pass filter, not counting its vote", log.FieldNamed("voting_block", votingBlockID))
				continue
			}

			// check if this block has an opinion on the block to vote on.
			// no opinion (on a block in an older layer) counts as an explicit vote against the block.
			// note: in this case, the weight is already factored into the vote, so no need to fetch weight.
			if opinionVote, exists := votingBlockOpinion[blockID]; exists {
				// logger.With().Debug("added block opinion to vote sum",
				// 	log.FieldNamed("voting_block", votingBlockID),
				// 	log.FieldNamed("vote", opinionVote),
				// 	sum)
				sum = sum.Add(opinionVote)
			} else {
				// in this case, we still need to fetch the block's voting weight.
				weight, err := t.voteWeightByID(ctx, votingBlockID, blockID)
				if err != nil {
					return sum, fmt.Errorf("error getting vote weight for block %v: %w",
						votingBlockID, err)
				}
				sum = sum.Add(against.Multiply(weight))
				logger.With().Debug("no opinion on older block, counted vote against",
					log.Uint64("weight", weight),
					sum)
			}
		}
	}
	return
}

// Manually count all votes for all layers since the last verified layer, up to the newly-arrived layer (there's no
// point in going further since we have no new information about any newer layers). Self-healing does not take into
// consideration local opinion, it relies solely on global opinion.
func (t *turtle) heal(ctx context.Context, targetLayerID types.LayerID) {
	// These are our starting values
	pbaseOld := t.Verified
	pbaseNew := t.Verified

	for candidateLayerID := pbaseOld.Add(1); candidateLayerID.Before(targetLayerID); candidateLayerID = candidateLayerID.Add(1) {
		logger := t.logger.WithContext(ctx).WithFields(
			log.FieldNamed("old_verified_layer", pbaseOld),
			log.FieldNamed("last_verified_layer", pbaseNew),
			log.FieldNamed("target_layer", targetLayerID),
			log.FieldNamed("candidate_layer", candidateLayerID),
			log.FieldNamed("last_layer_received", t.Last),
			log.Uint32("hdist", t.Hdist))

		// we should never run on layers newer than Hdist back (from last layer received)
		// when bootstrapping, don't attempt any verification at all
		var latestLayerWeCanVerify types.LayerID
		if t.Last.Before(types.NewLayerID(t.Hdist)) {
			latestLayerWeCanVerify = mesh.GenesisLayer().Index()
		} else {
			latestLayerWeCanVerify = t.Last.Sub(t.Hdist)
		}
		if candidateLayerID.After(latestLayerWeCanVerify) {
			logger.With().Error("cannot heal layer that's not at least hdist layers old",
				log.FieldNamed("highest_healable_layer", latestLayerWeCanVerify))
			return
		}

		// Calculate the global opinion on all blocks in the layer
		// Note: we look at ALL blocks we've seen for the layer, not just those we've previously marked contextually valid
		logger.Info("self healing attempting to verify candidate layer")

		layerBlockIds, err := t.bdp.LayerBlockIds(candidateLayerID)
		if err != nil {
			logger.Error("inconsistent state: can't find layer in database, cannot heal")

			// there's no point in trying to verify later layers so just give up now
			return
		}

		// record the contextual validity for all blocks in this layer
		for _, blockID := range layerBlockIds {
			logger := logger.WithFields(log.FieldNamed("candidate_block_id", blockID))

			// count all votes for or against this block by all blocks in later layers: don't filter out any
			sum, err := t.sumVotesForBlock(ctx, blockID, candidateLayerID.Add(1), func(id types.BlockID) bool { return true })
			if err != nil {
				logger.Error("error summing votes for candidate block in candidate layer", log.Err(err))
				return
			}

			// check that the total weight exceeds the global threshold
			globalOpinionOnBlock := calculateOpinionWithThreshold(t.logger, sum, t.GlobalThreshold, t.AvgLayerSize, t.Last.Difference(candidateLayerID))
			logger.With().Debug("self healing calculated global opinion on candidate block",
				log.FieldNamed("global_opinion", globalOpinionOnBlock),
				sum)

			if globalOpinionOnBlock == abstain {
				logger.With().Info("self healing failed to verify candidate layer, will reattempt later")
				return
			}

			isValid := globalOpinionOnBlock == support
			if err := t.bdp.SaveContextualValidity(blockID, candidateLayerID, isValid); err != nil {
				logger.With().Error("error saving block contextual validity", blockID, log.Err(err))
			}
		}

		t.Verified = candidateLayerID
		pbaseNew = candidateLayerID
		logger.Info("self healing verified candidate layer")
	}

	return
}
