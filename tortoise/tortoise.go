package tortoise

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/metrics"
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

	mode mode

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
		t.validity[blk.ID()] = support
		t.hareOutput[blk.ID()] = support
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
			delete(t.hareOutput, block)
			delete(t.validity, block)
			delete(t.full.weights, block)
		}
		delete(t.blocks, lid)
		delete(t.undecided, lid)
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
		logger        = t.logger.WithContext(ctx)
		disagreements = map[types.BallotID]types.LayerID{}
		choices       []types.BallotID

		ballotID   types.BallotID
		ballotLID  types.LayerID
		exceptions []map[types.BlockID]struct{}
		err        error
	)

	// after switching into verifying mode we need to recompute goodness of ballots
	// it will be handled in https://github.com/spacemeshos/go-spacemesh/issues/2985
	// for now just disable this optimization.
	if t.mode.isVerifying() {
		ballotID, ballotLID = t.getGoodBallot(logger)
		if ballotID != types.EmptyBallotID {
			// we need only 1 ballot from the most recent layer, this ballot will be by definition the most
			// consistent with our local opinion.
			// then we just need to encode our local opinion from layer of the ballot up to last processed as votes
			exceptions, err = t.encodeVotes(ctx, ballotLID, ballotLID, func(types.BlockID) sign { return against })
		}
	}

	if ballotID == types.EmptyBallotID || err != nil {
		logger.With().Warning("failed to select good base ballot. reverting to the least bad choices", log.Err(err))
		for lid := t.evicted.Add(1); !lid.After(t.processed); lid = lid.Add(1) {
			for _, ballotID := range t.ballots[lid] {
				dis, err := t.firstDisagreement(ctx, lid, ballotID, disagreements)
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
			exceptions, err = t.encodeVotes(ctx, ballotLID, t.evicted.Add(1), func(blockID types.BlockID) sign {
				return t.full.getVote(logger, ballotID, blockID)
			})
			if err == nil {
				break
			}
			logger.With().Warning("error calculating vote exceptions for ballot",
				ballotID,
				log.Err(err),
				log.Stringer("last_layer", t.last),
			)
		}
	}

	if exceptions == nil {
		// TODO: special error encoding when exceeding exception list size
		return types.EmptyBallotID, nil, errNoBaseBallotFound
	}

	logger.With().Info("choose base ballot",
		ballotID,
		ballotLID,
		log.Int("against_count", len(exceptions[0])),
		log.Int("support_count", len(exceptions[1])),
		log.Int("neutral_count", len(exceptions[2])),
	)

	metrics.LayerDistanceToBaseBallot.WithLabelValues().Observe(float64(t.last.Value - ballotLID.Value))

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
func (t *turtle) firstDisagreement(ctx context.Context, blid types.LayerID, ballotID types.BallotID, disagreements map[types.BallotID]types.LayerID) (types.LayerID, error) {
	var (
		// using it as a mark that the votes for block are completely consistent
		// with a local opinion. so if two blocks have consistent histories select block
		// from a higher layer as it is more consistent.
		consistent  = t.last
		start       = t.evicted
		base, exist = t.full.base[ballotID]
		basedis     = disagreements[base]
	)
	if exist && basedis != consistent {
		return basedis, nil
	}
	start = t.ballotLayer[base]

	for lid := start; lid.Before(blid); lid = lid.Add(1) {
		for _, block := range t.blocks[lid] {
			localVote, _, err := t.getFullVote(ctx, lid, block)
			if err != nil {
				return types.LayerID{}, err
			}
			vote := t.full.getVote(t.logger, ballotID, block)
			if localVote != vote {
				t.logger.With().Debug("found disagreement on a block",
					ballotID,
					block,
					log.Stringer("block_layer", lid),
					log.Stringer("ballot_layer", blid),
					log.Stringer("local_vote", localVote),
					log.Stringer("vote", vote),
				)
				return lid, nil
			}
		}
	}
	return consistent, nil
}

type opinionsGetter func(types.BlockID) sign

// encode differences between selected base ballot and local votes.
func (t *turtle) encodeVotes(
	ctx context.Context,
	baselid,
	startlid types.LayerID,
	getter opinionsGetter,
) ([]map[types.BlockID]struct{}, error) {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Stringer("base_layer", baselid),
		log.Stringer("last_layer", t.last),
	)

	againstDiff := make(map[types.BlockID]struct{})
	forDiff := make(map[types.BlockID]struct{})
	neutralDiff := make(map[types.BlockID]struct{})

	for lid := startlid; !lid.After(t.processed); lid = lid.Add(1) {
		logger := logger.WithFields(log.Named("block_layer", lid))

		addVotes := func(logger log.Log, bid types.BlockID, localVote sign, diffMap map[types.BlockID]struct{}) {
			baseVote := getter(bid)
			needsException := localVote != baseVote

			logFunc := logger.With().Debug
			if lid.Before(baselid) && needsException {
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

		for _, bid := range t.blocks[lid] {
			localVote, reason, err := t.getFullVote(ctx, lid, bid)
			if err != nil {
				return nil, err
			}
			logger := logger.WithFields(
				log.Stringer("block", bid),
				log.Stringer("local_vote_reason", reason),
				log.Stringer("local_vote", localVote),
			)
			switch localVote {
			case support:
				// add exceptions for blocks that base ballot doesn't support
				addVotes(logger, bid, support, forDiff)
			case abstain:
				// NOTE special case, we can abstain on whole layer not on individual block
				// and only within hdist from last layer. before hdist opinion is always cast
				// accordingly to weakcoin
				addVotes(logger, bid, abstain, neutralDiff)
			case against:
				// add exceptions for blocks that base ballot supports
				//
				// we don't save the ballot unless all the dependencies are saved.
				// condition where ballot has a vote that is not in our local view is impossible.
				addVotes(logger, bid, against, againstDiff)
			}
		}
	}

	explen := len(againstDiff) + len(forDiff) + len(neutralDiff)
	if explen > t.MaxExceptions {
		return nil, fmt.Errorf("%s (%v)", errstrTooManyExceptions, explen)
	}

	return []map[types.BlockID]struct{}{againstDiff, forDiff, neutralDiff}, nil
}

// getFullVote unlike getLocalVote will vote according to the counted votes on blocks that are
// outside of hdist. if opinion is undecided according to the votes it will use coinflip recorded
// in the current layer.
func (t *turtle) getFullVote(ctx context.Context, lid types.LayerID, bid types.BlockID) (sign, voteReason, error) {
	vote, reason := getLocalVote(&t.commonState, t.Config, lid, bid)
	if !(vote == abstain && reason == reasonValiditiy) {
		return vote, reason, nil
	}
	sum := t.full.weights[bid]
	vote = sum.cmp(t.localThreshold)
	if vote != abstain {
		return vote, reasonLocalThreshold, nil
	}
	coin, exist := t.bdp.GetCoinflip(ctx, t.last)
	if !exist {
		return 0, "", fmt.Errorf("coinflip is not recorded in %s. required for vote on %s / %s",
			t.last, bid, lid)
	}
	if coin {
		return support, reasonCoinflip, nil
	}
	return against, reasonCoinflip, nil
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
	good := beacon == epochBeacon
	if !good {
		logger.With().Warning("ballot has different beacon",
			log.String("ballot_beacon", beacon.ShortString()),
			log.String("epoch_beacon", epochBeacon.ShortString()))
		t.badBeaconBallots[ballot.ID()] = struct{}{}
	}
	return good
}

func (t *turtle) getBallotBeacon(ballot *types.Ballot, logger log.Log) (types.Beacon, error) {
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
		t.refBallotBeacons[epoch] = make(map[types.BallotID]types.Beacon)
	}

	var beacon types.Beacon
	if ballot.EpochData != nil {
		beacon = ballot.EpochData.Beacon
	} else if ballot.RefBallot == types.EmptyBallotID {
		logger.With().Panic("ref ballot missing epoch data", ballot.ID())
	} else {
		refBlock, err := t.bdp.GetBlock(types.BlockID(refBallotID))
		if err != nil {
			logger.With().Error("failed to find ref ballot",
				log.String("ref_ballot_id", refBallotID.String()))
			return types.EmptyBeacon, fmt.Errorf("get ref ballot: %w", err)
		}
		beacon = refBlock.TortoiseBeacon
	}
	t.refBallotBeacons[epoch][refBallotID] = beacon
	return beacon, nil
}

// HandleIncomingLayer processes all layer ballot votes.
func (t *turtle) HandleIncomingLayer(ctx context.Context, lid types.LayerID) error {
	defer t.evict(ctx)
	return t.processLayer(ctx, t.logger.WithContext(ctx).WithFields(lid), lid)
}

func (t *turtle) processLayer(ctx context.Context, logger log.Log, lid types.LayerID) error {
	logger.With().Info("adding layer to the state")

	if err := t.updateLayerState(logger, lid); err != nil {
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
	ballots, err := t.processBallots(lid, original)
	if err != nil {
		return err
	}

	t.full.processBallots(ballots)
	t.full.processBlocks(blocks)

	if err := t.updateLocalVotes(ctx, logger, lid); err != nil {
		return err
	}

	previous := t.verified
	if t.mode.isVerifying() {
		t.verifying.countVotes(logger, lid, ballots)
		iterateLayers(t.verified.Add(1), t.processed.Sub(1), func(lid types.LayerID) bool {
			ok := t.verifying.verify(logger, lid)
			if !ok {
				return false
			}
			t.verified = lid
			updateThresholds(logger, t.Config, &t.commonState, t.mode)
			return true
		})
	}
	if t.canUseFullMode() || t.mode.isFull() {
		if t.mode.isVerifying() {
			t.mode = t.mode.toggleMode()
			logger.With().Info("switching to full self-healing tortoise",
				lid,
				log.Stringer("verified", t.verified),
				log.Stringer("mode", t.mode),
			)
			// update threshold since we have different windows for vote counting in each mode
			updateThresholds(logger, t.Config, &t.commonState, t.mode)
		}
		t.full.countVotes(logger)
		iterateLayers(t.verified.Add(1), t.processed.Sub(1), func(lid types.LayerID) bool {
			ok := t.full.verify(logger, lid)
			if !ok {
				return false
			}
			t.verified = lid
			updateThresholds(logger, t.Config, &t.commonState, t.mode)
			return true
		})
	}
	if err := persistContextualValidity(logger,
		t.bdp,
		previous, t.verified,
		t.blocks,
		t.validity,
	); err != nil {
		return err
	}

	t.updateHistoricallyVerified()
	return nil
}

func (t *turtle) updateLayerState(logger log.Log, lid types.LayerID) error {
	lastUpdated := t.last.Before(lid)
	if lastUpdated {
		t.last = lid
	}
	if t.processed.Before(lid) {
		t.processed = lid
	}

	for epoch := t.last.GetEpoch(); epoch >= t.evicted.GetEpoch(); epoch-- {
		if _, exist := t.epochWeight[epoch]; exist {
			break
		}
		layerWeight, err := computeEpochWeight(t.atxdb, t.epochWeight, epoch)
		if err != nil {
			return err
		}
		logger.With().Info("computed weight for layers in an epoch", epoch, log.Stringer("weight", layerWeight))
	}

	if lastUpdated || t.globalThreshold.isNil() {
		updateThresholds(logger, t.Config, &t.commonState, t.mode)
	}
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

func (t *turtle) processBallots(lid types.LayerID, original []*types.Ballot) ([]tortoiseBallot, error) {
	var (
		ballots    = make([]tortoiseBallot, 0, len(original))
		ballotsIDs = make([]types.BallotID, 0, len(original))
	)

	for _, ballot := range original {
		ballotWeight, err := computeBallotWeight(t.atxdb, t.bdp, t.ballotWeight, ballot, t.LayerSize, types.GetLayersPerEpoch())
		if err != nil {
			return nil, err
		}
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

	t.ballots[lid] = ballotsIDs
	return ballots, nil
}

func (t *turtle) updateLocalVotes(ctx context.Context, logger log.Log, lid types.LayerID) (err error) {
	for lid := range t.undecided {
		if err := t.addLocalVotes(ctx, logger.WithFields(log.Bool("undecided", true)), lid); err != nil {
			return err
		}
	}
	return t.addLocalVotes(ctx, logger, lid)
}

// the idea here is to give enough room for verifying tortoise to complete. during live tortoise execution this will be limited by the hdist.
// during rerun we need to use another heuristic, as hdist is irrelevant by that time.
func (t *turtle) canUseFullMode() bool {
	target := t.verified.Add(1)
	// TODO(dshulyak) this condition should be enabled when the node is in sync.
	if t.mode.isRerun() {
		return t.processed.Difference(target) > t.VerifyingModeRerunWindow
	}
	return target.Before(t.layerCutoff())
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
func (t *turtle) addLocalVotes(ctx context.Context, logger log.Log, lid types.LayerID) error {
	logger.With().Debug("fetching local votes for layer",
		log.Stringer("for_layer", lid),
	)

	if !lid.Before(t.layerCutoff()) {
		// for newer layers, we vote according to the local opinion (input vector, from hare or sync)
		opinionVec, err := t.bdp.GetLayerInputVectorByID(lid)
		if err != nil {
			if t.last.After(types.NewLayerID(t.Zdist)) && lid.Before(t.last.Sub(t.Zdist)) {
				// Layer has passed the Hare abort distance threshold, so we give up waiting for Hare results. At this point
				// our opinion on this layer is that we vote against blocks (i.e., we support an empty layer).
				delete(t.undecided, lid)
				return nil
			}
			// Hare hasn't failed and layer has not passed the Hare abort threshold, so we abstain while we keep waiting
			// for Hare results.
			logger.With().Warning("local opinion abstains on all blocks in layer", log.Err(err))
			t.undecided[lid] = struct{}{}
			return nil
		}
		for _, bid := range opinionVec {
			t.hareOutput[bid] = support
		}
		return nil
	}

	// for layers older than hdist, we vote according to global opinion
	if !lid.After(t.historicallyVerified) {
		// this layer has been verified, so we should be able to read the set of contextual blocks
		logger.Debug("using contextually valid blocks as opinion on old, verified layer")
		valid, err := t.bdp.LayerContextuallyValidBlocks(ctx, lid)
		if err != nil {
			return fmt.Errorf("failed to load contextually balid blocks for layer %s: %w", lid, err)
		}
		for bid := range valid {
			t.validity[bid] = support
		}
		return nil
	}
	return nil
}

func getLocalVote(state *commonState, config Config, lid types.LayerID, block types.BlockID) (sign, voteReason) {
	genesis := types.GetEffectiveGenesis()
	limit := types.GetEffectiveGenesis()
	if state.last.After(genesis.Add(config.Hdist)) {
		limit = state.last.Sub(config.Hdist)
	}
	if !lid.Before(limit) {
		vote, exist := state.hareOutput[block]
		if exist {
			return vote, reasonHareOutput
		}
		if state.last.After(genesis.Add(config.Zdist)) {
			limit = state.last.Sub(config.Zdist)
		}
		_, isUndecided := state.undecided[lid]
		if !lid.Before(limit) && isUndecided {
			return abstain, reasonHareOutput
		}
		return against, reasonHareOutput
	}

	vote, exists := state.validity[block]
	if exists {
		return vote, reasonValiditiy
	}
	return abstain, reasonValiditiy
}
