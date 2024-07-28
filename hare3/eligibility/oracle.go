package eligibility

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/spacemeshos/fixed"
	"golang.org/x/exp/maps"

	"github.com/spacemeshos/go-spacemesh/atxsdata"
	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/miner"
	"github.com/spacemeshos/go-spacemesh/signing"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/activesets"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/system"
)

const (
	// CertifyRound is not part of the hare protocol, but it shares the same oracle for eligibility.
	CertifyRound uint32 = math.MaxUint32 >> 1
)

const (
	activesCacheSize = 5                       // we don't expect to handle more than two layers concurrently
	maxSupportedN    = (math.MaxInt32 / 2) + 1 // higher values result in an overflow when calculating CDF
)

var (
	errZeroCommitteeSize = errors.New("zero committee size")
	errEmptyActiveSet    = errors.New("empty active set")
	errZeroTotalWeight   = errors.New("zero total weight")
	ErrNotActive         = errors.New("oracle: miner is not active in epoch")
)

type identityWeight struct {
	atx    types.ATXID
	weight uint64
}

type cachedActiveSet struct {
	set   map[types.NodeID]identityWeight
	total uint64
}

func (c *cachedActiveSet) atxs() []types.ATXID {
	atxs := make([]types.ATXID, 0, len(c.set))
	for _, id := range c.set {
		atxs = append(atxs, id.atx)
	}
	return atxs
}

// Config is the configuration of the oracle package.
type Config struct {
	// ConfidenceParam specifies how many layers into the epoch hare uses active set generated in the previous epoch.
	// For example, if epoch size is 100 and confidence is 10 hare will use previous active set for layers 0-9
	// and then generate a new activeset.
	//
	// This was done like that so that we have higher `confidence` that hare will succeed atleast
	// once during this interval. If it doesn't we have to provide centralized fallback.
	ConfidenceParam uint32 `mapstructure:"eligibility-confidence-param"`
}

func (c *Config) MarshalLogObject(encoder log.ObjectEncoder) error {
	encoder.AddUint32("confidence param", c.ConfidenceParam)
	return nil
}

// DefaultConfig returns the default configuration for the oracle package.
func DefaultConfig() Config {
	return Config{ConfidenceParam: 1}
}

// Oracle is the hare eligibility oracle.
type Oracle struct {
	mu           sync.Mutex
	activesCache activeSetCache
	fallback     map[types.EpochID][]types.ATXID
	sync         system.SyncStateProvider
	// NOTE(dshulyak) on switch from synced to not synced reset the cache
	// to cope with https://github.com/spacemeshos/go-spacemesh/issues/4552
	// until graded oracle is implemented
	synced bool

	beacons        system.BeaconGetter
	atxsdata       *atxsdata.Data
	db             sql.Executor
	vrfVerifier    vrfVerifier
	layersPerEpoch uint32
	cfg            Config
	log            log.Log
}

type Opt func(*Oracle)

func WithConfig(config Config) Opt {
	return func(o *Oracle) {
		o.cfg = config
	}
}

func WithLogger(logger log.Log) Opt {
	return func(o *Oracle) {
		o.log = logger
	}
}

// New returns a new eligibility oracle instance.
func New(
	beacons system.BeaconGetter,
	db sql.Executor,
	atxsdata *atxsdata.Data,
	vrfVerifier vrfVerifier,
	layersPerEpoch uint32,
	opts ...Opt,
) *Oracle {
	activesCache, err := lru.New[types.EpochID, *cachedActiveSet](activesCacheSize)
	if err != nil {
		panic("failed to create lru cache for active set" + err.Error())
	}
	oracle := &Oracle{
		beacons:        beacons,
		db:             db,
		atxsdata:       atxsdata,
		vrfVerifier:    vrfVerifier,
		layersPerEpoch: layersPerEpoch,
		activesCache:   activesCache,
		fallback:       map[types.EpochID][]types.ATXID{},
		cfg:            DefaultConfig(),
		log:            log.NewNop(),
	}
	for _, opt := range opts {
		opt(oracle)
	}
	oracle.log.With().Info("hare oracle initialized", log.Uint32("epoch size", layersPerEpoch), log.Inline(&oracle.cfg))
	return oracle
}

//go:generate scalegen -types VrfMessage

