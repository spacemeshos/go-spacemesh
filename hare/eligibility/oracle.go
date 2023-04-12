package eligibility

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	"github.com/spacemeshos/fixed"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql/atxs"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	// HarePreRound ...
	HarePreRound uint32 = math.MaxUint32
	// CertifyRound is not part of the hare protocol, but it shares the same oracle for eligibility.
	CertifyRound uint32 = math.MaxUint32 >> 1
)

const (
	// HareStatusRound ...
	HareStatusRound uint32 = iota
	// HareProposalRound ...
	HareProposalRound
	// HareCommitRound ...
	HareCommitRound
	// HareNotifyRound ...
	HareNotifyRound
)

const (
	activesCacheSize = 5          // we don't expect to handle more than two layers concurrently
	maxSupportedN    = 1073741824 // higher values result in an overflow
	// TODO(mafa): why (MaxUint32 + 1)/4?
)

var (
	errZeroCommitteeSize = errors.New("zero committee size")
	errEmptyActiveSet    = errors.New("empty active set")
	errZeroTotalWeight   = errors.New("zero total weight")
)

// Oracle is the hare eligibility oracle.
type Oracle struct {
	lock           sync.Mutex
	beacons        system.BeaconGetter
	cdb            *datastore.CachedDB
	vrfSigner      *signing.VRFSigner
	vrfVerifier    vrfVerifier
	nonceFetcher   nonceFetcher
	layersPerEpoch uint32
	activesCache   cache
	cfg            config.Config
	log.Log
}

// Returns a range of safe layers that should be used to construct the Hare active set for a given target layer
// Safe layer is a layer prior to the input layer on which w.h.p. we have agreement (i.e., on its contextually valid
// blocks), defined to be confidence param number of layers prior to the input layer.
func safeLayerRange(targetLayer types.LayerID, safetyParam, layersPerEpoch, epochOffset uint32) (safeLayerStart, safeLayerEnd types.LayerID) {
	// prevent overflow
	if targetLayer.Uint32() <= safetyParam {
		return types.GetEffectiveGenesis(), types.GetEffectiveGenesis()
	}
	safeLayer := targetLayer.Sub(safetyParam)
	safeEpoch := safeLayer.GetEpoch()
	safeLayerStart = safeEpoch.FirstLayer()
	safeLayerEnd = safeLayerStart.Add(epochOffset)

	// If the safe layer is in the first epochOffset layers of an epoch,
	// return a range from the beginning of the previous epoch
	if safeLayer.Before(safeLayerEnd) {
		// prevent overflow
		if safeLayerStart.Uint32() <= layersPerEpoch {
			return types.GetEffectiveGenesis(), types.GetEffectiveGenesis()
		}
		safeLayerStart = safeLayerStart.Sub(layersPerEpoch)
		safeLayerEnd = safeLayerEnd.Sub(layersPerEpoch)
	}

	// If any portion of the range is in the genesis layers, just return the effective genesis
	if !safeLayerStart.After(types.GetEffectiveGenesis()) {
		return types.GetEffectiveGenesis(), types.GetEffectiveGenesis()
	}

	return
}

type defaultFetcher struct {
	cdb *datastore.CachedDB
}

func (f defaultFetcher) VRFNonce(nodeID types.NodeID, epoch types.EpochID) (types.VRFPostIndex, error) {
	nonce, err := f.cdb.VRFNonce(nodeID, epoch)
	if err != nil {
		return types.VRFPostIndex(0), fmt.Errorf("get vrf nonce: %w", err)
	}
	return nonce, nil
}

// Opt for configuring Validator.
type Opt func(h *Oracle)

func withNonceFetcher(nf nonceFetcher) Opt {
	return func(h *Oracle) {
		h.nonceFetcher = nf
	}
}

// New returns a new eligibility oracle instance.
func New(
	beacons system.BeaconGetter,
	db *datastore.CachedDB,
	vrfVerifier vrfVerifier,
	vrfSigner *signing.VRFSigner,
	layersPerEpoch uint32,
	cfg config.Config,
	logger log.Log,
	opts ...Opt,
) *Oracle {
	ac, err := lru.New(activesCacheSize)
	if err != nil {
		logger.With().Fatal("failed to create lru cache for active set", log.Err(err))
	}

	o := &Oracle{
		beacons:        beacons,
		cdb:            db,
		vrfVerifier:    vrfVerifier,
		vrfSigner:      vrfSigner,
		layersPerEpoch: layersPerEpoch,
		activesCache:   ac,
		cfg:            cfg,
		Log:            logger,
	}
	for _, opt := range opts {
		opt(o)
	}

	if o.nonceFetcher == nil {
		o.nonceFetcher = defaultFetcher{cdb: db}
	}

	return o
}

