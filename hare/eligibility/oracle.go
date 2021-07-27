package eligibility

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"

	lru "github.com/hashicorp/golang-lru"
	xdr "github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/fixed"
	"github.com/spacemeshos/go-spacemesh/common/types"
	eCfg "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
)

const vrfMsgCacheSize = 20       // numRounds per layer is <= 2. numConcurrentLayers<=10 (typically <=2) so numRounds*numConcurrentLayers <= 2*10 = 20 is a good upper bound
const activesCacheSize = 5       // we don't expect to handle more than two layers concurrently
const maxSupportedN = 1073741824 // higher values result in an overflow

type valueProvider interface {
	Value(context.Context, types.EpochID) (uint32, error)
}

type signer interface {
	Sign(msg []byte) []byte
}

type atxProvider interface {
	// GetMinerWeightsInEpochFromView gets the active set (node IDs) for a set of blocks
	GetMinerWeightsInEpochFromView(targetEpoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]uint64, error)
	// GetEpochAtxs is used to get the tortoise active set for an epoch
	GetEpochAtxs(types.EpochID) []types.ATXID
	// GetAtxHeader returns the ATX header for an ATX ID
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
}

type meshProvider interface {
	// LayerContextuallyValidBlocks returns the set of contextually valid blocks from the mesh for a given layer
	LayerContextuallyValidBlocks(types.LayerID) (map[types.BlockID]struct{}, error)
}

// a function to verify the message with the signature and its public key.
type verifierFunc = func(pub, msg, sig []byte) bool

// Oracle is the hare eligibility oracle
type Oracle struct {
	lock           sync.Mutex
	beacon         valueProvider
	atxdb          atxProvider
	meshdb         meshProvider
	vrfSigner      signer
	vrfVerifier    verifierFunc
	layersPerEpoch uint32
	vrfMsgCache    addGet
	activesCache   addGet
	cfg            eCfg.Config
	log.Log
}

