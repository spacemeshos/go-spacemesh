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
	Value(layer types.LayerID) (uint32, error)
}

type signer interface {
	Sign(msg []byte) ([]byte, error)
}

type atxProvider interface {
	// ActiveSetFromBlocks gets the size of the active set (valid ATXs) for a set of blocks
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
	safeLayer := targetLayer - safetyParam
	safeEpoch := safeLayer.GetEpoch()
	safeLayerStart = safeEpoch.FirstLayer()
	safeLayerEnd = safeLayerStart + epochOffset

	// If the safe layer is in the first epochOffset layers of an epoch,
	// return a range from the beginning of the previous epoch
	if safeLayer < safeLayerEnd {
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

	// get value from Beacon
	v, err := o.beacon.Value(layer)
	if err != nil {
		o.WithContext(ctx).With().Error("could not get hare beacon value",
			log.Err(err),
			layer,
			log.Int32("round", round))
		return nil, err
	}

	// marshal message
	var w bytes.Buffer
	msg := vrfMessage{Beacon: v, Round: round, Layer: layer}
	_, err = xdr.Marshal(&w, &msg)
	if err != nil {
		o.WithContext(ctx).With().Error("could not marshal xdr", log.Err(err))
		return nil, err
	}

	val := w.Bytes()
	o.vrfMsgCache.Add(key, val) // update cache

	return val, nil
}

func (o *Oracle) activeSetSize(layer types.LayerID) (uint32, error) {
	actives, err := o.actives(layer)
	if err != nil {
		if err == errGenesis { // we are in genesis
			return uint32(o.genesisActiveSetSize), nil
		}

		o.With().Error("error calling actives func", log.Err(err), layer)
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
	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		o.Error("eligibility: could not build VRF message")
		return false, err
	}

	// validate message
	res, err := o.vrfVerifier(msg, sig, id.VRFPublicKey)
	if err != nil {
		o.Error("eligibility: VRF verification failed: %v", err)
		return false, err
	}
	if !res {
		o.With().Info("eligibility: a node did not pass VRF signature verification",
			id,
			layer)
		return false, nil
	}

	// get active set size
	activeSetSize, err := o.activeSetSize(layer)
	if err != nil {
		return false, err
	}

	// require activeSetSize > 0
	if activeSetSize == 0 {
		o.Warning("eligibility: active set size is zero")
		return false, errors.New("active set size is zero")
	}

	// calc hash & check threshold
	sha := sha256.Sum256(sig)
	shaUint32 := binary.LittleEndian.Uint32(sha[:4])
	// avoid division (no floating point) & do operations on uint64 to avoid overflow
	if uint64(activeSetSize)*uint64(shaUint32) > uint64(committeeSize)*uint64(math.MaxUint32) {
		o.With().Info("eligibility: node did not pass vrf eligibility threshold",
			id,
			log.Int("committee_size", committeeSize),
			log.Uint32("active_set_size", activeSetSize),
			log.Int32("round", round),
			layer)
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
func (o *Oracle) actives(targetLayer types.LayerID) (map[string]struct{}, error) {
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
		return val.(map[string]struct{}), nil
	}

	// we first try to get the hare active set for a range of safe layers: start with the set of active blocks
	safeLayerStart, safeLayerEnd := safeLayerRange(
		targetLayer, types.LayerID(o.cfg.ConfidenceParam), types.LayerID(o.layersPerEpoch), types.LayerID(o.cfg.EpochOffset))
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

	// now read the set of ATXs referenced by these blocks
	hareAtxs, err := o.atxdb.ActiveSetFromBlocks(safeLayerStart.GetEpoch(), activeBlockIDs)
	if err != nil {
		return nil, fmt.Errorf("error getting ATXs for target layer %v: %w", targetLayer, err)
	}

	if len(hareAtxs) > 0 {
		o.activesCache.Add(targetLayer.GetEpoch(), hareAtxs)
		return hareAtxs, nil
	}

	// if we failed to get a Hare active set, we fall back on reading the Tortoise active set targeting this epoch
	atxs := o.atxdb.GetEpochAtxs(targetLayer.GetEpoch()-1)
	if len(atxs) == 0 {
		return nil, fmt.Errorf("empty active set for layer %v in non-genesis epoch %v",
			targetLayer, targetLayer.GetEpoch())
	}

	// extract the nodeIDs
	activeMap := make(map[string]struct{}, len(atxs))
	for _, atxid := range atxs {
		atx, err := o.atxdb.GetAtxHeader(atxid)
		if err != nil {
			return nil, fmt.Errorf("error getting ATX %v for target layer %v: %w", atxid, targetLayer, err)
		}
		activeMap[atx.NodeID.Key] = struct{}{}
	}

	// update cache and return
	o.activesCache.Add(targetLayer.GetEpoch(), activeMap)
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
