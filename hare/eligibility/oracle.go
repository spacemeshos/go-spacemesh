package eligibility

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/hashicorp/golang-lru"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/common/types"
	eCfg "github.com/spacemeshos/go-spacemesh/hare/eligibility/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"math"
	"sync"
)

const vrfMsgCacheSize = 20 // numRounds per layer is <= 2. numConcurrentLayers<=10 (typically <=2) so numRounds*numConcurrentLayers <= 2*10 = 20 is a good upper bound
const activesCacheSize = 5 // we don't expect to handle more than two layers concurrently

var (
	errGenesis = errors.New("no data about active nodes for genesis")
)

type valueProvider interface {
	Value(context.Context, types.EpochID) (uint32, error)
}

type signer interface {
	Sign(msg []byte) ([]byte, error)
}

type atxProvider interface {
	// ActiveSetFromBlocks gets the active set (node IDs) for a set of blocks
	ActiveSetFromBlocks(targetEpoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error)
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
type verifierFunc = func(msg, sig, pub []byte) (bool, error)

// Oracle is the hare eligibility oracle
type Oracle struct {
	lock                 sync.Mutex
	beacon               valueProvider
	atxdb                atxProvider
	meshdb               meshProvider
	vrfSigner            signer
	vrfVerifier          verifierFunc
	layersPerEpoch       uint16
	vrfMsgCache          addGet
	activesCache         addGet
	genesisActiveSetSize int
	hDist                int
	cfg                  eCfg.Config
	log.Log
}

// Returns a range of safe layers that should be used to construct the Hare active set for a given target layer
// Safe layer is a layer prior to the input layer on which w.h.p. we have agreement (i.e., on its contextually valid
// blocks), defined to be confidence param number of layers prior to the input layer.
func safeLayerRange(
	targetLayer types.LayerID,
	safetyParam types.LayerID,
	layersPerEpoch types.LayerID,
	epochOffset types.LayerID,
) (safeLayerStart, safeLayerEnd types.LayerID) {
	// prevent overflow
	if targetLayer <= safetyParam {
		return types.GetEffectiveGenesis(), types.GetEffectiveGenesis()
	}
	safeLayer := targetLayer - safetyParam
	safeEpoch := safeLayer.GetEpoch()
	safeLayerStart = safeEpoch.FirstLayer()
	safeLayerEnd = safeLayerStart + epochOffset

	// If the safe layer is in the first epochOffset layers of an epoch,
	// return a range from the beginning of the previous epoch
	if safeLayer < safeLayerEnd {
		// prevent overflow
		if safeLayerStart <= layersPerEpoch {
			return types.GetEffectiveGenesis(), types.GetEffectiveGenesis()
		}
		safeLayerStart = safeLayerStart - layersPerEpoch
		safeLayerEnd = safeLayerEnd - layersPerEpoch
	}

	// If any portion of the range is in the genesis layers, just return the effective genesis
	if safeLayerStart <= types.GetEffectiveGenesis() {
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
	layersPerEpoch uint16,
	genesisActiveSet int,
	hDist int,
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
		beacon:               beacon,
		atxdb:                atxdb,
		meshdb:               meshdb,
		vrfVerifier:          vrfVerifier,
		vrfSigner:            vrfSigner,
		layersPerEpoch:       layersPerEpoch,
		vrfMsgCache:          vmc,
		activesCache:         ac,
		genesisActiveSetSize: genesisActiveSet,
		hDist:                hDist,
		cfg:                  cfg,
		Log:                  logger,
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

func (o *Oracle) activeSetSize(ctx context.Context, layer types.LayerID) (uint32, error) {
	actives, err := o.actives(ctx, layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return uint32(o.genesisActiveSetSize), nil
		}

		o.WithContext(ctx).With().Error("error calling actives func", log.Err(err), layer)
		return 0, err
	}

	return uint32(len(actives)), nil
}

// Eligible checks if ID is eligible on the given Layer where msg is the VRF message, sig is the role proof and assuming
// commSize as the expected committee size
func (o *Oracle) Eligible(
	ctx context.Context,
	layer types.LayerID,
	round int32,
	committeeSize int,
	id types.NodeID,
	sig []byte,
) (bool, error) {
	logger := o.WithContext(ctx).WithFields(
		layer,
		id,
		log.Int32("round", round),
		log.Int("committee_size", committeeSize))
	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		logger.Error("eligibility: could not build vrf message")
		return false, err
	}

	// validate message
	res, err := o.vrfVerifier(msg, sig, id.VRFPublicKey)
	if err != nil {
		logger.With().Error("eligibility: vrf verification failed", log.Err(err))
		return false, err
	}
	if !res {
		logger.Info("eligibility: node did not pass vrf signature verification")
		return false, nil
	}

	// get active set size
	activeSetSize, err := o.activeSetSize(ctx, layer)
	if err != nil {
		return false, err
	}

	// this should never happen, because we should have already gotten an error, above
	if activeSetSize == 0 {
		logger.Warning("eligibility: active set size is zero")
		return false, errors.New("active set size is zero")
	}

	// calc hash & check threshold
	sha := sha256.Sum256(sig)
	shaUint32 := binary.LittleEndian.Uint32(sha[:4])
	// avoid division (no floating point) & do operations on uint64 to avoid overflow
	if uint64(activeSetSize)*uint64(shaUint32) > uint64(committeeSize)*uint64(math.MaxUint32) {
		logger.With().Info("eligibility: node did not pass vrf eligibility threshold",
			log.Uint32("active_set_size", activeSetSize))
		return false, nil
	}

	// lower or equal
	return true, nil
}

// Proof returns the role proof for the current Layer & Round
func (o *Oracle) Proof(ctx context.Context, layer types.LayerID, round int32) ([]byte, error) {
	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		o.WithContext(ctx).With().Error("proof: could not build vrf message", log.Err(err))
		return nil, err
	}

	sig, err := o.vrfSigner.Sign(msg)
	if err != nil {
		o.WithContext(ctx).With().Error("proof: could not sign vrf message", log.Err(err))
		return nil, err
	}

	return sig, nil
}

