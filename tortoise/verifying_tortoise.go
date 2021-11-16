package tortoise

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/tortoise/metrics"
)

type atxDataProvider interface {
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
}

var (
	errNoBaseBlockFound                 = errors.New("no good base block within exception vector limit")
	errNotSorted                        = errors.New("input blocks are not sorted by layerID")
	errstrNoCoinflip                    = "no weak coin value for layer"
	errstrTooManyExceptions             = "too many exceptions to base block vote"
	errstrConflictingVotes              = "conflicting votes found in block"
	errstrUnableToCalculateLocalOpinion = "unable to calculate local opinion for layer"
)

func blockMapToArray(m map[types.BlockID]struct{}) []types.BlockID {
	arr := make([]types.BlockID, 0, len(m))
	for b := range m {
		arr = append(arr, b)
	}
	return arr
}

type turtle struct {
	state
	logger log.Log

	atxdb   atxDataProvider
	bdp     blockDataProvider
	beacons blocks.BeaconGetter

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

	// we will delay counting votes in blocks with different beacon values by this many layers in self-healing.
	// for regular verifying tortoise runs, we don't count these votes at all.
	badBeaconVoteDelayLayers uint32

	// how often we want to rerun from genesis
	RerunInterval time.Duration
}

// newTurtle creates a new verifying tortoise algorithm instance.
func newTurtle(
	lg log.Log,
	db database.Database,
	bdp blockDataProvider,
	atxdb atxDataProvider,
	beacons blocks.BeaconGetter,
	hdist,
	zdist,
	confidenceParam,
	windowSize uint32,
	avgLayerSize uint32,
	globalThreshold,
	localThreshold *big.Rat,
	badBeaconVoteDelayLayers uint32,
) *turtle {
	return &turtle{
		state: state{
			diffMode:             true,
			db:                   db,
			log:                  lg,
			refBlockBeacons:      map[types.EpochID]map[types.BlockID][]byte{},
			badBeaconBlocks:      map[types.BlockID]struct{}{},
			GoodBlocksIndex:      map[types.BlockID]bool{},
			BlockOpinionsByLayer: map[types.LayerID]map[types.BlockID]Opinion{},
			BlockLayer:           map[types.BlockID]types.LayerID{},
		},
		logger:                   lg.Named("turtle"),
		Hdist:                    hdist,
		Zdist:                    zdist,
		ConfidenceParam:          confidenceParam,
		WindowSize:               windowSize,
		GlobalThreshold:          globalThreshold,
		LocalThreshold:           localThreshold,
		badBeaconVoteDelayLayers: badBeaconVoteDelayLayers,
		bdp:                      bdp,
		atxdb:                    atxdb,
		beacons:                  beacons,
		AvgLayerSize:             avgLayerSize,
		MaxExceptions:            int(hdist) * int(avgLayerSize) * 100,
	}
}

