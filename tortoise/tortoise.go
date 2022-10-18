package tortoise

import (
	"context"
	"errors"
	"fmt"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/proposals"
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

	state

	verifying *verifying

	isFull bool
	full   *full
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

func (t *turtle) init(ctx context.Context, genesisLayer *types.Layer) {
	// Mark the genesis layer as “good”
	genesis := genesisLayer.Index()

	t.epochs[genesis.GetEpoch()] = &epochInfo{}
	t.layers[genesis] = &layerInfo{
		lid:            genesis,
		empty:          util.WeightFromUint64(0),
		hareTerminated: true,
	}
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
		t.addBallot(binfo)
	}
	for _, block := range genesisLayer.Blocks() {
		blinfo := &blockInfo{
			id:       block.ID(),
			layer:    genesis,
			hare:     support,
			validity: support,
			margin:   util.WeightFromUint64(0),
		}
		t.layers[genesis].blocks = append(t.layers[genesis].blocks, blinfo)
		t.blockRefs[blinfo.id] = blinfo
	}
	t.last = genesis
	t.processed = genesis
	t.verified = genesis
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
		for _, ballot := range t.layer(lid).ballots {
			delete(t.ballotRefs, ballot.id)
		}
		for _, block := range t.layers[lid].blocks {
			delete(t.blockRefs, block.id)
		}
		delete(t.layers, lid)
		if lid.OrdinalInEpoch() == types.GetLayersPerEpoch()-1 {
			delete(t.epochs, lid.GetEpoch())
		}
	}
	for _, ballot := range t.layer(windowStart).ballots {
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

		votes   *types.Votes
		current = t.last.Add(1)
		err     error
	)
	if conf.current != nil {
		current = *conf.current
	}

	for lid := t.evicted.Add(1); !lid.After(t.processed); lid = lid.Add(1) {
		for _, ballot := range t.layer(lid).ballots {
			if ballot.weight.IsNil() {
				continue
			}
			dis, err := t.firstDisagreement(ctx, current, ballot, disagreements)
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
		votes, err = t.encodeVotes(ctx, base, t.evicted.Add(1), current)
		if err == nil {
			break
		}
		logger.With().Warning("error calculating vote exceptions for ballot",
			base.id,
			log.Err(err),
			log.Stringer("current layer", current),
		)
	}

	if votes == nil {
		// TODO: special error encoding when exceeding exception list size
		return nil, errNoBaseBallotFound
	}

	logger.With().Info("choose base ballot",
		log.Stringer("base layer", base.layer),
		log.Stringer("current layer", current),
		log.Inline(votes),
		log.Bool("is full", t.isFull),
	)

	metrics.LayerDistanceToBaseBallot.WithLabelValues().Observe(float64(t.last.Value - base.layer.Value))

	return votes, nil
}