//go:generate scalegen -types VrfMessage

// VrfMessage is a verification message.
type VrfMessage struct {
	Type   types.EligibilityType
	Nonce  types.VRFPostIndex
	Beacon types.Beacon
	Round  uint32
	Layer  types.LayerID
}

// buildVRFMessage builds the VRF message used as input for the BLS (msg=Beacon##Layer##Round).
func (o *Oracle) buildVRFMessage(ctx context.Context, nonce types.VRFPostIndex, layer types.LayerID, round uint32) ([]byte, error) {
	beacon, err := o.beacons.GetBeacon(layer.GetEpoch())
	if err != nil {
		return nil, fmt.Errorf("get beacon: %w", err)
	}

	msg := VrfMessage{Type: types.EligibilityHare, Nonce: nonce, Beacon: beacon, Round: round, Layer: layer}
	buf, err := codec.Encode(&msg)
	if err != nil {
		o.WithContext(ctx).With().Fatal("failed to encode", log.Err(err))
	}
	return buf, nil
}

func (o *Oracle) totalWeight(ctx context.Context, layer types.LayerID) (uint64, error) {
	actives, err := o.actives(ctx, layer)
	if err != nil {
		return 0, err
	}

	var totalWeight uint64
	for _, w := range actives {
		totalWeight += w
	}
	return totalWeight, nil
}

func (o *Oracle) minerWeight(ctx context.Context, layer types.LayerID, id types.NodeID) (uint64, error) {
	actives, err := o.actives(ctx, layer)
	if err != nil {
		return 0, err
	}

	w, ok := actives[id]
	if !ok {
		o.With().Debug("miner is not active in specified layer",
			log.Int("active_set_size", len(actives)),
			log.String("actives", fmt.Sprintf("%v", actives)),
			layer, log.Stringer("id.Key", id),
		)
		return 0, errors.New("miner is not active in specified layer")
	}
	return w, nil
}

func calcVrfFrac(vrfSig types.VrfSignature) fixed.Fixed {
	return fixed.FracFromBytes(vrfSig[:8])
}