// cloneTurtleParams creates a new verifying tortoise instance using the params of this instance.
func (t *turtle) cloneTurtleParams() *turtle {
	return newTurtle(
		t.log,
		t.db,
		t.bdp,
		t.atxdb,
		t.beacons,
		t.Hdist,
		t.Zdist,
		t.ConfidenceParam,
		t.WindowSize,
		t.AvgLayerSize,
		t.GlobalThreshold,
		t.LocalThreshold,
		t.badBeaconVoteDelayLayers,
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
		t.BlockLayer[id] = genesisLayer.Index()
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
	if err := t.state.Evict(ctx, windowStart); err != nil {
		logger.With().Panic("can't evict persisted state", log.Err(err))
	}
}

// returns the local opinion on the validity of a block in a layer (support, against, or abstain).
func (t *turtle) getLocalBlockOpinion(ctx *tcontext, blid types.LayerID, bid types.BlockID) (vec, error) {
	if !blid.After(types.GetEffectiveGenesis()) {
		return support, nil
	}
	local, err := t.getLocalOpinion(ctx, blid)
	if err != nil {
		return abstain, err
	}
	return local[bid], nil
}

func (t *turtle) checkBlockAndGetLocalOpinion(
	ctx *tcontext,
	diffList []types.BlockID,
	className string,
	voteVector vec,
	baseBlockLayer types.LayerID,
	logger log.Logger,
) bool {
	for _, exceptionBlockID := range diffList {
		lid, exist := t.BlockLayer[exceptionBlockID]
		if !exist {
			// NOTE(dshulyak) if exception is out of sliding window it will not be found in t.BlockLayer,
			// in such case we look it up in db.
			// i am clarifying with a research if we can use same rule for exceptions as for base blocks
			exceptionBlock, err := t.bdp.GetBlock(exceptionBlockID)
			if err != nil {
				logger.With().Error("inconsistent state: can't find block from diff list",
					log.FieldNamed("exception_block_id", exceptionBlockID),
					log.Err(err),
				)
				return false
			}
			lid = exceptionBlock.LayerIndex
		}

		if lid.Before(baseBlockLayer) {
			logger.With().Error("good block candidate contains exception for block older than its base block",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", lid),
				log.FieldNamed("base_block_layer", baseBlockLayer))
			return false
		}

		v, err := t.getLocalBlockOpinion(ctx, lid, exceptionBlockID)
		if err != nil {
			logger.With().Error("unable to get single block opinion for block in exception list",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", lid),
				log.FieldNamed("base_block_layer", baseBlockLayer),
				log.Err(err))
			return false
		}

		if v != voteVector {
			logger.With().Debug("not adding block to good blocks because its vote differs from local opinion",
				log.FieldNamed("older_block", exceptionBlockID),
				log.FieldNamed("older_layer", lid),
				log.FieldNamed("local_opinion", v),
				log.String("block_exception_vote", className))
			return false
		}
	}
	return true
}

// BaseBlock selects a base block from sliding window based on a following priorities in order:
// - choose good block
// - choose block with the least difference to the local opinion
// - choose block from higher layer
// - otherwise deterministically select block with lowest id.
func (t *turtle) BaseBlock(ctx context.Context) (types.BlockID, [][]types.BlockID, error) {
	var (
		tctx          = wrapContext(ctx)
		logger        = t.logger.WithContext(ctx)
		disagreements = map[types.BlockID]types.LayerID{}
		choices       []types.BlockID // choices from the best to the least bad

		// TODO change it per https://github.com/spacemeshos/go-spacemesh/issues/2920
		votinglid = t.Last
	)

	for lid := t.LastEvicted.Add(1); !lid.After(t.Last); lid = lid.Add(1) {
		for bid := range t.BlockOpinionsByLayer[lid] {
			dis, err := t.firstDisagreement(tctx, lid, bid, disagreements)
			if err != nil {
				logger.With().Error("failed to compute first disagremement", bid, log.Err(err))
				continue
			}
			disagreements[bid] = dis
			choices = append(choices, bid)
		}
	}

	prioritizeBlocks(choices, t.GoodBlocksIndex, disagreements, t.BlockLayer)

	for _, bid := range choices {
		lid := t.BlockLayer[bid]
		exceptions, err := t.calculateExceptions(tctx, votinglid, lid, t.BlockOpinionsByLayer[lid][bid])
		if err != nil {
			logger.With().Warning("error calculating vote exceptions for block", bid, log.Err(err))
			continue
		}
		logger.With().Info("chose base block",
			bid,
			log.Int("against_count", len(exceptions[0])),
			log.Int("support_count", len(exceptions[1])),
			log.Int("neutral_count", len(exceptions[2])))

		metrics.LayerDistanceToBaseBlock.WithLabelValues().Observe(float64(t.Last.Value - lid.Value))

		return bid, [][]types.BlockID{
			blockMapToArray(exceptions[0]),
			blockMapToArray(exceptions[1]),
			blockMapToArray(exceptions[2]),
		}, nil
	}

	// TODO: special error encoding when exceeding exception list size
	return types.BlockID{}, nil, errNoBaseBlockFound
}

// firstDisagreement returns first layer where local opinion is different from blocks opinion within sliding window.
func (t *turtle) firstDisagreement(ctx *tcontext, blid types.LayerID, bid types.BlockID, disagreements map[types.BlockID]types.LayerID) (types.LayerID, error) {
	var (
		opinions = t.BlockOpinionsByLayer[blid][bid]
		from     = t.LastEvicted.Add(1)
		// using it as a mark that the votes for block are completely consistent
		// with a local opinion. so if two blocks have consistent histories select block
		// from a higher layer as it is more consistent.
		consistent = t.Last
	)

	// if the block is good we know that there are no exceptions that precede base block.
	// in such case current block will not fix any previous disagreements of the base block.
	// or if the base block completely agrees with local opinion compute disagreements after base block.
	if _, exist := t.GoodBlocksIndex[bid]; exist {
		block, err := t.bdp.GetBlock(bid)
		if err != nil {
			return types.LayerID{}, fmt.Errorf("reading block %s: %w", bid, err)
		}
		basedis, exist := disagreements[block.BaseBlock]
		if exist {
			baselid := t.BlockLayer[block.BaseBlock]
			if basedis.Before(consistent) {
				return basedis, nil
			}
			from = baselid
		}
	}
	for lid := from; lid.Before(blid); lid = lid.Add(1) {
		locals, err := t.getLocalOpinion(ctx, lid)
		if err != nil {
			return types.LayerID{}, err
		}
		for on, local := range locals {
			opinion, exist := opinions[on]
			if !exist {
				opinion = against
			}
			if !equalVotes(local, opinion) {
				return lid, nil
			}
		}
	}
	return consistent, nil
}

// calculate and return a list of exceptions, i.e., differences between the opinions of a base block and the local
// opinion.
func (t *turtle) calculateExceptions(
	ctx *tcontext,
	votinglid,
	baselid types.LayerID,
	baseopinion Opinion, // candidate base block's opinion vector
) ([]map[types.BlockID]struct{}, error) {
	logger := t.logger.WithContext(ctx).WithFields(log.FieldNamed("base_block_layer_id", baselid))

	// using maps prevents duplicates
	againstDiff := make(map[types.BlockID]struct{})
	forDiff := make(map[types.BlockID]struct{})
	neutralDiff := make(map[types.BlockID]struct{})

	// we support all genesis blocks by default
	if baselid == types.GetEffectiveGenesis() {
		for _, i := range types.BlockIDs(mesh.GenesisLayer().Blocks()) {
			forDiff[i] = struct{}{}
		}
	}

	// Add latest layers input vector results to the diff
	// Note: a block may only be selected as a candidate base block if it's marked "good", and it may only be marked
	// "good" if its own base block is marked "good" and all exceptions it contains agree with our local opinion.
	// We only look for and store exceptions within the sliding window set of layers as an optimization, but a block
	// can contain exceptions from any layer, back to genesis.
	for lid := t.LastEvicted.Add(1); !lid.After(t.Last); lid = lid.Add(1) {
		logger := logger.WithFields(log.FieldNamed("diff_layer_id", lid))
		logger.Debug("checking input vector diffs")

		local, err := t.getLocalOpinion(ctx, lid)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				continue
			}
			return nil, err
		}

		// helper function for adding diffs
		addDiffs := func(logger log.Log, bid types.BlockID, voteVec vec, diffMap map[types.BlockID]struct{}) {
			if v, ok := baseopinion[bid]; !ok || simplifyVote(v) != voteVec {
				logger.With().Debug("added vote diff", log.Named("opinion", v))
				if lid.Before(baselid) {
					logger.With().Warning("added exception before base block layer, this block will not be marked good")
				}
				diffMap[bid] = struct{}{}
			}
		}

		for bid, opinion := range local {
			usecoinflip := opinion == abstain && lid.Before(t.layerCutoff())
			if usecoinflip {
				if votinglid.After(types.GetEffectiveGenesis().Add(1)) {
					coin, exist := t.bdp.GetCoinflip(ctx, votinglid.Sub(1))
					if !exist {
						return nil, fmt.Errorf("coinflip is not recorded in %s", votinglid.Sub(1))
					}
					if coin {
						opinion = support
					} else {
						opinion = against
					}
				}
			}
			logger := logger.WithFields(
				log.Bool("use_coinflip", usecoinflip),
				log.Named("diff_block", bid),
				log.String("diff_class", opinion.String()),
			)
			switch opinion {
			case support:
				addDiffs(logger, bid, support, forDiff)
			case abstain:
				addDiffs(logger, bid, abstain, neutralDiff)
			case against:
				// Finally, we need to consider the case where the base block supports a block in this layer that is not in our
				// input vector (e.g., one we haven't seen), by adding a diff against the block.
				// We do not explicitly add votes against blocks that the base block does _not_ support, since by not voting to
				// support a block in an old layer, we are implicitly voting against it. But if the base block does explicitly
				// support a block and we disagree, we need to add a vote against here.
				//
				// TODO(dshulyak) consider what is written above and in https://github.com/spacemeshos/go-spacemesh/issues/2424
				// but generally we will not process a block if dependencies are not fetched
				// so the block that has some unknown to us dependency will not be even added to tortoise state
				// addDiffs(bid, logger, against, againstDiff)
			}
		}
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
		return 0, fmt.Errorf("get ATX header: %w", err)
	}

	blockWeight := atxHeader.GetWeight()
	logger.With().Debug("voting block atx was timely",
		votingBlock.ID(),
		votingBlock.ATXID,
		log.Uint64("block_weight", blockWeight))
	return blockWeight, nil
}