// VrfMessage is a verification message. It is also the payload for the signature in `types.HareEligibility`.
type VrfMessage struct {
	Type   types.EligibilityType // always types.EligibilityHare
	Beacon types.Beacon
	Round  uint32
	Layer  types.LayerID
}

func (o *Oracle) SetSync(sync system.SyncStateProvider) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sync = sync
}

func (o *Oracle) resetCacheOnSynced(ctx context.Context) {
	if o.sync == nil {
		return
	}
	synced := o.synced
	o.synced = o.sync.IsSynced(ctx)
	if !synced && o.synced {
		ac, err := lru.New[types.EpochID, *cachedActiveSet](activesCacheSize)
		if err != nil {
			o.log.With().Fatal("failed to create lru cache for active set", log.Err(err))
		}
		o.activesCache = ac
	}
}

// buildVRFMessage builds the VRF message used as input for hare eligibility validation.
func (o *Oracle) buildVRFMessage(ctx context.Context, layer types.LayerID, round uint32) ([]byte, error) {
	beacon, err := o.beacons.GetBeacon(layer.GetEpoch())
	if err != nil {
		return nil, fmt.Errorf("get beacon: %w", err)
	}
	return codec.MustEncode(&VrfMessage{Type: types.EligibilityHare, Beacon: beacon, Round: round, Layer: layer}), nil
}

func (o *Oracle) totalWeight(ctx context.Context, layer types.LayerID) (uint64, error) {
	actives, err := o.actives(ctx, layer)
	if err != nil {
		return 0, err
	}
	return actives.total, nil
}

func (o *Oracle) minerWeight(ctx context.Context, layer types.LayerID, id types.NodeID) (uint64, error) {
	actives, err := o.actives(ctx, layer)
	if err != nil {
		return 0, err
	}

	w, ok := actives.set[id]
	if !ok {
		return 0, fmt.Errorf("%w: %v", ErrNotActive, id)
	}
	return w.weight, nil
}

func calcVrfFrac(vrfSig types.VrfSignature) fixed.Fixed {
	return fixed.FracFromBytes(vrfSig[:8])
}