func (o *Oracle) prepareEligibilityCheck(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, nonce types.VRFPostIndex, vrfSig types.VrfSignature) (int, fixed.Fixed, fixed.Fixed, bool, error) {
	logger := o.WithContext(ctx).WithFields(
		layer,
		log.Stringer("smesher", id),
		log.Uint32("round", round),
		log.Int("committee_size", committeeSize),
	)

	if committeeSize < 1 {
		logger.With().Error("committee size must be positive", log.Int("committee_size", committeeSize))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, errZeroCommitteeSize
	}

	// calc hash & check threshold
	// this is cheap in case the node is not eligible
	minerWeight, err := o.minerWeight(ctx, layer, id)
	if err != nil {
		logger.With().Error("failed to get miner weight", log.Err(err))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	msg, err := o.buildVRFMessage(ctx, nonce, layer, round)
	if err != nil {
		logger.With().Warning("could not build vrf message", log.Err(err))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// validate message
	if !o.vrfVerifier.Verify(id, msg, vrfSig) {
		logger.With().Info("eligibility: a node did not pass vrf signature verification",
			log.FieldNamed("sender_vrf_nonce", nonce),
		)
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, nil
	}

	// get active set size
	totalWeight, err := o.totalWeight(ctx, layer)
	if err != nil {
		logger.With().Error("failed to get total weight", log.Err(err))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// require totalWeight > 0
	if totalWeight == 0 {
		logger.Warning("eligibility: total weight is zero")
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, errZeroTotalWeight
	}

	logger.With().Debug("preparing eligibility check",
		log.Uint64("miner_weight", minerWeight),
		log.Uint64("total_weight", totalWeight),
	)

	// ensure miner weight fits in int
	n := int(minerWeight)
	if uint64(n) != minerWeight { // TODO(mafa): why not [if minerWeight > maxSupportedN]? leave n uint64 and cast to int (and check for overflow) before returning?
		logger.Fatal(fmt.Sprintf("minerWeight overflows int (%d)", minerWeight))
	}

	// calc p
	if committeeSize > int(totalWeight) { // TODO(mafa): why not [uint64(committeeSize) > totalWeight]? can totalWeight overflow here?
		logger.With().Warning("committee size is greater than total weight",
			log.Int("committee_size", committeeSize),
			log.Uint64("total_weight", totalWeight),
		)
		totalWeight *= uint64(committeeSize)
		n *= committeeSize
	}
	p := fixed.DivUint64(uint64(committeeSize), totalWeight)

	if n > maxSupportedN {
		return 0, fixed.Fixed{}, fixed.Fixed{}, false, fmt.Errorf("miner weight exceeds supported maximum (id: %v, weight: %d, max: %d", id, minerWeight, maxSupportedN)
	}
	return n, p, calcVrfFrac(vrfSig), false, nil
}

// Validate validates the number of eligibilities of ID on the given Layer where msg is the VRF message, sig is the role
// proof and assuming commSize as the expected committee size.
func (o *Oracle) Validate(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, sig types.VrfSignature, eligibilityCount uint16) (bool, error) {
	nonce, err := o.nonceFetcher.VRFNonce(id, layer.GetEpoch())
	if err != nil {
		o.Log.WithContext(ctx).With().Warning("failed to find nonce for node",
			log.Stringer("smesher", id),
			log.Err(err),
		)
		return false, fmt.Errorf("nonce not found for node %s: %w", id, err)
	}

	n, p, vrfFrac, done, err := o.prepareEligibilityCheck(ctx, layer, round, committeeSize, id, nonce, sig)
	if done || err != nil {
		return false, err
	}

	defer func() {
		if msg := recover(); msg != nil {
			o.WithContext(ctx).With().Fatal("panic in validate",
				log.String("msg", fmt.Sprint(msg)),
				log.Int("n", n),
				log.String("p", p.String()),
				log.String("vrf_frac", vrfFrac.String()),
			)
		}
	}()

	x := int(eligibilityCount)
	if !fixed.BinCDF(n, p, x-1).GreaterThan(vrfFrac) && vrfFrac.LessThan(fixed.BinCDF(n, p, x)) {
		return true, nil
	}
	o.WithContext(ctx).With().Warning("eligibility: node did not pass vrf eligibility threshold",
		layer,
		log.Uint32("round", round),
		log.Int("committee_size", committeeSize),
		id,
		log.Uint64("eligibility_count", uint64(eligibilityCount)),
		log.Int("n", n),
		log.String("p", p.String()),
		log.String("vrf_frac", vrfFrac.String()),
		log.Int("x", x),
	)
	return false, nil
}

// CalcEligibility calculates the number of eligibilities of ID on the given Layer where msg is the VRF message, sig is
// the role proof and assuming commSize as the expected committee size.
func (o *Oracle) CalcEligibility(ctx context.Context, layer types.LayerID, round uint32, committeeSize int,
	id types.NodeID, nonce types.VRFPostIndex, vrfSig types.VrfSignature,
) (uint16, error) {
	n, p, vrfFrac, done, err := o.prepareEligibilityCheck(ctx, layer, round, committeeSize, id, nonce, vrfSig)
	if done {
		return 0, err
	}

	defer func() {
		if msg := recover(); msg != nil {
			o.With().Fatal("panic in calc eligibility",
				layer, layer.GetEpoch(), log.Uint32("round_id", round),
				log.String("msg", fmt.Sprint(msg)),
				log.Int("committee_size", committeeSize),
				log.Int("n", n),
				log.String("p", fmt.Sprintf("%g", p.Float())),
				log.String("vrf_frac", fmt.Sprintf("%g", vrfFrac.Float())),
			)
		}
	}()

	o.With().Debug("params",
		layer, layer.GetEpoch(), log.Uint32("round_id", round),
		log.Int("committee_size", committeeSize),
		log.Int("n", n),
		log.String("p", fmt.Sprintf("%g", p.Float())),
		log.String("vrf_frac", fmt.Sprintf("%g", vrfFrac.Float())),
	)

	for x := 0; x < n; x++ {
		if fixed.BinCDF(n, p, x).GreaterThan(vrfFrac) {
			return uint16(x), nil
		}
	}
	return uint16(n), nil
}

// Proof returns the role proof for the current Layer & Round.
func (o *Oracle) Proof(ctx context.Context, nonce types.VRFPostIndex, layer types.LayerID, round uint32) (types.VrfSignature, error) {
	msg, err := o.buildVRFMessage(ctx, nonce, layer, round)
	if err != nil {
		return types.EmptyVrfSignature, err
	}
	return o.vrfSigner.Sign(msg), nil
}

// Returns a map of all active node IDs in the specified layer id.
func (o *Oracle) actives(ctx context.Context, targetLayer types.LayerID) (map[types.NodeID]uint64, error) {
	logger := o.WithContext(ctx).WithFields(
		log.FieldNamed("target_layer", targetLayer),
		log.FieldNamed("target_layer_epoch", targetLayer.GetEpoch()),
	)
	logger.Debug("hare oracle getting active set")

	o.lock.Lock()
	defer o.lock.Unlock()
	// we first try to get the hare active set for a range of safe layers
	safeLayerStart, safeLayerEnd := safeLayerRange(targetLayer, o.cfg.ConfidenceParam, o.layersPerEpoch, o.cfg.EpochOffset)
	logger.With().Debug("safe layer range",
		log.FieldNamed("safe_layer_start", safeLayerStart),
		log.FieldNamed("safe_layer_end", safeLayerEnd),
		log.FieldNamed("safe_layer_start_epoch", safeLayerStart.GetEpoch()),
		log.FieldNamed("safe_layer_end_epoch", safeLayerEnd.GetEpoch()),
		log.Uint32("confidence_param", o.cfg.ConfidenceParam),
		log.Uint32("epoch_offset", o.cfg.EpochOffset),
		log.Uint64("layers_per_epoch", uint64(o.layersPerEpoch)),
		log.FieldNamed("effective_genesis", types.GetEffectiveGenesis()),
	)

	if value, exists := o.activesCache.Get(safeLayerStart.GetEpoch()); exists {
		return value.(map[types.NodeID]uint64), nil
	}
	activeSet, err := o.computeActiveSet(logger, targetLayer, safeLayerStart, safeLayerEnd)
	if err != nil {
		return nil, err
	}
	o.activesCache.Add(safeLayerStart.GetEpoch(), activeSet)
	return activeSet, nil
}

func (o *Oracle) computeActiveSet(logger log.Log, targetLayer, safeLayerStart, safeLayerEnd types.LayerID) (map[types.NodeID]uint64, error) {
	// check cache first
	// as long as epochOffset < layersPerEpoch, we expect safeLayerStart and safeLayerEnd to be in the same epoch.
	// if not, don't attempt to cache on the basis of a single epoch since the safe layer range will span multiple
	// epochs.
	if safeLayerStart.GetEpoch() == safeLayerEnd.GetEpoch() {
		logger.With().Debug("no value in cache for safe layer start epoch",
			log.FieldNamed("safe_layer_start", safeLayerStart),
			log.FieldNamed("safe_layer_start_epoch", safeLayerStart.GetEpoch()))
	} else {
		// TODO: should we panic or return an error instead?
		logger.With().Error("safe layer range spans multiple epochs, not caching active set results",
			log.FieldNamed("safe_layer_start", safeLayerStart),
			log.FieldNamed("safe_layer_end", safeLayerEnd),
			log.FieldNamed("safe_layer_start_epoch", safeLayerStart.GetEpoch()),
			log.FieldNamed("safe_layer_end_epoch", safeLayerEnd.GetEpoch()))
	}

	var (
		beacons       = make(map[types.EpochID]types.Beacon)
		activeBallots = make(map[types.BallotID]*types.Ballot)
		epoch         types.EpochID
		beacon        types.Beacon
		err           error
	)
	for layerID := safeLayerStart; !layerID.After(safeLayerEnd); layerID = layerID.Add(1) {
		epoch = layerID.GetEpoch()
		if _, exist := beacons[epoch]; !exist {
			if beacon, err = o.beacons.GetBeacon(epoch); err != nil {
				return nil, fmt.Errorf("error getting beacon for epoch %v: %w", epoch, err)
			}
			logger.With().Debug("found beacon for epoch", epoch, beacon)
			beacons[epoch] = beacon
		}
		blts, err := ballots.Layer(o.cdb, layerID)
		if err != nil {
			return nil, fmt.Errorf("error getting ballots for layer %v (target layer %v): %w",
				layerID, targetLayer, err)
		}
		for _, b := range blts {
			activeBallots[b.ID()] = b
		}
	}
	logger.With().Debug("got ballots in safe layer range, reading active set from ballots",
		log.Int("count", len(activeBallots)))

	// now read the set of ATXs referenced by these blocks
	// TODO: can the set of blocks ever span multiple epochs?
	hareActiveSet := make(map[types.NodeID]uint64)
	badBeaconATXIDs := make(map[types.ATXID]struct{})
	seenATXIDs := make(map[types.ATXID]struct{})
	seenBallots := make(map[types.BallotID]struct{})
	for _, ballot := range activeBallots {
		if _, exist := seenBallots[ballot.ID()]; exist {
			continue
		}
		seenBallots[ballot.ID()] = struct{}{}
		if ballot.EpochData == nil {
			// not a ref ballot, no beacon value to check
			continue
		}
		beacon = beacons[ballot.Layer.GetEpoch()]
		if ballot.EpochData.Beacon != beacon {
			badBeaconATXIDs[ballot.AtxID] = struct{}{}
			logger.With().Warning("hare actives find ballot with different beacon",
				ballot.ID(),
				ballot.AtxID,
				ballot.Layer.GetEpoch(),
				log.String("ballot_beacon", ballot.EpochData.Beacon.ShortString()),
				log.String("epoch_beacon", beacon.ShortString()))
			continue
		}
		for _, id := range ballot.ActiveSet {
			if _, exist := seenATXIDs[id]; exist {
				continue
			}
			seenATXIDs[id] = struct{}{}
			atx, err := o.cdb.GetAtxHeader(id)
			if err != nil {
				return nil, fmt.Errorf("hare actives (target layer %v) get ATX: %w", targetLayer, err)
			}
			hareActiveSet[atx.NodeID] = atx.GetWeight()
		}
	}

	// remove miners who published ballots with bad beacons
	for id := range badBeaconATXIDs {
		atx, err := o.cdb.GetAtxHeader(id)
		if err != nil {
			return nil, fmt.Errorf("hare actives (target layer %v) get bad beacon ATX: %w", targetLayer, err)
		}
		delete(hareActiveSet, atx.NodeID)
		logger.With().Warning("smesher removed from hare active set", log.Stringer("node_key", atx.NodeID))
	}

	if len(hareActiveSet) > 0 {
		logger.With().Info("successfully got hare active set for layer range",
			log.Uint32("layer_range_epoch", uint32(safeLayerStart.GetEpoch())),
			log.Int("count", len(hareActiveSet)))
		return hareActiveSet, nil
	}

	// if we failed to get a Hare active set, we fall back on reading the Tortoise active set targeting this epoch
	if safeLayerStart.GetEpoch().IsGenesis() {
		logger.With().Debug("no hare active set for genesis layer range, reading tortoise set for epoch instead",
			targetLayer.GetEpoch())
	} else {
		logger.With().Warning("no hare active set for layer range, reading tortoise set for epoch instead",
			targetLayer.GetEpoch())
	}
	atxids, err := atxs.GetIDsByEpoch(o.cdb, targetLayer.GetEpoch()-1)
	if err != nil {
		return nil, fmt.Errorf("can't get atxs for an epoch %d: %w", targetLayer.GetEpoch()-1, err)
	}
	if len(atxids) == 0 {
		return nil, fmt.Errorf("%w: layer %v / epoch %v", errEmptyActiveSet, targetLayer, targetLayer.GetEpoch())
	}

	// extract the nodeIDs and weights
	activeMap := make(map[types.NodeID]uint64, len(atxids))
	for _, atxid := range atxids {
		atxHeader, err := o.cdb.GetAtxHeader(atxid)
		if err != nil {
			return nil, fmt.Errorf("inconsistent state: error getting atx header %v for target layer %v: %w", atxid, targetLayer, err)
		}
		activeMap[atxHeader.NodeID] = atxHeader.GetWeight()
	}
	logger.With().Debug("got tortoise active set", log.Int("count", len(activeMap)))
	return activeMap, nil
}

// IsIdentityActiveOnConsensusView returns true if the provided identity is active on the consensus view derived
// from the specified layer, false otherwise.
func (o *Oracle) IsIdentityActiveOnConsensusView(ctx context.Context, edID types.NodeID, layer types.LayerID) (bool, error) {
	o.WithContext(ctx).With().Debug("hare oracle checking for active identity")
	defer func() {
		o.WithContext(ctx).With().Debug("hare oracle active identity check complete")
	}()
	actives, err := o.actives(ctx, layer)
	if err != nil {
		o.WithContext(ctx).With().Error("error getting active set", layer, log.Err(err))
		return false, err
	}
	_, exist := actives[edID]
	return exist, nil
}

func (o *Oracle) UpdateActiveSet(epoch types.EpochID, activeSet []types.ATXID) {
	// TODO: implement
	o.Log.With().Info("received activeset update",
		epoch,
		log.Array("activeset", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, atxid := range activeSet {
				encoder.AppendString(atxid.String())
			}
			return nil
		})))
}
