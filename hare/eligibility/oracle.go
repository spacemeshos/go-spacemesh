package eligibility

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"github.com/hashicorp/golang-lru"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-spacemesh/common/types"
	eCfg "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
)

const vrfMsgCacheSize = 20       // numRounds per layer is <= 2. numConcurrentLayers<=10 (typically <=2) so numRounds*numConcurrentLayers <= 2*10 = 20 is a good upper bound
const activesCacheSize = 5       // we don't expect to handle more than two layers concurrently
const maxSupportedN = 1073741824 // higher values result in an overflow

var (
	errGenesis            = errors.New("no data about active nodes for genesis")
	errNoContextualBlocks = errors.New("no contextually valid blocks")
)

type valueProvider interface {
	Value(context.Context, types.EpochID) (uint32, error)
}

// a func to retrieve the active set size for the provided layer
// this func is assumed to be cpu intensive and hence we cache its results
type activeSetFunc func(epoch types.EpochID, view map[types.BlockID]struct{}) (map[string]uint64, error)

type signer interface {
	Sign(msg []byte) []byte
}

type goodBlocksProvider interface {
	ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error)
}

// a function to verify the message with the signature and its public key.
type verifierFunc = func(pub, msg, sig []byte) bool

// Oracle is the hare eligibility oracle
type Oracle struct {
	lock               sync.Mutex
	beacon             valueProvider
	getActiveSet       activeSetFunc
	vrfSigner          signer
	vrfVerifier        verifierFunc
	layersPerEpoch     uint16
	spacePerUnit       uint64
	vrfMsgCache        addGet
	activesCache       addGet
	genesisTotalWeight uint64
	genesisMinerWeight uint64
	blocksProvider     goodBlocksProvider
	cfg                eCfg.Config
	log.Log
}

// Returns the relative layer id that w.h.p. we have agreement on its contextually valid blocks
// safe layer is defined to be the confidence param layers prior to the provided Layer
func safeLayer(layer types.LayerID, safetyParam types.LayerID) types.LayerID {
	if layer <= types.GetEffectiveGenesis()+safetyParam { // assuming genesis is zero
		return types.GetEffectiveGenesis()
	}

	return layer - safetyParam
}

func roundedSafeLayer(layer types.LayerID, safetyParam types.LayerID,
	layersPerEpoch uint16, epochOffset types.LayerID) types.LayerID {

	sl := safeLayer(layer, safetyParam)
	if sl == types.GetEffectiveGenesis() {
		return types.GetEffectiveGenesis()
	}

	se := types.LayerID(sl.GetEpoch()) // the safe epoch

	roundedLayer := se*types.LayerID(layersPerEpoch) + epochOffset
	if sl >= roundedLayer { // the safe layer is after the rounding threshold
		return roundedLayer // round to threshold
	}

	if roundedLayer <= types.LayerID(layersPerEpoch) { // we can't go before genesis
		return types.GetEffectiveGenesis() // just return genesis
	}

	// round to the previous epoch threshold
	return roundedLayer - types.LayerID(layersPerEpoch)
}

// New returns a new eligibility oracle instance.
func New(beacon valueProvider, activeSetFunc activeSetFunc, vrfVerifier verifierFunc, vrfSigner signer,
	layersPerEpoch uint16, spacePerUnit, genesisTotalWeight, genesisMinerWeight uint64, goodBlocksProvider goodBlocksProvider,
	cfg eCfg.Config, log log.Log) *Oracle {
	vmc, e := lru.New(vrfMsgCacheSize)
	if e != nil {
		log.Panic("Could not create lru cache err=%v", e)
	}

	ac, e := lru.New(activesCacheSize)
	if e != nil {
		log.Panic("Could not create lru cache err=%v", e)
	}

	return &Oracle{
		beacon:             beacon,
		getActiveSet:       activeSetFunc,
		vrfVerifier:        vrfVerifier,
		vrfSigner:          vrfSigner,
		layersPerEpoch:     layersPerEpoch,
		spacePerUnit:       spacePerUnit,
		vrfMsgCache:        vmc,
		activesCache:       ac,
		genesisTotalWeight: genesisTotalWeight,
		genesisMinerWeight: genesisMinerWeight,
		blocksProvider:     goodBlocksProvider,
		cfg:                cfg,
		Log:                log,
	}
}

type vrfMessage struct {
	Beacon uint32
	Round  int32
	Layer  types.LayerID
}

func buildKey(l types.LayerID, r int32) [2]uint64 {
	return [2]uint64{uint64(l), uint64(r)}
}

