package tortoise

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"time"

	"github.com/spacemeshos/fixed"
	"go.uber.org/zap"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
)

var (
	errBeaconUnavailable = errors.New("beacon unavailable")
	errDanglingBase      = errors.New("base ballot not in state")
	errEvictedBlocks     = errors.New("voted blocks were evicted")
	ErrBallotExists      = errors.New("tortoise: ballot exists")
)

type turtle struct {
	Config
	logger *zap.Logger

	*state

	// pending is a minimal layer where opinion has changed
	pending types.LayerID

	// a linked list with retriable ballots
	// the purpose is to add ballot to the state even
	// if beacon is not available locally, as tortoise
	// can't count ballots without knowing local beacon
	retriable list.List

	verifying *verifying

	isFull bool
	full   *full
}

// newTurtle creates a new verifying tortoise algorithm instance.
func newTurtle(logger *zap.Logger, config Config, atxdata *atxsdata.Data) *turtle {
	t := &turtle{
		Config: config,
		state:  newState(atxdata),
		logger: logger,
	}
	genesis := types.GetEffectiveGenesis()

	t.pending = genesis
	t.last = genesis
	t.processed = genesis
	t.evicted = genesis.Sub(1)

	t.epochs[genesis.GetEpoch()] = &epochInfo{}
	genlayer := t.layer(genesis)
	genlayer.hareTerminated = true
	t.verifying = newVerifying(config, t.state)
	t.full = newFullTortoise(config, t.state)
	t.full.counted = genesis

	gen := t.layer(genesis)
	gen.computeOpinion(t.Hdist, t.last)
	return t
}

func (t *turtle) evict() {
	if !t.verified.After(types.GetEffectiveGenesis().Add(t.Hdist)) {
		return
	}
	size := t.Config.WindowSizeLayers(t.verified)
	if t.verified.Before(size) {
		return
	}
	windowStart := t.verified - size
	t.logger.Debug("evict in memory state",
		zap.Stringer("pending", t.pending),
		zap.Stringer("from_layer", t.evicted.Add(1)),
		zap.Stringer("upto_layer", windowStart),
	)
	layersNumber.Set(float64(len(t.layers.data)))
	if !windowStart.After(t.evicted) {
		return
	}
	if t.pending != 0 && t.pending < windowStart {
		return
	}
	for lid := t.evicted.Add(1); lid.Before(windowStart); lid = lid.Add(1) {
		for _, ballot := range t.ballots[lid] {
			delete(t.ballotRefs, ballot.id)
		}

		ballotsNumber.Sub(float64(len(t.ballots[lid])))
		blocksNumber.Sub(float64(len(t.layer(lid).blocks)))
		t.layers.pop()

		delete(t.ballots, lid)
		if lid.OrdinalInEpoch() == types.GetLayersPerEpoch()-1 {
			delete(t.epochs, lid.GetEpoch())
			t.atxsdata.EvictEpoch(lid.GetEpoch())
		}
	}
	for _, ballot := range t.ballots[windowStart] {
		ballot.votes.cutBefore(windowStart)
	}
	t.evicted = windowStart.Sub(1)
	evictedLayer.Set(float64(t.evicted))
}

// EncodeVotes by choosing base ballot and explicit votes.
func (t *turtle) EncodeVotes(ctx context.Context, conf *encodeConf) (*types.Opinion, error) {
	if err := t.checkDrained(); err != nil {
		return nil, err
	}
	var (
		err error

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
				t.logger.Debug("encoded votes",
					log.ZContext(ctx),
					zap.Stringer("base ballot", base.id),
					zap.Stringer("base layer", base.layer),
					zap.Stringer("voting layer", current),
					zap.Inline(opinion),
				)
				return opinion, nil
			}
			t.logger.Debug("failed to encode votes using base ballot id",
				log.ZContext(ctx),
				zap.Stringer("ballot", base.id),
				zap.Error(err),
				zap.Stringer("current layer", current),
			)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("failed to encode votes: %w", err)
	}
	return nil, errors.New("no ballots within a sliding window")
}

