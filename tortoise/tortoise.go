package tortoise

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/metrics"
)

var (
	errNoBaseBallotFound    = errors.New("no good base ballot within exception vector limit")
	errstrTooManyExceptions = "too many exceptions to base ballot vote"
)

type turtle struct {
	Config
	state
	logger log.Log

	atxdb   atxDataProvider
	bdp     blockDataProvider
	beacons system.BeaconGetter

	common    commonState
	verifying *verifying
	full      fullState
}

// newTurtle creates a new verifying tortoise algorithm instance.
func newTurtle(
	logger log.Log,
	bdp blockDataProvider,
	atxdb atxDataProvider,
	beacons system.BeaconGetter,
	config Config,
) *turtle {
	t := &turtle{
		Config:  config,
		common:  newCommonState(),
		full:    newFullState(),
		logger:  logger,
		bdp:     bdp,
		atxdb:   atxdb,
		beacons: beacons,
	}
	t.verifying = newVerifying(logger, config, t.common)
	return t
}

// cloneTurtleParams creates a new verifying tortoise instance using the params of this instance.
func (t *turtle) cloneTurtleParams() *turtle {
	return newTurtle(
		t.logger,
		t.bdp,
		t.atxdb,
		t.beacons,
		t.Config,
	)
}

func (t *turtle) init(ctx context.Context, genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	t.logger.WithContext(ctx).With().Info("initializing genesis layer for verifying tortoise",
		genesisLayer.Index(),
		genesisLayer.Hash().Field())
	t.BallotOpinionsByLayer[genesisLayer.Index()] = make(map[types.BallotID]Opinion)
	for _, blk := range genesisLayer.Blocks() {
		ballot := blk.ToBallot()
		id := ballot.ID()
		t.BallotOpinionsByLayer[genesisLayer.Index()][id] = Opinion{}
		t.BallotLayer[id] = genesisLayer.Index()
		t.BlockLayer[blk.ID()] = genesisLayer.Index()
		t.GoodBallotsIndex[id] = false // false means good block, not flushed
	}
	t.Last = genesisLayer.Index()
	t.Processed = genesisLayer.Index()
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
		log.Named("effective_genesis", types.GetEffectiveGenesis()),
		log.Uint32("hdist", t.Hdist),
		log.Named("verified", t.Verified),
		log.Uint32("window_size", t.WindowSize),
		log.Named("last_evicted", t.LastEvicted),
		log.Named("window_start", windowStart))

	// evict from last evicted to the beginning of our window
	t.state.Evict(ctx, windowStart)
}

// BaseBallot selects a base ballot from sliding window based on a following priorities in order:
// - choose good ballot
// - choose ballot with the least difference to the local opinion
// - choose ballot from higher layer
// - otherwise deterministically select ballot with lowest id.
func (t *turtle) BaseBallot(ctx context.Context) (types.BallotID, [][]types.BlockID, error) {
	var (
		tctx          = wrapContext(ctx)
		logger        = t.logger.WithContext(ctx)
		disagreements = map[types.BallotID]types.LayerID{}
		choices       []types.BallotID // choices from the best to the least bad

		// TODO change it per https://github.com/spacemeshos/go-spacemesh/issues/2920
		// in current interpretation Last is the last layer that was sent to us after hare completed
		votinglid = t.Last
	)

	// TODO(dshulyak) do not even compute disagreements if there is a good base

	for lid := t.LastEvicted.Add(1); !lid.After(t.Last); lid = lid.Add(1) {
		for ballotID := range t.BallotOpinionsByLayer[lid] {
			dis, err := t.firstDisagreement(tctx, lid, ballotID, disagreements)
			if err != nil {
				logger.With().Error("failed to compute first disagreement", ballotID, log.Err(err))
				continue
			}
			disagreements[ballotID] = dis
			choices = append(choices, ballotID)
		}
	}

	prioritizeBallots(choices, t.GoodBallotsIndex, disagreements, t.BallotLayer)

	for _, ballotID := range choices {
		lid := t.BallotLayer[ballotID]
		exceptions, err := t.calculateExceptions(tctx, votinglid, lid, t.BallotOpinionsByLayer[lid][ballotID])
		if err != nil {
			logger.With().Warning("error calculating vote exceptions for ballot", ballotID, log.Err(err))
			continue
		}
		logger.With().Info("chose base ballot",
			ballotID,
			lid,
			log.Int("against_count", len(exceptions[0])),
			log.Int("support_count", len(exceptions[1])),
			log.Int("neutral_count", len(exceptions[2])))

		metrics.LayerDistanceToBaseBallot.WithLabelValues().Observe(float64(t.Last.Value - lid.Value))

		return ballotID, [][]types.BlockID{
			blockMapToArray(exceptions[0]),
			blockMapToArray(exceptions[1]),
			blockMapToArray(exceptions[2]),
		}, nil
	}

	// TODO: special error encoding when exceeding exception list size
	return types.EmptyBallotID, nil, errNoBaseBallotFound
}