// buildVRFMessage builds the VRF message used as input for the BLS (msg=Beacon##Layer##Round)
func (o *Oracle) buildVRFMessage(ctx context.Context, layer types.LayerID, round int32) ([]byte, error) {
	key := buildKey(layer, round)

	o.lock.Lock()
	defer o.lock.Unlock()

	// check cache
	if val, exist := o.vrfMsgCache.Get(key); exist {
		return val.([]byte), nil
	}

	// get value from beacon
	v, err := o.beacon.Value(ctx, layer.GetEpoch())
	if err != nil {
		o.WithContext(ctx).With().Error("could not get hare beacon value for epoch",
			log.Err(err),
			layer,
			layer.GetEpoch(),
			log.Int32("round", round))
		return nil, err
	}

	// marshal message
	var w bytes.Buffer
	msg := vrfMessage{Beacon: v, Round: round, Layer: layer}
	if _, err := xdr.Marshal(&w, &msg); err != nil {
		o.WithContext(ctx).With().Error("could not marshal xdr", log.Err(err))
		return nil, err
	}

	val := w.Bytes()
	o.vrfMsgCache.Add(key, val) // update cache

	return val, nil
}

func (o *Oracle) totalWeight(layer types.LayerID) (uint64, error) {
	actives, err := o.actives(layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return o.genesisTotalWeight, nil
		}

		o.With().Error("totalWeight erred while calling actives func", log.Err(err), layer)
		return 0, err
	}

	var totalWeight uint64
	for _, w := range actives {
		totalWeight += w
	}
	return totalWeight, nil
}

func (o *Oracle) minerWeight(layer types.LayerID, id types.NodeID) (uint64, error) {
	actives, err := o.actives(layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return o.genesisMinerWeight, nil
		}

		o.With().Error("minerWeight erred while calling actives func", log.Err(err), layer)
		return 0, err
	}

	w, ok := actives[id.Key]
	if !ok {
		o.With().Debug("miner is not active in specified layer",
			log.Int("active_set_size", len(actives)),
			log.String("actives", fmt.Sprintf("%v", actives)),
			layer, log.String("id.Key", id.Key),
		)
		return 0, errors.New("miner is not active in specified layer")
	}
	return w, nil
}

func calcVrfFrac(vrfSig []byte) fixed.Fixed {
	return fixed.FracFromBytes(vrfSig[:8])
}

