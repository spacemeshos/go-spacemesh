package tortoise

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/big"

	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"github.com/spacemeshos/go-spacemesh/tortoise/metrics"
)

var (
	errNoBaseBallotFound    = errors.New("no good base ballot within exception vector limit")
	errNotSorted            = errors.New("input blocks are not sorted by layerID")
	errstrNoCoinflip        = "no weak coin value for layer"
	errstrTooManyExceptions = "too many exceptions to base ballot vote"
	errstrConflictingVotes  = "conflicting votes found in ballot"
)

type turtle struct {
	Config
	state
	logger log.Log

	atxdb   atxDataProvider
	bdp     blockDataProvider
	beacons blocks.BeaconGetter
}

// newTurtle creates a new verifying tortoise algorithm instance.
func newTurtle(
	lg log.Log,
	db database.Database,
	bdp blockDataProvider,
	atxdb atxDataProvider,
	beacons blocks.BeaconGetter,
	cfg Config,
) *turtle {
	return &turtle{
		Config: cfg,
		state: state{
			diffMode:              true,
			db:                    db,
			log:                   lg,
			refBallotBeacons:      map[types.EpochID]map[types.BallotID][]byte{},
			badBeaconBallots:      map[types.BallotID]struct{}{},
			epochWeight:           map[types.EpochID]uint64{},
			GoodBallotsIndex:      map[types.BallotID]bool{},
			BallotOpinionsByLayer: map[types.LayerID]map[types.BallotID]Opinion{},
			BallotLayer:           map[types.BallotID]types.LayerID{},
			BlockLayer:            map[types.BlockID]types.LayerID{},
		},
		logger:  lg,
		bdp:     bdp,
		atxdb:   atxdb,
		beacons: beacons,
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

func (t *turtle) checkBallotAndGetLocalOpinion(
	ctx *tcontext,
	diffList []types.BlockID,
	className string,
	voteVector vec,
	baseBallotLayer types.LayerID,
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
					log.Named("exception_block_id", exceptionBlockID),
					log.Err(err),
				)
				return false
			}
			lid = exceptionBlock.LayerIndex
		}

		if lid.Before(baseBallotLayer) {
			logger.With().Error("good ballot candidate contains exception for block older than its base ballot",
				log.Named("older_block", exceptionBlockID),
				log.Named("older_layer", lid),
				log.Named("base_ballot_layer", baseBallotLayer))
			return false
		}

		v, err := t.getLocalBlockOpinion(ctx, lid, exceptionBlockID)
		if err != nil {
			logger.With().Error("unable to get single ballot opinion for block in exception list",
				log.Named("older_block", exceptionBlockID),
				log.Named("older_layer", lid),
				log.Named("base_ballot_layer", baseBallotLayer),
				log.Err(err))
			return false
		}

		if v != voteVector {
			logger.With().Debug("not adding ballot to good ballots because its vote differs from local opinion",
				log.Named("older_block", exceptionBlockID),
				log.Named("older_layer", lid),
				log.Named("local_opinion", v),
				log.String("ballot_exception_vote", className))
			return false
		}
	}
	return true
}

// BaseBlock selects a base ballot from sliding window based on a following priorities in order:
// - choose good ballot
// - choose ballot with the least difference to the local opinion
// - choose ballot from higher layer
// - otherwise deterministically select ballot with lowest id.
// TODO: return a Ballot instead.
func (t *turtle) BaseBlock(ctx context.Context) (types.BlockID, [][]types.BlockID, error) {
	var (
		tctx          = wrapContext(ctx)
		logger        = t.logger.WithContext(ctx)
		disagreements = map[types.BallotID]types.LayerID{}
		choices       []types.BallotID // choices from the best to the least bad

		// TODO change it per https://github.com/spacemeshos/go-spacemesh/issues/2920
		// in current interpretation Last is the last layer that was sent to us after hare completed
		votinglid = t.Last
	)

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

		return types.BlockID(ballotID), [][]types.BlockID{
			blockMapToArray(exceptions[0]),
			blockMapToArray(exceptions[1]),
			blockMapToArray(exceptions[2]),
		}, nil
	}

	// TODO: special error encoding when exceeding exception list size
	return types.BlockID{}, nil, errNoBaseBallotFound
}