func (o *Oracle) prepareEligibilityCheck(
	ctx context.Context,
	layer types.LayerID,
	round uint32,
	committeeSize int,
	id types.NodeID,
	vrfSig types.VrfSignature,
) (int, fixed.Fixed, fixed.Fixed, bool, error) {
	logger := o.log.WithContext(ctx).WithFields(
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
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		logger.With().Warning("could not build vrf message", log.Err(err))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// validate message
	if !o.vrfVerifier.Verify(id, msg, vrfSig) {
		logger.Debug("eligibility: a node did not pass vrf signature verification")
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

	n := minerWeight

	// calc p
	if uint64(committeeSize) > totalWeight {
		logger.With().Warning("committee size is greater than total weight",
			log.Int("committee_size", committeeSize),
			log.Uint64("total_weight", totalWeight),
		)
		totalWeight *= uint64(committeeSize)
		n *= uint64(committeeSize)
	}
	if n > maxSupportedN {
		return 0, fixed.Fixed{}, fixed.Fixed{}, false, fmt.Errorf(
			"miner weight exceeds supported maximum (id: %v, weight: %d, max: %d",
			id,
			minerWeight,
			maxSupportedN,
		)
	}

	p := fixed.DivUint64(uint64(committeeSize), totalWeight)
	return int(n), p, calcVrfFrac(vrfSig), false, nil
}

// Validate validates the number of eligibilities of ID on the given Layer where msg is the VRF message, sig is the role
// proof and assuming commSize as the expected committee size.
func (o *Oracle) Validate(
	ctx context.Context,
	layer types.LayerID,
	round uint32,
	committeeSize int,
	id types.NodeID,
	sig types.VrfSignature,
	eligibilityCount uint16,
) (bool, error) {
	n, p, vrfFrac, done, err := o.prepareEligibilityCheck(ctx, layer, round, committeeSize, id, sig)
	if done || err != nil {
		return false, err
	}

	defer func() {
		if msg := recover(); msg != nil {
			o.log.WithContext(ctx).With().Fatal("panic in validate",
				log.Any("msg", msg),
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
	o.log.WithContext(ctx).With().Info("eligibility: node did not pass vrf eligibility threshold",
		layer,
		log.Uint32("round", round),
		log.Int("committee_size", committeeSize),
		id,
		log.Uint16("eligibility_count", eligibilityCount),
		log.Int("n", n),
		log.Float64("p", p.Float()),
		log.Float64("vrf_frac", vrfFrac.Float()),
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
	vrfSig types.VrfSignature,
) (uint16, error) {
	n, p, vrfFrac, done, err := o.prepareEligibilityCheck(ctx, layer, round, committeeSize, id, vrfSig)
	if done {
		return 0, err
	}

	defer func() {
		if msg := recover(); msg != nil {
			o.log.With().Fatal("panic in calc eligibility",
				layer,
				layer.GetEpoch(),
				log.Uint32("round_id", round),
				log.Any("msg", msg),
				log.Int("committee_size", committeeSize),
				log.Int("n", n),
				log.Float64("p", p.Float()),
				log.Float64("vrf_frac", vrfFrac.Float()),
			)
		}
	}()

	o.log.With().Debug("params",
		layer,
		layer.GetEpoch(),
		log.Uint32("round_id", round),
		log.Int("committee_size", committeeSize),
		log.Int("n", n),
		log.Float64("p", p.Float()),
		log.Float64("vrf_frac", vrfFrac.Float()),
	)

	for x := 0; x < n; x++ {
		if fixed.BinCDF(n, p, x).GreaterThan(vrfFrac) {
			// even with large N and large P, x will be << 2^16, so this cast is safe
			return uint16(x), nil
		}
	}

	// since BinCDF(n, p, n) is 1 for any p, this code can only be reached if n is much smaller
	// than 2^16 (so that BinCDF(n, p, n-1) is still lower than vrfFrac)
	return uint16(n), nil
}

// Proof returns the role proof for the current Layer & Round.
func (o *Oracle) Proof(
	ctx context.Context,
	signer *signing.VRFSigner,
	layer types.LayerID,
	round uint32,
) (types.VrfSignature, error) {
	beacon, err := o.beacons.GetBeacon(layer.GetEpoch())
	if err != nil {
		return types.EmptyVrfSignature, fmt.Errorf("get beacon: %w", err)
	}
	return GenVRF(ctx, signer, beacon, layer, round), nil
}

// GenVRF generates vrf for hare eligibility.
func GenVRF(
	ctx context.Context,
	signer *signing.VRFSigner,
	beacon types.Beacon,
	layer types.LayerID,
	round uint32,
) types.VrfSignature {
	return signer.Sign(
		codec.MustEncode(&VrfMessage{Type: types.EligibilityHare, Beacon: beacon, Round: round, Layer: layer}),
	)
}

// Returns a map of all active node IDs in the specified layer id.
func (o *Oracle) actives(ctx context.Context, targetLayer types.LayerID) (*cachedActiveSet, error) {
	if !targetLayer.After(types.GetEffectiveGenesis()) {
		return nil, errEmptyActiveSet
	}
	targetEpoch := targetLayer.GetEpoch()
	// the first bootstrap data targets first epoch after genesis (epoch 2)
	// and the epoch where checkpoint recovery happens
	if targetEpoch > types.GetEffectiveGenesis().Add(1).GetEpoch() &&
		targetLayer.Difference(targetEpoch.FirstLayer()) < o.cfg.ConfidenceParam {
		targetEpoch -= 1
	}
	o.log.WithContext(ctx).With().Debug("hare oracle getting active set",
		log.Stringer("target_layer", targetLayer),
		log.Stringer("target_layer_epoch", targetLayer.GetEpoch()),
		log.Stringer("target_epoch", targetEpoch),
	)

	o.mu.Lock()
	defer o.mu.Unlock()
	o.resetCacheOnSynced(ctx)
	if value, exists := o.activesCache.Get(targetEpoch); exists {
		return value, nil
	}
	activeSet, err := o.computeActiveSet(ctx, targetEpoch)
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

	aset := &cachedActiveSet{set: activeWeights}
	for _, aweight := range activeWeights {
		aset.total += aweight.weight
	}
	o.log.WithContext(ctx).With().Info("got hare active set", log.Int("count", len(activeWeights)))
	o.activesCache.Add(targetEpoch, aset)
	return aset, nil
}

func (o *Oracle) ActiveSet(ctx context.Context, targetEpoch types.EpochID) ([]types.ATXID, error) {
	aset, err := o.actives(ctx, targetEpoch.FirstLayer().Add(o.cfg.ConfidenceParam))
	if err != nil {
		return nil, err
	}
	return aset.atxs(), nil
}

func (o *Oracle) computeActiveSet(ctx context.Context, targetEpoch types.EpochID) ([]types.ATXID, error) {
	activeSet, ok := o.fallback[targetEpoch]
	if ok {
		o.log.WithContext(ctx).With().Info("using fallback active set",
			targetEpoch,
			log.Int("size", len(activeSet)),
		)
		return activeSet, nil
	}

	activeSet, err := miner.ActiveSetFromEpochFirstBlock(o.db, targetEpoch)
	if err != nil && !errors.Is(err, sql.ErrNotFound) {
		return nil, err
	}
	if len(activeSet) == 0 {
		return o.activeSetFromRefBallots(targetEpoch)
	}
	return activeSet, nil
}

func (o *Oracle) computeActiveWeights(
	targetEpoch types.EpochID,
	activeSet []types.ATXID,
) (map[types.NodeID]identityWeight, error) {
	identities := make(map[types.NodeID]identityWeight, len(activeSet))
	for _, id := range activeSet {
		atx := o.atxsdata.Get(targetEpoch, id)
		if atx == nil {
			return nil, fmt.Errorf("oracle: missing atx in atxsdata %s/%s", targetEpoch, id.ShortString())
		}
		identities[atx.Node] = identityWeight{atx: id, weight: atx.Weight}
	}
	return identities, nil
}

func (o *Oracle) activeSetFromRefBallots(epoch types.EpochID) ([]types.ATXID, error) {
	beacon, err := o.beacons.GetBeacon(epoch)
	if err != nil {
		return nil, fmt.Errorf("get beacon: %w", err)
	}
	ballotsrst, err := ballots.AllFirstInEpoch(o.db, epoch)
	if err != nil {
		return nil, fmt.Errorf("first in epoch %d: %w", epoch, err)
	}
	activeMap := make(map[types.ATXID]struct{}, len(ballotsrst))
	for _, ballot := range ballotsrst {
		if ballot.EpochData == nil {
			o.log.With().Error("invalid data. first ballot doesn't have epoch data", log.Inline(ballot))
			continue
		}
		if ballot.EpochData.Beacon != beacon {
			o.log.With().Debug("beacon mismatch", log.Stringer("local", beacon), log.Object("ballot", ballot))
			continue
		}
		actives, err := activesets.Get(o.db, ballot.EpochData.ActiveSetHash)
		if err != nil {
			o.log.With().Error("failed to get active set",
				log.String("actives hash", ballot.EpochData.ActiveSetHash.ShortString()),
				log.String("ballot ", ballot.ID().String()),
				log.Err(err),
			)
			continue
		}
		for _, id := range actives.Set {
			activeMap[id] = struct{}{}
		}
	}
	o.log.With().Warning("using tortoise active set",
		log.Int("actives size", len(activeMap)),
		log.Uint32("epoch", epoch.Uint32()),
		log.Stringer("beacon", beacon),
	)
	return maps.Keys(activeMap), nil
}

// IsIdentityActiveOnConsensusView returns true if the provided identity is active on the consensus view derived
// from the specified layer, false otherwise.
func (o *Oracle) IsIdentityActiveOnConsensusView(
	ctx context.Context,
	edID types.NodeID,
	layer types.LayerID,
) (bool, error) {
	o.log.WithContext(ctx).With().Debug("hare oracle checking for active identity")
	defer func() {
		o.log.WithContext(ctx).With().Debug("hare oracle active identity check complete")
	}()
	actives, err := o.actives(ctx, layer)
	if err != nil {
		return false, err
	}
	_, exist := actives.set[edID]
	return exist, nil
}

func (o *Oracle) UpdateActiveSet(epoch types.EpochID, activeSet []types.ATXID) {
	o.log.With().Info("received activeset update",
		epoch,
		log.Int("size", len(activeSet)),
	)
	o.mu.Lock()
	defer o.mu.Unlock()
	if _, ok := o.fallback[epoch]; ok {
		o.log.With().Debug("fallback active set already exists", epoch)
		return
	}
	o.fallback[epoch] = activeSet
}