func (o *Oracle) prepareEligibilityCheck(ctx context.Context, layer types.LayerID, round int32, committeeSize int,
	id types.NodeID, vrfSig []byte) (n int, p fixed.Fixed, vrfFrac fixed.Fixed, done bool, err error) {

	if committeeSize < 1 {
		o.Error("committee size must be positive (received %d)", committeeSize)
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, nil
	}

	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		o.Error("eligibility: could not build VRF message")
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// validate message
	if !o.vrfVerifier(id.VRFPublicKey, msg, vrfSig) {
		o.With().Info("eligibility: a node did not pass VRF signature verification",
			id,
			layer)
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, nil
	}

	// get active set size
	totalWeight, err := o.totalWeight(layer)
	if err != nil {
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// require totalWeight > 0
	if totalWeight == 0 {
		o.Warning("eligibility: total weight is zero")
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, errors.New("total weight is zero")
	}

	// calc hash & check threshold
	minerWeight, err := o.minerWeight(layer, id)
	if err != nil {
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	minerUnits := minerWeight / o.spacePerUnit
	// TODO: Consider checking/disallowing space not in round units
	totalUnits := totalWeight / o.spacePerUnit
	// TODO: If space is not in round units, only consider the amount of space in round units for the total weight

	o.With().Info("prep",
		log.Uint64("minerWeight", minerWeight),
		log.Uint64("totalWeight", totalWeight),
		log.Uint64("o.spacePerUnit", o.spacePerUnit),
	)
	n = int(minerUnits)

	// ensure miner weight fits in int
	if uint64(n) != minerUnits {
		o.Panic(fmt.Sprintf("minerUnits overflows int (%d)", minerUnits))
	}

	// calc p
	if committeeSize > int(totalUnits) {
		o.With().Warning("committee size is greater than total units",
			log.Int("committeeSize", committeeSize),
			log.Uint64("totalUnits", totalUnits),
		)
		totalUnits *= uint64(committeeSize)
		n *= committeeSize
	}
	p = fixed.DivUint64(uint64(committeeSize), totalUnits)

	if minerUnits > maxSupportedN {
		return 0, fixed.Fixed{}, fixed.Fixed{}, false,
			fmt.Errorf("miner weight exceeds supported maximum (id: %v, weight: %d, units: %d, max: %d",
				id, minerWeight, minerUnits, maxSupportedN)
	}
	return n, p, calcVrfFrac(vrfSig), false, nil
}

// Validate validates the number of eligibilities of ID on the given Layer where msg is the VRF message, sig is the role
// proof and assuming commSize as the expected committee size.
func (o *Oracle) Validate(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte, eligibilityCount uint16) (bool, error) {
	n, p, vrfFrac, done, err := o.prepareEligibilityCheck(ctx, layer, round, committeeSize, id, sig)
	if done || err != nil {
		return false, err
	}

	defer func() {
		if msg := recover(); msg != nil {
			o.With().Error("panic in Validate",
				log.String("msg", fmt.Sprint(msg)),
				log.Int("n", n),
				log.String("p", p.String()),
				log.String("vrfFrac", vrfFrac.String()),
			)
			o.Panic("%s", msg)
		}
	}()

	x := int(eligibilityCount)
	if !fixed.BinCDF(n, p, x-1).GreaterThan(vrfFrac) && vrfFrac.LessThan(fixed.BinCDF(n, p, x)) {
		return true, nil
	}
	o.With().Warning("eligibility: node did not pass vrf eligibility threshold",
		layer,
		log.Int32("round", round),
		log.Int("committee_size", committeeSize),
		id,
		log.Uint64("eligibilityCount", uint64(eligibilityCount)),
		log.Int("n", n),
		log.String("p", p.String()),
		log.String("vrfFrac", vrfFrac.String()),
		log.Int("x", x),
	)
	return false, nil
}

// CalcEligibility calculates the number of eligibilities of ID on the given Layer where msg is the VRF message, sig is
// the role proof and assuming commSize as the expected committee size.
func (o *Oracle) CalcEligibility(ctx context.Context, layer types.LayerID, round int32, committeeSize int,
	id types.NodeID, vrfSig []byte) (uint16, error) {

	n, p, vrfFrac, done, err := o.prepareEligibilityCheck(ctx, layer, round, committeeSize, id, vrfSig)
	if done {
		return 0, err
	}

	defer func() {
		if msg := recover(); msg != nil {
			o.With().Error("panic in CalcEligibility",
				layer, layer.GetEpoch(), log.Int32("round_id", round),
				log.String("msg", fmt.Sprint(msg)),
				log.Int("committeeSize", committeeSize),
				log.Int("n", n),
				log.String("p", fmt.Sprintf("%g", p.Float())),
				log.String("vrfFrac", fmt.Sprintf("%g", vrfFrac.Float())),
			)
			o.Panic("%s", msg)
		}
	}()

	o.With().Info("params",
		layer, layer.GetEpoch(), log.Int32("round_id", round),
		log.Int("committeeSize", committeeSize),
		log.Int("n", n),
		log.String("p", fmt.Sprintf("%g", p.Float())),
		log.String("vrfFrac", fmt.Sprintf("%g", vrfFrac.Float())),
	)

	for x := 0; x < n; x++ {
		if fixed.BinCDF(n, p, x).GreaterThan(vrfFrac) {
			return uint16(x), nil
		}
	}
	return uint16(n), nil
}

// Proof returns the role proof for the current Layer & Round
func (o *Oracle) Proof(ctx context.Context, layer types.LayerID, round int32) ([]byte, error) {
	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		o.WithContext(ctx).With().Error("proof: could not build vrf message", log.Err(err))
		return nil, err
	}

	return o.vrfSigner.Sign(msg), nil
}

// Returns a map of all active nodes in the specified layer id
func (o *Oracle) actives(layer types.LayerID) (map[string]uint64, error) {
	sl := roundedSafeLayer(layer, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	safeEp := sl.GetEpoch()

	o.With().Info("safe layer and epoch", sl, safeEp)
	// check genesis
	// genesis is for 3 epochs with hare since it can only count active identities found in blocks
	if safeEp < 3 {
		return nil, errGenesis
	}

	// lock until any return
	// note: no need to lock per safeEp - we do not expect many concurrent requests per safeEp (max two)
	o.lock.Lock()
	defer o.lock.Unlock()

	// check cache
	if val, exist := o.activesCache.Get(safeEp); exist {
		return val.(map[string]uint64), nil
	}

	// build a map of all blocks on the current layer
	mp, err := o.blocksProvider.ContextuallyValidBlock(sl)
	if err != nil {
		return nil, err
	}

	// no contextually valid blocks
	if len(mp) == 0 {
		o.With().Error("could not calculate hare active set size: no contextually valid blocks",
			layer,
			layer.GetEpoch(),
			log.FieldNamed("safe_layer_id", sl),
			log.FieldNamed("safe_epoch_id", safeEp))
		return nil, errNoContextualBlocks
	}

	activeMap, err := o.getActiveSet(safeEp-1, mp)
	if err != nil {
		o.With().Error("could not retrieve active set size",
			log.Err(err),
			layer,
			layer.GetEpoch(),
			log.FieldNamed("safe_layer_id", sl),
			log.FieldNamed("safe_epoch_id", safeEp))
		return nil, err
	}

	// update
	o.activesCache.Add(safeEp, activeMap)

	return activeMap, nil
}

// IsIdentityActiveOnConsensusView returns true if the provided identity is active on the consensus view derived
// from the specified layer, false otherwise.
func (o *Oracle) IsIdentityActiveOnConsensusView(edID string, layer types.LayerID) (bool, error) {
	actives, err := o.actives(layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return true, nil // all ids are active in genesis
		}

		o.With().Error("method IsIdentityActiveOnConsensusView erred while calling actives func",
			layer, log.Err(err))
		return false, err
	}
	_, exist := actives[edID]

	return exist, nil
}