// firstDisagreement returns first layer where local opinion is different from ballot's opinion within sliding window.
func (t *turtle) firstDisagreement(ctx *tcontext, blid types.LayerID, ballotID types.BallotID, disagreements map[types.BallotID]types.LayerID) (types.LayerID, error) {
	var (
		opinions = t.BallotOpinionsByLayer[blid][ballotID]
		from     = t.LastEvicted.Add(1)
		// using it as a mark that the votes for block are completely consistent
		// with a local opinion. so if two blocks have consistent histories select block
		// from a higher layer as it is more consistent.
		consistent = t.Last
	)

	// if the ballot is good we know that there are no exceptions that precede base ballot.
	// in such case current ballot will not fix any previous disagreements of the base ballot.
	// or if the base ballot completely agrees with local opinion compute disagreements after base ballot.
	if _, exist := t.GoodBallotsIndex[ballotID]; exist {
		block, err := t.bdp.GetBlock(types.BlockID(ballotID))
		if err != nil {
			return types.LayerID{}, fmt.Errorf("reading block %s: %w", ballotID, err)
		}
		ballot := block.ToBallot()
		basedis, exist := disagreements[ballot.BaseBallot]
		if exist {
			baselid := t.BallotLayer[ballot.BaseBallot]
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

// calculate and return a list of exceptions, i.e., differences between the opinions of a base ballot and the local
// opinion.
func (t *turtle) calculateExceptions(
	ctx *tcontext,
	votinglid,
	baselid types.LayerID,
	baseopinion Opinion, // candidate base ballot's opinion vector
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
		addDiffs := func(logger log.Log, bid types.BlockID, voteVec vec, diffMap map[types.BlockID]struct{}) {
			v, ok := baseopinion[bid]
			if !ok {
				v = against
			}
			if !equalVotes(voteVec, v) {
				logger.With().Debug("added vote diff", log.Named("opinion", v))
				if lid.Before(baselid) {
					logger.With().Warning("added exception before base ballot layer, this ballot will not be marked good")
				}
				diffMap[bid] = struct{}{}
			}
		}

		for bid, opinion := range local {
			usecoinflip := opinion == abstain && lid.Before(t.layerCutoff())
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

// voteWeight returns the weight to assign to one ballot's vote for another.
func (t *turtle) voteWeight(ballot *types.Ballot) (uint64, error) {
	atxHeader, err := t.atxdb.GetAtxHeader(ballot.AtxID)
	if err != nil {
		return 0, fmt.Errorf("get ATX header: %w", err)
	}
	return atxHeader.GetWeight(), nil
}

func (t *turtle) voteWeightByID(ballotID types.BallotID) (uint64, error) {
	block, err := t.bdp.GetBlock(types.BlockID(ballotID))
	if err != nil {
		return 0, fmt.Errorf("get block: %w", err)
	}
	return t.voteWeight(block.ToBallot())
}

// Persist saves the current tortoise state to the database.
func (t *turtle) persist() error {
	return t.state.Persist()
}

func (t *turtle) getBaseBallotOpinions(bid types.BallotID) Opinion {
	lid, ok := t.BallotLayer[bid]
	if !ok {
		return nil
	}
	byLayer, ok := t.BallotOpinionsByLayer[lid]
	if !ok {
		return nil
	}
	return byLayer[bid]
}

func (t *turtle) processBallot(ctx context.Context, ballot *types.Ballot) error {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Named("processing_ballot_id", ballot.ID()),
		log.Named("processing_ballot_layer", ballot.LayerIndex()))

	// When a new ballot arrives, we look up the ballot it points to in our table,
	// and add the corresponding vector (multiplied by the ballot weight) to our own vote-totals vector.
	// We then add the vote difference vector and the explicit vote vector to our vote-totals vector.
	logger.With().Debug("processing ballot", ballot.Fields()...)

	logger.With().Debug("ballot adds support for",
		log.Int("count", len(ballot.ForDiff)),
		types.BlockIdsField(ballot.ForDiff))

	baseBallotOpinion := t.getBaseBallotOpinions(ballot.BaseBallot)
	if len(baseBallotOpinion) == 0 {
		logger.With().Warning("base ballot opinions are empty. base ballot is unknown or was evicted",
			ballot.BaseBallot,
		)
	}

	voteWeight, err := t.voteWeight(ballot)
	if err != nil {
		return fmt.Errorf("error getting vote weight for ballot %v: %w", ballot.ID(), err)
	}

	// TODO: this logic would be simpler if For and Against were a single list
	//   see https://github.com/spacemeshos/go-spacemesh/issues/2369
	lth := len(ballot.ForDiff) +
		len(ballot.NeutralDiff) +
		len(ballot.AgainstDiff) +
		len(baseBallotOpinion)
	opinion := make(map[types.BlockID]vec, lth)

	for _, bid := range ballot.ForDiff {
		opinion[bid] = support.Multiply(voteWeight)
	}
	for _, bid := range ballot.AgainstDiff {
		opinion[bid] = against.Multiply(voteWeight)
	}
	for _, bid := range ballot.NeutralDiff {
		opinion[bid] = abstain
	}

	for blk, vote := range baseBallotOpinion {
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
	t.BallotLayer[ballot.ID()] = ballot.LayerIndex()
	logger.With().Debug("adding or updating ballot opinion")
	t.BallotOpinionsByLayer[ballot.LayerIndex()][ballot.ID()] = opinion
	return nil
}

func (t *turtle) processBallots(ctx *tcontext, ballots []*types.Ballot) error {
	logger := t.logger.WithContext(ctx)

	for _, b := range ballots {
		logger := logger.WithFields(b.ID(), b.LayerIndex())
		// make sure we don't write data on old ballots whose layer has already been evicted
		if b.LayerIndex().Before(t.LastEvicted) {
			logger.With().Warning("not processing ballot from layer older than last evicted layer",
				log.Named("last_evicted", t.LastEvicted))
			continue
		}
		if _, ok := t.BallotOpinionsByLayer[b.LayerIndex()]; !ok {
			t.BallotOpinionsByLayer[b.LayerIndex()] = make(map[types.BallotID]Opinion, t.LayerSize)
		}
		if err := t.processBallot(ctx, b); err != nil {
			return err
		}
	}

	t.scoreBallots(ctx, ballots)
	return nil
}

func (t *turtle) scoreBallotsByLayerID(ctx *tcontext, layerID types.LayerID) error {
	blks, err := t.bdp.LayerBlocks(layerID)
	if err != nil {
		return fmt.Errorf("layer blocks: %w", err)
	}
	t.scoreBallots(ctx, types.ToBallots(blks))
	return nil
}

func (t *turtle) scoreBallots(ctx *tcontext, ballots []*types.Ballot) {
	logger := t.logger.WithContext(ctx)
	logger.With().Debug("marking good ballots", log.Int("count", len(ballots)))
	numGood := 0
	for _, b := range ballots {
		if t.determineBallotGoodness(ctx, b) {
			// note: we have no way of warning if a ballot was previously marked as not good
			logger.With().Debug("marking ballot good", b.ID(), b.LayerIndex())
			t.GoodBallotsIndex[b.ID()] = false // false means good ballot, not flushed
			numGood++
		} else {
			logger.With().Info("not marking ballot good", b.ID(), b.LayerIndex())
			if _, isGood := t.GoodBallotsIndex[b.ID()]; isGood {
				logger.With().Warning("marking previously good ballot as not good", b.ID(), b.LayerIndex())
				delete(t.GoodBallotsIndex, b.ID())
			}
		}
	}

	logger.With().Info("finished marking good ballots",
		log.Int("total_ballots", len(ballots)),
		log.Int("good_ballots", numGood))
}

func (t *turtle) determineBallotGoodness(ctx *tcontext, ballot *types.Ballot) bool {
	logger := t.logger.WithContext(ctx).WithFields(
		ballot.ID(),
		ballot.LayerIndex(),
		log.Named("base_ballot_id", ballot.BaseBallot))
	// Go over all ballots, in order. Mark ballot i "good" if:
	// (1) it has the right beacon value
	if !t.ballotHasGoodBeacon(ballot, logger) {
		return false
	}
	// (2) the base ballot is marked as good
	if _, good := t.GoodBallotsIndex[ballot.BaseBallot]; !good {
		logger.Debug("base ballot is not good")
	} else if baselid, exist := t.BallotLayer[ballot.BaseBallot]; !exist {
		logger.With().Error("inconsistent state: base ballot not found")
	} else if true &&
		// (3) all diffs appear after the base ballot and are consistent with the current local opinion
		t.checkBallotAndGetLocalOpinion(ctx, ballot.ForDiff, "support", support, baselid, logger) &&
		t.checkBallotAndGetLocalOpinion(ctx, ballot.AgainstDiff, "against", against, baselid, logger) &&
		t.checkBallotAndGetLocalOpinion(ctx, ballot.NeutralDiff, "abstain", abstain, baselid, logger) {
		return true
	}
	return false
}

func (t *turtle) ballotHasGoodBeacon(ballot *types.Ballot, logger log.Log) bool {
	layerID := ballot.LayerIndex()

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

	epoch := ballot.LayerIndex().GetEpoch()
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

// HandleIncomingLayer processes all layer ballot votes
// returns the old pbase and new pbase after taking into account ballot votes.
func (t *turtle) HandleIncomingLayer(ctx context.Context, layerID types.LayerID) error {
	defer t.evict(ctx)

	tctx := wrapContext(ctx)
	// unconditionally set the layer to the last one that we have seen once tortoise or sync
	// submits this layer. it doesn't matter if we fail to process it.
	if t.Last.Before(layerID) {
		t.Last = layerID
	}
	if err := t.handleLayer(tctx, layerID); err != nil {
		return err
	}
	// attempt to verify layers up to the latest one for which we have new ballots
	return t.verifyLayers(tctx)
}

func (t *turtle) handleLayer(ctx *tcontext, layerID types.LayerID) error {
	logger := t.logger.WithContext(ctx).WithFields(layerID)

	if !layerID.After(types.GetEffectiveGenesis()) {
		logger.Debug("not attempting to handle genesis layer")
		return nil
	}

	// Note: we don't compare newlyr and t.Verified, so this method could be called again on an already-verified layer.
	// That would update the stored opinions, but it would not attempt to re-verify an already-verified layer.

	// read layer blocks
	layerBlocks, err := t.bdp.LayerBlocks(layerID)
	if err != nil {
		return fmt.Errorf("unable to read contents of layer %v: %w", layerID, err)
	}
	if len(layerBlocks) == 0 {
		t.logger.WithContext(ctx).Warning("cannot process empty layer block list")
		return nil
	}
	ballots := make([]*types.Ballot, 0, len(layerBlocks))
	for _, b := range layerBlocks {
		// cache block's layer so we can check to ignore opinions on very old blocks
		t.BlockLayer[b.ID()] = b.LayerIndex
		ballots = append(ballots, b.ToBallot())
	}
	return t.processBallots(ctx, ballots)
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

	filter := func(ballotID types.BallotID) bool {
		_, isgood := t.GoodBallotsIndex[ballotID]
		return isgood
	}

	weight, err := computeExpectedVoteWeight(t.atxdb, t.epochWeight, lid, t.Last)
	if err != nil {
		return nil, err
	}
	logger.Info("verifying: expected voting weight for layer", log.Uint64("weight", weight), lid)

	// Count the votes of good ballots. localOpinionOnBlock is our local opinion on this block.
	// Declare the vote vector "verified" up to position k if the total weight exceeds the confidence threshold in
	// all positions up to k: in other words, we can verify a layer k if the total weight of the global opinion
	// exceeds the confidence threshold, and agrees with local opinion.
	for bid, localOpinionOnBlock := range localOpinion {
		// count the votes of the input vote vector by summing the weighted votes on the block
		logger.With().Debug("summing votes for block",
			bid,
			log.Named("layer_start", lid.Add(1)),
			log.Named("layer_end", t.Last),
		)

		sum, err := t.sumVotesForBlock(ctx, bid, lid.Add(1), filter)
		if err != nil {
			return nil, fmt.Errorf("error summing votes for block %v in candidate layer %v: %w",
				bid, lid, err)
		}

		// check that the total weight exceeds the global threshold
		globalOpinionOnBlock := calculateOpinionWithThreshold(
			t.logger, sum, t.GlobalThreshold, weight)
		logger.With().Debug("verifying tortoise calculated global opinion on block",
			log.Named("block_voted_on", bid),
			lid,
			log.Named("global_vote_sum", sum),
			log.Named("global_opinion", globalOpinionOnBlock),
			log.Named("local_opinion", localOpinionOnBlock))

		// At this point, we have all the data we need to make a decision on this block. There are three possible
		// outcomes:
		// 1. record our opinion on this block and go on evaluating the rest of the blocks in this layer to see if
		//    we can verify the layer (if local and global consensus match, and global consensus is decided)
		// 2. keep waiting to verify the layer (if not, and the layer is relatively recent)
		// 3. trigger self healing (if not, and the layer is sufficiently old)
		consensusMatches := globalOpinionOnBlock == localOpinionOnBlock
		globalOpinionDecided := globalOpinionOnBlock != abstain

		// If, for any block in this layer, the global opinion (summed block votes) disagrees with our vote (the
		// input vector), or if the global opinion is abstain, then we do not verify this layer. This could be the
		// result of a reorg (e.g., resolution of a network partition), or a malicious peer during sync, or
		// disagreement about Hare success.
		if !consensusMatches {
			logger.With().Warning("global opinion on block differs from our vote, cannot verify layer",
				bid,
				log.Named("global_opinion", globalOpinionOnBlock),
				log.Named("local_opinion", localOpinionOnBlock))
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
				log.Named("global_opinion", globalOpinionOnBlock),
				log.Named("local_opinion", localOpinionOnBlock))
			return nil, nil
		}

		contextualValidity[bid] = globalOpinionOnBlock == support
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
		log.Named("layer_cutoff", t.layerCutoff()),
		log.Uint32("zdist", t.Zdist),
		log.Named("last_layer_received", t.Last),
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
	// rescore goodness of ballots in all intervening layers on the basis of new information
	for layerID := lastVerified.Add(1); !layerID.After(t.Last); layerID = layerID.Add(1) {
		if err := t.scoreBallotsByLayerID(ctx, layerID); err != nil {
			// if we fail to process a layer, there's probably no point in trying to rescore ballots
			// in later layers, so just print an error and bail
			logger.With().Error("error trying to rescore good ballots in healed layers",
				log.Named("layer_from", lastVerified),
				log.Named("layer_to", t.Last),
				log.Err(err))
			return false, err
		}
	}
	return true, nil
}

// loops over all layers from the last verified up to a new target layer and attempts to verify each in turn.
func (t *turtle) verifyLayers(ctx *tcontext) error {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Named("verification_target", t.Last),
		log.Named("old_verified", t.Verified))

	// attempt to verify each layer from the last verified up to one prior to the newly-arrived layer.
	// this is the full range of unverified layers that we might possibly be able to verify at this point.
	// Note: t.Verified is initialized to the effective genesis layer, so the first candidate layer here necessarily
	// follows and is post-genesis. There's no need for an additional check here.
	for candidateLayerID := t.Verified.Add(1); candidateLayerID.Before(t.Last); candidateLayerID = candidateLayerID.Add(1) {
		logger := logger.WithFields(log.Named("candidate_layer", candidateLayerID))

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
			logger.With().Info("verified candidate layer", log.Named("new_verified", t.Verified))
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

	weight, err := computeExpectedVoteWeight(t.atxdb, t.epochWeight, lid, t.Last)
	if err != nil {
		return nil, err
	}
	logger.Info("local opinion: expected voting weight for layer", log.Uint64("weight", weight), lid)

	// this layer has not yet been verified
	// we must have an opinion about older layers at this point. if the layer hasn't been verified yet, count votes
	// and see if they pass the local threshold.
	logger.With().Debug("counting votes for and against blocks in old, unverified layer",
		log.Int("num_blocks", len(bids)))
	for _, bid := range bids {
		logger := logger.WithFields(log.Named("candidate_block_id", bid))
		sum, err := t.sumVotesForBlock(ctx, bid, lid.Add(1), func(ballotID types.BallotID) bool { return true })
		if err != nil {
			return nil, fmt.Errorf("error summing votes for block %v in old layer %v: %w",
				bid, lid, err)
		}

		local := calculateOpinionWithThreshold(t.logger, sum, t.LocalThreshold, weight)
		logger.With().Debug("local opinion on block in old layer",
			sum,
			log.Named("local_opinion", local),
		)
		opinion[bid] = local
	}

	return opinion, nil
}

func (t *turtle) sumVotesForBlock(
	ctx context.Context,
	blockID types.BlockID, // the block we're summing votes for/against
	startLayer types.LayerID,
	filter func(types.BallotID) bool,
) (vec, error) {
	sum := abstain
	logger := t.logger.WithContext(ctx).WithFields(
		log.Named("start_layer", startLayer),
		log.Named("end_layer", t.Last),
		log.Named("block_voting_on", blockID),
		log.Named("layer_voting_on", startLayer.Sub(1)))
	for voteLayer := startLayer; !voteLayer.After(t.Last); voteLayer = voteLayer.Add(1) {
		logger := logger.WithFields(voteLayer)
		for ballotID, ballotOpinion := range t.BallotOpinionsByLayer[voteLayer] {
			if !filter(ballotID) {
				logger.With().Debug("voting block did not pass filter, not counting its vote", ballotID)
				continue
			}

			// check if this ballot has an opinion on the block.
			// no opinion (on a block in an older layer) counts as an explicit vote against the block.
			// note: in this case, the weight is already factored into the vote, so no need to fetch weight.
			if opinionVote, exists := ballotOpinion[blockID]; exists {
				sum = sum.Add(opinionVote)
			} else {
				// in this case, we still need to fetch the ballot's weight.
				weight, err := t.voteWeightByID(ballotID)
				if err != nil {
					return sum, fmt.Errorf("error getting weight for ballot %v: %w",
						ballotID, err)
				}
				sum = sum.Add(against.Multiply(weight))
				logger.With().Debug("no opinion on older block, counted vote against",
					log.Uint64("weight", weight),
					sum)
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
				log.Named("highest_healable_layer", latestLayerWeCanVerify))
			return
		}

		// Calculate the global opinion on all blocks in the layer
		// Note: we look at ALL blocks we've seen for the layer, not just those we've previously marked contextually valid
		logger.Info("self healing attempting to verify candidate layer")

		layerBlockIds, err := getLayerBlocksIDs(ctx, t.bdp, candidateLayerID)
		if err != nil {
			logger.Error("inconsistent state: can't find layer in database, cannot heal", log.Err(err))
			return
		}

		weight, err := computeExpectedVoteWeight(t.atxdb, t.epochWeight, candidateLayerID, t.Last)
		if err != nil {
			logger.Error("failed to compute expected vote weight", log.Err(err))
			return
		}
		logger.Info("healing: expected voting weight for layer", log.Uint64("weight", weight), candidateLayerID)

		// record the contextual validity for all blocks in this layer
		for _, blockID := range layerBlockIds {
			logger := logger.WithFields(log.Named("candidate_block_id", blockID))

			// count all votes for or against this block by all blocks in later layers. for ballots with a different
			// beacon values, we delay their votes by badBeaconVoteDelays layers
			sum, err := t.sumVotesForBlock(ctx, blockID, candidateLayerID.Add(1), t.ballotFilterForHealing(candidateLayerID, logger))
			if err != nil {
				logger.Error("error summing votes for candidate block in candidate layer", log.Err(err))
				return
			}

			// check that the total weight exceeds the global threshold
			globalOpinionOnBlock := calculateOpinionWithThreshold(t.logger, sum, t.GlobalThreshold, weight)
			logger.With().Debug("self healing calculated global opinion on candidate block",
				log.Named("global_opinion", globalOpinionOnBlock),
				sum,
			)

			if globalOpinionOnBlock == abstain {
				logger.With().Info("self healing failed to verify candidate layer, will reattempt later",
					log.Named("global_opinion", globalOpinionOnBlock),
					sum,
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
// and delay their votes by badBeaconVoteDelays layers.
func (t *turtle) ballotFilterForHealing(candidateLayerID types.LayerID, logger log.Log) func(types.BallotID) bool {
	return func(ballotID types.BallotID) bool {
		if _, bad := t.badBeaconBallots[ballotID]; !bad {
			return true
		}
		lid, exist := t.BallotLayer[ballotID]
		if !exist {
			logger.With().Error("inconsistent state: ballot not found", ballotID)
			return false
		}
		return lid.Uint32() > t.BadBeaconVoteDelayLayers && lid.Sub(t.BadBeaconVoteDelayLayers).After(candidateLayerID)
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
func computeExpectedVoteWeight(atxdb atxDataProvider, epochWeight map[types.EpochID]uint64, target, last types.LayerID) (uint64, error) {
	var (
		total  = new(big.Float)
		layers = uint64(types.GetLayersPerEpoch())
	)
	// TODO(dshulyak) this can be improved by computing weight for epoch at once
	for lid := target.Add(1); !lid.After(last); lid = lid.Add(1) {
		weight, err := getEpochWeight(atxdb, epochWeight, lid.GetEpoch())
		if err != nil {
			return 0, err
		}
		layerWeight := new(big.Float).SetUint64(weight)
		layerWeight.Quo(layerWeight, new(big.Float).SetUint64(layers))
		total.Add(total, layerWeight)
	}
	rst, _ := total.Uint64()
	return rst, nil
}