// Returns a range of safe layers that should be used to construct the Hare active set for a given target layer
// Safe layer is a layer prior to the input layer on which w.h.p. we have agreement (i.e., on its contextually valid
// blocks), defined to be confidence param number of layers prior to the input layer.
func safeLayerRange(
	targetLayer types.LayerID,
	safetyParam,
	layersPerEpoch,
	epochOffset uint32,
) (safeLayerStart, safeLayerEnd types.LayerID) {
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

// New returns a new eligibility oracle instance.
func New(
	beacon valueProvider,
	atxdb atxProvider,
	meshdb meshProvider,
	vrfVerifier verifierFunc,
	vrfSigner signer,
	layersPerEpoch uint32,
	cfg eCfg.Config,
	logger log.Log) *Oracle {
	vmc, err := lru.New(vrfMsgCacheSize)
	if err != nil {
		logger.With().Panic("could not create lru cache", log.Err(err))
	}

	ac, err := lru.New(activesCacheSize)
	if err != nil {
		logger.With().Panic("could not create lru cache", log.Err(err))
	}

	return &Oracle{
		beacon:         beacon,
		atxdb:          atxdb,
		meshdb:         meshdb,
		vrfVerifier:    vrfVerifier,
		vrfSigner:      vrfSigner,
		layersPerEpoch: layersPerEpoch,
		vrfMsgCache:    vmc,
		activesCache:   ac,
		cfg:            cfg,
		Log:            logger,
	}
}

type vrfMessage struct {
	Beacon uint32
	Round  int32
	Layer  types.LayerID
}

func buildKey(l types.LayerID, r int32) [2]uint64 {
	return [2]uint64{uint64(l.Uint32()), uint64(r)}
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

func (o *Oracle) totalWeight(ctx context.Context, layer types.LayerID) (uint64, error) {
	actives, err := o.actives(ctx, layer)
	if err != nil {
		o.WithContext(ctx).With().Error("totalWeight erred while calling actives func", log.Err(err), layer)
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
	logger := o.WithContext(ctx).WithFields(
		layer,
		id,
		log.Int32("round", round),
		log.Int("committee_size", committeeSize))

	if committeeSize < 1 {
		logger.Error("committee size must be positive (received %d)", committeeSize)
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, nil
	}

	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		logger.Error("eligibility: could not build vrf message")
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// validate message
	if !o.vrfVerifier(id.VRFPublicKey, msg, vrfSig) {
		logger.With().Info("eligibility: a node did not pass vrf signature verification",
			id,
			layer)
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, nil
	}

	// get active set size
	totalWeight, err := o.totalWeight(ctx, layer)
	if err != nil {
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	// require totalWeight > 0
	if totalWeight == 0 {
		logger.Warning("eligibility: total weight is zero")
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, errors.New("total weight is zero")
	}

	// calc hash & check threshold
	minerWeight, err := o.minerWeight(ctx, layer, id)
	if err != nil {
		return 0, fixed.Fixed{}, fixed.Fixed{}, true, err
	}

	logger.With().Info("prep",
		log.Uint64("minerWeight", minerWeight),
		log.Uint64("totalWeight", totalWeight),
	)
	n = int(minerWeight)

	// ensure miner weight fits in int
	if uint64(n) != minerWeight {
		logger.Panic(fmt.Sprintf("minerWeight overflows int (%d)", minerWeight))
	}

	// calc p
	if committeeSize > int(totalWeight) {
		logger.With().Warning("committee size is greater than total weight",
			log.Int("committeeSize", committeeSize),
			log.Uint64("totalWeight", totalWeight),
		)
		totalWeight *= uint64(committeeSize)
		n *= committeeSize
	}
	p = fixed.DivUint64(uint64(committeeSize), totalWeight)

	if minerWeight > maxSupportedN {
		return 0, fixed.Fixed{}, fixed.Fixed{}, false,
			fmt.Errorf("miner weight exceeds supported maximum (id: %v, weight: %d, max: %d",
				id, minerWeight, maxSupportedN)
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

// Returns a map of all active node IDs in the specified layer id
func (o *Oracle) actives(ctx context.Context, targetLayer types.LayerID) (map[string]uint64, error) {
	logger := o.WithContext(ctx).WithFields(
		log.FieldNamed("target_layer", targetLayer),
		log.FieldNamed("target_layer_epoch", targetLayer.GetEpoch()))
	logger.Debug("hare oracle getting active set")

	// lock until any return
	// note: no need to lock per safeEp - we do not expect many concurrent requests per safeEp (max two)
	o.lock.Lock()
	defer o.lock.Unlock()

	// we first try to get the hare active set for a range of safe layers: start with the set of active blocks
	safeLayerStart, safeLayerEnd := safeLayerRange(
		targetLayer, o.cfg.ConfidenceParam, o.layersPerEpoch, o.cfg.EpochOffset)
	logger.With().Debug("safe layer range",
		log.FieldNamed("safe_layer_start", safeLayerStart),
		log.FieldNamed("safe_layer_end", safeLayerEnd),
		log.FieldNamed("safe_layer_start_epoch", safeLayerStart.GetEpoch()),
		log.FieldNamed("safe_layer_end_epoch", safeLayerEnd.GetEpoch()),
		log.Uint32("confidence_param", o.cfg.ConfidenceParam),
		log.Uint32("epoch_offset", o.cfg.EpochOffset),
		log.Uint64("layers_per_epoch", uint64(o.layersPerEpoch)),
		log.FieldNamed("effective_genesis", types.GetEffectiveGenesis()))

	// check cache first
	// as long as epochOffset < layersPerEpoch, we expect safeLayerStart and safeLayerEnd to be in the same epoch.
	// if not, don't attempt to cache on the basis of a single epoch since the safe layer range will span multiple
	// epochs.
	if safeLayerStart.GetEpoch() == safeLayerEnd.GetEpoch() {
		if val, exist := o.activesCache.Get(safeLayerStart.GetEpoch()); exist {
			activeMap := val.(map[string]uint64)
			logger.With().Debug("found value in cache for safe layer start epoch",
				log.FieldNamed("safe_layer_start", safeLayerStart),
				log.FieldNamed("safe_layer_start_epoch", safeLayerStart.GetEpoch()),
				log.Int("count", len(activeMap)))
			return activeMap, nil
		}
		logger.With().Debug("no value in cache for safe layer start epoch",
			log.FieldNamed("safe_layer_start", safeLayerStart),
			log.FieldNamed("safe_layer_start_epoch", safeLayerStart.GetEpoch()))
	} else {
		// this is an error because GetMinerWeightsInEpochFromView, below, will probably also fail if it's true
		// TODO: should we panic or return an error instead?
		logger.With().Error("safe layer range spans multiple epochs, not caching active set results",
			log.FieldNamed("safe_layer_start", safeLayerStart),
			log.FieldNamed("safe_layer_end", safeLayerEnd),
			log.FieldNamed("safe_layer_start_epoch", safeLayerStart.GetEpoch()),
			log.FieldNamed("safe_layer_end_epoch", safeLayerEnd.GetEpoch()))
	}

	activeBlockIDs := make(map[types.BlockID]struct{})
	for layerID := safeLayerStart; !layerID.After(safeLayerEnd); layerID = layerID.Add(1) {
		layerBlockIDs, err := o.meshdb.LayerContextuallyValidBlocks(layerID)
		if err != nil {
			return nil, fmt.Errorf("error getting active blocks for layer %v for target layer %v: %w",
				layerID, targetLayer, err)
		}
		for blockID := range layerBlockIDs {
			activeBlockIDs[blockID] = struct{}{}
		}
	}
	logger.With().Debug("got blocks in safe layer range, reading active set from blocks",
		log.Int("count", len(activeBlockIDs)))

	// now read the set of ATXs referenced by these blocks
	// TODO: can the set of blocks ever span multiple epochs?
	hareActiveSet, err := o.atxdb.GetMinerWeightsInEpochFromView(safeLayerStart.GetEpoch(), activeBlockIDs)
	if err != nil {
		return nil, fmt.Errorf("error getting ATXs for target layer %v: %w", targetLayer, err)
	}

	if len(hareActiveSet) > 0 {
		logger.With().Debug("successfully got hare active set for layer range",
			log.Int("count", len(hareActiveSet)))
		o.activesCache.Add(safeLayerStart.GetEpoch(), hareActiveSet)
		return hareActiveSet, nil
	}

	// if we failed to get a Hare active set, we fall back on reading the Tortoise active set targeting this epoch
	// TODO: do we want to cache tortoise active set too?
	if safeLayerStart.GetEpoch().IsGenesis() {
		logger.With().Info("no hare active set for genesis layer range, reading tortoise set for epoch instead",
			targetLayer.GetEpoch())
	} else {
		logger.With().Warning("no hare active set for layer range, reading tortoise set for epoch instead",
			targetLayer.GetEpoch())
	}
	atxs := o.atxdb.GetEpochAtxs(targetLayer.GetEpoch() - 1)
	logger.With().Debug("got tortoise atxs", log.Int("count", len(atxs)))
	if len(atxs) == 0 {
		return nil, fmt.Errorf("empty active set for layer %v in epoch %v",
			targetLayer, targetLayer.GetEpoch())
	}

	// extract the nodeIDs and weights
	activeMap := make(map[string]uint64, len(atxs))
	for _, atxid := range atxs {
		atxHeader, err := o.atxdb.GetAtxHeader(atxid)
		if err != nil {
			return nil, fmt.Errorf("inconsistent state: error getting atx header %v for target layer %v: %w", atxid, targetLayer, err)
		}
		activeMap[atxHeader.NodeID.Key] = atxHeader.GetWeight()
	}
	logger.With().Debug("got tortoise active set", log.Int("count", len(activeMap)))

	// update cache and return
	o.activesCache.Add(targetLayer.GetEpoch(), activeMap)
	return activeMap, nil
}

// IsIdentityActiveOnConsensusView returns true if the provided identity is active on the consensus view derived
// from the specified layer, false otherwise.
func (o *Oracle) IsIdentityActiveOnConsensusView(ctx context.Context, edID string, layer types.LayerID) (bool, error) {
	o.WithContext(ctx).With().Debug("hare oracle checking for active identity")
	defer func() {
		o.WithContext(ctx).With().Debug("hare oracle active identity check complete")
	}()
	actives, err := o.actives(ctx, layer)
	if err != nil {
		o.WithContext(ctx).With().Error("method IsIdentityActiveOnConsensusView erred while calling actives func",
			layer, log.Err(err))
		return false, err
	}
	_, exist := actives[edID]
	return exist, nil
}
