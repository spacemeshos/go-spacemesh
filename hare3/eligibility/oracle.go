package eligibility

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/spacemeshos/fixed"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/signing"
)

const (
	// CertifyRound is not part of the hare protocol, but it shares the same oracle for eligibility.
	CertifyRound uint32 = math.MaxUint32 >> 1
)

const (
	maxSupportedN = (math.MaxInt32 / 2) + 1 // higher values result in an overflow when calculating CDF
)

var (
	errZeroCommitteeSize = errors.New("zero committee size")
	errEmptyActiveSet    = errors.New("empty active set")
	errZeroTotalWeight   = errors.New("zero total weight")
	ErrNotActive         = errors.New("oracle: miner is not active in epoch")
)

// Config is the configuration of the oracle package.
type Config struct {
	// ConfidenceParam specifies how many layers into the epoch hare uses active set generated in the previous epoch.
	// For example, if epoch size is 100 and confidence is 10 hare will use previous active set for layers 0-9
	// and then generate a new activeset.
	//
	// This was done like that so that we have higher `confidence` that hare will succeed at least
	// once during this interval. If it doesn't we have to provide centralized fallback.
	ConfidenceParam uint32 `mapstructure:"eligibility-confidence-param"`
}

func (c *Config) MarshalLogObject(encoder zapcore.ObjectEncoder) error {
	encoder.AddUint32("confidence param", c.ConfidenceParam)
	return nil
}

// DefaultConfig returns the default configuration for the oracle package.
func DefaultConfig() Config {
	return Config{ConfidenceParam: 1}
}

// Oracle is the hare eligibility oracle.
type Oracle struct {
	weights weights

	beacons     BeaconProvider
	vrfVerifier vrfVerifier
	cfg         Config
	log         *zap.Logger
}

type Opt func(*Oracle)

func WithConfig(config Config) Opt {
	return func(o *Oracle) {
		o.cfg = config
	}
}

func WithLogger(logger *zap.Logger) Opt {
	return func(o *Oracle) {
		o.log = logger
	}
}

// New returns a new eligibility oracle instance.
func New(
	weights weights,
	beacons BeaconProvider,
	vrfVerifier vrfVerifier,
	layersPerEpoch uint32,
	opts ...Opt,
) (*Oracle, error) {
	oracle := &Oracle{
		beacons:     beacons,
		vrfVerifier: vrfVerifier,
		weights:     weights,
		cfg:         DefaultConfig(),
		log:         zap.NewNop(),
	}
	for _, opt := range opts {
		opt(oracle)
	}
	// we can't have an epoch offset which is greater/equal than the number of layers in an epoch
	if oracle.cfg.ConfidenceParam >= layersPerEpoch {
		return nil, fmt.Errorf(
			"hare eligibility confidence param (%d) larger than layers per epoch (%d)",
			oracle.cfg.ConfidenceParam,
			layersPerEpoch,
		)
	}
	oracle.log.Info("hare oracle initialized", zap.Uint32("epoch size", layersPerEpoch), zap.Inline(&oracle.cfg))
	return oracle, nil
}

//go:generate scalegen -types VrfMessage

// VrfMessage is a verification message. It is also the payload for the signature in `types.HareEligibility`.
type VrfMessage struct {
	Type   types.EligibilityType // always types.EligibilityHare
	Beacon types.Beacon
	Round  uint32
	Layer  types.LayerID
}