// firstDisagreement returns first layer where local opinion is different from ballot's opinion within sliding window.
func (t *turtle) firstDisagreement(ctx *tcontext, blid types.LayerID, ballotID types.BallotID, disagreements map[types.BallotID]types.LayerID) (types.LayerID, error) {
	// using it as a mark that the votes for block are completely consistent
	// with a local opinion. so if two blocks have consistent histories select block
	// from a higher layer as it is more consistent.
	consistent := t.Last

	if _, exist := t.verifying.goodBallots[ballotID]; exist {
		return consistent, nil
	}
	opinions := t.BallotOpinionsByLayer[blid][ballotID]
	for lid := t.LastEvicted.Add(1); lid.Before(blid); lid = lid.Add(1) {
		locals, err := t.getLocalOpinion(ctx, lid)
		if err != nil {
			return types.LayerID{}, err
		}
		for on, local := range locals {
			opinion, exist := opinions[on]
			if !exist {
				opinion = against
			}
			if local != opinion {
				return lid, nil
			}
		}
	}
	return consistent, nil
}

// calculate and return a list of exceptions, i.e., differences between the opinions of a base ballot and the local
// opinion.
func (t *turtle) calculateExceptions(
	ctx *tcontext,
	votinglid,
	baselid types.LayerID,
	baseopinion Opinion,
) ([]map[types.BlockID]struct{}, error) {
	logger := t.logger.WithContext(ctx).WithFields(log.Named("base_ballot_layer_id", baselid))

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
	// Note: a ballot may only be selected as a candidate base ballot if it's marked "good", and it may only be marked
	// "good" if its own base ballot is marked "good" and all exceptions it contains agree with our local opinion.
	// We only look for and store exceptions within the sliding window set of layers as an optimization, but a ballot
	// can contain exceptions from any layer, back to genesis.
	startLayer := t.LastEvicted.Add(1)
	if startLayer.Before(types.GetEffectiveGenesis()) {
		startLayer = types.GetEffectiveGenesis()
	}
	for lid := startLayer; !lid.After(t.Last); lid = lid.Add(1) {
		logger := logger.WithFields(log.Named("diff_layer_id", lid))
		logger.Debug("checking input vector diffs")

		local, err := t.getLocalOpinion(ctx, lid)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				continue
			}
			return nil, err
		}

		// helper function for adding diffs
		addDiffs := func(logger log.Log, bid types.BlockID, voteVec sign, diffMap map[types.BlockID]struct{}) {
			v, ok := baseopinion[bid]
			if !ok {
				v = against
			}
			if voteVec != v {
				logger.With().Debug("added vote diff", log.Stringer("opinion", v))
				if lid.Before(baselid) {
					logger.With().Warning("added exception before base ballot layer, this ballot will not be marked good")
				}
				diffMap[bid] = struct{}{}
			}
		}

		for bid, opinion := range local {
			usecoinflip := opinion == abstain && lid.Before(t.layerCutoff())
			// TODO(dshulyak) compute coin threshold and only then decide if to use coinflip
			if usecoinflip {
				coin, exist := t.bdp.GetCoinflip(ctx, votinglid)
				// TODO: check if we need to sync the coinflip if this node didn't participate in hare for votinglid
				if !exist {
					return nil, fmt.Errorf("coinflip is not recorded in %s", votinglid)
				}
				if coin {
					opinion = support
				} else {
					opinion = against
				}
			}
			logger := logger.WithFields(
				log.Bool("use_coinflip", usecoinflip),
				log.Named("diff_block", bid),
				log.String("diff_class", opinion.String()),
			)
			switch opinion {
			case support:
				// add exceptions for blocks that base ballot doesn't support
				addDiffs(logger, bid, support, forDiff)
			case abstain:
				// NOTE special case, we can abstain on whole layer not on individual block
				// and only within hdist from last layer. before hdist opinion is always cast
				// accordingly to weakcoin
				addDiffs(logger, bid, abstain, neutralDiff)
			case against:
				// add exceptions for blocks that base ballot supports
				//
				// we don't save the ballot unless all the dependencies are saved.
				// condition where ballot has a vote that is not in our local view is impossible.
				addDiffs(logger, bid, against, againstDiff)
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

// Persist saves the current tortoise state to the database.
func (t *turtle) persist() error {
	return nil
}

func (t *turtle) ballotHasGoodBeacon(ballot *types.Ballot, logger log.Log) bool {
	layerID := ballot.LayerIndex

	// first check if we have it in the cache
	if _, bad := t.common.badBeaconBallots[ballot.ID()]; bad {
		return false
	}

	epochBeacon, err := t.beacons.GetBeacon(layerID.GetEpoch())
	if err != nil {
		logger.With().Error("failed to get beacon for epoch", layerID.GetEpoch(), log.Err(err))
		return false
	}

	beacon, err := t.getBallotBeacon(ballot, logger)
	if err != nil {
		return false
	}
	good := bytes.Equal(beacon, epochBeacon)
	if !good {
		logger.With().Warning("ballot has different beacon",
			log.String("ballot_beacon", types.BytesToHash(beacon).ShortString()),
			log.String("epoch_beacon", types.BytesToHash(epochBeacon).ShortString()))
		t.common.badBeaconBallots[ballot.ID()] = struct{}{}
	}
	return good
}

func (t *turtle) getBallotBeacon(ballot *types.Ballot, logger log.Log) ([]byte, error) {
	refBallotID := ballot.ID()
	if ballot.RefBallot != types.EmptyBallotID {
		refBallotID = ballot.RefBallot
	}

	epoch := ballot.LayerIndex.GetEpoch()
	beacons, ok := t.common.refBallotBeacons[epoch]
	if ok {
		if beacon, ok := beacons[refBallotID]; ok {
			return beacon, nil
		}
	} else {
		t.common.refBallotBeacons[epoch] = make(map[types.BallotID][]byte)
	}

	var beacon []byte
	if ballot.EpochData != nil {
		beacon = ballot.EpochData.Beacon
	} else if ballot.RefBallot == types.EmptyBallotID {
		logger.With().Panic("ref ballot missing epoch data", ballot.ID())
	} else {
		refBlock, err := t.bdp.GetBlock(types.BlockID(refBallotID))
		if err != nil {
			logger.With().Error("failed to find ref ballot",
				log.String("ref_ballot_id", refBallotID.String()))
			return nil, fmt.Errorf("get ref ballot: %w", err)
		}
		beacon = refBlock.TortoiseBeacon
	}
	t.common.refBallotBeacons[epoch][refBallotID] = beacon
	return beacon, nil
}

// HandleIncomingLayer processes all layer ballot votes
// returns the old pbase and new pbase after taking into account ballot votes.
func (t *turtle) HandleIncomingLayer(ctx context.Context, lid types.LayerID) error {
	defer t.evict(ctx)

	tctx := wrapContext(ctx)
	// unconditionally set the layer to the last one that we have seen once tortoise or sync
	// submits this layer. it doesn't matter if we fail to process it.
	if t.common.last.Before(lid) {
		t.common.last = lid
	}
	if t.common.processed.Before(lid) {
		t.common.processed = lid
	}
	return t.processLayer(tctx, lid)
}

func (t *turtle) processLayer(ctx *tcontext, lid types.LayerID) error {
	blocks, err := t.bdp.LayerBlocks(lid)
	if err != nil {
		return fmt.Errorf("failed to read blocks for layer %s: %w", lid, err)
	}

	original := make([]*types.Ballot, 0, len(blocks))
	for _, block := range blocks {
		original = append(original, block.ToBallot())
	}

	t.processBlocks(lid, blocks)

	// TODO(dshulyak) we should keep local opinion in memory, but it is it no very convenient
	// because within hdist it may change due to the dependency on the current layer.
	// maybe keep in memory but update it every time within hdist?
	ballots, localOpinion, err := t.processBallots(ctx, lid, original)
	if err != nil {
		return err
	}

	current := t.common.verified
	newVerified := t.verifying.processLayer(lid, localOpinion, ballots)
	if newVerified.After(current) {
		for lid := current.Add(1); !lid.After(newVerified); lid = lid.Add(1) {
			for _, bid := range t.common.blocks[lid] {
				sign := localOpinion[bid]
				if sign == abstain {
					t.logger.With().Panic("bug: layer should not be verified if there is an undecided block")
				}
				if err := t.bdp.SaveContextualValidity(bid, lid, sign == support); err != nil {
					return fmt.Errorf("saving validity for %s: %w", bid, err)
				}
			}
		}
		t.common.verified = newVerified
	}
	// TODO(dshulyak) check if there are any layers that can be verified by full tortoise
	// if yes switch to full tortoise:
	// - update full state with votes from each ballot
	// - run healing tortoise
	return nil
}

func (t *turtle) processBlocks(lid types.LayerID, blocks []*types.Block) {
	blockIDs := make([]types.BlockID, 0, len(blocks))
	for _, block := range blocks {
		blockIDs = append(blockIDs, block.ID())
		t.common.blockLayer[block.ID()] = lid
	}
	t.common.blocks[lid] = blockIDs

}

func (t *turtle) processBallots(ctx *tcontext, lid types.LayerID, original []*types.Ballot) ([]tortoiseBallot, Opinion, error) {
	var (
		ballots     = make([]tortoiseBallot, 0, len(original))
		ballotsIDs  = make([]types.BallotID, 0, len(original))
		processed   = t.common.processed
		min         = t.common.last // just max value
		refs, other []*types.Ballot
	)

	for _, ballot := range original {
		if ballot.EpochData != nil {
			refs = append(refs, ballot)
		} else {
			other = append(other, ballot)
		}
	}

	for _, part := range [...][]*types.Ballot{refs, other} {
		for _, ballot := range part {
			ballotWeight, err := computeBallotWeight(t.atxdb, t.common.ballotWeight, ballot, t.LayerSize, types.GetLayersPerEpoch())
			if err != nil {
				return nil, nil, err
			}
			t.common.ballotWeight[ballot.ID()] = weight{Float: ballotWeight}

			// TODO(dshulyak) this should not fail without terminating tortoise
			t.ballotHasGoodBeacon(ballot, t.logger)

			ballotsIDs = append(ballotsIDs, ballot.ID())

			baselid := t.common.ballotLayer[ballot.BaseBallot]
			if baselid.Before(min) {
				min = baselid
			}
			votes := Opinion{}
			for lid := baselid; lid.Before(processed); lid = lid.Add(1) {
				for _, bid := range t.common.blocks[lid] {
					votes[bid] = against
				}
			}
			for _, bid := range ballot.ForDiff {
				votes[bid] = support
			}
			for _, bid := range ballot.NeutralDiff {
				votes[bid] = abstain
			}
			for _, bid := range ballot.AgainstDiff {
				votes[bid] = against
			}

			ballots = append(ballots, tortoiseBallot{
				id:     ballot.ID(),
				base:   ballot.BaseBallot,
				weight: weight{Float: ballotWeight},
				votes:  votes,
			})
		}
	}
	t.common.ballots[lid] = ballotsIDs

	opinion := Opinion{}
	for lid := min; lid.Before(processed); lid = lid.Add(1) {
		if err := t.addLocalOpinion(ctx, lid, opinion); err != nil {
			return nil, nil, err
		}
	}

	return ballots, opinion, nil
}

func (t *turtle) healingTortoise(ctx *tcontext, logger log.Log, lid types.LayerID) (bool, error) {
	// Verifying tortoise will wait `zdist' layers for consensus, then an additional `ConfidenceParam'
	// layers until all other nodes achieve consensus. If it's still stuck after this point, i.e., if the gap
	// between this unverified candidate layer and the latest layer is greater than this distance, then we trigger
	// self healing. But there's no point in trying to heal a layer that's not at least Hdist layers old since
	// we only consider the local opinion for recent layers.
	logger.With().Debug("considering attempting to heal layer",
		log.Named("layer_cutoff", t.layerCutoff()),
		log.Uint32("zdist", t.Zdist),
		log.Named("last_layer_received", t.Last),
		log.Uint32("confidence_param", t.ConfidenceParam))
	if !(lid.Before(t.layerCutoff()) && t.Last.Difference(lid) > t.Zdist+t.ConfidenceParam) {
		return false, nil
	}
	logger.With().Info("start self-healing with verified layer", t.Verified)
	lastLayer := t.Processed
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

	// TODO(dshulyak) before merge consider to rescover ballotsa after healing
	return true, nil
}

// for layers older than this point, we vote according to global opinion (rather than local opinion).
func (t *turtle) layerCutoff() types.LayerID {
	// if we haven't seen at least Hdist layers yet, we always rely on local opinion
	if t.Last.Before(types.NewLayerID(t.Hdist)) {
		return types.NewLayerID(0)
	}
	return t.Last.Sub(t.Hdist)
}

// addLocalOpinion for layer.
func (t *turtle) addLocalOpinion(ctx *tcontext, lid types.LayerID, opinion Opinion) error {
	var (
		logger = t.logger.WithContext(ctx).WithFields(lid)
	)
	bids := t.common.blocks[lid]
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
				return nil
			}
			// Hare hasn't failed and layer has not passed the Hare abort threshold, so we abstain while we keep waiting
			// for Hare results.
			logger.With().Warning("local opinion abstains on all blocks in layer", log.Err(err))
			for _, bid := range bids {
				opinion[bid] = abstain
			}
			return nil
		}
		for _, bid := range opinionVec {
			opinion[bid] = support
		}
		return nil
	}

	// for layers older than hdist, we vote according to global opinion
	if !lid.After(t.Verified) {
		// this layer has been verified, so we should be able to read the set of contextual blocks
		logger.Debug("using contextually valid blocks as opinion on old, verified layer")
		valid, err := getValidBlocks(ctx, t.bdp, lid)
		if err != nil {
			return err
		}
		for _, bid := range valid {
			opinion[bid] = support
		}
		return nil
	}

	for _, bid := range bids {
		opinion[bid] = abstain
	}
	return nil
}