func (t *turtle) voteWeightByID(ctx context.Context, votingBlockID types.BlockID) (uint64, error) {
	block, err := t.bdp.GetBlock(votingBlockID)
	if err != nil {
		return 0, fmt.Errorf("get block: %w", err)
	}
	return t.voteWeight(ctx, block)
}

// Persist saves the current tortoise state to the database.
func (t *turtle) persist() error {
	return t.state.Persist()
}

func (t *turtle) getBaseBlockOpinions(bid types.BlockID) Opinion {
	lid, ok := t.BlockLayer[bid]
	if !ok {
		return nil
	}
	byLayer, ok := t.BlockOpinionsByLayer[lid]
	if !ok {
		return nil
	}
	return byLayer[bid]
}

func (t *turtle) processBlock(ctx context.Context, block *types.Block) error {
	logger := t.logger.WithContext(ctx).WithFields(
		log.FieldNamed("processing_block_id", block.ID()),
		log.FieldNamed("processing_block_layer", block.LayerIndex))

	// When a new block arrives, we look up the block it points to in our table,
	// and add the corresponding vector (multiplied by the block weight) to our own vote-totals vector.
	// We then add the vote difference vector and the explicit vote vector to our vote-totals vector.
	logger.With().Debug("processing block", block.Fields()...)

	logger.With().Debug("block adds support for",
		log.Int("count", len(block.BlockHeader.ForDiff)),
		types.BlockIdsField(block.BlockHeader.ForDiff))

	baseBlockOpinion := t.getBaseBlockOpinions(block.BaseBlock)
	if len(baseBlockOpinion) == 0 {
		logger.With().Warning("base block opinions are empty. base block is unknown or was evicted",
			block.BaseBlock,
		)
	}

	voteWeight, err := t.voteWeight(ctx, block)
	if err != nil {
		return fmt.Errorf("error getting vote weight for block %v: %w", block.ID(), err)
	}

	// TODO: this logic would be simpler if For and Against were a single list
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2369
	// TODO: save and vote against blocks that exceed the max exception list size (DoS prevention)
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2673
	lth := len(block.ForDiff) +
		len(block.NeutralDiff) +
		len(block.AgainstDiff) +
		len(baseBlockOpinion)
	opinion := make(map[types.BlockID]vec, lth)

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
		// ignore opinions on very old blocks
		_, exist := t.BlockLayer[blk]
		if !exist {
			continue
		}
		if _, exist := opinion[blk]; !exist {
			nvote := simplifyVote(vote).Multiply(voteWeight)
			opinion[blk] = nvote
		}
	}
	t.BlockLayer[block.ID()] = block.LayerIndex
	logger.With().Debug("adding or updating block opinion")
	t.BlockOpinionsByLayer[block.LayerIndex][block.ID()] = opinion
	return nil
}

