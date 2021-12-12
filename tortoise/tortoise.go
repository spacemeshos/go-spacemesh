package tortoise

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/metrics"
)

const (
	verifyingMode = iota + 1
	fullMode
)

var (
	errNoBaseBallotFound    = errors.New("no good base ballot within exception vector limit")
	errstrTooManyExceptions = "too many exceptions to base ballot vote"
)

type turtle struct {
	Config
	logger log.Log

	atxdb   atxDataProvider
	bdp     blockDataProvider
	beacons system.BeaconGetter

	mode int8

	commonState
	verifying *verifying
	full      *full
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
		Config:      config,
		commonState: newCommonState(),
		logger:      logger,
		mode:        verifyingMode,
		bdp:         bdp,
		atxdb:       atxdb,
		beacons:     beacons,
	}
	t.verifying = newVerifying(config, &t.commonState)
	t.full = newFullTortoise(config, &t.commonState)
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
		genesisLayer.Hash().Field(),
	)
	genesis := genesisLayer.Index()
	for _, blk := range genesisLayer.Blocks() {
		ballot := blk.ToBallot()
		id := ballot.ID()
		t.ballotLayer[id] = genesis
		t.blockLayer[blk.ID()] = genesis
		t.blocks[genesis] = []types.BlockID{blk.ID()}
		t.ballots[genesis] = []types.BallotID{id}
		t.localVotes[blk.ID()] = support
		t.verifying.goodBallots[id] = struct{}{}
	}
	t.last = genesis
	t.processed = genesis
	t.verified = genesis
	t.historicallyVerified = genesis
	t.evicted = genesis.Sub(1)
	t.full.counted = genesis
}

func (t *turtle) lookbackWindowStart() (types.LayerID, bool) {
	// prevent overflow/wraparound
	if t.verified.Before(types.NewLayerID(t.WindowSize)) {
		return types.NewLayerID(0), false
	}
	return t.verified.Sub(t.WindowSize), true
}

func (t *turtle) updateHistoricallyVerified() {
	if t.verified.After(t.historicallyVerified) {
		t.historicallyVerified = t.verified
	}
}

