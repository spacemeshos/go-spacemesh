package eligibility

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	"github.com/spacemeshos/fixed"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/blocks"
	"github.com/spacemeshos/go-spacemesh/sql/certificates"
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
	errMinerNotActive    = errors.New("miner not active in epoch")
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
	fallback       map[types.EpochID][]types.ATXID
	cfg            config.Config
	log.Log
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
		fallback:       map[types.EpochID][]types.ATXID{},
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
		return 0, errMinerNotActive
	}
	return w, nil
}

func calcVrfFrac(vrfSig types.VrfSignature) fixed.Fixed {
	return fixed.FracFromBytes(vrfSig[:8])
}

func (o *Oracle) prepareEligibilityCheck(ctx context.Context, layer types.LayerID, round uint32, committeeSize int, id types.NodeID, nonce types.VRFPostIndex, vrfSig types.VrfSignature) (int, fixed.Fixed, fixed.Fixed, bool, error) {
	logger := o.WithContext(ctx).WithFields(
		layer,
		layer.GetEpoch(),
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
		logger.With().Debug("eligibility: a node did not pass vrf signature verification",
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
func (o *Oracle) CalcEligibility(
	ctx context.Context,
	layer types.LayerID,
	round uint32,
	committeeSize int,
	id types.NodeID,
	nonce types.VRFPostIndex,
	vrfSig types.VrfSignature,
) (uint16, error) {
	n, p, vrfFrac, done, err := o.prepareEligibilityCheck(ctx, layer, round, committeeSize, id, nonce, vrfSig)
	if done {
		return 0, err
	}

	defer func() {
		if msg := recover(); msg != nil {
			o.With().Fatal("panic in calc eligibility",
				layer,
				layer.GetEpoch(),
				log.Uint32("round_id", round),
				log.String("msg", fmt.Sprint(msg)),
				log.Int("committee_size", committeeSize),
				log.Int("n", n),
				log.String("p", fmt.Sprintf("%g", p.Float())),
				log.String("vrf_frac", fmt.Sprintf("%g", vrfFrac.Float())),
			)
		}
	}()

	o.With().Debug("params",
		layer,
		layer.GetEpoch(),
		log.Uint32("round_id", round),
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
	if !targetLayer.After(types.GetEffectiveGenesis()) {
		return nil, errEmptyActiveSet
	}
	targetEpoch := targetLayer.GetEpoch()
	// the first bootstrap data targets first epoch after genesis (epoch 2)
	if targetEpoch != (types.GetEffectiveGenesis().GetEpoch()+1) &&
		targetLayer.Difference(targetEpoch.FirstLayer()) < o.cfg.ConfidenceParam {
		targetEpoch -= 1
	}
	logger := o.WithContext(ctx).WithFields(
		log.FieldNamed("target_layer", targetLayer),
		log.FieldNamed("target_layer_epoch", targetLayer.GetEpoch()),
		log.FieldNamed("target_epoch", targetEpoch),
	)
	logger.Debug("hare oracle getting active set")

	o.lock.Lock()
	defer o.lock.Unlock()
	if value, exists := o.activesCache.Get(targetEpoch); exists {
		return value.(map[types.NodeID]uint64), nil
	}
	activeSet, err := o.computeActiveSet(logger, targetEpoch)
	if err != nil {
		return nil, err
	}
	if len(activeSet) == 0 {
		return nil, errEmptyActiveSet
	}
	activeWeights, err := o.computeActiveWeights(targetEpoch, activeSet)
	if err != nil {
		return nil, err
	}
	logger.With().Info("got hare active set", log.Int("count", len(activeWeights)))
	o.activesCache.Add(targetEpoch, activeWeights)
	return activeWeights, nil
}

func (o *Oracle) ActiveSet(ctx context.Context, targetEpoch types.EpochID) ([]types.ATXID, error) {
	activeWeights, err := o.actives(ctx, targetEpoch.FirstLayer().Add(o.cfg.ConfidenceParam))
	if err != nil {
		return nil, err
	}
	activeSet := make([]types.ATXID, 0, 10_000)
	for nodeID := range activeWeights {
		hdr, err := o.cdb.GetEpochAtx(targetEpoch-1, nodeID)
		if err != nil {
			return nil, err
		}
		activeSet = append(activeSet, hdr.ID)
	}
	return activeSet, nil
}

func (o *Oracle) computeActiveSet(logger log.Log, targetEpoch types.EpochID) ([]types.ATXID, error) {
	activeSet, ok := o.fallback[targetEpoch]
	if ok {
		logger.With().Info("using fallback active set", log.Int("size", len(activeSet)))
		return activeSet, nil
	}
	bid, err := certificates.FirstInEpoch(o.cdb, targetEpoch)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, err
	}
	if bid == types.EmptyBlockID {
		return nil, nil
	}
	return o.activeSetFromBlock(bid)
}

func (o *Oracle) computeActiveWeights(targetEpoch types.EpochID, activeSet []types.ATXID) (map[types.NodeID]uint64, error) {
	weightedActiveSet := make(map[types.NodeID]uint64)
	for _, id := range activeSet {
		atx, err := o.cdb.GetAtxHeader(id)
		if err != nil {
			return nil, fmt.Errorf("hare actives get ATX %s, epoch %d: %w", id, targetEpoch, err)
		}
		weightedActiveSet[atx.NodeID] = atx.GetWeight()
	}
	return weightedActiveSet, nil
}

func (o *Oracle) activeSetFromBlock(bid types.BlockID) ([]types.ATXID, error) {
	block, err := blocks.Get(o.cdb, bid)
	if err != nil {
		return nil, fmt.Errorf("actives get block: %w", err)
	}
	activeMap := make(map[types.ATXID]struct{})
	for _, r := range block.Rewards {
		// only the reference ballots record the active set
		ballot, err := ballots.FirstInEpoch(o.cdb, r.AtxID, block.LayerIndex.GetEpoch())
		if err != nil {
			return nil, fmt.Errorf("actives get ballot: %w", err)
		}
		for _, id := range ballot.ActiveSet {
			activeMap[id] = struct{}{}
		}
	}
	return maps.Keys(activeMap), nil
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
	o.Log.With().Info("received activeset update",
		epoch,
		log.Int("size", len(activeSet)),
		log.Array("activeset", log.ArrayMarshalerFunc(func(encoder log.ArrayEncoder) error {
			for _, atxid := range activeSet {
				encoder.AppendString(atxid.String())
			}
			return nil
		})))
	o.lock.Lock()
	defer o.lock.Unlock()
	if _, ok := o.fallback[epoch]; ok {
		o.Log.With().Debug("fallback active set already exists", epoch)
		return
	}
	o.fallback[epoch] = activeSet
}
