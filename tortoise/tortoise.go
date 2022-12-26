package tortoise

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/log"
	putil "github.com/spacemeshos/go-spacemesh/proposals/util"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/system"
	"github.com/spacemeshos/go-spacemesh/tortoise/metrics"
)

type turtle struct {
	Config
	logger log.Log
	cdb    *datastore.CachedDB

	beacons system.BeaconGetter
	updated []types.BlockContextualValidity

	*state

	verifying *verifying

	isFull bool
	full   *full
}

// newTurtle creates a new verifying tortoise algorithm instance.
func newTurtle(
	logger log.Log,
	cdb *datastore.CachedDB,
	beacons system.BeaconGetter,
	config Config,
) *turtle {
	t := &turtle{
		Config:  config,
		state:   newState(),
		logger:  logger,
		cdb:     cdb,
		beacons: beacons,
	}
	genesis := types.GetEffectiveGenesis()

	t.last = genesis
	t.processed = genesis
	t.verified = genesis
	t.evicted = genesis.Sub(1)

	t.epochs[genesis.GetEpoch()] = &epochInfo{}
	t.layers[genesis] = &layerInfo{
		lid:            genesis,
		hareTerminated: true,
	}
	t.verifying = newVerifying(config, t.state)
	t.full = newFullTortoise(config, t.state)
	t.full.counted = genesis

	gen := t.layer(genesis)
	gen.computeOpinion(t.Hdist, t.last)
	return t
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
		for _, ballot := range t.ballots[lid] {
			ballotsNumber.Dec()
			delete(t.ballotRefs, ballot.id)
		}
		for _, block := range t.layers[lid].blocks {
			blocksNumber.Dec()
			delete(t.blockRefs, block.id)
		}
		layersNumber.Dec()
		delete(t.layers, lid)
		delete(t.ballots, lid)
		if lid.OrdinalInEpoch() == types.GetLayersPerEpoch()-1 {
			layersNumber.Dec()
			delete(t.epochs, lid.GetEpoch())
		}
	}
	for _, ballot := range t.ballots[windowStart] {
		ballot.votes.cutBefore(windowStart)
	}
	t.evicted = windowStart.Sub(1)
	evictedLayer.Set(float64(t.evicted.Value))
}