func (t *turtle) sumVotesForBlock(
	ctx context.Context,
	blockID types.BlockID, // the block we're summing votes for/against
	startlid types.LayerID,
	filter func(types.BallotID) bool,
) (*big.Float, error) {
	sum := new(big.Float)
	end := t.Processed
	logger := t.logger.WithContext(ctx).WithFields(
		log.Stringer("start_layer", startlid),
		log.Stringer("end_layer", end),
		log.Stringer("block_voting_on", blockID),
		log.Stringer("layer_voting_on", startlid.Sub(1)))
	for votelid := startlid; !votelid.After(end); votelid = votelid.Add(1) {
		logger := logger.WithFields(votelid)
		for ballotID, ballotOpinion := range t.BallotOpinionsByLayer[votelid] {
			if !filter(ballotID) {
				logger.With().Debug("voting block did not pass filter, not counting its vote", ballotID)
				continue
			}
			weight, exists := t.BallotWeight[ballotID]
			if !exists {
				// consider to panic here, it is possible only because of bug in the implementation
				return nil, fmt.Errorf("weight for %s is not computed", ballotID)
			}
			vote, exists := ballotOpinion[blockID]
			if !exists {
				logger.With().Debug("no opinion on older block, counted vote against")
				vote = against
			}
			adjusted := weight
			if vote == against {
				// copy is needed only if we modify sign
				adjusted = new(big.Float).Mul(weight, big.NewFloat(-1))
			}
			if vote != abstain {
				sum = sum.Add(sum, adjusted)
			}
		}
	}
	return sum, nil
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
			log.Named("old_verified_layer", pbaseOld),
			log.Named("last_verified_layer", pbaseNew),
			log.Named("target_layer", targetLayerID),
			log.Named("candidate_layer", candidateLayerID),
			log.Named("last_layer_received", t.Last),
			log.Uint32("hdist", t.Hdist),
		)

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
				log.Named("highest_healable_layer", latestLayerWeCanVerify))
			return
		}

		// Calculate the global opinion on all blocks in the layer
		// Note: we look at ALL blocks we've seen for the layer, not just those we've previously marked contextually valid
		logger.Info("self healing attempting to verify candidate layer")

		weight, err := computeExpectedVoteWeight(t.atxdb, t.epochWeight, candidateLayerID, t.Processed)
		if err != nil {
			logger.Error("failed to compute expected vote weight", log.Err(err))
			return
		}
		logger.With().Info("healing: expected voting weight for layer", log.Stringer("weight", weight), candidateLayerID)

		// record the contextual validity for all blocks in this layer
		for _, blockID := range t.common.blocks[candidateLayerID] {
			logger := logger.WithFields(log.Named("candidate_block_id", blockID))

			// count all votes for or against this block by all blocks in later layers. for ballots with a different
			// beacon values, we delay their votes by badBeaconVoteDelays layers
			sum, err := t.sumVotesForBlock(ctx, blockID, candidateLayerID.Add(1), t.ballotFilterForHealing(logger))
			if err != nil {
				logger.Error("error summing votes for candidate block in candidate layer", log.Err(err))
				return
			}

			// check that the total weight exceeds the global threshold
			globalOpinionOnBlock := calculateOpinionWithThreshold(t.logger, sum, t.GlobalThreshold, weight)
			logger.With().Debug("self healing calculated global opinion on candidate block",
				log.Stringer("global_opinion", globalOpinionOnBlock),
				log.Stringer("vote_sum", sum),
			)

			if globalOpinionOnBlock == abstain {
				logger.With().Info("self healing failed to verify candidate layer, will reattempt later",
					log.Stringer("global_opinion", globalOpinionOnBlock),
					log.Stringer("vote_sum", sum),
				)
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

// only ballots with the correct beacon value are considered good ballots and their votes counted by
// verifying tortoise. for ballots with a different beacon values, we count their votes only in self-healing mode
// if they are from previous epoch.
func (t *turtle) ballotFilterForHealing(logger log.Log) func(types.BallotID) bool {
	return func(ballotID types.BallotID) bool {
		if _, bad := t.badBeaconBallots[ballotID]; !bad {
			return true
		}
		lid, exist := t.BallotLayer[ballotID]
		if !exist {
			logger.With().Error("inconsistent state: ballot not found", ballotID)
			return false
		}
		return t.Last.Difference(lid) > t.BadBeaconVoteDelayLayers
	}
}

func getEpochWeight(atxdb atxDataProvider, epochWeight map[types.EpochID]uint64, eid types.EpochID) (uint64, error) {
	weight, exist := epochWeight[eid]
	if exist {
		return weight, nil
	}
	weight, _, err := atxdb.GetEpochWeight(eid)
	if err != nil {
		return 0, fmt.Errorf("epoch weight %s: %w", eid, err)
	}
	epochWeight[eid] = weight
	return weight, nil
}

// computeExpectedVoteWeight computes the expected weight for a block in the target layer, from summing up votes from ballots up to and including last layer.
func computeExpectedVoteWeight(atxdb atxDataProvider, epochWeight map[types.EpochID]uint64, target, last types.LayerID) (*big.Float, error) {
	var (
		total  = new(big.Float)
		layers = uint64(types.GetLayersPerEpoch())
	)
	// TODO(dshulyak) this can be improved by computing weight for epoch at once
	for lid := target.Add(1); !lid.After(last); lid = lid.Add(1) {
		weight, err := getEpochWeight(atxdb, epochWeight, lid.GetEpoch())
		if err != nil {
			return nil, err
		}
		layerWeight := new(big.Float).SetUint64(weight)
		layerWeight.Quo(layerWeight, new(big.Float).SetUint64(layers))
		total.Add(total, layerWeight)
	}
	return total, nil
}

// computeBallotWeight compute and assign ballot weight to the weights map.
func computeBallotWeight(
	atxdb atxDataProvider,
	weights map[types.BallotID]weight,
	ballot *types.Ballot,
	layerSize,
	layersPerEpoch uint32,
) (*big.Float, error) {
	if ballot.EpochData != nil {
		var total, weight uint64

		for _, atxid := range ballot.EpochData.ActiveSet {
			atx, err := atxdb.GetAtxHeader(atxid)
			if err != nil {
				return nil, fmt.Errorf("atx %s in active set of %s is unknown", atxid, ballot.ID())
			}
			atxweight := atx.GetWeight()
			total += atxweight
			if atxid == ballot.AtxID {
				weight = atxweight
			}
		}

		expected, err := proposals.GetNumEligibleSlots(weight, total, layerSize, layersPerEpoch)
		if err != nil {
			return nil, fmt.Errorf("unable to compute number of eligibile ballots for atx %s", ballot.AtxID)
		}
		rst := new(big.Float).SetUint64(weight)
		rst.Quo(rst, new(big.Float).SetUint64(uint64(expected)))
		return rst, nil
	}
	if ballot.RefBallot == types.EmptyBallotID {
		return nil, fmt.Errorf("empty ref ballot and no epoch data on ballot %s", ballot.ID())
	}
	weight, exist := weights[ballot.RefBallot]
	if !exist {
		return nil, fmt.Errorf("ref ballot %s for %s is unknown", ballot.ID(), ballot.RefBallot)
	}
	return weight.Float, nil
}