func (t *turtle) processBlocks(ctx *tcontext, blocks []*types.Block) error {
	logger := t.logger.WithContext(ctx)
	lastLayerID := types.NewLayerID(0)

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

func (t *turtle) scoreBlocksByLayerID(ctx *tcontext, layerID types.LayerID) error {
	blocks, err := t.bdp.LayerBlocks(layerID)
	if err != nil {
		return fmt.Errorf("layer blocks: %w", err)
	}
	t.scoreBlocks(ctx, blocks)
	return nil
}

func (t *turtle) scoreBlocks(ctx *tcontext, blocks []*types.Block) {
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

func (t *turtle) determineBlockGoodness(ctx *tcontext, block *types.Block) bool {
	logger := t.logger.WithContext(ctx).WithFields(
		block.ID(),
		block.LayerIndex,
		log.FieldNamed("base_block_id", block.BaseBlock))
	// Go over all blocks, in order. Mark block i "good" if:
	// (1) it has the right beacon value
	if !t.blockHasGoodBeacon(block, logger) {
		return false
	}
	// (2) the base block is marked as good
	if _, good := t.GoodBlocksIndex[block.BaseBlock]; !good {
		logger.Debug("base block is not good")
	} else if baselid, exist := t.BlockLayer[block.BaseBlock]; !exist {
		logger.With().Error("inconsistent state: base block not found")
	} else if true &&
		// (3) all diffs appear after the base block and are consistent with the current local opinion
		t.checkBlockAndGetLocalOpinion(ctx, block.ForDiff, "support", support, baselid, logger) &&
		t.checkBlockAndGetLocalOpinion(ctx, block.AgainstDiff, "against", against, baselid, logger) &&
		t.checkBlockAndGetLocalOpinion(ctx, block.NeutralDiff, "abstain", abstain, baselid, logger) {
		logger.Debug("block is good")
		return true
	}
	logger.Debug("block is not good")
	return false
}

func (t *turtle) blockHasGoodBeacon(block *types.Block, logger log.Log) bool {
	layerID := block.LayerIndex

	// first check if we have it in the cache
	if _, bad := t.badBeaconBlocks[block.ID()]; bad {
		return false
	}

	epochBeacon, err := t.beacons.GetBeacon(layerID.GetEpoch())
	if err != nil {
		logger.Error("failed to get beacon for epoch", layerID.GetEpoch())
		return false
	}

	beacon, err := t.getBlockBeacon(block, logger)
	if err != nil {
		return false
	}
	good := bytes.Equal(beacon, epochBeacon)
	if !good {
		logger.With().Warning("block has different beacon",
			log.String("block_beacon", types.BytesToHash(beacon).ShortString()),
			log.String("epoch_beacon", types.BytesToHash(epochBeacon).ShortString()))
		t.badBeaconBlocks[block.ID()] = struct{}{}
	}
	return good
}

func (t *turtle) getBlockBeacon(block *types.Block, logger log.Log) ([]byte, error) {
	refBlockID := block.ID()
	if block.RefBlock != nil {
		refBlockID = *block.RefBlock
	}

	epoch := block.LayerIndex.GetEpoch()
	beacons, ok := t.refBlockBeacons[epoch]
	if ok {
		if beacon, ok := beacons[refBlockID]; ok {
			return beacon, nil
		}
	} else {
		t.refBlockBeacons[epoch] = make(map[types.BlockID][]byte)
	}

	beacon := block.TortoiseBeacon
	if block.RefBlock != nil {
		refBlock, err := t.bdp.GetBlock(refBlockID)
		if err != nil {
			logger.With().Error("failed to find ref block",
				log.String("ref_block_id", refBlockID.AsHash32().ShortString()))
			return nil, fmt.Errorf("get ref block: %w", err)
		}
		beacon = refBlock.TortoiseBeacon
	}
	t.refBlockBeacons[epoch][refBlockID] = beacon
	return beacon, nil
}

// HandleIncomingLayer processes all layer block votes
// returns the old pbase and new pbase after taking into account block votes.
func (t *turtle) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) error {
	defer t.evict(ctx)

	tctx := wrapContext(ctx)
	// unconditionally set the layer to the last one that we have seen once tortoise or sync
	// submits this layer. it doesn't matter if we fail to process it.
	if t.Last.Before(layerID) {
		t.Last = layerID
	}
	if err := t.handleLayerBlocks(tctx, layerID); err != nil {
		return err
	}
	// attempt to verify layers up to the latest one for which we have new block data
	return t.verifyLayers(tctx)
}

func (t *turtle) handleLayerBlocks(ctx *tcontext, layerID types.LayerID) error {
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
		t.logger.WithContext(ctx).Warning("cannot process empty layer block list")
		return nil
	}
	return t.processBlocks(ctx, layerBlocks)
}