// encode differences between selected base ballot and local votes.
func (t *turtle) encodeVotes(
	ctx context.Context,
	base *ballotInfo,
	start types.LayerID,
	current types.LayerID,
) (*types.Opinion, error) {
	votes := types.Votes{
		Base: base.id,
	}
	// encode difference with local opinion between [start, base.layer)
	for lvote := base.votes.tail; lvote != nil; lvote = lvote.prev {
		if lvote.lid.Before(start) {
			break
		}
		layer := t.layer(lvote.lid)
		if lvote.vote == abstain && layer.hareTerminated {
			return nil, fmt.Errorf("ballot %s can't be used as a base ballot", base.id)
		} else if lvote.vote != abstain && !layer.hareTerminated {
			t.logger.Debug("voting abstain on the layer",
				log.ZContext(ctx),
				zap.Stringer("base layer", base.layer),
				zap.Stringer("current layer", current),
				zap.Uint32("lid", lvote.lid.Uint32()),
			)
			votes.Abstain = append(votes.Abstain, lvote.lid)
			continue
		} else if lvote.vote == abstain && !layer.hareTerminated {
			// there is nothing to encode if hare didn't terminate
			// and base ballot voted abstain on the previous layer
			continue
		}
		for _, block := range layer.blocks {
			vote, reason, err := t.getFullVote(t.verified, current, block)
			if err != nil {
				return nil, err
			}
			// ballot vote is consistent with local opinion, exception is not necessary
			bvote := lvote.getVote(block)
			if vote == bvote {
				continue
			}
			switch vote {
			case support:
				t.logger.Debug("support before base ballot",
					log.ZContext(ctx),
					zap.Stringer("base layer", base.layer),
					zap.Stringer("current layer", current),
					zap.Inline(block))
				votes.Support = append(votes.Support, block.header())
			case against:
				t.logger.Debug("explicit against overwrites base ballot opinion",
					log.ZContext(ctx),
					zap.Stringer("base layer", base.layer),
					zap.Stringer("current layer", current),
					zap.Inline(block))
				votes.Against = append(votes.Against, block.header())
			case abstain:
				t.logger.Error("layers that are not terminated should have been encoded earlier",
					log.ZContext(ctx),
					zap.Stringer("base layer", base.layer),
					zap.Stringer("current layer", current),
					zap.Inline(block),
					zap.Stringer("reason", reason),
				)
			}
		}
	}
	// encode votes after base ballot votes [base layer, last)
	for lid := base.layer; lid.Before(current); lid = lid.Add(1) {
		layer := t.layer(lid)
		if !layer.hareTerminated {
			t.logger.Debug("voting abstain on the layer", zap.Uint32("lid", lid.Uint32()))
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
				t.logger.Debug("support after base ballot",
					log.ZContext(ctx),
					zap.Inline(block),
					zap.Stringer("reason", reason),
				)
				votes.Support = append(votes.Support, block.header())
			case against:
				t.logger.Debug("implicit against after base ballot",
					log.ZContext(ctx),
					zap.Inline(block),
					zap.Stringer("reason", reason),
				)
			case abstain:
				t.logger.Error("layers that are not terminated should have been encoded earlier",
					log.ZContext(ctx),
					zap.Inline(block),
					zap.Stringer("reason", reason),
				)
			}
		}
	}

	if explen := len(votes.Support) + len(votes.Against); explen > t.MaxExceptions {
		return nil, fmt.Errorf("too many exceptions (%v)", explen)
	}
	decoded, _, err := decodeVotes(t.evicted, current, base, votes)
	if err != nil {
		return nil, err
	}
	return &types.Opinion{
		Hash:  decoded.opinion(),
		Votes: votes,
	}, nil
}

// getFullVote unlike getLocalVote will vote according to the counted votes on blocks that are
// outside of hdist. If opinion is undecided according to the votes it will use coinflip recorded
// in the current layer.
func (t *turtle) getFullVote(verified, current types.LayerID, block *blockInfo) (sign, voteReason, error) {
	if !block.data {
		return against, reasonMissingData, nil
	}
	vote, reason := getLocalVote(t.Config, verified, current, block)
	if !(vote == abstain && reason == reasonValidity) {
		return vote, reason, nil
	}
	vote = crossesThreshold(block.margin, t.localThreshold)
	if vote != abstain {
		return vote, reasonLocalThreshold, nil
	}
	layer := t.layer(current.Sub(1))
	if layer.coinflip == neutral {
		return 0, "", fmt.Errorf("coinflip is not recorded in %s. required for vote on %s / %s",
			current.Sub(1), block.id, block.layer)
	}
	return layer.coinflip, reasonCoinflip, nil
}