// evict makes sure we only keep a window of the last hdist layers.
func (t *turtle) evict(ctx context.Context) {
	// Don't evict before we've verified at least hdist layers
	if !t.verified.After(types.GetEffectiveGenesis().Add(t.Hdist)) {
		return
	}
	// TODO: fix potential leak when we can't verify but keep receiving layers
	//    see https://github.com/spacemeshos/go-spacemesh/issues/2671

	windowStart, ok := t.lookbackWindowStart()
	if !ok {
		return
	}
	if !windowStart.After(t.evicted) {
		return
	}

	oldestEpoch := windowStart.GetEpoch()

	t.logger.With().Debug("evict in memory state",
		log.Stringer("from_layer", t.evicted.Add(1)),
		log.Stringer("upto_layer", windowStart),
		log.Stringer("from_epoch", oldestEpoch),
		log.Stringer("upto_epoch", windowStart.GetEpoch()),
	)

	for lid := t.evicted.Add(1); lid.Before(windowStart); lid = lid.Add(1) {
		for _, ballot := range t.ballots[lid] {
			delete(t.ballotLayer, ballot)
			delete(t.ballotWeight, ballot)
			delete(t.badBeaconBallots, ballot)
			delete(t.verifying.goodBallots, ballot)
			delete(t.full.votes, ballot)
			delete(t.full.base, ballot)
		}
		delete(t.ballots, lid)
		for _, block := range t.blocks[lid] {
			delete(t.blockLayer, block)
			delete(t.localVotes, block)
		}
		delete(t.blocks, lid)
		if lid.GetEpoch() < oldestEpoch {
			delete(t.refBallotBeacons, lid.GetEpoch())
			delete(t.epochWeight, lid.GetEpoch())
			oldestEpoch = lid.GetEpoch()
		}
	}
	t.evicted = windowStart.Sub(1)
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
		choices       []types.BallotID

		// TODO change it per https://github.com/spacemeshos/go-spacemesh/issues/2920
		// in current interpretation Last is the last layer that was sent to us after hare completed
		votinglid = t.last

		ballotID   types.BallotID
		ballotLID  types.LayerID
		exceptions []map[types.BlockID]struct{}
		err        error
	)

	ballotID, ballotLID = t.getGoodBallot(logger)
	if ballotID != types.EmptyBallotID {
		// we need only 1 ballot from the most recent layer, this ballot will be by definition the most
		// consistent with our local opinion.
		// then we just need to encode our local opinion from layer of the ballot up to last processed as votes
		exceptions, err = t.computeVotes(tctx, votinglid, ballotLID, ballotLID, func(types.BlockID) sign { return against })
	}
	if ballotID == types.EmptyBallotID || err != nil {
		logger.With().Warning("failed to select good base ballot. reverting to the least bad choices", log.Err(err))
		for lid := t.evicted.Add(1); !lid.After(t.processed); lid = lid.Add(1) {
			for _, ballotID := range t.ballots[lid] {
				dis, err := t.firstDisagreement(tctx, lid, ballotID, disagreements)
				if err != nil {
					logger.With().Error("failed to compute first disagreement", ballotID, log.Err(err))
					continue
				}
				disagreements[ballotID] = dis
				choices = append(choices, ballotID)
			}
		}

		prioritizeBallots(choices, disagreements, t.ballotLayer)
		for _, ballotID = range choices {
			ballotLID = t.ballotLayer[ballotID]
			exceptions, err = t.computeVotes(tctx, votinglid, ballotLID, t.evicted.Add(1), func(blockID types.BlockID) sign {
				return t.full.getVote(logger, ballotID, blockID)
			})
			if err == nil {
				break
			}
			logger.With().Warning("error calculating vote exceptions for ballot", ballotID, log.Err(err))
		}
	}

	if exceptions == nil {
		// TODO: special error encoding when exceeding exception list size
		return types.EmptyBallotID, nil, errNoBaseBallotFound
	}

	logger.With().Info("chose base ballot",
		ballotID,
		ballotLID,
		log.Int("against_count", len(exceptions[0])),
		log.Int("support_count", len(exceptions[1])),
		log.Int("neutral_count", len(exceptions[2])),
	)

	metrics.LayerDistanceToBaseBallot.WithLabelValues().Observe(float64(votinglid.Value - ballotLID.Value))

	return ballotID, [][]types.BlockID{
		blockMapToArray(exceptions[0]),
		blockMapToArray(exceptions[1]),
		blockMapToArray(exceptions[2]),
	}, nil
}

func (t *turtle) getGoodBallot(logger log.Log) (types.BallotID, types.LayerID) {
	var choices []types.BallotID
	for lid := t.processed; lid.After(t.evicted); lid = lid.Sub(1) {
		for _, ballotID := range t.ballots[lid] {
			if _, exist := t.verifying.goodBallots[ballotID]; exist {
				choices = append(choices, ballotID)
			}
		}
		if len(choices) > 0 {
			sort.Slice(choices, func(i, j int) bool {
				return choices[i].Compare(choices[j])
			})

			t.logger.With().Info("considering good base ballot", choices[0], lid)
			return choices[0], lid
		}
	}
	return types.BallotID{}, types.LayerID{}
}

// firstDisagreement returns first layer where local opinion is different from ballot's opinion within sliding window.
func (t *turtle) firstDisagreement(ctx *tcontext, blid types.LayerID, ballotID types.BallotID, disagreements map[types.BallotID]types.LayerID) (types.LayerID, error) {
	// using it as a mark that the votes for block are completely consistent
	// with a local opinion. so if two blocks have consistent histories select block
	// from a higher layer as it is more consistent.
	consistent := t.last

	for lid := t.evicted.Add(1); lid.Before(blid); lid = lid.Add(1) {
		localOpinion, err := t.getLocalVotes(ctx, lid)
		if err != nil {
			return types.LayerID{}, err
		}
		for block, local := range localOpinion {
			vote := t.full.getVote(t.logger, ballotID, block)
			if local != vote {
				t.logger.With().Debug("found disagreement on a block",
					ballotID,
					block,
					log.Stringer("block_layer", lid),
					log.Stringer("ballot_layer", blid),
					log.Stringer("local_opinion", local),
					log.Stringer("vote", vote),
				)
				return lid, nil
			}
		}
	}
	return consistent, nil
}