func (t *turtle) verifyingTortoise(ctx *tcontext, logger log.Log, lid types.LayerID) (map[types.BlockID]bool, error) {
	localOpinion, err := t.getLocalOpinion(ctx, lid)
	if err != nil {
		return nil, err
	}
	if len(localOpinion) == 0 {
		// warn about this to be safe, but we do allow empty layers and must be able to verify them
		logger.With().Warning("empty vote vector for layer", lid)
	}

	contextualValidity := make(map[types.BlockID]bool, len(localOpinion))

	// Count the votes of good blocks. localOpinionOnBlock is our local opinion on this block.
	// Declare the vote vector "verified" up to position k if the total weight exceeds the confidence threshold in
	// all positions up to k: in other words, we can verify a layer k if the total weight of the global opinion
	// exceeds the confidence threshold, and agrees with local opinion.
	for bid, localOpinionOnBlock := range localOpinion {
		// count the votes of the input vote vector by summing the voting weight of good blocks
		logger.With().Debug("summing votes for candidate layer block",
			bid,
			log.FieldNamed("layer_start", lid.Add(1)),
			log.FieldNamed("layer_end", t.Last),
		)
		sum, err := t.sumVotesForBlock(ctx, bid, lid.Add(1), func(votingBlockID types.BlockID) bool {
			if _, isgood := t.GoodBlocksIndex[votingBlockID]; !isgood {
				logger.With().Debug("not counting vote of block not marked good",
					log.FieldNamed("voting_block", votingBlockID))
				return false
			}
			return true
		})
		if err != nil {
			return nil, fmt.Errorf("error summing votes for block %v in candidate layer %v: %w",
				bid, lid, err)
		}

		// check that the total weight exceeds the global threshold
		globalOpinionOnBlock := calculateOpinionWithThreshold(
			t.logger, sum, t.GlobalThreshold, t.AvgLayerSize, t.Last.Difference(lid))
		logger.With().Debug("verifying tortoise calculated global opinion on block",
			log.FieldNamed("block_voted_on", bid),
			lid,
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
			contextualValidity[bid] = globalOpinionOnBlock == support
			continue
		}

		// If, for any block in this layer, the global opinion (summed block votes) disagrees with our vote (the
		// input vector), or if the global opinion is abstain, then we do not verify this layer. This could be the
		// result of a reorg (e.g., resolution of a network partition), or a malicious peer during sync, or
		// disagreement about Hare success.
		if !consensusMatches {
			logger.With().Warning("global opinion on block differs from our vote, cannot verify layer",
				bid,
				log.FieldNamed("global_opinion", globalOpinionOnBlock),
				log.FieldNamed("local_opinion", localOpinionOnBlock))
			return nil, nil
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
				bid,
				log.FieldNamed("global_opinion", globalOpinionOnBlock),
				log.FieldNamed("local_opinion", localOpinionOnBlock))
			return nil, nil
		}
	}
	return contextualValidity, nil
}

