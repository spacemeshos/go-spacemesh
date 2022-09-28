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

	state

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
		Config:  config,
		state:   *newState(),
		logger:  logger,
		cdb:     cdb,
		beacons: beacons,
		updater: updater,
	}
	t.verifying = newVerifying(config, &t.state)
	t.full = newFullTortoise(config, &t.state)
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
	for _, ballot := range genesisLayer.Ballots() {
		binfo := &ballotInfo{
			id:     ballot.ID(),
			layer:  ballot.LayerIndex,
			weight: util.WeightFromUint64(0),
			conditions: conditions{
				baseGood:   true,
				consistent: true,
			},
		}
		t.ballots[genesis] = append(t.ballots[genesis], binfo)
		t.ballotRefs[ballot.ID()] = binfo
	}
	t.layers[genesis] = &layerInfo{
		lid:            genesis,
		empty:          util.WeightFromUint64(0),
		hareTerminated: true,
	}
	for _, block := range genesisLayer.Blocks() {
		blinfo := &blockInfo{
			id:       block.ID(),
			layer:    genesis,
			hare:     support,
			validity: support,
		}
		t.layers[genesis].blocks = append(t.layers[genesis].blocks, blinfo)
		t.blockRefs[blinfo.id] = blinfo
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

	t.logger.With().Debug("evict in memory state",
		log.Stringer("from_layer", t.evicted.Add(1)),
		log.Stringer("upto_layer", windowStart),
	)

	for lid := t.evicted.Add(1); lid.Before(windowStart); lid = lid.Add(1) {
		for _, ballot := range t.ballots[lid] {
			delete(t.ballotRefs, ballot.id)
			delete(t.referenceWeight, ballot.id)
		}
		delete(t.ballots, lid)
		for _, block := range t.layers[lid].blocks {
			delete(t.blockRefs, block.id)
		}
		delete(t.layers, lid)
		if lid.OrdinalInEpoch() == types.GetLayersPerEpoch()-1 {
			delete(t.epochWeight, lid.GetEpoch())
			delete(t.referenceHeight, lid.GetEpoch())
		}
	}
	for _, ballot := range t.ballots[windowStart] {
		ballot.votes.cutBefore(windowStart)
	}
	t.evicted = windowStart.Sub(1)
}