func (t *turtle) updateLast(last types.LayerID) {
	if last.After(t.last) {
		update := t.last.GetEpoch() != last.GetEpoch()
		t.last = last
		lastLayer.Set(float64(t.last))
		if update {
			epoch := t.epoch(last.GetEpoch())
			t.localThreshold = epoch.weight.
				Div(fixed.New(localThresholdFraction)).
				Div(fixed.New64(int64(types.GetLayersPerEpoch())))
		}
	}
}

func (t *turtle) tallyVotes(last types.LayerID) {
	defer t.evict()

	t.logger.Debug("on layer", zap.Uint32("last", last.Uint32()))
	t.updateLast(last)
	if err := t.drainRetriable(); err != nil {
		return
	}
	for process := t.processed.Add(1); !process.After(t.last); process = process.Add(1) {
		if process.FirstInEpoch() {
			t.computeEpochHeight(process)
		}
		layer := t.layer(process)
		for _, block := range layer.blocks {
			t.updateRefHeight(layer, block)
		}

		// NOTE(dshulyak) i need this when running verifying tortoise not from the genesis.
		if previous := process - 1; previous > t.evicted {
			prev := t.layer(previous)
			layer.verifying.goodUncounted = layer.verifying.goodUncounted.Add(prev.verifying.goodUncounted)
			layer.prevOpinion = &prev.opinion
		}

		t.processed = process
		processedLayer.Set(float64(t.processed))

		if t.isFull {
			t.full.countDelayed(t.logger, process)
			t.full.counted = process
		}
		for _, ballot := range t.ballots[process] {
			if err := t.countBallot(ballot); err != nil {
				if errors.Is(err, errBeaconUnavailable) {
					t.retryLater(ballot)
				} else {
					panic(err)
				}
			}
		}

		opinion := layer.opinion
		layer.computeOpinion(t.Hdist, t.last)
		if opinion != layer.opinion {
			if t.pending == 0 {
				t.pending = t.last
			}
			t.pending = min(t.pending, t.last)
		}

		t.logger.Debug("initial local opinion",
			zap.Bool("hate terminated", layer.hareTerminated),
			zap.Uint32("lid", layer.lid.Uint32()),
			log.ZShortStringer("previous", opinion),
			log.ZShortStringer("opinion", layer.opinion),
			zapBlocks(layer.blocks),
		)
		// terminate layer that falls out of the zdist window and wasn't terminated
		// by any other component
		if process.After(types.LayerID(t.Zdist)) {
			terminated := process.Sub(t.Zdist)
			if terminated.After(t.evicted) && !t.layer(terminated).hareTerminated {
				t.onHareOutput(terminated, types.EmptyBlockID)
			}
		}
		if process.After(types.LayerID(t.Hdist)) {
			if process.Sub(t.Hdist).After(t.evicted) {
				t.onOpinionChange(process.Sub(t.Hdist), true)
			}
		}
	}
	t.verifyLayers()
}

func (t *turtle) switchModes() {
	t.isFull = !t.isFull
	if t.isFull {
		modeGauge.Set(1)
	} else {
		modeGauge.Set(0)
	}
	t.logger.Debug("switching tortoise mode",
		zap.Uint32("last", t.last.Uint32()),
		zap.Uint32("hdist", t.Hdist),
		zap.Stringer("processed_layer", t.processed),
		zap.Stringer("verified_layer", t.verified),
		zap.Bool("is full", t.isFull),
	)
}