func (t *turtle) healingTortoise(ctx *tcontext, logger log.Log, lid types.LayerID) (bool, error) {
	// Verifying tortoise will wait `zdist' layers for consensus, then an additional `ConfidenceParam'
	// layers until all other nodes achieve consensus. If it's still stuck after this point, i.e., if the gap
	// between this unverified candidate layer and the latest layer is greater than this distance, then we trigger
	// self healing. But there's no point in trying to heal a layer that's not at least Hdist layers old since
	// we only consider the local opinion for recent layers.
	logger.With().Debug("considering attempting to heal layer",
		log.FieldNamed("layer_cutoff", t.layerCutoff()),
		log.Uint32("zdist", t.Zdist),
		log.FieldNamed("last_layer_received", t.Last),
		log.Uint32("confidence_param", t.ConfidenceParam))
	if !(lid.Before(t.layerCutoff()) && t.Last.Difference(lid) > t.Zdist+t.ConfidenceParam) {
		return false, nil
	}
	logger.With().Info("start self-healing with verified layer", t.Verified)
	lastLayer := t.Last
	// don't attempt to heal layers newer than Hdist
	if lastLayer.After(t.layerCutoff()) {
		lastLayer = t.layerCutoff()
	}
	lastVerified := t.Verified
	t.heal(ctx, lastLayer)
	logger.With().Info("finished self-healing with verified layer", t.Verified)

	if !t.Verified.After(lastVerified) {
		// if self healing didn't make any progress, there's no point in continuing to attempt
		// verification
		// give up trying to verify layers and keep waiting
		logger.With().Info("failed to verify candidate layer, will reattempt later")
		return false, nil
	}

	// reinitialize context because contextually valid blocks were changed
	ctx = wrapContext(ctx)
	// rescore goodness of blocks in all intervening layers on the basis of new information
	for layerID := lastVerified.Add(1); !layerID.After(t.Last); layerID = layerID.Add(1) {
		if err := t.scoreBlocksByLayerID(ctx, layerID); err != nil {
			// if we fail to process a layer, there's probably no point in trying to rescore blocks
			// in later layers, so just print an error and bail
			logger.With().Error("error trying to rescore good blocks in healed layers",
				log.FieldNamed("layer_from", lastVerified),
				log.FieldNamed("layer_to", t.Last),
				log.Err(err))
			return false, err
		}
	}
	return true, nil
}

// loops over all layers from the last verified up to a new target layer and attempts to verify each in turn.
func (t *turtle) verifyLayers(ctx *tcontext) error {
	logger := t.logger.WithContext(ctx).WithFields(
		log.FieldNamed("verification_target", t.Last),
		log.FieldNamed("old_verified", t.Verified))

	// attempt to verify each layer from the last verified up to one prior to the newly-arrived layer.
	// this is the full range of unverified layers that we might possibly be able to verify at this point.
	// Note: t.Verified is initialized to the effective genesis layer, so the first candidate layer here necessarily
	// follows and is post-genesis. There's no need for an additional check here.
	for candidateLayerID := t.Verified.Add(1); candidateLayerID.Before(t.Last); candidateLayerID = candidateLayerID.Add(1) {
		logger := logger.WithFields(log.FieldNamed("candidate_layer", candidateLayerID))

		// it's possible that self healing already verified a layer
		if !t.Verified.Before(candidateLayerID) {
			logger.Info("self healing already verified this layer")
			continue
		}

		logger.Info("attempting to verify candidate layer")
		contextualValidity, err := t.verifyingTortoise(ctx, logger, candidateLayerID)
		if err != nil {
			return err
		}
		if contextualValidity != nil {
			// Declare the vote vector "verified" up to this layer and record the contextual validity for all blocks in this
			// layer
			for blk, v := range contextualValidity {
				if err := t.bdp.SaveContextualValidity(blk, candidateLayerID, v); err != nil {
					logger.With().Error("error saving contextual validity on block", blk, log.Err(err))
				}
			}
			t.Verified = candidateLayerID
			logger.With().Info("verified candidate layer", log.FieldNamed("new_verified", t.Verified))
		} else {
			healed, err := t.healingTortoise(ctx, logger, candidateLayerID)
			if err != nil || !healed {
				return err
			}
		}
	}
	return nil
}

