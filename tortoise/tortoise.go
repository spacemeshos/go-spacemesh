package tortoise

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
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
	cdb    *datastore.CachedDB

	beacons system.BeaconGetter
	updater blockValidityUpdater

	mode mode

	commonState
	verifying *verifying
	full      *full
}

// newTurtle creates a new verifying tortoise algorithm instance.
func newTurtle(
	logger log.Log,
	cdb *datastore.CachedDB,
	beacons system.BeaconGetter,
	updater blockValidityUpdater,
	config Config,
) *turtle {
	t := &turtle{
		Config:      config,
		commonState: newCommonState(),
		logger:      logger,
		cdb:         cdb,
		beacons:     beacons,
		updater:     updater,
	}
	t.verifying = newVerifying(config, &t.commonState)
	t.full = newFullTortoise(config, &t.commonState)
	return t
}

// cloneTurtleParams creates a new verifying tortoise instance using the params of this instance.
func (t *turtle) cloneTurtleParams() *turtle {
	return newTurtle(
		t.logger,
		t.cdb,
		t.beacons,
		t.updater,
		t.Config,
	)
}

func (t *turtle) init(ctx context.Context, genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	genesis := genesisLayer.Index()
	for _, blk := range genesisLayer.Blocks() {
		t.blockLayer[blk.ID()] = genesis
		t.blocks[genesis] = []blockInfo{{id: blk.ID()}}
		t.validity[blk.ID()] = support
		t.hareOutput[genesisLayer.Index()] = blk.ID()
	}
	for _, ballot := range genesisLayer.Ballots() {
		t.ballotLayer[ballot.ID()] = genesis
		t.ballots[genesis] = []ballotInfo{{id: ballot.ID(), weight: util.WeightFromInt64(0)}}
		t.verifying.goodBallots[ballot.ID()] = good
	}
	t.last = genesis
	t.processed = genesis
	t.verified = genesis
	t.historicallyVerified = genesis
	t.evicted = genesis.Sub(1)
	t.minprocessed = genesis
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
			delete(t.ballotLayer, ballot.id)
			delete(t.referenceWeight, ballot.id)
			delete(t.badBeaconBallots, ballot.id)
			delete(t.verifying.goodBallots, ballot.id)
			delete(t.full.votes, ballot.id)
			delete(t.full.abstain, ballot.id)
			delete(t.full.base, ballot.id)
		}
		delete(t.ballots, lid)
		for _, block := range t.blocks[lid] {
			delete(t.blockLayer, block.id)
			delete(t.validity, block.id)
		}
		delete(t.blocks, lid)
		delete(t.hareOutput, lid)
		delete(t.verifying.goodWeight, lid)
		delete(t.verifying.abstainedWeight, lid)
		delete(t.verifying.layerReferenceHeight, lid)
		delete(t.full.empty, lid)
		if lid.GetEpoch() < oldestEpoch {
			delete(t.refBallotBeacons, lid.GetEpoch())
			delete(t.epochWeight, lid.GetEpoch())
			delete(t.referenceHeight, lid.GetEpoch())
			oldestEpoch = lid.GetEpoch()
		}
	}
	t.evicted = windowStart.Sub(1)
}