// buildVRFMessage builds the VRF message used as input for hare eligibility validation.
func (o *Oracle) buildVRFMessage(ctx context.Context, layer types.LayerID, round uint32) ([]byte, error) {
	beacon, err := o.beacons.Beacon(ctx, layer.GetEpoch())
	if err != nil {
		return nil, fmt.Errorf("get beacon: %w", err)
	}
	return codec.MustEncode(&VrfMessage{Type: types.EligibilityHare, Beacon: beacon, Round: round, Layer: layer}), nil
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
	logger := o.log.With(
		log.ZContext(ctx),
		zap.Uint32("layer", layer.Uint32()),
		zap.Uint32("epoch", layer.GetEpoch().Uint32()),
		log.ZShortStringer("smesherID", id),
		zap.Uint32("round", round),
		zap.Int("committee_size", committeeSize),
	)

	if committeeSize < 1 {
		logger.Error("committee size must be positive", zap.Int("committee_size", committeeSize))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, errZeroCommitteeSize
	}

	// calc hash & check threshold
	// this is cheap in case the node is not eligible
	minerWeight, err := o.weights.MinerWeight(ctx, o.layerToEpoch(layer), id)
	if err != nil {
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		logger.Warn("could not build vrf message", zap.Error(err))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// validate message
	if !o.vrfVerifier.Verify(id, msg, vrfSig) {
		logger.Debug("eligibility: a node did not pass vrf signature verification")
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, nil
	}

	// get active set size
	totalWeight, err := o.weights.TotalWeight(ctx, o.layerToEpoch(layer))
	if err != nil {
		logger.Error("failed to get total weight", zap.Error(err))
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// require totalWeight > 0
	if totalWeight == 0 {
		logger.Warn("eligibility: total weight is zero")
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, errZeroTotalWeight
	}

	logger.Debug("preparing eligibility check",
		zap.Uint64("miner_weight", minerWeight),
		zap.Uint64("total_weight", totalWeight),
	)

	n := minerWeight

	// calc p
	if uint64(committeeSize) > totalWeight {
		logger.Warn("committee size is greater than total weight",
			zap.Int("committee_size", committeeSize),
			zap.Uint64("total_weight", totalWeight),
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
			o.log.Fatal("panic in validate",
				log.ZContext(ctx),
				zap.Any("msg", msg),
				zap.Int("n", n),
				zap.String("p", p.String()),
				zap.String("vrf_frac", vrfFrac.String()),
			)
		}
	}()

	x := int(eligibilityCount)
	if !fixed.BinCDF(n, p, x-1).GreaterThan(vrfFrac) && vrfFrac.LessThan(fixed.BinCDF(n, p, x)) {
		return true, nil
	}
	o.log.Info("eligibility: node did not pass vrf eligibility threshold",
		log.ZContext(ctx),
		zap.Uint32("layer", layer.Uint32()),
		zap.Uint32("round", round),
		zap.Int("committee_size", committeeSize),
		log.ZShortStringer("smesherID", id),
		zap.Uint16("eligibility_count", eligibilityCount),
		zap.Int("n", n),
		zap.Float64("p", p.Float()),
		zap.Float64("vrf_frac", vrfFrac.Float()),
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

	o.log.Debug("calculating eligibility",
		zap.Uint32("layer", layer.Uint32()),
		zap.Uint32("epoch", layer.GetEpoch().Uint32()),
		zap.Uint32("round_id", round),
		zap.Int("committee_size", committeeSize),
		zap.Int("n", n),
		zap.Float64("p", p.Float()),
		zap.Float64("vrf_frac", vrfFrac.Float()),
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

// GenVRF generates vrf for hare eligibility.
func GenVRF(
	signer *signing.VRFSigner,
	beacon types.Beacon,
	layer types.LayerID,
	round uint32,
) types.VrfSignature {
	return signer.Sign(
		codec.MustEncode(&VrfMessage{Type: types.EligibilityHare, Beacon: beacon, Round: round, Layer: layer}),
	)
}

func (o *Oracle) layerToEpoch(layer types.LayerID) types.EpochID {
	epoch := layer.GetEpoch()
	// the first bootstrap data targets first epoch after genesis (epoch 2)
	// and the epoch where checkpoint recovery happens
	if epoch > types.GetEffectiveGenesis().Add(1).GetEpoch() &&
		layer.Difference(epoch.FirstLayer()) < o.cfg.ConfidenceParam {
		epoch -= 1
	}
	return epoch
}