// for layers older than this point, we vote according to global opinion (rather than local opinion).
func (t *turtle) layerCutoff() types.LayerID {
	// if we haven't seen at least Hdist layers yet, we always rely on local opinion
	if t.Last.Before(types.NewLayerID(t.Hdist)) {
		return types.NewLayerID(0)
	}
	return t.Last.Sub(t.Hdist)
}

func (t *turtle) computeLocalOpinion(ctx *tcontext, lid types.LayerID) (map[types.BlockID]vec, error) {
	var (
		logger  = t.logger.WithContext(ctx).WithFields(lid)
		opinion = Opinion{}
	)
	bids, err := getLayerBlocksIDs(ctx, t.bdp, lid)
	if err != nil {
		return nil, fmt.Errorf("layer block IDs: %w", err)
	}
	for _, bid := range bids {
		opinion[bid] = against
	}

	if !lid.Before(t.layerCutoff()) {
		// for newer layers, we vote according to the local opinion (input vector, from hare or sync)
		opinionVec, err := getInputVector(ctx, t.bdp, lid)
		if err != nil {
			if t.Last.After(types.NewLayerID(t.Zdist)) && lid.Before(t.Last.Sub(t.Zdist)) {
				// Layer has passed the Hare abort distance threshold, so we give up waiting for Hare results. At this point
				// our opinion on this layer is that we vote against blocks (i.e., we support an empty layer).
				return opinion, nil
			}
			// Hare hasn't failed and layer has not passed the Hare abort threshold, so we abstain while we keep waiting
			// for Hare results.
			logger.With().Warning("local opinion abstains on all blocks in layer", log.Err(err))
			for _, bid := range bids {
				opinion[bid] = abstain
			}
			return opinion, nil
		}
		for _, bid := range opinionVec {
			opinion[bid] = support
		}
		return opinion, nil
	}

	// for layers older than hdist, we vote according to global opinion
	if !lid.After(t.Verified) {
		// this layer has been verified, so we should be able to read the set of contextual blocks
		logger.Debug("using contextually valid blocks as opinion on old, verified layer")
		valid, err := getValidBlocks(ctx, t.bdp, lid)
		if err != nil {
			return nil, err
		}
		for _, bid := range valid {
			opinion[bid] = support
		}
		return opinion, nil
	}

	// this layer has not yet been verified
	// we must have an opinion about older layers at this point. if the layer hasn't been verified yet, count votes
	// and see if they pass the local threshold.
	logger.With().Debug("counting votes for and against blocks in old, unverified layer",
		log.Int("num_blocks", len(bids)))
	for _, bid := range bids {
		logger := logger.WithFields(log.FieldNamed("candidate_block_id", bid))
		sum, err := t.sumVotesForBlock(ctx, bid, lid.Add(1), func(id types.BlockID) bool { return true })
		if err != nil {
			return nil, fmt.Errorf("error summing votes for block %v in old layer %v: %w",
				bid, lid, err)
		}

		local := calculateOpinionWithThreshold(t.logger, sum, t.LocalThreshold, t.AvgLayerSize, 1)
		logger.With().Debug("local opinion on block in old layer",
			sum,
			log.FieldNamed("local_opinion", local),
		)
		opinion[bid] = local
	}

	return opinion, nil
}