// EncodeVotes by choosing base ballot and explicit votes.
func (t *turtle) EncodeVotes(ctx context.Context, conf *encodeConf) (*types.Votes, error) {
	var (
		logger        = t.logger.WithContext(ctx)
		disagreements = map[types.BallotID]types.LayerID{}
		choices       []*ballotInfo
		base          *ballotInfo

		votes *types.Votes
		last  = t.last.Add(1)
		err   error
	)
	if conf.current != nil {
		last = *conf.current
	}
	// goodness of the ballot determined using hare output or tortoise output for old layers.
	// if tortoise is full mode some ballot in old layer is undecided and we can't use it this optimization.
	if t.mode.isVerifying() {
		base = t.getGoodBallot(logger)
		if base != nil {
			// we need only 1 ballot from the most recent layer, this ballot will be by definition the most
			// consistent with our local opinion.
			// then we just need to encode our local opinion from layer of the ballot up to last processed as votes
			votes, err = t.encodeVotes(ctx, base, base.layer, last)
			if err != nil {
				logger.With().Error("failed to encode votes for good ballot", log.Err(err))
			}
		}
	}
	if votes == nil {
		for lid := t.evicted.Add(1); !lid.After(t.processed); lid = lid.Add(1) {
			for _, ballot := range t.ballots[lid] {
				if ballot.weight.IsNil() {
					continue
				}
				dis, err := t.firstDisagreement(ctx, last, ballot, disagreements)
				if err != nil {
					logger.With().Error("failed to compute first disagreement", ballot.id, log.Err(err))
					continue
				}
				disagreements[ballot.id] = dis
				choices = append(choices, ballot)
			}
		}

		prioritizeBallots(choices, disagreements)
		for _, base = range choices {
			votes, err = t.encodeVotes(ctx, base, t.evicted.Add(1), last)
			if err == nil {
				break
			}
			logger.With().Warning("error calculating vote exceptions for ballot",
				base.id,
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
		log.Stringer("base layer", base.layer),
		log.Stringer("voting layer", last),
		log.Inline(votes),
	)

	metrics.LayerDistanceToBaseBallot.WithLabelValues().Observe(float64(t.last.Value - base.layer.Value))

	return votes, nil
}

func (t *turtle) getGoodBallot(logger log.Log) *ballotInfo {
	var choices []*ballotInfo
	for lid := t.processed; lid.After(t.evicted); lid = lid.Sub(1) {
		for _, ballot := range t.ballots[lid] {
			if ballot.weight.IsNil() {
				continue
			}
			if ballot.good() {
				choices = append(choices, ballot)
			}
		}
		if len(choices) > 0 {
			sort.Slice(choices, func(i, j int) bool {
				return choices[i].id.Compare(choices[j].id)
			})
			return choices[0]
		}
	}
	return nil
}

// firstDisagreement returns first layer where local opinion is different from ballot's opinion within sliding window.
func (t *turtle) firstDisagreement(ctx context.Context, last types.LayerID, ballot *ballotInfo, disagreements map[types.BallotID]types.LayerID) (types.LayerID, error) {
	// using it as a mark that the votes for block are completely consistent
	// with a local opinion. so if two blocks have consistent histories select block
	// from a higher layer as it is more consistent.
	consistent := ballot.layer
	if basedis, exists := disagreements[ballot.base.id]; exists && basedis != ballot.base.layer {
		return basedis, nil
	}

	for lvote := ballot.votes.tail; lvote != nil; lvote = lvote.prev {
		if lvote.lid.Before(ballot.base.layer) {
			break
		}
		if lvote.vote == abstain && (lvote.hareTerminated || !withinDistance(t.Zdist, lvote.lid, last)) {
			t.logger.With().Debug("ballot votes abstain on a terminated layer. can't use as a base ballot",
				ballot.id,
				lvote.lid,
			)
			return types.LayerID{}, nil
		}
		for _, bvote := range lvote.blocks {
			vote, _, err := t.getFullVote(ctx, bvote.blockInfo)
			if err != nil {
				return types.LayerID{}, err
			}
			if vote != bvote.vote {
				t.logger.With().Debug("found disagreement on a block",
					ballot.id,
					bvote.id,
					log.Stringer("block_layer", lvote.lid),
					log.Stringer("ballot_layer", ballot.layer),
					log.Stringer("local_vote", vote),
					log.Stringer("vote", bvote.vote),
				)
				return lvote.lid, nil
			}
		}
	}
	return consistent, nil
}

// encode differences between selected base ballot and local votes.
func (t *turtle) encodeVotes(
	ctx context.Context,
	base *ballotInfo,
	start types.LayerID,
	last types.LayerID,
) (*types.Votes, error) {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Stringer("base layer", base.layer),
		log.Stringer("voting layer", last),
	)
	votes := &types.Votes{
		Base: base.id,
	}
	// encode difference with local opinion between [start, base.layer)
	for lvote := base.votes.tail; lvote != nil; lvote = lvote.prev {
		if lvote.lid.Before(start) {
			break
		}
		if lvote.vote == abstain && lvote.hareTerminated {
			return nil, fmt.Errorf("ballot %s can't be used as a base ballot", base.id)
		}
		for _, bvote := range lvote.blocks {
			vote, _, err := t.getFullVote(ctx, bvote.blockInfo)
			if err != nil {
				return nil, err
			}
			// ballot vote is consistent with local opinion, exception is not necessary
			if vote == bvote.vote {
				continue
			}
			switch vote {
			case support:
				logger.With().Debug("support before base ballot", bvote.id, bvote.layer)
				votes.Support = append(votes.Support, bvote.id)
			case against:
				logger.With().Debug("explicit against overwrites base ballot opinion", bvote.id, bvote.layer)
				votes.Against = append(votes.Against, bvote.id)
			case abstain:
				logger.With().Error("layers that are not terminated should have been encoded earlier",
					bvote.id, bvote.layer,
				)
			}
		}
	}
	// encode votes after base ballot votes [base layer, last)
	for lid := base.layer; lid.Before(last); lid = lid.Add(1) {
		layer := t.layer(lid)
		if !layer.hareTerminated && withinDistance(t.Zdist, lid, last) {
			logger.With().Debug("voting abstain on the layer", lid)
			votes.Abstain = append(votes.Abstain, lid)
			continue
		}
		for _, block := range layer.blocks {
			vote, _, err := t.getFullVote(ctx, block)
			if err != nil {
				return nil, err
			}
			switch vote {
			case support:
				logger.With().Debug("support after base ballot", block.id, block.layer)
				votes.Support = append(votes.Support, block.id)
			case against:
				logger.With().Debug("implicit against after base ballot", block.id, block.layer)
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
func (t *turtle) getFullVote(ctx context.Context, block *blockInfo) (sign, voteReason, error) {
	vote, reason := getLocalVote(&t.state, t.Config, block)
	if !(vote == abstain && reason == reasonValidity) {
		return vote, reason, nil
	}
	vote = sign(block.margin.Cmp(t.localThreshold))
	if vote != abstain {
		return vote, reasonLocalThreshold, nil
	}
	coin, err := layers.GetWeakCoin(t.cdb, t.last)
	if err != nil {
		return 0, "", fmt.Errorf("coinflip is not recorded in %s. required for vote on %s / %s",
			t.last, block.id, block.layer)
	}
	if coin {
		return support, reasonCoinflip, nil
	}
	return against, reasonCoinflip, nil
}

// tallyVotes is expected to be called
func (t *turtle) tallyVotes(ctx context.Context, lid types.LayerID) error {
	t.logger.With().Debug("on layer terminated", lid)
	defer t.evict(ctx)
	if err := t.updateLayer(t.logger, lid); err != nil {
		return err
	}
	for process := t.processed.Add(1); !process.After(lid); process = process.Add(1) {
		t.processed = process
		if err := t.loadBlocksData(process); err != nil {
			return err
		}
		if err := t.loadBallots(process); err != nil {
			return err
		}
		if err := t.processLayer(t.logger.WithContext(ctx).WithFields(process), process); err != nil {
			return err
		}
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

	t.verifying.countVotes(logger, t.ballots[lid])

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
		t.layers,
	); err != nil {
		return err
	}

	t.updateHistoricallyVerified()
	return nil
}

func (t *turtle) catchupToVerifyingInFullMode(logger log.Log, vcounted, target types.LayerID) bool {
	counted := maxLayer(t.full.counted.Add(1), target.Add(1))
	for ; !counted.After(vcounted); counted = counted.Add(1) {
		t.full.tallyVotes(logger, counted)

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
	if t.verifying.markGoodCut(logger, t.ballots[target]) {
		// TODO(dshulyak) it should be enough to start from target + 1. can't do that right now as it is expected
		// that accumulated weight has a weight of the layer that is going to be verified.
		for lid := target; !lid.After(counted); lid = lid.Add(1) {
			t.verifying.countVotes(logger, t.ballots[lid])
		}
		if t.verifying.verify(logger, target) {
			for lid := counted.Add(1); !lid.After(t.processed); lid = lid.Add(1) {
				t.verifying.countVotes(logger, t.ballots[lid])
			}
			t.switchModes(logger)
		}
	}
	return true
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
	// if they are synced from peers
	validities, err := blocks.ContextualValidity(t.cdb, lid)
	if err != nil {
		return fmt.Errorf("contextual validity %s: %w", lid, err)
	}
	for _, validity := range validities {
		s := support
		if !validity.Validity {
			s = against
		}
		block := t.blockRefs[validity.ID]
		block.validity = s
	}
	return nil
}

func (t *turtle) updateLayer(logger log.Log, lid types.LayerID) error {
	lastUpdated := t.last.Before(lid)
	if lastUpdated {
		t.last = lid
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
	if _, exist := t.state.blockRefs[block.ID()]; exist {
		return
	}
	t.logger.With().Debug("on block", log.Inline(block))
	t.state.addBlock(&blockInfo{
		id:     block.ID(),
		layer:  block.LayerIndex,
		height: block.TickHeight,
		margin: util.WeightFromUint64(0),
	})
}

func (t *turtle) onHareOutput(lid types.LayerID, bid types.BlockID) {
	if !lid.After(t.evicted) {
		return
	}
	t.logger.With().Debug("on hare output", lid, bid, log.Bool("empty", bid == types.EmptyBlockID))
	var (
		layer    = t.state.layer(lid)
		previous types.BlockID
		exists   bool
	)
	layer.hareTerminated = true
	for i := range layer.blocks {
		block := layer.blocks[i]
		if block.hare == support {
			previous = layer.blocks[i].id
			exists = true
		}
		if block.id == bid {
			block.hare = support
		} else {
			block.hare = against
		}
	}
	if exists && previous == bid {
		return
	}
	if lid.Before(t.processed) && t.mode.isVerifying() && withinDistance(t.Config.Hdist, lid, t.last) {
		t.logger.With().Info("local opinion changed within hdist",
			lid,
			log.Stringer("verified", t.verified),
			log.Stringer("previous", previous),
			log.Stringer("new", bid),
		)
		t.verifying.resetWeights()

		// if local opinion within hdist was changed about the layer
		// that was already verified we need to revert that
		t.verified = minLayer(t.verified, lid)
		for target := t.verified; !target.After(t.processed); target = target.Add(1) {
			// TODO(dshulyak) this condition can be removed together with genesis ballot
			if target.GetEpoch().IsGenesis() {
				continue
			}
			t.verifying.countVotes(t.logger, t.ballots[target])
		}
	}
}

func (t *turtle) onBallot(ballot *types.Ballot) error {
	if !ballot.LayerIndex.After(t.evicted) {
		return nil
	}
	if _, exist := t.state.ballotRefs[ballot.ID()]; exist {
		return nil
	}
	t.logger.With().Debug("on ballot", log.Inline(ballot))

	base, exists := t.state.ballotRefs[ballot.Votes.Base]
	if !exists {
		t.logger.With().Warning("base ballot not in state",
			log.Stringer("base", ballot.Votes.Base),
		)
		return nil
	}
	var (
		weight util.Weight
		height uint64
		beacon types.Beacon

		err error
	)
	if ballot.EpochData != nil {
		beacon = ballot.EpochData.Beacon
		height, err = getBallotHeight(t.cdb, ballot)
		if err != nil {
			return err
		}
	} else {
		ref, exists := t.state.ballotRefs[ballot.RefBallot]
		if !exists {
			t.logger.With().Warning("ref ballot not in state",
				log.Stringer("ref", ballot.RefBallot),
			)
			return nil
		}
		beacon = ref.beacon
		height = ref.height
	}
	if !ballot.IsMalicious() {
		weight, err = computeBallotWeight(
			t.cdb, t.referenceWeight,
			ballot, t.LayerSize, types.GetLayersPerEpoch(),
		)
		if err != nil {
			return err
		}
	} else {
		t.logger.With().Warning("malicious ballot with zeroed weight", ballot.LayerIndex, ballot.ID())
	}
	badBeacon, err := t.compareBeacons(t.logger, ballot.ID(), ballot.LayerIndex, beacon)
	if err != nil {
		return err
	}
	t.logger.With().Debug("computed weight and height for ballot",
		ballot.ID(),
		log.Stringer("weight", weight),
		log.Uint64("height", height),
	)
	binfo := &ballotInfo{
		id: ballot.ID(),
		base: baseInfo{
			id:    base.id,
			layer: base.layer,
		},
		layer:  ballot.LayerIndex,
		weight: weight,
		height: height,
		beacon: beacon,
	}
	binfo.conditions.badBeacon = badBeacon
	t.decodeExceptions(base, binfo, ballot.Votes)
	t.state.addBallot(binfo)
	return nil
}

func (t *turtle) compareBeacons(logger log.Log, bid types.BallotID, layerID types.LayerID, beacon types.Beacon) (bool, error) {
	epochBeacon, err := t.beacons.GetBeacon(layerID.GetEpoch())
	if err != nil {
		return false, err
	}
	if beacon != epochBeacon {
		logger.With().Warning("ballot has different beacon",
			layerID,
			bid,
			log.String("ballot_beacon", beacon.ShortString()),
			log.String("epoch_beacon", epochBeacon.ShortString()))
		return true, nil
	}
	return false, nil
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

func (t *turtle) decodeExceptions(base, ballot *ballotInfo, exceptions types.Votes) {
	from := base.layer
	diff := map[types.LayerID]map[types.BlockID]sign{}
	for vote, bids := range map[sign][]types.BlockID{
		support: exceptions.Support,
		against: exceptions.Against,
	} {
		for _, bid := range bids {
			block, exist := t.blockRefs[bid]
			if !exist {
				ballot.conditions.votesBeforeBase = true
				continue
			}
			if block.layer.Before(from) {
				ballot.conditions.votesBeforeBase = true
				from = block.layer
			}
			layerdiff, exist := diff[block.layer]
			if !exist {
				layerdiff = map[types.BlockID]sign{}
				diff[block.layer] = layerdiff
			}
			layerdiff[block.id] = vote
		}
	}
	for _, lid := range exceptions.Abstain {
		if lid.Before(from) {
			ballot.conditions.votesBeforeBase = true
			from = lid
		}
		_, exist := diff[lid]
		if !exist {
			diff[lid] = map[types.BlockID]sign{}
		}
	}

	// inherit opinion from the base ballot by copying votes
	ballot.votes = base.votes.update(from, diff)
	// add new opinions after the base layer
	for lid := base.layer; lid.Before(ballot.layer); lid = lid.Add(1) {
		layer := t.layer(lid)
		lvote := layerVote{
			layerInfo: layer,
			vote:      against,
		}
		layerdiff, exist := diff[lid]
		if exist && len(layerdiff) == 0 {
			lvote.vote = abstain
		}
		for _, block := range layer.blocks {
			bvote := blockVote{
				blockInfo: block,
				vote:      against,
			}
			if len(layerdiff) > 0 {
				vote, exist := layerdiff[block.id]
				if exist {
					bvote.vote = vote
				}
			}
			lvote.blocks = append(lvote.blocks, bvote)
		}
		ballot.votes.append(&lvote)
	}
}

func validateConsistency(state *state, config Config, ballot *ballotInfo) bool {
	for lvote := ballot.votes.tail; lvote != nil; lvote = lvote.prev {
		if lvote.lid.Before(ballot.base.layer) {
			return true
		}
		if lvote.vote == abstain {
			continue
		}
		for j := range lvote.blocks {
			local, _ := getLocalVote(state, config, lvote.blocks[j].blockInfo)
			vote := lvote.blocks[j].vote
			if vote != local {
				return false
			}
		}
	}
	return true
}

func withinDistance(dist uint32, lid, last types.LayerID) bool {
	genesis := types.GetEffectiveGenesis()
	limit := types.GetEffectiveGenesis()
	if last.After(genesis.Add(dist)) {
		limit = last.Sub(dist)
	}
	return !lid.Before(limit)
}

func getLocalVote(state *state, config Config, block *blockInfo) (sign, voteReason) {
	if withinDistance(config.Hdist, block.layer, state.last) {
		if block.hare != neutral {
			return block.hare, reasonHareOutput
		}
		if !withinDistance(config.Zdist, block.layer, state.last) {
			return against, reasonHareOutput
		}
		return abstain, reasonHareOutput
	}
	if block.layer.After(state.historicallyVerified) {
		return abstain, reasonValidity
	}
	return block.validity, reasonValidity
}