func (t *turtle) countBallot(ballot *ballotInfo) error {
	if ballot.layer-1 <= t.evicted {
		return nil
	}
	bad, err := t.compareBeacons(ballot.id, ballot.layer, ballot.reference.beacon)
	if err != nil {
		return fmt.Errorf("%w: %s", errBeaconUnavailable, err.Error())
	}
	ballot.conditions.badBeacon = bad
	t.verifying.countBallot(t.logger, ballot)
	if !ballot.layer.After(t.full.counted) {
		t.full.countBallot(t.logger, ballot)
	}
	return nil
}

func (t *turtle) verifyLayers() {
	var (
		verified           = max(t.evicted, types.GetEffectiveGenesis())
		nverified, changed types.LayerID
	)
	if t.isFull {
		nverified, changed = t.runFull()
		vverified, vchanged := t.runVerifying()
		if nverified == t.processed-1 && nverified == vverified {
			t.switchModes()
			changed = min(changed, vchanged)
		}
	} else {
		nverified, changed = t.runVerifying()
		// count all votes if next layer after verified is outside hdist
		if !withinDistance(t.Hdist, nverified+1, t.last) {
			fverified, fchanged := t.runFull()
			nverified = fverified
			changed = min(changed, fchanged)
		}
	}
	for target := t.evicted.Add(1); target.Before(t.processed); target = target.Add(1) {
		if nverified < target {
			if target < t.verified {
				changed = min(changed, target)
			}
			break
		} else if target > t.verified {
			changed = min(changed, target)
		}
		verified = target
	}
	t.logger.Debug("verified layer",
		zap.Uint32("last", t.last.Uint32()),
		zap.Uint32("processed", t.last.Uint32()),
		zap.Uint32("verified", verified.Uint32()),
		zap.Uint32("changed", changed.Uint32()),
	)
	if changed != math.MaxUint32 {
		if t.pending == 0 {
			t.pending = changed
		}
		t.pending = min(t.pending, changed)
		t.onOpinionChange(changed, false)
	}
	t.verified = verified
	verifiedLayer.Set(float64(t.verified))
}

func (t *turtle) runVerifying() (verified, changed types.LayerID) {
	verified = t.evicted
	changed = math.MaxUint32
	for target := t.evicted.Add(1); target.Before(t.processed); target = target.Add(1) {
		v, c := t.verifying.verify(t.logger, target)
		if !v {
			return verified, changed
		}
		if c {
			changed = min(changed, target)
		}
		verified = target
	}
	return verified, changed
}

func (t *turtle) runFull() (verified, changed types.LayerID) {
	if !t.isFull {
		t.switchModes()
		start := max(t.full.counted.Add(1), t.evicted.Add(1))
		for counted := start; !counted.After(t.processed); counted = counted.Add(1) {
			for _, ballot := range t.ballots[counted] {
				t.full.countBallot(t.logger, ballot)
			}
			t.full.countDelayed(t.logger, counted)
			t.full.counted = counted
		}
	}
	verified = t.evicted
	changed = math.MaxUint32
	for target := t.evicted.Add(1); target.Before(t.processed); target = target.Add(1) {
		v, c := t.full.verify(t.logger, target)
		if !v {
			return verified, changed
		}
		if c {
			changed = min(changed, target)
		}
		verified = target
	}
	return verified, changed
}

func (t *turtle) computeEpochHeight(lid types.LayerID) {
	einfo := t.epoch(lid.GetEpoch())
	var heights []uint64
	t.atxsdata.IterateInEpoch(
		lid.GetEpoch(),
		func(_ types.ATXID, atx *atxsdata.ATX) {
			heights = append(heights, atx.Height)
		},
		atxsdata.NotMalicious,
	)
	einfo.height = getMedian(heights)
	t.logger.Debug(
		"computed epoch height",
		zap.Uint32("in layer", lid.GetEpoch().Uint32()),
		zap.Uint32("for epoch", lid.GetEpoch().Uint32()),
		zap.Uint64("height", einfo.height),
	)
}

func (t *turtle) onBlock(header types.BlockHeader, data, valid bool) {
	if header.LayerID <= t.evicted {
		return
	}

	t.logger.Debug("on block", zap.Inline(&header), zap.Bool("data", data), zap.Bool("valid", valid))

	// update existing state without calling t.addBlock
	if binfo := t.state.getBlock(header); binfo != nil {
		binfo.data = data
		if valid {
			binfo.validity = support
		}
		return
	}
	binfo := newBlockInfo(header)
	binfo.data = data
	if valid {
		binfo.validity = support
	}
	t.addBlock(binfo)
}