// firstDisagreement returns first layer where local opinion is different from ballot's opinion within sliding window.
func (t *turtle) firstDisagreement(ctx context.Context, current types.LayerID, ballot *ballotInfo, disagreements map[types.BallotID]types.LayerID) (types.LayerID, error) {
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
		if lvote.vote == abstain && lvote.hareTerminated {
			t.logger.With().Debug("ballot votes abstain on a terminated layer. can't use as a base ballot",
				ballot.id,
				lvote.lid,
			)
			return types.LayerID{}, nil
		}
		for _, block := range lvote.blocks {
			vote, _, err := t.getFullVote(t.verified, current, block)
			if err != nil {
				return types.LayerID{}, err
			}
			if bvote := lvote.getVote(block.id); vote != bvote {
				t.logger.With().Debug("found disagreement on a block",
					ballot.id,
					block.id,
					log.Stringer("block_layer", lvote.lid),
					log.Stringer("ballot_layer", ballot.layer),
					log.Stringer("local_vote", vote),
					log.Stringer("vote", bvote),
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
	current types.LayerID,
) (*types.Votes, error) {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Stringer("base layer", base.layer),
		log.Stringer("current layer", current),
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
		if lvote.vote != abstain && !lvote.hareTerminated {
			logger.With().Debug("voting abstain on the layer", lvote.lid)
			votes.Abstain = append(votes.Abstain, lvote.lid)
			continue
		}
		for _, block := range lvote.blocks {
			vote, reason, err := t.getFullVote(t.verified, current, block)
			if err != nil {
				return nil, err
			}
			// ballot vote is consistent with local opinion, exception is not necessary
			bvote := lvote.getVote(block.id)
			if vote == bvote {
				continue
			}
			switch vote {
			case support:
				logger.With().Debug("support before base ballot", block.id, block.layer)
				votes.Support = append(votes.Support, block.id)
			case against:
				logger.With().Debug("explicit against overwrites base ballot opinion", block.id, block.layer)
				votes.Against = append(votes.Against, block.id)
			case abstain:
				logger.With().Error("layers that are not terminated should have been encoded earlier",
					block.id, block.layer, log.Stringer("reason", reason),
				)
			}
		}
	}
	// encode votes after base ballot votes [base layer, last)
	for lid := base.layer; lid.Before(current); lid = lid.Add(1) {
		layer := t.layer(lid)
		if !layer.hareTerminated {
			logger.With().Debug("voting abstain on the layer", lid)
			votes.Abstain = append(votes.Abstain, lid)
			continue
		}
		for _, block := range layer.blocks {
			vote, reason, err := t.getFullVote(t.verified, current, block)
			if err != nil {
				return nil, err
			}
			switch vote {
			case support:
				logger.With().Debug("support after base ballot", block.id, block.layer, log.Stringer("reason", reason))
				votes.Support = append(votes.Support, block.id)
			case against:
				logger.With().Debug("implicit against after base ballot", block.id, block.layer, log.Stringer("reason", reason))
			case abstain:
				logger.With().Error("layers that are not terminated should have been encoded earlier",
					block.id, lid, log.Stringer("reason", reason),
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
func (t *turtle) getFullVote(verified, current types.LayerID, block *blockInfo) (sign, voteReason, error) {
	vote, reason := getLocalVote(verified, current, t.Config, block)
	if !(vote == abstain && reason == reasonValidity) {
		return vote, reason, nil
	}
	vote = sign(block.margin.Cmp(t.localThreshold))
	if vote != abstain {
		return vote, reasonLocalThreshold, nil
	}
	coin, err := layers.GetWeakCoin(t.cdb, current.Sub(1))
	if err != nil {
		return 0, "", fmt.Errorf("coinflip is not recorded in %s. required for vote on %s / %s",
			t.last, block.id, block.layer)
	}
	if coin {
		return support, reasonCoinflip, nil
	}
	return against, reasonCoinflip, nil
}

func (t *turtle) onLayer(ctx context.Context, last types.LayerID) error {
	t.logger.With().Debug("on layer", last)
	defer t.evict(ctx)
	if last.After(t.last) {
		t.last = last
	}
	for process := t.processed.Add(1); !process.After(t.last); process = process.Add(1) {
		if process.FirstInEpoch() {
			if err := t.loadAtxs(process.GetEpoch()); err != nil {
				return err
			}
		}
		layer := t.layer(process)
		for _, block := range layer.blocks {
			if err := t.updateRefHeight(layer, block); err != nil {
				return err
			}
		}
		layer.verifying.goodUncounted = layer.verifying.goodUncounted.Add(t.layer(process.Sub(1)).verifying.goodUncounted)
		for _, ballot := range t.layer(process).ballots {
			t.countBallot(t.logger, ballot)
		}
		if t.isFull {
			t.full.countDelayed(t.logger, process)
			t.full.counted = process
		}
		t.processed = process

		if err := t.loadBlocksData(process); err != nil {
			return err
		}
		if err := t.loadBallots(process); err != nil {
			return err
		}

		// terminate layer that falls out of the zdist window and wasn't terminated
		// by any other component
		if !process.After(types.NewLayerID(t.Zdist)) {
			continue
		}
		terminated := process.Sub(t.Zdist)
		if terminated.After(t.evicted) && !t.layer(terminated).hareTerminated {
			t.onHareOutput(terminated, types.EmptyBlockID)
		}
	}
	return t.verifyLayers()
}

func (t *turtle) switchModes(logger log.Log) {
	t.isFull = !t.isFull
	logger.With().Info("switching tortoise mode",
		log.Uint32("hdist", t.Hdist),
		log.Stringer("processed_layer", t.processed),
		log.Stringer("verified_layer", t.verified),
		log.Bool("is full", t.isFull),
	)
}

func (t *turtle) countBallot(logger log.Log, ballot *ballotInfo) error {
	badBeacon, err := t.compareBeacons(t.logger, ballot.id, ballot.layer, ballot.reference.beacon)
	if err != nil {
		return err
	}
	ballot.conditions.badBeacon = badBeacon
	t.verifying.countBallot(logger, ballot)
	if t.isFull {
		t.full.countBallot(logger, ballot)
	}
	return nil
}

func (t *turtle) verifyLayers() error {
	logger := t.logger.WithFields(
		log.Stringer("last layer", t.last),
	)
	verified := t.verified
	for target := t.evicted.Add(1); target.Before(t.processed); target = target.Add(1) {
		var success bool
		if !t.isFull {
			success = t.verifying.verify(logger, target)
		}
		if !success && (t.isFull || !withinDistance(t.Hdist, target, t.last)) {
			if !t.isFull {
				t.switchModes(logger)
				for counted := maxLayer(t.full.counted.Add(1), t.evicted.Add(1)); !counted.After(t.processed); counted = counted.Add(1) {
					for _, ballot := range t.layer(counted).ballots {
						t.full.countBallot(logger, ballot)
					}
					t.full.countDelayed(logger, counted)
					t.full.counted = counted
				}
			}
			success = t.full.verify(logger, target)
		}
		if !success {
			break
		}
		verified = target
	}
	t.verified = verified
	return persistContextualValidity(
		logger, t.updater, t.evicted.Add(1), t.verified, t.layers,
	)
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
	for _, block := range t.layer(lid).blocks {
		valid, err := blocks.IsValid(t.cdb, block.id)
		if err != nil {
			if !errors.Is(err, sql.ErrNotFound) {
				return err
			}
		} else if valid {
			block.validity = support
		} else if !valid {
			block.validity = against
		}
	}
	return nil
}

// loadAtxs and compute reference height.
func (t *turtle) loadAtxs(epoch types.EpochID) error {
	var heights []uint64
	if err := t.cdb.IterateEpochATXHeaders(epoch, func(header *types.ActivationTxHeader) bool {
		t.onAtx(header)
		heights = append(heights, header.TickHeight())
		return true
	}); err != nil {
		return fmt.Errorf("computing epoch data for %d: %w", epoch, err)
	}
	einfo := t.epochs[epoch]
	einfo.height = getMedian(heights)
	t.logger.With().Info("computed height and weight for epoch",
		epoch,
		log.Uint64("weight", einfo.weight),
		log.Uint64("height", einfo.height),
	)
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
			t.logger.With().Error("failed to add ballot to the state", log.Err(err), log.Inline(ballot))
		}
	}
	return nil
}

func (t *turtle) onBlock(lid types.LayerID, block *types.Block) error {
	if !lid.After(t.evicted) {
		return nil
	}
	if _, exist := t.state.blockRefs[block.ID()]; exist {
		return nil
	}
	t.logger.With().Debug("on block", log.Inline(block))
	binfo := &blockInfo{
		id:     block.ID(),
		layer:  block.LayerIndex,
		hare:   neutral,
		height: block.TickHeight,
		margin: util.WeightFromUint64(0),
	}
	if t.layer(block.LayerIndex).hareTerminated {
		binfo.hare = against
	}
	t.addBlock(binfo)
	t.full.countForLateBlock(binfo)
	if !binfo.layer.After(t.processed) {
		if err := t.updateRefHeight(t.layer(binfo.layer), binfo); err != nil {
			return err
		}
	}
	return nil
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
	if lid.Before(t.processed) && !t.isFull && withinDistance(t.Config.Hdist, lid, t.last) {
		t.logger.With().Info("local opinion changed within hdist",
			lid,
			log.Stringer("verified", t.verified),
			log.Stringer("previous", previous),
			log.Stringer("new", bid),
		)

		t.verifying.resetWeights(lid)
		for target := lid.Add(1); !target.After(t.processed); target = target.Add(1) {
			t.verifying.countVotes(t.logger, t.layer(target).ballots)
		}
	}
}

func (t *turtle) onAtx(atx *types.ActivationTxHeader) {
	epoch, exist := t.epochs[atx.TargetEpoch()]
	if !exist {
		epoch = &epochInfo{atxs: map[types.ATXID]uint64{}}
		t.epochs[atx.TargetEpoch()] = epoch
	}
	if _, exist := epoch.atxs[atx.ID]; !exist {
		t.logger.With().Debug("on atx",
			log.Stringer("id", atx.ID),
			log.Uint32("epoch", uint32(atx.TargetEpoch())),
			log.Uint64("weight", atx.GetWeight()),
		)
		epoch.atxs[atx.ID] = atx.GetWeight()
		epoch.weight += atx.GetWeight()
	}
	if atx.TargetEpoch() == t.last.GetEpoch() {
		t.localThreshold = util.WeightFromUint64(epoch.weight).
			Fraction(localThresholdFraction).
			Div(util.WeightFromUint64(uint64(types.GetLayersPerEpoch())))
	}
}

func (t *turtle) onBallot(ballot *types.Ballot) error {
	if !ballot.LayerIndex.After(t.evicted) {
		return nil
	}
	if _, exist := t.state.ballotRefs[ballot.ID()]; exist {
		return nil
	}
	t.logger.With().Debug("on ballot",
		log.Inline(ballot),
		log.Uint32("processed", t.processed.Value),
	)

	base, exists := t.state.ballotRefs[ballot.Votes.Base]
	if !exists {
		t.logger.With().Warning("base ballot not in state",
			log.Stringer("base", ballot.Votes.Base),
		)
		return nil
	}
	var (
		weight  util.Weight
		refinfo *referenceInfo
	)
	if ballot.EpochData != nil {
		beacon := ballot.EpochData.Beacon
		height, err := getBallotHeight(t.cdb, ballot)
		if err != nil {
			return err
		}
		refweight, err := proposals.ComputeWeightPerEligibility(t.cdb, ballot, t.LayerSize, types.GetLayersPerEpoch())
		if err != nil {
			return err
		}
		refinfo = &referenceInfo{
			height: height,
			beacon: beacon,
			weight: refweight,
		}
	} else {
		ref, exists := t.state.ballotRefs[ballot.RefBallot]
		if !exists {
			t.logger.With().Warning("ref ballot not in state",
				log.Stringer("ref", ballot.RefBallot),
			)
			return nil
		}
		if ref.reference == nil {
			t.logger.With().Warning("invalid ballot used as a reference",
				log.Stringer("ref", ballot.RefBallot),
			)
			return nil
		}
		refinfo = ref.reference
	}
	if !ballot.IsMalicious() {
		weight = refinfo.weight.Copy().Mul(
			util.WeightFromUint64(uint64(len(ballot.EligibilityProofs))))
	} else {
		t.logger.With().Warning("malicious ballot with zeroed weight", ballot.LayerIndex, ballot.ID())
	}
	t.logger.With().Debug("computed weight and height for ballot",
		ballot.ID(),
		log.Stringer("weight", weight),
		log.Uint64("height", refinfo.height),
		log.Uint32("lid", ballot.LayerIndex.Value),
	)
	binfo := &ballotInfo{
		id: ballot.ID(),
		base: baseInfo{
			id:    base.id,
			layer: base.layer,
		},
		reference: refinfo,
		layer:     ballot.LayerIndex,
		weight:    weight,
	}
	t.decodeExceptions(base, binfo, ballot.Votes)
	t.logger.With().Debug("decoded exceptions", binfo.id, binfo.layer, log.Stringer("opinion", binfo.opinion()))
	if !binfo.layer.After(t.processed) {
		if err := t.countBallot(t.logger, binfo); err != nil {
			return err
		}
	}
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
		} else if exist && len(layerdiff) > 0 {
			for _, block := range layer.blocks {
				vote, exist := layerdiff[block.id]
				if exist && vote == support {
					lvote.supported = append(lvote.supported, block)
				}
			}
		}
		ballot.votes.append(&lvote)
	}
}

func validateConsistency(state *state, config Config, ballot *ballotInfo) bool {
	for lvote := ballot.votes.tail; lvote != nil; lvote = lvote.prev {
		if lvote.lid.Before(ballot.base.layer) {
			return true
		}
		// local opinion is undecided yet. tortoise will revisit consistency
		// after hare is terminated or zdist passes.
		if !lvote.hareTerminated {
			return false
		}
		if lvote.vote == abstain {
			continue
		}
		for _, block := range lvote.blocks {
			local, _ := getLocalVote(state.verified, state.last, config, block)
			if lvote.getVote(block.id) != local {
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

func getLocalVote(verified, last types.LayerID, config Config, block *blockInfo) (sign, voteReason) {
	if withinDistance(config.Hdist, block.layer, last) {
		return block.hare, reasonHareOutput
	}
	if block.layer.After(verified) {
		return abstain, reasonValidity
	}
	return block.validity, reasonValidity
}