// return the set of blocks we currently consider valid for the layer. it's based on both local and global opinion,
// depending how old the layer is, and uses weak coin to break ties.
func (t *turtle) layerOpinionVector(ctx *tcontext, lid types.LayerID) ([]types.BlockID, error) {
	var (
		logger = t.logger.WithContext(ctx).WithFields(lid)

		voteAbstain    []types.BlockID
		voteAgainstAll = []types.BlockID{}
	)

	if !lid.Before(t.layerCutoff()) {
		// for newer layers, we vote according to the local opinion (input vector, from hare or sync)
		opinionVec, err := getInputVector(ctx, t.bdp, lid)
		if err != nil {
			if t.Last.After(types.NewLayerID(t.Zdist)) && lid.Before(t.Last.Sub(t.Zdist)) {
				// Layer has passed the Hare abort distance threshold, so we give up waiting for Hare results. At this point
				// our opinion on this layer is that we vote against blocks (i.e., we support an empty layer).
				return voteAgainstAll, nil
			}
			// Hare hasn't failed and layer has not passed the Hare abort threshold, so we abstain while we keep waiting
			// for Hare results.
			logger.With().Warning("local opinion abstains on all blocks in layer", log.Err(err))
			return voteAbstain, nil
		}
		logger.With().Debug("got contextually valid blocks for layer",
			log.Int("count", len(opinionVec)))
		return opinionVec, nil
	}

	// for layers older than hdist, we vote according to global opinion
	if !lid.After(t.Verified) {
		// this layer has been verified, so we should be able to read the set of contextual blocks
		logger.Debug("using contextually valid blocks as opinion on old, verified layer")
		return getValidBlocks(ctx, t.bdp, lid)
	}

	// this layer has not yet been verified
	// we must have an opinion about older layers at this point. if the layer hasn't been verified yet, count votes
	// and see if they pass the local threshold. if not, use the current weak coin instead to determine our vote for
	// the blocks in the layer.
	bids, err := getLayerBlocksIDs(ctx, t.bdp, lid)
	if err != nil {
		return nil, fmt.Errorf("layer block IDs: %w", err)
	}
	logger.With().Debug("counting votes for and against blocks in old, unverified layer",
		log.Int("num_blocks", len(bids)))
	supported := make([]types.BlockID, 0, len(bids))
	for _, bid := range bids {
		logger := logger.WithFields(log.FieldNamed("candidate_block_id", bid))
		sum, err := t.sumVotesForBlock(ctx, bid, lid.Add(1), func(id types.BlockID) bool { return true })
		if err != nil {
			return nil, fmt.Errorf("error summing votes for block %v in old layer %v: %w",
				bid, lid, err)
		}

		localOpinionOnBlock := calculateOpinionWithThreshold(t.logger, sum, t.LocalThreshold, t.AvgLayerSize, 1)
		logger.With().Debug("local opinion on block in old layer",
			sum,
			log.FieldNamed("local_opinion", localOpinionOnBlock))
		if localOpinionOnBlock == support {
			supported = append(supported, bid)
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
					log.Int("count", len(bids)),
					log.Bool("coinflip", coin),
					log.FieldNamed("coinflip_layer", t.Last.Sub(1)))
				if coin {
					// heads on the weak coin means vote for all blocks in the layer
					return bids, nil
				}
				// tails on the weak coin means vote against all blocks in the layer
				return voteAgainstAll, nil
			}
			return nil, fmt.Errorf("%s %v", errstrNoCoinflip, t.Last.Sub(1))
		} // (nothing to do if local opinion is against, just don't include block in output)
	}
	logger.With().Debug("local opinion supports blocks in old, unverified layer",
		log.Int("count", len(supported)))
	return supported, nil
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
				weight, err := t.voteWeightByID(ctx, votingBlockID)
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
func (t *turtle) heal(ctx *tcontext, targetLayerID types.LayerID) {
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

		layerBlockIds, err := getLayerBlocksIDs(ctx, t.bdp, candidateLayerID)
		if err != nil {
			logger.Error("inconsistent state: can't find layer in database, cannot heal")
			return
		}

		// record the contextual validity for all blocks in this layer
		for _, blockID := range layerBlockIds {
			logger := logger.WithFields(log.FieldNamed("candidate_block_id", blockID))

			// count all votes for or against this block by all blocks in later layers. for blocks with a different
			// beacon values, we delay their votes by badBeaconVoteDelays layers
			sum, err := t.sumVotesForBlock(ctx, blockID, candidateLayerID.Add(1), t.voteBlockFilterForHealing(candidateLayerID, logger))
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

// only blocks with the correct beacon value are considered good blocks and their votes counted by
// verifying tortoise. for blocks with a different beacon values, we count their votes only in self-healing mode
// and delay their votes by badBeaconVoteDelays layers.
func (t *turtle) voteBlockFilterForHealing(candidateLayerID types.LayerID, logger log.Log) func(types.BlockID) bool {
	return func(bid types.BlockID) bool {
		if _, bad := t.badBeaconBlocks[bid]; !bad {
			return true
		}
		voteLayer, exist := t.BlockLayer[bid]
		if !exist {
			logger.With().Error("inconsistent state: voting block not found", bid)
			return false
		}
		return voteLayer.Uint32() > t.badBeaconVoteDelayLayers && voteLayer.Sub(t.badBeaconVoteDelayLayers).After(candidateLayerID)
	}
}