func (t *turtle) addBlock(binfo *blockInfo) {
	start := time.Now()
	t.state.addBlock(binfo)
	t.full.countForLateBlock(binfo)
	addBlockDuration.Observe(float64(time.Since(start).Nanoseconds()))
}

func (t *turtle) onHareOutput(lid types.LayerID, bid types.BlockID) {
	start := time.Now()
	if !lid.After(t.evicted) {
		return
	}
	var (
		layer    = t.state.layer(lid)
		previous types.BlockID
		exists   bool
	)
	t.logger.Debug("on hare output",
		zap.Uint32("lid", lid.Uint32()),
		zap.Stringer("block", bid),
		zap.Bool("empty", bid == types.EmptyBlockID),
		zap.Uint32("processed", t.processed.Uint32()),
		zap.Uint32("hdist", t.Hdist),
		zap.Uint32("last", t.last.Uint32()),
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
	// we do not compute opinion because opinion hashing recursive.
	// so if we didn't receive layer in order this opinion will be wrong
	// and we also need to copy previous layer opinion into layer.prevOpinion
	if !lid.After(t.processed) && withinDistance(t.Config.Hdist, lid, t.last) {
		t.logger.Debug("local opinion changed within hdist",
			zap.Uint32("lid", lid.Uint32()),
			zap.Stringer("verified", t.verified),
			zap.Stringer("previous", previous),
			zap.Stringer("new", bid),
		)
		t.onOpinionChange(lid, true)
	}
	addHareOutput.Observe(float64(time.Since(start).Nanoseconds()))
}

func (t *turtle) onOpinionChange(lid types.LayerID, early bool) {
	changed := types.LayerID(math.MaxUint32)
	for recompute := lid; !recompute.After(t.processed); recompute = recompute.Add(1) {
		layer := t.layer(recompute)
		opinion := layer.opinion
		layer.computeOpinion(t.Hdist, t.last)
		t.logger.Debug("computed local opinion",
			zap.Uint32("last", t.last.Uint32()),
			zap.Uint32("lid", layer.lid.Uint32()),
			zap.Bool("changed from previous", opinion != layer.opinion),
			log.ZShortStringer("previous", opinion),
			log.ZShortStringer("new", layer.opinion),
			log.ZShortStringer("prev layer", layer.prevOpinion),
			zapBlocks(layer.blocks),
		)
		if opinion != layer.opinion {
			changed = min(changed, recompute)
		} else if early {
			break
		}
	}
	if changed != math.MaxUint32 {
		if t.pending == 0 {
			t.pending = changed
		}
		t.pending = min(t.pending, changed)
		t.verifying.resetWeights(lid)
		for target := lid.Add(1); !target.After(t.processed); target = target.Add(1) {
			t.verifying.countVotes(t.logger, t.ballots[target])
		}
	}
}

func (t *turtle) onAtx(target types.EpochID, id types.ATXID, atx *atxsdata.ATX) {
	start := time.Now()
	epoch := t.epoch(target)
	mal := t.isMalfeasant(atx.Node)
	t.logger.Debug("on atx",
		zap.Stringer("id", id),
		zap.Uint32("epoch", uint32(target)),
		zap.Uint64("weight", atx.Weight),
		zap.Uint64("height", atx.Height),
		zap.Bool("malfeasant", mal),
	)

	if atx.Weight > math.MaxInt64 {
		t.logger.Panic("fixme: atx size is not expected to overflow int64", zap.Uint64("weight", atx.Weight))
	}
	if !mal {
		epoch.weight = epoch.weight.Add(fixed.New64(int64(atx.Weight)))
	}

	if target == t.last.GetEpoch() {
		t.localThreshold = epoch.weight.
			Div(fixed.New(localThresholdFraction)).
			Div(fixed.New64(int64(types.GetLayersPerEpoch())))
	}
	addAtxDuration.Observe(float64(time.Since(start).Nanoseconds()))
}

func (t *turtle) decodeBallot(ballot *types.BallotTortoiseData) (*ballotInfo, types.LayerID, error) {
	start := time.Now()

	if !ballot.Layer.After(t.evicted) {
		return nil, 0, nil
	}
	if info, exist := t.state.ballotRefs[ballot.ID]; exist {
		return info, 0, nil
	}

	t.logger.Debug("on ballot",
		zap.Inline(ballot),
		zap.Uint32("processed", t.processed.Uint32()),
	)

	var (
		base    *ballotInfo
		refinfo *referenceInfo
		verr    error
	)

	if ballot.Opinion.Votes.Base == types.EmptyBallotID {
		base = &ballotInfo{layer: types.GetEffectiveGenesis()}
	} else if stored := t.state.ballotRefs[ballot.Opinion.Votes.Base]; stored != nil {
		base = stored
	}
	if base == nil {
		base = &ballotInfo{layer: t.evicted}
		verr = errors.Join(verr, fmt.Errorf("%w: %s", errDanglingBase, ballot.Opinion.Base))
	} else if !base.layer.Before(ballot.Layer) {
		return nil, 0, fmt.Errorf(
			"votes for ballot (%s/%s) should be encoded with base ballot (%s/%s) from previous layers",
			ballot.Layer, ballot.ID, base.layer, base.id,
		)
	}

	switch {
	case ballot.EpochData != nil:
		atx := t.atxsdata.Get(ballot.Layer.GetEpoch(), ballot.AtxID)
		if atx == nil {
			return nil, 0, fmt.Errorf("atx %s/%d not in state", ballot.AtxID, ballot.Layer.GetEpoch())
		}
		refinfo = &referenceInfo{
			smesher:         ballot.Smesher,
			atxid:           ballot.AtxID,
			expectedBallots: ballot.EpochData.Eligibilities,
			beacon:          ballot.EpochData.Beacon,
			height:          atx.Height,
			weight:          big.NewRat(int64(atx.Weight), int64(ballot.EpochData.Eligibilities)),
		}
	case ballot.Ref != nil:
		ptr := *ballot.Ref
		ref, exists := t.state.ballotRefs[ptr]
		if !exists {
			t.logger.Warn("ref ballot not in state",
				zap.Stringer("ref", ptr),
			)
			return nil, 0, nil
		}
		if ref.reference == nil {
			return nil, 0, fmt.Errorf("ballot %s is not a reference ballot", ptr)
		}
		refinfo = ref.reference
	default:
		return nil, 0, fmt.Errorf("epoch data and pointer are nil for ballot %s", ballot.ID)
	}

	binfo := &ballotInfo{
		id:        ballot.ID,
		reference: refinfo,
		layer:     ballot.Layer,
		malicious: ballot.Malicious || t.isMalfeasant(ballot.Smesher),
	}

	if !binfo.malicious {
		binfo.weight = fixed.DivUint64(
			refinfo.weight.Num().Uint64(),
			refinfo.weight.Denom().Uint64(),
		).Mul(fixed.New(int(ballot.Eligibilities)))
	}

	t.logger.Debug("computed weight and height for ballot",
		zap.Stringer("ballot", ballot.ID),
		zap.Stringer("weight", binfo.weight),
		zap.Uint64("height", refinfo.height),
		zap.Uint32("lid", ballot.Layer.Uint32()),
	)

	layer := t.layer(binfo.layer)

	existing, exists := layer.opinions[ballot.Opinion.Hash]
	var min types.LayerID
	if exists {
		binfo.votes = existing
	} else {
		var (
			votes votes
			err   error
		)
		votes, min, err = decodeVotes(t.evicted, binfo.layer, base, ballot.Opinion.Votes)
		if err != nil {
			return nil, 0, err
		}
		binfo.votes = votes
		if min <= t.evicted {
			err := fmt.Errorf("%w: layer (%d) outside the window (evicted %d)", errEvictedBlocks, min, t.evicted)
			verr = errors.Join(verr, err)
		}
	}
	t.logger.Debug("decoded exceptions",
		zap.Stringer("block", binfo.id),
		zap.Uint32("lid", binfo.layer.Uint32()),
		zap.Stringer("opinion", binfo.opinion()),
	)
	decodeBallotDuration.Observe(float64(time.Since(start).Nanoseconds()))
	return binfo, min, verr
}

func (t *turtle) storeBallot(ballot *ballotInfo, offset types.LayerID) error {
	if !ballot.layer.After(t.evicted) {
		return nil
	}
	if _, exists := t.ballotRefs[ballot.id]; exists {
		return fmt.Errorf("%w: %s", ErrBallotExists, ballot.id)
	}

	t.state.addBallot(ballot)
	layer := t.layer(ballot.layer)
	existing, exists := layer.opinions[ballot.opinion()]
	if exists {
		ballot.votes = existing
	} else {
		for current := ballot.votes.tail; current != nil && !current.lid.Before(offset); current = current.prev {
			if current.lid <= t.evicted {
				continue
			}
			for i, block := range current.supported {
				existing := t.getBlock(block.header())
				if existing != nil {
					current.supported[i] = existing
					continue
				}
				if !withinDistance(t.Hdist, block.layer, t.last) {
					block.validity = against
					block.hare = against
				}
				t.addBlock(block)
			}
		}
		layer.opinions[ballot.opinion()] = ballot.votes
	}
	if !ballot.layer.After(t.processed) {
		if err := t.countBallot(ballot); err != nil {
			if errors.Is(err, errBeaconUnavailable) {
				t.retryLater(ballot)
			} else {
				t.logger.Panic("unexpected error in counting ballots", zap.Error(err))
			}
		}
	}
	return nil
}

func (t *turtle) onRecoveredBallot(ballot *types.BallotTortoiseData) error {
	decoded, min, err := t.decodeBallot(ballot)
	if decoded == nil || err != nil && !(errors.Is(err, errDanglingBase) || errors.Is(err, errEvictedBlocks)) {
		return err
	}
	decoded.overwriteOpinion(ballot.Opinion.Hash)
	return t.storeBallot(decoded, min)
}

func (t *turtle) onBallot(ballot *types.BallotTortoiseData) error {
	decoded, min, err := t.decodeBallot(ballot)
	if decoded == nil || err != nil {
		return err
	}
	return t.storeBallot(decoded, min)
}

func (t *turtle) compareBeacons(bid types.BallotID, lid types.LayerID, beacon types.Beacon) (bool, error) {
	epoch := t.epoch(lid.GetEpoch())
	if epoch.beacon == nil {
		return false, errBeaconUnavailable
	}
	if beacon != *epoch.beacon {
		t.logger.Debug("ballot has different beacon",
			zap.Uint32("layer_id", lid.Uint32()),
			zap.Stringer("block", bid),
			zap.Stringer("ballot_beacon", beacon),
			zap.Stringer("epoch_beacon", epoch.beacon),
		)
		return true, nil
	}
	return false, nil
}

func (t *turtle) retryLater(ballot *ballotInfo) {
	t.retriable.PushBack(ballot)
}

func (t *turtle) drainRetriable() error {
	for front := t.retriable.Front(); front != nil; {
		if err := t.countBallot(front.Value.(*ballotInfo)); err != nil {
			// if beacon is still unavailable - exit and wait for the next call
			// to drain this queue
			if errors.Is(err, errBeaconUnavailable) {
				return nil
			}
			return err
		}
		next := front.Next()
		t.retriable.Remove(front)
		front = next
	}
	return nil
}

func (t *turtle) checkDrained() error {
	if lth := t.retriable.Len(); lth != 0 {
		return fmt.Errorf("all ballots from processed layers (%d) must be counted before encoding votes", lth)
	}
	return nil
}

func withinDistance(dist uint32, lid, last types.LayerID) bool {
	// layer + distance > last
	return lid.Add(dist).After(last)
}

func getLocalVote(config Config, verified, last types.LayerID, block *blockInfo) (sign, voteReason) {
	if withinDistance(config.Hdist, block.layer, last) {
		return block.hare, reasonHareOutput
	}
	// if layer was verified, but then global threshold became unreachable we will not
	// update validity, but verified variable will be lowered
	if block.layer.After(verified) {
		return abstain, reasonValidity
	}
	return block.validity, reasonValidity
}