// Returns a map of all active node IDs in the specified layer id
func (o *Oracle) actives(ctx context.Context, targetLayer types.LayerID) (map[string]struct{}, error) {
	logger := o.WithContext(ctx).WithFields(
		log.FieldNamed("target_layer", targetLayer),
		log.FieldNamed("target_layer_epoch", targetLayer.GetEpoch()))
	logger.Debug("hare oracle getting active set")

	// lock until any return
	// note: no need to lock per safeEp - we do not expect many concurrent requests per safeEp (max two)
	o.lock.Lock()
	defer o.lock.Unlock()

	// we can't read blocks during genesis epochs as there are none
	if targetLayer.GetEpoch().IsGenesis() {
		return nil, errGenesis
	}

	// check cache first
	if val, exist := o.activesCache.Get(targetLayer.GetEpoch()); exist {
		activeMap := val.(map[string]struct{})
		logger.With().Debug("found value in cache", log.Int("count", len(activeMap)))
		return activeMap, nil
	}

	// we first try to get the hare active set for a range of safe layers: start with the set of active blocks
	safeLayerStart, safeLayerEnd := safeLayerRange(
		targetLayer, types.LayerID(o.cfg.ConfidenceParam), types.LayerID(o.layersPerEpoch), types.LayerID(o.cfg.EpochOffset))
	logger.With().Debug("got safe layer range",
		log.FieldNamed("safe_layer_start", safeLayerStart),
		log.FieldNamed("safe_layer_end", safeLayerEnd),
		log.Uint64("confidence_param", o.cfg.ConfidenceParam),
		log.Int("epoch_offset", o.cfg.EpochOffset),
		log.Uint64("layers_per_epoch", uint64(o.layersPerEpoch)),
		log.FieldNamed("effective_genesis", types.GetEffectiveGenesis()))
	activeBlockIDs := make(map[types.BlockID]struct{})
	for layerID := safeLayerStart; layerID <= safeLayerEnd; layerID++ {
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
	hareActiveSet, err := o.atxdb.ActiveSetFromBlocks(safeLayerStart.GetEpoch(), activeBlockIDs)
	if err != nil {
		return nil, fmt.Errorf("error getting ATXs for target layer %v: %w", targetLayer, err)
	}

	if len(hareActiveSet) > 0 {
		logger.With().Debug("successfully got hare active set for layer range",
			log.Int("count", len(hareActiveSet)))
		o.activesCache.Add(targetLayer.GetEpoch(), hareActiveSet)
		return hareActiveSet, nil
	}

	// if we failed to get a Hare active set, we fall back on reading the Tortoise active set targeting this epoch
	logger.With().Warning("no hare active set for layer range, reading tortoise set for epoch instead",
		targetLayer.GetEpoch())
	atxs := o.atxdb.GetEpochAtxs(targetLayer.GetEpoch() - 1)
	logger.With().Debug("got tortoise atxs", log.Int("count", len(atxs)), targetLayer.GetEpoch())
	if len(atxs) == 0 {
		return nil, fmt.Errorf("empty active set for layer %v in non-genesis epoch %v",
			targetLayer, targetLayer.GetEpoch())
	}

	// extract the nodeIDs
	activeMap := make(map[string]struct{}, len(atxs))
	for _, atxid := range atxs {
		atx, err := o.atxdb.GetAtxHeader(atxid)
		if err != nil {
			return nil, fmt.Errorf("inconsistent state: error getting atx header %v for target layer %v: %w", atxid, targetLayer, err)
		}
		activeMap[atx.NodeID.Key] = struct{}{}
	}
	logger.With().Debug("got tortoise active set", log.Int("count", len(activeMap)), targetLayer.GetEpoch())

	// update cache and return
	o.activesCache.Add(targetLayer.GetEpoch(), activeMap)
	return activeMap, nil
}

// IsIdentityActiveOnConsensusView returns true if the provided identity is active on the consensus view derived
// from the specified layer, false otherwise.
func (o *Oracle) IsIdentityActiveOnConsensusView(ctx context.Context, edID string, layer types.LayerID) (bool, error) {
	actives, err := o.actives(ctx, layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return true, nil // all ids are active in genesis
		}

		o.WithContext(ctx).With().Error("method IsIdentityActiveOnConsensusView erred while calling actives func",
			layer, log.Err(err))
		return false, err
	}
	_, exist := actives[edID]

	return exist, nil
}