// EncodeVotes by choosing base ballot and explicit votes.
func (t *turtle) EncodeVotes(ctx context.Context, conf *encodeConf) (*types.Opinion, error) {
	var (
		logger = t.logger.WithContext(ctx)
		err    error

		current = t.last.Add(1)
	)
	if conf.current != nil {
		current = *conf.current
	}
	for lid := current.Sub(1); lid.After(t.evicted); lid = lid.Sub(1) {
		var choices []*ballotInfo
		if lid == types.GetEffectiveGenesis() {
			choices = []*ballotInfo{{layer: types.GetEffectiveGenesis()}}
		} else {
			choices = t.ballots[lid]
		}
		for _, base := range choices {
			if base.malicious {
				// skip them as they are candidates for pruning
				continue
			}
			var opinion *types.Opinion
			opinion, err = t.encodeVotes(ctx, base, t.evicted.Add(1), current)
			if err == nil {
				metrics.LayerDistanceToBaseBallot.WithLabelValues().Observe(float64(t.last.Value - base.layer.Value))
				logger.With().Info("encoded votes",
					log.Stringer("base ballot", base.id),
					log.Stringer("base layer", base.layer),
					log.Stringer("voting layer", current),
					log.Inline(opinion),
				)
				return opinion, nil
			}
			logger.With().Debug("failed to encode votes using base ballot id",
				base.id,
				log.Err(err),
				log.Stringer("current layer", current),
			)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to encode votes: %w", err)
	}
	return nil, fmt.Errorf("no ballots within a sliding window")
}

// encode differences between selected base ballot and local votes.
func (t *turtle) encodeVotes(
	ctx context.Context,
	base *ballotInfo,
	start types.LayerID,
	current types.LayerID,
) (*types.Opinion, error) {
	logger := t.logger.WithContext(ctx).WithFields(
		log.Stringer("base layer", base.layer),
		log.Stringer("current layer", current),
	)
	votes := types.Votes{
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
				votes.Support = append(votes.Support, types.Vote{
					ID:      block.id,
					LayerID: block.layer,
					Height:  block.height,
				})
			case against:
				logger.With().Debug("explicit against overwrites base ballot opinion", block.id, block.layer)
				votes.Against = append(votes.Against, types.Vote{
					ID:      block.id,
					LayerID: block.layer,
					Height:  block.height,
				})
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
				votes.Support = append(votes.Support, types.Vote{
					ID:      block.id,
					LayerID: block.layer,
					Height:  block.height,
				})
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
		return nil, fmt.Errorf("too many exceptions (%v)", explen)
	}
	decoded, err := t.decodeExceptions(current, base, votes)
	if err != nil {
		return nil, err
	}
	return &types.Opinion{
		Hash:  decoded.opinion(),
		Votes: votes,
	}, nil
}

// getFullVote unlike getLocalVote will vote according to the counted votes on blocks that are
// outside of hdist. if opinion is undecided according to the votes it will use coinflip recorded
// in the current layer.
func (t *turtle) getFullVote(verified, current types.LayerID, block *blockInfo) (sign, voteReason, error) {
	vote, reason := getLocalVote(t.Config, verified, current, block)
	if !(vote == abstain && reason == reasonValidity) {
		return vote, reason, nil
	}
	vote = crossesThreshold(block.margin, t.localThreshold)
	if vote != abstain {
		return vote, reasonLocalThreshold, nil
	}
	coin, err := layers.GetWeakCoin(t.cdb, current.Sub(1))
	if err != nil {
		return 0, "", fmt.Errorf("coinflip is not recorded in %s. required for vote on %s / %s",
			current.Sub(1), block.id, block.layer)
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
		lastLayer.Set(float64(t.last.Value))
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
		prev := t.layer(process.Sub(1))
		layer.verifying.goodUncounted = layer.verifying.goodUncounted.Add(prev.verifying.goodUncounted)
		t.processed = process
		processedLayer.Set(float64(t.processed.Value))

		if t.isFull {
			t.full.countDelayed(t.logger, process)
			t.full.counted = process
		}
		for _, ballot := range t.ballots[process] {
			t.countBallot(t.logger, ballot)
		}

		if err := t.loadBlocksData(process); err != nil {
			return err
		}
		if err := t.loadBallots(process); err != nil {
			return err
		}

		layer.prevOpinion = &prev.opinion
		layer.computeOpinion(t.Hdist, t.last)
		t.logger.With().Debug("initial local opinion",
			layer.lid,
			log.Stringer("local opinion", layer.opinion))

		// terminate layer that falls out of the zdist window and wasn't terminated
		// by any other component
		if process.After(types.NewLayerID(t.Zdist)) {
			terminated := process.Sub(t.Zdist)
			if terminated.After(t.evicted) && !t.layer(terminated).hareTerminated {
				t.onHareOutput(terminated, types.EmptyBlockID)
			}
		}
	}
	return t.verifyLayers()
}

func (t *turtle) switchModes(logger log.Log) {
	t.isFull = !t.isFull
	if t.isFull {
		modeGauge.Set(1)
	} else {
		modeGauge.Set(0)
	}
	logger.With().Debug("switching tortoise mode",
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
	if !ballot.layer.After(t.full.counted) {
		t.full.countBallot(logger, ballot)
	}
	return nil
}

func (t *turtle) verifyLayers() error {
	var (
		logger = t.logger.WithFields(
			log.Stringer("last layer", t.last),
		)
		verified = maxLayer(t.evicted, types.GetEffectiveGenesis())
	)

	if t.changedOpinion.min.Value != 0 && !withinDistance(t.Hdist, t.changedOpinion.max, t.last) {
		logger.With().Debug("changed opinion outside hdist", log.Stringer("from", t.changedOpinion.min), log.Stringer("to", t.changedOpinion.max))
		t.onOpinionChange(t.changedOpinion.min)
		t.changedOpinion.min = types.LayerID{}
		t.changedOpinion.max = types.LayerID{}
	}

	for target := t.evicted.Add(1); target.Before(t.processed); target = target.Add(1) {
		success := t.verifying.verify(logger, target)
		if success && t.isFull {
			t.switchModes(logger)
		}
		if !success && (t.isFull || !withinDistance(t.Hdist, target, t.last)) {
			if !t.isFull {
				t.switchModes(logger)
				for counted := maxLayer(t.full.counted.Add(1), t.evicted.Add(1)); !counted.After(t.processed); counted = counted.Add(1) {
					for _, ballot := range t.ballots[counted] {
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
		for _, block := range t.layers[target].blocks {
			if block.emitted == block.validity {
				continue
			}
			// record range of layers where opinion has changed.
			// once those layers fall out of hdist window - opinion can be recomputed
			if block.validity != block.hare || (block.emitted != block.validity && block.emitted != abstain) {
				if target.After(t.changedOpinion.max) {
					t.changedOpinion.max = target
				}
				if t.changedOpinion.min.Value == 0 || target.Before(t.changedOpinion.min) {
					t.changedOpinion.min = target
				}
			}
			if block.validity == abstain {
				logger.With().Fatal("bug: layer should not be verified if there is an undecided block", target, block.id)
			}
			logger.With().Debug("update validity", block.layer, block.id,
				log.Stringer("validity", block.validity),
				log.Stringer("hare", block.hare),
				log.Stringer("emitted", block.emitted),
			)
			if t.updated == nil {
				t.updated = []types.BlockContextualValidity{}
			}
			t.updated = append(t.updated, types.BlockContextualValidity{
				ID:       block.id,
				Layer:    target,
				Validity: block.validity == support,
			})
			block.emitted = block.validity
		}
	}
	t.verified = verified
	verifiedLayer.Set(float64(t.verified.Value))
	return nil
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
	output, err := certificates.GetHareOutput(t.cdb, lid)
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
		} else {
			block.validity = against
		}
	}
	return nil
}

// loadAtxs and compute reference height.
func (t *turtle) loadAtxs(epoch types.EpochID) error {
	var heights []uint64
	_, ids, err := t.cdb.GetEpochWeight(epoch)
	if err != nil {
		return fmt.Errorf("get epoch %d weight: %w", epoch, err)
	}
	for _, id := range ids {
		atx, err := t.cdb.GetFullAtx(id)
		if err != nil {
			return fmt.Errorf("get atx %v: %w", id, err)
		}
		t.onAtx(atx)
		heights = append(heights, atx.TickHeight())
	}
	einfo := t.epoch(epoch)
	einfo.height = getMedian(heights)
	t.logger.With().Info("computed height and weight for epoch",
		epoch,
		log.Stringer("weight", einfo.weight),
		log.Uint64("height", einfo.height),
	)
	return nil
}

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
	start := time.Now()
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
	}
	if t.layer(block.LayerIndex).hareTerminated {
		binfo.hare = against
	}
	t.addBlock(binfo)
	addBlockDuration.Observe(float64(time.Since(start).Nanoseconds()))
	t.full.countForLateBlock(binfo)
	if !binfo.layer.After(t.processed) {
		if err := t.updateRefHeight(t.layer(binfo.layer), binfo); err != nil {
			return err
		}
	}
	return nil
}

func (t *turtle) onHareOutput(lid types.LayerID, bid types.BlockID) {
	start := time.Now()
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
	if !lid.After(t.processed) && withinDistance(t.Config.Hdist, lid, t.last) {
		t.logger.With().Info("local opinion changed within hdist",
			lid,
			log.Stringer("verified", t.verified),
			log.Stringer("previous", previous),
			log.Stringer("new", bid),
		)
		t.onOpinionChange(lid)
	}
	addHareOutput.Observe(float64(time.Since(start).Nanoseconds()))
}

func (t *turtle) onOpinionChange(lid types.LayerID) {
	for recompute := lid; !recompute.After(t.processed); recompute = recompute.Add(1) {
		layer := t.layer(recompute)
		layer.computeOpinion(t.Hdist, t.last)
		t.logger.With().Debug("computed local opinion",
			layer.lid,
			log.Stringer("local opinion", layer.opinion))
	}
	t.verifying.resetWeights(lid)
	for target := lid.Add(1); !target.After(t.processed); target = target.Add(1) {
		t.verifying.countVotes(t.logger, t.ballots[target])
	}
}

func (t *turtle) onAtx(atx *types.VerifiedActivationTx) {
	start := time.Now()
	epoch := t.epoch(atx.TargetEpoch())
	if _, exist := epoch.atxs[atx.ID()]; !exist {
		t.logger.With().Debug("on atx",
			log.Stringer("id", atx.ID()),
			log.Uint32("epoch", uint32(atx.TargetEpoch())),
			log.Uint64("weight", atx.GetWeight()),
		)
		epoch.atxs[atx.ID()] = atx.GetWeight()
		if atx.GetWeight() > math.MaxInt64 {
			// atx weight is not expected to overflow int64
			t.logger.With().Fatal("fixme: atx size overflows int64", log.Uint64("weight", atx.GetWeight()))
		}
		epoch.weight = epoch.weight.Add(fixed.New64(int64(atx.GetWeight())))
	}
	if atx.TargetEpoch() == t.last.GetEpoch() {
		t.localThreshold = epoch.weight.
			Div(fixed.New(localThresholdFraction)).
			Div(fixed.New64(int64(types.GetLayersPerEpoch())))
	}
	addAtxDuration.Observe(float64(time.Since(start).Nanoseconds()))
}

func (t *turtle) decodeBallot(ballot *types.Ballot) (*ballotInfo, error) {
	start := time.Now()

	if !ballot.LayerIndex.After(t.evicted) {
		return nil, nil
	}
	if _, exist := t.state.ballotRefs[ballot.ID()]; exist {
		return nil, nil
	}

	t.logger.With().Debug("on ballot",
		log.Inline(ballot),
		log.Uint32("processed", t.processed.Value),
	)

	var (
		base    *ballotInfo
		refinfo *referenceInfo
	)

	if ballot.Votes.Base == types.EmptyBallotID {
		base = &ballotInfo{layer: types.GetEffectiveGenesis()}
	} else {
		base = t.state.ballotRefs[ballot.Votes.Base]
		if base == nil {
			t.logger.With().Warning("base ballot not in state",
				log.Stringer("base", ballot.Votes.Base),
			)
			return nil, nil
		}
	}
	if !base.layer.Before(ballot.LayerIndex) {
		return nil, fmt.Errorf("votes for ballot (%s/%s) should be encoded with base ballot (%s/%s) from previous layers",
			ballot.LayerIndex, ballot.ID(), base.layer, base.id)
	}

	if ballot.EpochData != nil {
		beacon := ballot.EpochData.Beacon
		height, err := getBallotHeight(t.cdb, ballot)
		if err != nil {
			return nil, err
		}
		refweight, err := putil.ComputeWeightPerEligibility(t.cdb, ballot, t.LayerSize, types.GetLayersPerEpoch())
		if err != nil {
			return nil, err
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
			return nil, nil
		}
		if ref.reference == nil {
			return nil, fmt.Errorf("invalid ballot use as a reference %s", ballot.RefBallot)
		}
		refinfo = ref.reference
	}

	binfo := &ballotInfo{
		id: ballot.ID(),
		base: baseInfo{
			id:    base.id,
			layer: base.layer,
		},
		reference: refinfo,
		layer:     ballot.LayerIndex,
	}

	if !ballot.IsMalicious() {
		binfo.weight = fixed.DivUint64(
			refinfo.weight.Num().Uint64(),
			refinfo.weight.Denom().Uint64(),
		).Mul(fixed.New(len(ballot.EligibilityProofs)))
	} else {
		binfo.malicious = true
		t.logger.With().Warning("malicious ballot with zeroed weight", ballot.LayerIndex, ballot.ID())
	}

	t.logger.With().Debug("computed weight and height for ballot",
		ballot.ID(),
		log.Stringer("weight", binfo.weight),
		log.Uint64("height", refinfo.height),
		log.Uint32("lid", ballot.LayerIndex.Value),
	)

	var err error
	binfo.votes, err = t.decodeExceptions(binfo.layer, base, ballot.Votes)
	if err != nil {
		return nil, err
	}
	t.logger.With().Debug("decoded exceptions",
		binfo.id, binfo.layer,
		log.Stringer("opinion", binfo.opinion()),
	)
	decodeBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	return binfo, nil
}

func (t *turtle) storeBallot(ballot *ballotInfo) error {
	if !ballot.layer.After(t.evicted) {
		return nil
	}
	if !ballot.layer.After(t.processed) {
		if err := t.countBallot(t.logger, ballot); err != nil {
			return err
		}
	}
	t.state.addBallot(ballot)
	return nil
}

func (t *turtle) onBallot(ballot *types.Ballot) error {
	decoded, err := t.decodeBallot(ballot)
	if decoded == nil || err != nil {
		return err
	}
	return t.storeBallot(decoded)
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

func (t *turtle) decodeExceptions(blid types.LayerID, base *ballotInfo, exceptions types.Votes) (votes, error) {
	from := base.layer
	diff := map[types.LayerID]map[types.BlockID]sign{}
	for _, svote := range exceptions.Support {
		block, exist := t.blockRefs[svote.ID]
		if !exist {
			return votes{}, fmt.Errorf("block %s not in state", svote.ID)
		}
		if block.layer.Before(from) {
			from = block.layer
		}
		layerdiff, exist := diff[block.layer]
		if !exist {
			layerdiff = map[types.BlockID]sign{}
			diff[block.layer] = layerdiff
		}
		layerdiff[block.id] = support
	}
	for _, avote := range exceptions.Against {
		block, exist := t.blockRefs[avote.ID]
		if !exist {
			return votes{}, fmt.Errorf("block %s not in state", avote.ID)
		}
		if block.layer.Before(from) {
			from = block.layer
		}
		layerdiff, exist := diff[block.layer]
		if !exist {
			layerdiff = map[types.BlockID]sign{}
			diff[block.layer] = layerdiff
		}
		layerdiff[block.id] = against
	}
	for _, lid := range exceptions.Abstain {
		if lid.Before(from) {
			from = lid
		}
		_, exist := diff[lid]
		if !exist {
			diff[lid] = map[types.BlockID]sign{}
		}
	}

	// inherit opinion from the base ballot by copying votes
	decoded := base.votes.update(from, diff)
	// add new opinions after the base layer
	for lid := base.layer; lid.Before(blid); lid = lid.Add(1) {
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
		decoded.append(&lvote)
	}
	return decoded, nil
}

func withinDistance(dist uint32, lid, last types.LayerID) bool {
	// layer + distance > last
	return lid.Add(dist).After(last)
}

func getLocalVote(config Config, verified, last types.LayerID, block *blockInfo) (sign, voteReason) {
	if withinDistance(config.Hdist, block.layer, last) {
		return block.hare, reasonHareOutput
	}
	if block.layer.After(verified) {
		return abstain, reasonValidity
	}
	return block.validity, reasonValidity
}