// EncodeVotes by choosing base ballot and explicit votes.
func (t *turtle) EncodeVotes(ctx context.Context, conf *encodeConf) (*types.Votes, error) {
	var (
		logger        = t.logger.WithContext(ctx)
		disagreements = map[types.BallotID]types.LayerID{}
		choices       []types.BallotID

		ballotID  types.BallotID
		ballotLID types.LayerID
		votes     *types.Votes
		last      = t.last.Add(1)
		err       error
	)
	if conf.current != nil {
		last = *conf.current
	}
	// goodness of the ballot determined using hare output or tortoise output for old layers.
	// if tortoise is full mode some ballot in old layer is undecided and we can't use it this optimization.
	if t.mode.isVerifying() {
		ballotID, ballotLID = t.getGoodBallot(logger)
		if ballotID != types.EmptyBallotID {
			// we need only 1 ballot from the most recent layer, this ballot will be by definition the most
			// consistent with our local opinion.
			// then we just need to encode our local opinion from layer of the ballot up to last processed as votes
			votes, err = t.encodeVotes(ctx, ballotID, ballotLID, ballotLID, last, func(types.LayerID, types.BlockID) sign { return against })
			if err != nil {
				return nil, err
			}
		}
	}
	if votes == nil {
		for lid := t.evicted.Add(1); !lid.After(t.processed); lid = lid.Add(1) {
			for _, ballot := range t.ballots[lid] {
				if ballot.weight.IsNil() {
					continue
				}
				dis, err := t.firstDisagreement(ctx, lid, last, ballot.id, disagreements)
				if err != nil {
					logger.With().Error("failed to compute first disagreement", ballot.id, log.Err(err))
					continue
				}
				disagreements[ballot.id] = dis
				choices = append(choices, ballot.id)
			}
		}

		prioritizeBallots(choices, disagreements, t.ballotLayer, t.badBeaconBallots)
		for _, ballotID = range choices {
			ballotLID = t.ballotLayer[ballotID]
			votes, err = t.encodeVotes(ctx, ballotID, ballotLID, t.evicted.Add(1), last, func(lid types.LayerID, blockID types.BlockID) sign {
				return t.full.getVote(logger, ballotID, lid, blockID)
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

	if votes == nil {
		// TODO: special error encoding when exceeding exception list size
		return nil, errNoBaseBallotFound
	}

	logger.With().Info("choose base ballot",
		log.Stringer("mode", t.mode),
		log.Stringer("base_layer", ballotLID),
		log.Stringer("voting_layer", last),
		log.Inline(votes),
	)

	metrics.LayerDistanceToBaseBallot.WithLabelValues().Observe(float64(t.last.Value - ballotLID.Value))

	return votes, nil
}

func (t *turtle) getGoodBallot(logger log.Log) (types.BallotID, types.LayerID) {
	var choices []types.BallotID

	for lid := t.processed; lid.After(t.evicted); lid = lid.Sub(1) {
		for _, ballot := range t.ballots[lid] {
			if ballot.weight.IsNil() {
				continue
			}
			if rst := t.verifying.goodBallots[ballot.id]; rst == good {
				choices = append(choices, ballot.id)
			}
		}
		if len(choices) > 0 {
			sort.Slice(choices, func(i, j int) bool {
				return choices[i].Compare(choices[j])
			})
			return choices[0], lid
		}
	}
	return types.BallotID{}, types.LayerID{}
}

// firstDisagreement returns first layer where local opinion is different from ballot's opinion within sliding window.
func (t *turtle) firstDisagreement(ctx context.Context, blid, last types.LayerID, ballotID types.BallotID, disagreements map[types.BallotID]types.LayerID) (types.LayerID, error) {
	var (
		// using it as a mark that the votes for block are completely consistent
		// with a local opinion. so if two blocks have consistent histories select block
		// from a higher layer as it is more consistent.
		consistent  = last
		start       = t.evicted
		base, exist = t.full.base[ballotID]
		basedis     = disagreements[base]
	)
	if exist && basedis != consistent {
		return basedis, nil
	}
	start = t.ballotLayer[base]

	for lid := start; lid.Before(blid); lid = lid.Add(1) {
		if len(t.blocks[lid]) == 0 {
			// it is not possible to encode against for empty layer
			// because there are no blocks
			// therefore we should not pick base ballot that votes abstain on
			// layer outside zdist if layer is empty
			if !isUndecided(t.Config, t.hareOutput, lid, last) && t.full.abstained(ballotID, lid) {
				t.logger.With().Debug("ballot votes abstain on a layer without blocks. can't use as a base ballot",
					ballotID,
					lid,
				)
				return types.LayerID{}, nil
			}
		}
		for _, block := range t.blocks[lid] {
			localVote, _, err := t.getFullVote(ctx, lid, block)
			if err != nil {
				return types.LayerID{}, err
			}
			vote := t.full.getVote(t.logger, ballotID, lid, block.id)
			if localVote != vote {
				t.logger.With().Debug("found disagreement on a block",
					ballotID,
					block.id,
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

type opinionsGetter func(types.LayerID, types.BlockID) sign

// encode differences between selected base ballot and local votes.
func (t *turtle) encodeVotes(
	ctx context.Context,
	ballot types.BallotID,
	baselid,
	startlid types.LayerID,
	last types.LayerID,
	getter opinionsGetter,
) (*types.Votes, error) {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Stringer("base_layer", baselid),
		log.Stringer("last_layer", last),
	)
	votes := &types.Votes{
		Base: ballot,
	}

	for lid := startlid; lid.Before(last); lid = lid.Add(1) {
		logger := logger.WithFields(log.Named("block_layer", lid))

		if isUndecided(t.Config, t.hareOutput, lid, last) {
			votes.Abstain = append(votes.Abstain, lid)
			continue
		}

		for _, block := range t.blocks[lid] {
			localVote, reason, err := t.getFullVote(ctx, lid, block)
			if err != nil {
				return nil, err
			}
			logger := logger.WithFields(
				log.Stringer("block", block.id),
				log.Stringer("local_vote_reason", reason),
				log.Stringer("local_vote", localVote),
			)

			baseVote := getter(lid, block.id)
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

			if !needsException {
				continue
			}

			switch localVote {
			case support:
				votes.Support = append(votes.Support, block.id)
			case against:
				votes.Against = append(votes.Against, block.id)
			case abstain:
				logger.With().Error("layers that are not terminated should have been encoded earlier",
					block.id, lid,
				)
			}
		}
	}

	if explen := len(votes.Support) + len(votes.Against); explen > t.MaxExceptions {
		return nil, fmt.Errorf("%s (%v)", errstrTooManyExceptions, explen)
	}

	return votes, nil
}

// getFullVote unlike getLocalVote will vote according to the counted votes on blocks that are
// outside of hdist. if opinion is undecided according to the votes it will use coinflip recorded
// in the current layer.
func (t *turtle) getFullVote(ctx context.Context, lid types.LayerID, block blockInfo) (sign, voteReason, error) {
	vote, reason := getLocalVote(&t.commonState, t.Config, lid, block.id)
	if !(vote == abstain && reason == reasonValidity) {
		return vote, reason, nil
	}
	vote = sign(block.weight.Cmp(t.localThreshold))
	if vote != abstain {
		return vote, reasonLocalThreshold, nil
	}
	coin, err := layers.GetWeakCoin(t.cdb, t.last)
	if err != nil {
		return 0, "", fmt.Errorf("coinflip is not recorded in %s. required for vote on %s / %s",
			t.last, block.id, lid)
	}
	if coin {
		return support, reasonCoinflip, nil
	}
	return against, reasonCoinflip, nil
}

func (t *turtle) markBeaconWithBadBallot(logger log.Log, ballot *types.Ballot) error {
	layerID := ballot.LayerIndex

	// first check if we have it in the cache
	if _, bad := t.badBeaconBallots[ballot.ID()]; bad {
		return nil
	}

	epochBeacon, err := t.beacons.GetBeacon(layerID.GetEpoch())
	if err != nil {
		return err
	}

	beacon, err := t.getBallotBeacon(ballot, logger)
	if err != nil {
		return err
	}
	good := beacon == epochBeacon
	if !good {
		logger.With().Warning("ballot has different beacon",
			ballot.LayerIndex,
			ballot.ID(),
			log.String("ballot_beacon", beacon.ShortString()),
			log.String("epoch_beacon", epochBeacon.ShortString()))
		t.badBeaconBallots[ballot.ID()] = struct{}{}
	}
	return nil
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
		refBallot, err := ballots.Get(t.cdb, refBallotID)
		if err != nil {
			return types.EmptyBeacon, fmt.Errorf("get ref ballot: %w", err)
		}
		if refBallot.EpochData == nil {
			return types.EmptyBeacon, fmt.Errorf("ballot %v missing epoch data", refBallotID)
		}
		beacon = refBallot.EpochData.Beacon
	}
	t.refBallotBeacons[epoch][refBallotID] = beacon
	return beacon, nil
}

// onLayerTerminated is expected to be called when hare terminated for a layer.
// Internally tortoise will verify all layers before the last one if there are no gaps in
// terminated layers.
func (t *turtle) onLayerTerminated(ctx context.Context, lid types.LayerID) error {
	t.logger.With().Debug("on layer terminated", lid)
	defer t.evict(ctx)
	if err := t.updateLayer(t.logger, lid); err != nil {
		return err
	}
	if err := t.loadBlocksData(lid); err != nil {
		return err
	}
	for process := t.minprocessed.Add(1); !process.After(t.processed); process = process.Add(1) {
		if isUndecided(t.Config, t.hareOutput, process, t.last) {
			t.logger.With().Info("gap in the layers received by tortoise",
				lid,
				log.Stringer("undecided", process),
			)
			break
		}
		// load data for layers that were skipped due to zdist limit
		if err := t.loadBlocksData(process); err != nil {
			return err
		}
		if err := t.loadBallots(process); err != nil {
			return err
		}
		if err := t.processLayer(t.logger.WithContext(ctx).WithFields(process), process); err != nil {
			return err
		}
		t.minprocessed = process
	}

	return nil
}

func (t *turtle) switchModes(logger log.Log) {
	from := t.mode
	t.mode = from.toggleMode()
	logger.With().Info("switching tortoise mode",
		log.Stringer("processed_layer", t.processed),
		log.Stringer("verified_layer", t.verified),
		log.Stringer("from_mode", from),
		log.Stringer("to_mode", t.mode),
	)
}

func (t *turtle) processLayer(logger log.Log, lid types.LayerID) error {
	logger = logger.WithFields(
		log.Stringer("last_layer", t.last),
	)
	logger.With().Debug("processing layer", lid)

	// TODO(dshulyak) it should be possible to count votes from every single ballot separately
	// but may require changes to t.processed
	t.verifying.countVotes(logger, lid, t.getTortoiseBallots(lid))

	previous := t.verified
	for target := t.verified.Add(1); target.Before(lid); target = target.Add(1) {
		var success bool
		if t.mode.isVerifying() {
			success = t.verifying.verify(logger, target)
		}
		if !success && (t.canUseFullMode() || t.mode.isFull()) {
			if t.mode.isVerifying() {
				t.switchModes(logger)
			}

			// verifying has a large verification window (think 1_000_000) and if it failed to verify layer
			// the threshold will be computed according to that window.
			// if we won't reset threshold full tortoise will have to count votes for 1_000_000 layers before
			// any layer can be expected to get verified. this is infeasible given current performance
			// of the full tortoise and may take weeks to finish.
			// instead we recompute window using configuration for the full mode (think 2_000 layers)
			success = t.catchupToVerifyingInFullMode(logger, lid, target)
		}
		if success {
			t.verified = target
			t.localThreshold, t.globalThreshold = computeThresholds(logger, t.Config, t.mode,
				t.verified.Add(1), t.last, t.processed,
				t.epochWeight,
			)
		} else {
			break
		}
	}
	if err := persistContextualValidity(logger,
		t.updater,
		previous, t.verified,
		t.blocks,
		t.validity,
	); err != nil {
		return err
	}

	t.updateHistoricallyVerified()
	return nil
}

func (t *turtle) catchupToVerifyingInFullMode(logger log.Log, vcounted, target types.LayerID) bool {
	counted := maxLayer(t.full.counted.Add(1), target.Add(1))
	for ; !counted.After(vcounted); counted = counted.Add(1) {
		t.full.countLayerVotes(logger, counted)

		t.localThreshold, t.globalThreshold = computeThresholds(logger, t.Config, t.mode,
			target, t.last, counted,
			t.epochWeight,
		)
		if t.full.verify(logger, target) {
			break
		}
	}
	if !t.full.verify(logger, target) {
		return false
	}

	// try to find a cut with ballots that can be good (see verifying tortoise for definition)
	// if there are such ballots try to bootstrap verifying tortoise by marking them good
	t.verifying.resetWeights()
	if t.verifying.markGoodCut(logger, target, t.getTortoiseBallots(target)) {
		// TODO(dshulyak) it should be enough to start from target + 1. can't do that right now as it is expected
		// that accumulated weight has a weight of the layer that is going to be verified.
		for lid := target; !lid.After(counted); lid = lid.Add(1) {
			t.verifying.countVotes(logger, lid, t.getTortoiseBallots(lid))
		}
		if t.verifying.verify(logger, target) {
			for lid := counted.Add(1); !lid.After(t.processed); lid = lid.Add(1) {
				t.verifying.countVotes(logger, lid, t.getTortoiseBallots(lid))
			}
			t.switchModes(logger)
		}
	}
	return true
}

func (t *turtle) getTortoiseBallots(lid types.LayerID) []tortoiseBallot {
	ballots := t.ballots[lid]
	if len(ballots) == 0 {
		return nil
	}
	tballots := make([]tortoiseBallot, 0, len(ballots))
	for _, ballot := range ballots {
		tballots = append(tballots, tortoiseBallot{
			id:      ballot.id,
			base:    t.full.base[ballot.id],
			votes:   t.full.votes[ballot.id],
			abstain: t.full.abstain[ballot.id],
			weight:  ballot.weight,
			height:  ballot.height,
		})
	}
	return tballots
}

// loadBlocksData loads blocks, hare output and contextual validity.
func (t *turtle) loadBlocksData(lid types.LayerID) error {
	blocks, err := blocks.Layer(t.cdb, lid)
	if err != nil {
		return fmt.Errorf("read blocks for layer %s: %w", lid, err)
	}
	for _, block := range blocks {
		t.onBlock(lid, block)
	}

	if err := t.loadHare(lid); err != nil {
		return err
	}
	return t.loadContextualValidity(lid)
}

func (t *turtle) loadHare(lid types.LayerID) error {
	output, err := layers.GetHareOutput(t.cdb, lid)
	if err == nil {
		t.onHareOutput(lid, output)
		return nil
	}
	if errors.Is(err, sql.ErrNotFound) {
		t.logger.With().Debug("hare output for layer is not found", lid)
		return nil
	}
	return fmt.Errorf("get hare output %s: %w", lid, err)
}

func (t *turtle) loadContextualValidity(lid types.LayerID) error {
	// validities will be available only during rerun or
	// if they are synced from peers, which we don't do currently
	validities, err := blocks.ContextualValidity(t.cdb, lid)
	if err != nil {
		return fmt.Errorf("contextual validity %s: %w", lid, err)
	}
	for _, validity := range validities {
		s := support
		if !validity.Validity {
			s = against
		}
		t.validity[validity.ID] = s
	}
	return nil
}

func (t *turtle) updateLayer(logger log.Log, lid types.LayerID) error {
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
		weight, height, err := extractAtxsData(t.cdb, epoch)
		if err != nil {
			return err
		}
		t.epochWeight[epoch] = weight
		t.referenceHeight[epoch] = height
		logger.With().Info("computed height and weight for epoch",
			epoch,
			log.Stringer("weight", weight),
			log.Uint64("height", height),
		)
	}
	window := getVerificationWindow(t.Config, t.mode, t.verified.Add(1), t.last)
	if lastUpdated || window.Before(t.processed) || t.globalThreshold.IsNil() {
		t.localThreshold, t.globalThreshold = computeThresholds(logger, t.Config, t.mode,
			t.verified.Add(1), t.last, t.processed,
			t.epochWeight,
		)
	}
	return nil
}

// loadBallots from database.
// must be loaded in order, as base ballot information needs to be in the state.
func (t *turtle) loadBallots(lid types.LayerID) error {
	blts, err := ballots.Layer(t.cdb, lid)
	if err != nil {
		return fmt.Errorf("read ballots for layer %s: %w", lid, err)
	}

	for _, ballot := range blts {
		if err := t.onBallot(ballot); err != nil {
			t.logger.With().Warning("failed to add ballot to the state", log.Err(err), log.Inline(ballot))
		}
	}
	return nil
}

func (t *turtle) onBlock(lid types.LayerID, block *types.Block) {
	if !lid.After(t.evicted) {
		return
	}
	if _, exist := t.referenceHeight[lid.GetEpoch()]; !exist {
		// TODO(dshulyak) reference height is computed when first layer in the epoch
		// is sent to the onLayerTerminated. after that we will load blocks from that layer.
		// this warning is expected if block was sent to onBlock before the first event
		t.logger.With().Debug("block was submitted before computing reference height", block.ID(), lid)
		return
	}
	if _, exist := t.blockLayer[block.ID()]; exist {
		return
	}
	t.logger.With().Debug("on block", log.Inline(block))
	t.blockLayer[block.ID()] = lid
	t.blocks[lid] = append(t.blocks[lid],
		blockInfo{
			id:     block.ID(),
			height: block.TickHeight,
			weight: util.WeightFromUint64(0),
		},
	)
	t.verifying.onBlock(block)
}

func (t *turtle) onHareOutput(lid types.LayerID, bid types.BlockID) {
	if !lid.After(t.evicted) {
		return
	}
	t.logger.With().Debug("on hare output", lid, bid, log.Bool("empty", bid == types.EmptyBlockID))
	t.hareOutput[lid] = bid
}

func (t *turtle) onBallot(ballot *types.Ballot) error {
	if !ballot.LayerIndex.After(t.evicted) {
		return nil
	}
	if _, exist := t.referenceHeight[ballot.LayerIndex.GetEpoch()]; !exist {
		t.logger.With().Debug("ballot was submitted before computing reference height", ballot.ID(), ballot.LayerIndex)
		return nil
	}

	t.logger.With().Debug("on ballot", log.Inline(ballot))
	if _, exist := t.ballotLayer[ballot.ID()]; exist {
		return nil
	}

	if err := t.markBeaconWithBadBallot(t.logger, ballot); err != nil {
		return err
	}
	var baselid types.LayerID
	// NOTE this condition is useful only in tests, but will be used once we remove "genesis" ballot
	if ballot.Votes.Base == types.EmptyBallotID {
		baselid = types.GetEffectiveGenesis()
	} else {
		var exist bool
		baselid, exist = t.ballotLayer[ballot.Votes.Base]
		if !exist {
			return fmt.Errorf("cant decode votes without base ballot %v", ballot.Votes.Base)
		}
	}

	var (
		weight util.Weight
		height uint64
	)
	if !ballot.IsMalicious() {
		var err error
		weight, err = computeBallotWeight(
			t.cdb, t.referenceWeight,
			ballot, t.LayerSize, types.GetLayersPerEpoch(),
		)
		if err != nil {
			return err
		}
		height, err = getBallotHeight(t.cdb, ballot)
		if err != nil {
			return err
		}
	} else {
		t.logger.With().Warning("observed malicious ballot", ballot.ID(), ballot.LayerIndex)
	}
	t.logger.With().Debug("computed weight and height for ballot",
		ballot.ID(),
		log.Stringer("weight", weight),
		log.Uint64("height", height),
	)
	// all potential errors must be handled before modifying state

	t.ballotLayer[ballot.ID()] = ballot.LayerIndex
	t.ballots[ballot.LayerIndex] = append(t.ballots[ballot.LayerIndex],
		ballotInfo{id: ballot.ID(), weight: weight, height: height})
	abstainVotes := map[types.LayerID]struct{}{}
	for _, lid := range ballot.Votes.Abstain {
		abstainVotes[lid] = struct{}{}
	}

	votes := votes{}
	for lid := baselid; lid.Before(ballot.LayerIndex); lid = lid.Add(1) {
		if _, exist := abstainVotes[lid]; exist {
			continue
		}
		for _, block := range t.blocks[lid] {
			votes[block.id] = against
		}
	}
	for _, bid := range ballot.Votes.Support {
		votes[bid] = support
	}
	for _, bid := range ballot.Votes.Against {
		votes[bid] = against
	}

	tballot := tortoiseBallot{
		id:      ballot.ID(),
		base:    ballot.Votes.Base,
		weight:  weight,
		height:  height,
		votes:   votes,
		abstain: abstainVotes,
	}

	t.full.onBallot(&tballot)
	return nil
}

// the idea here is to give enough room for verifying tortoise to complete. during live tortoise execution this will be limited by the hdist.
// during rerun we need to use another heuristic, as hdist is irrelevant by that time.
func (t *turtle) canUseFullMode() bool {
	target := t.verified.Add(1)
	// TODO(dshulyak) this condition should be enabled when the node is syncing.
	if t.mode.isRerun() {
		return t.processed.Difference(target) > t.VerifyingModeVerificationWindow ||
			// if all layer were exhaused and verifying didn't made progress try switching
			t.last == t.processed
	}
	return target.Before(t.layerCutoff())
}

// layerCuttoff returns last layer that is in hdist distance.
func (t *turtle) layerCutoff() types.LayerID {
	// if we haven't seen at least Hdist layers yet, we always rely on local opinion
	if t.last.Before(types.NewLayerID(t.Hdist)) {
		return types.NewLayerID(0)
	}
	return t.last.Sub(t.Hdist)
}

func isUndecided(config Config, decided map[types.LayerID]types.BlockID, lid, last types.LayerID) bool {
	if _, exists := decided[lid]; exists {
		return false
	}
	genesis := types.GetEffectiveGenesis()
	limit := genesis
	if last.After(genesis.Add(config.Zdist)) {
		limit = last.Sub(config.Zdist)
	}
	return !lid.Before(limit)
}

func getLocalVote(state *commonState, config Config, lid types.LayerID, block types.BlockID) (sign, voteReason) {
	genesis := types.GetEffectiveGenesis()
	limit := types.GetEffectiveGenesis()
	if state.last.After(genesis.Add(config.Hdist)) {
		limit = state.last.Sub(config.Hdist)
	}
	if !lid.Before(limit) {
		vote, exist := state.hareOutput[lid]
		if !exist && isUndecided(config, state.hareOutput, lid, state.last) {
			return abstain, reasonHareOutput
		}
		if vote == block {
			return support, reasonHareOutput
		}
		return against, reasonHareOutput
	}
	if lid.After(state.historicallyVerified) {
		return abstain, reasonValidity
	}
	return state.validity[block], reasonValidity
}