type opinionsGetter func(types.BlockID) sign

// calculate and return a list of votes, i.e., differences between the votes of a base ballot and the local
// votes.
func (t *turtle) computeVotes(
	ctx *tcontext,
	votinglid,
	baselid,
	startlid types.LayerID,
	getter opinionsGetter,
) ([]map[types.BlockID]struct{}, error) {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Stringer("base_layer", baselid),
		log.Stringer("voting_layer", votinglid),
	)

	againstDiff := make(map[types.BlockID]struct{})
	forDiff := make(map[types.BlockID]struct{})
	neutralDiff := make(map[types.BlockID]struct{})

	localThreshold := weightFromUint64(0)
	// TODO(dshulyak) expected weight for local threshold should be based on last layer.
	weight, err := computeEpochWeight(t.atxdb, t.epochWeight, t.processed.GetEpoch())
	if err != nil {
		return nil, err
	}
	localThreshold = localThreshold.add(weight)
	localThreshold = localThreshold.fraction(t.LocalThreshold)

	for lid := startlid; !lid.After(t.processed); lid = lid.Add(1) {
		logger := logger.WithFields(log.Named("block_layer", lid))

		local, err := t.getLocalVotes(ctx, lid)
		if err != nil {
			return nil, err
		}

		// helper function for adding diffs
		encodeVotes := func(logger log.Log, bid types.BlockID, localVote sign, diffMap map[types.BlockID]struct{}) {
			baseVote := getter(bid)
			needsException := localVote != baseVote

			logFunc := logger.With().Debug
			if lid.Before(baselid) {
				logFunc = logger.With().Warning
			}
			logFunc("voting according to the local opinion",
				log.Stringer("base_vote", baseVote),
				log.Bool("vote_before_base", lid.Before(baselid)),
				log.Bool("needs_exception", needsException),
			)

			if needsException {
				diffMap[bid] = struct{}{}
			}
		}

		for bid, vote := range local {
			reason := "validity/hare"
			usecoinflip := vote == abstain && lid.Before(t.layerCutoff())
			if usecoinflip {
				sum := t.full.weights[bid]
				vote = sum.cmp(localThreshold)
				reason = "local threshold"
				if vote == abstain {
					reason = "coinflip"
					coin, exist := t.bdp.GetCoinflip(ctx, votinglid)
					if !exist {
						return nil, fmt.Errorf("coinflip is not recorded in %s", votinglid)
					}
					if coin {
						vote = support
					} else {
						vote = against
					}
				}
			}
			logger := logger.WithFields(
				log.Stringer("block", bid),
				log.String("local_opinion_reason", reason),
				log.Stringer("local_opinion", vote),
			)
			switch vote {
			case support:
				// add exceptions for blocks that base ballot doesn't support
				encodeVotes(logger, bid, support, forDiff)
			case abstain:
				// NOTE special case, we can abstain on whole layer not on individual block
				// and only within hdist from last layer. before hdist opinion is always cast
				// accordingly to weakcoin
				encodeVotes(logger, bid, abstain, neutralDiff)
			case against:
				// add exceptions for blocks that base ballot supports
				//
				// we don't save the ballot unless all the dependencies are saved.
				// condition where ballot has a vote that is not in our local view is impossible.
				encodeVotes(logger, bid, against, againstDiff)
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

func (t *turtle) markBeaconWithBadBallot(logger log.Log, ballot *types.Ballot) bool {
	layerID := ballot.LayerIndex

	// first check if we have it in the cache
	if _, bad := t.badBeaconBallots[ballot.ID()]; bad {
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
		t.badBeaconBallots[ballot.ID()] = struct{}{}
	}
	return good
}

func (t *turtle) getBallotBeacon(ballot *types.Ballot, logger log.Log) ([]byte, error) {
	refBallotID := ballot.ID()
	if ballot.RefBallot != types.EmptyBallotID {
		refBallotID = ballot.RefBallot
	}

	epoch := ballot.LayerIndex.GetEpoch()
	beacons, ok := t.refBallotBeacons[epoch]
	if ok {
		if beacon, ok := beacons[refBallotID]; ok {
			return beacon, nil
		}
	} else {
		t.refBallotBeacons[epoch] = make(map[types.BallotID][]byte)
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
	t.refBallotBeacons[epoch][refBallotID] = beacon
	return beacon, nil
}

// HandleIncomingLayer processes all layer ballot votes.
func (t *turtle) HandleIncomingLayer(ctx context.Context, lid types.LayerID) error {
	defer t.evict(ctx)
	tctx := wrapContext(ctx)
	if t.last.Before(lid) {
		t.last = lid
	}
	if t.processed.Before(lid) {
		t.processed = lid
	}
	return t.processLayer(tctx, lid)
}

func (t *turtle) processLayer(ctx *tcontext, lid types.LayerID) error {
	logger := t.logger.WithContext(ctx).WithFields(lid)

	logger.With().Info("adding layer to the state")

	if err := t.updateLayerState(lid); err != nil {
		return err
	}

	blocks, err := t.bdp.LayerBlocks(lid)
	if err != nil {
		return fmt.Errorf("failed to read blocks for layer %s: %w", lid, err)
	}

	t.processBlocks(lid, blocks)

	original := make([]*types.Ballot, 0, len(blocks))
	for _, block := range blocks {
		original = append(original, block.ToBallot())
	}
	ballots, err := t.processBallots(ctx, lid, original)
	if err != nil {
		return err
	}

	t.full.processBallots(ballots)
	t.full.processBlocks(blocks)

	previous := t.verified

	if t.mode == verifyingMode {
		// local opinion may change within hdist. this solution might be a bottleneck during rerun
		// reconsider while working on https://github.com/spacemeshos/go-spacemesh/issues/2980
		if err := t.updateLocalVotes(ctx, t.verified.Add(1), t.processed); err != nil {
			return err
		}
		t.verifying.countVotes(logger, lid, ballots)
		t.verified = t.verifying.verify(logger)
	}
	// condition to switch may change for rerun https://github.com/spacemeshos/go-spacemesh/issues/2980,
	// verified layer will be expected to be stuck as the expected weight will be high.
	// so we will have to use different heuristic for switching into full mode.
	// maybe the difference between counted and uncounted weight for a specific layer
	if t.canUseFullMode() || t.mode == fullMode {
		if t.mode == verifyingMode {
			logger.With().Info("switching to full self-healing tortoise",
				lid,
				log.Stringer("verified", t.verified),
			)
			t.mode = fullMode
		}
		// counting votes is what makes full tortoise expensive during rerun.
		// we want to wait until we know that verifying can't make progress before they are counted.
		// storing them in current version is cheap.
		t.full.countVotes(logger)
		t.verified = t.full.verify(logger)
	}
	if err := persistContextualValidity(logger,
		t.bdp,
		previous, t.verified,
		t.blocks,
		t.localVotes,
	); err != nil {
		return err
	}

	t.updateHistoricallyVerified()
	return nil
}

func (t *turtle) updateLayerState(lid types.LayerID) error {
	epochID := lid.GetEpoch()
	if _, exist := t.epochWeight[epochID]; exist {
		return nil
	}
	layerWeight, err := computeEpochWeight(t.atxdb, t.epochWeight, epochID)
	if err != nil {
		return err
	}
	t.logger.With().Info("computed weight for layers in an epoch", epochID, log.Stringer("weight", layerWeight))
	return nil
}

func (t *turtle) processBlocks(lid types.LayerID, blocks []*types.Block) {
	blockIDs := make([]types.BlockID, 0, len(blocks))
	for _, block := range blocks {
		blockIDs = append(blockIDs, block.ID())
		t.blockLayer[block.ID()] = lid
	}
	t.blocks[lid] = blockIDs
}

func (t *turtle) processBallots(ctx *tcontext, lid types.LayerID, original []*types.Ballot) ([]tortoiseBallot, error) {
	var (
		ballots     = make([]tortoiseBallot, 0, len(original))
		ballotsIDs  = make([]types.BallotID, 0, len(original))
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
			ballotWeight, err := computeBallotWeight(t.atxdb, t.bdp, t.ballotWeight, ballot, t.LayerSize, types.GetLayersPerEpoch())
			if err != nil {
				return nil, err
			}
			t.ballotWeight[ballot.ID()] = ballotWeight
			t.ballotLayer[ballot.ID()] = ballot.LayerIndex

			// TODO(dshulyak) this should not fail without terminating tortoise
			t.markBeaconWithBadBallot(t.logger, ballot)

			ballotsIDs = append(ballotsIDs, ballot.ID())

			baselid := t.ballotLayer[ballot.BaseBallot]

			votes := votes{}
			for lid := baselid; lid.Before(t.processed); lid = lid.Add(1) {
				for _, bid := range t.blocks[lid] {
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
				weight: ballotWeight,
				votes:  votes,
			})
		}
	}
	t.ballots[lid] = ballotsIDs
	return ballots, nil
}

func (t *turtle) updateLocalVotes(ctx *tcontext, from, to types.LayerID) error {
	for lid := from; !lid.After(to); lid = lid.Add(1) {
		if err := t.addLocalVotes(ctx, lid, t.localVotes); err != nil {
			return err
		}
	}
	return nil
}

// the idea here is to give enough room for verifying tortoise to complete. during live tortoise execution this will be limited by the hdist.
// during rerun we need to use another heuristic, as hdist is irrelevant by that time.
//
// verifying tortoise is a fastpath for full vote counting, that works if everyone is honest and have good synchrony.
// as for the safety and liveness both protocols are identical.
func (t *turtle) canUseFullMode() bool {
	lid := t.verified.Add(1)
	return lid.Before(t.layerCutoff()) &&
		t.last.Difference(lid) > t.Zdist &&
		// unlike previous two parameters we count confidence interval from processed layer
		// processed layer is significantly lower then the last layer during rerun.
		// we need to wait some distance before switching from verifying to full during rerun
		// as previous two parameters will be always true even if single layer is not verified.
		t.processed.Difference(lid) > t.ConfidenceParam
}

// for layers older than this point, we vote according to global opinion (rather than local opinion).
func (t *turtle) layerCutoff() types.LayerID {
	// if we haven't seen at least Hdist layers yet, we always rely on local opinion
	if t.last.Before(types.NewLayerID(t.Hdist)) {
		return types.NewLayerID(0)
	}
	return t.last.Sub(t.Hdist)
}

// addLocalVotes for layer.
func (t *turtle) addLocalVotes(ctx *tcontext, lid types.LayerID, opinion votes) error {
	var (
		logger = t.logger.WithContext(ctx).WithFields(lid)
		bids   = t.blocks[lid]
	)
	for _, bid := range bids {
		opinion[bid] = against
	}

	if !lid.Before(t.layerCutoff()) {
		// for newer layers, we vote according to the local opinion (input vector, from hare or sync)
		opinionVec, err := getInputVector(ctx, t.bdp, lid)
		if err != nil {
			if t.last.After(types.NewLayerID(t.Zdist)) && lid.Before(t.last.Sub(t.Zdist)) {
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
	if !lid.After(t.historicallyVerified) {
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

	// if layer is undecided and not in hdist layer - we can't decide based on local opinion
	// and need to switch to full tortoise
	for _, bid := range bids {
		opinion[bid] = abstain
	}
	return nil
}
