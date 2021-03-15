package eligibility

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
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
	errGenesis            = errors.New("no data about active nodes for genesis")
	errNoContextualBlocks = errors.New("no contextually valid blocks")
)

type valueProvider interface {
	Value(layer types.LayerID) (uint32, error)
}

// a func to retrieve the active set size for the provided layer
// this func is assumed to be cpu intensive and hence we cache its results
type activeSetFunc func(epoch types.EpochID, blocks map[types.BlockID]struct{}) (map[string]struct{}, error)

type signer interface {
	Sign(msg []byte) ([]byte, error)
}

type goodBlocksProvider interface {
	ContextuallyValidBlock(layer types.LayerID) (map[types.BlockID]struct{}, error)
}

// a function to verify the message with the signature and its public key.
type verifierFunc = func(msg, sig, pub []byte) (bool, error)

// Oracle is the hare eligibility oracle
type Oracle struct {
	lock                 sync.Mutex
	beacon               valueProvider
	getActiveSet         activeSetFunc
	vrfSigner            signer
	vrfVerifier          verifierFunc
	layersPerEpoch       uint16
	vrfMsgCache          addGet
	activesCache         addGet
	genesisActiveSetSize int
	blocksProvider       goodBlocksProvider
	cfg                  eCfg.Config
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
	layersPerEpoch uint16, genesisActiveSet int, goodBlocksProvider goodBlocksProvider,
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
		beacon:               beacon,
		getActiveSet:         activeSetFunc,
		vrfVerifier:          vrfVerifier,
		vrfSigner:            vrfSigner,
		layersPerEpoch:       layersPerEpoch,
		vrfMsgCache:          vmc,
		activesCache:         ac,
		genesisActiveSetSize: genesisActiveSet,
		blocksProvider:       goodBlocksProvider,
		cfg:                  cfg,
		Log:                  log,
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
func (o *Oracle) buildVRFMessage(layer types.LayerID, round int32) ([]byte, error) {
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
		o.With().Error("Could not get hare Beacon value", log.Err(err), layer, log.Int32("round", round))
		return nil, err
	}

	// marshal message
	var w bytes.Buffer
	msg := vrfMessage{Beacon: v, Round: round, Layer: layer}
	_, err = xdr.Marshal(&w, &msg)
	if err != nil {
		o.With().Error("Fatal: could not marshal xdr", log.Err(err))
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

		o.With().Error("activeSetSize erred while calling actives func", log.Err(err), layer)
		return 0, err
	}

	return uint32(len(actives)), nil
}

// Eligible checks if ID is eligible on the given Layer where msg is the VRF message, sig is the role proof and assuming commSize as the expected committee size
func (o *Oracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (bool, error) {
	msg, err := o.buildVRFMessage(layer, round)
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
		o.With().Info("eligibility: node did not pass VRF eligibility threshold",
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
func (o *Oracle) Proof(layer types.LayerID, round int32) ([]byte, error) {
	msg, err := o.buildVRFMessage(layer, round)
	if err != nil {
		o.With().Error("proof: could not build VRF message", log.Err(err))
		return nil, err
	}

	sig, err := o.vrfSigner.Sign(msg)
	if err != nil {
		o.With().Error("proof: could not sign VRF message", log.Err(err))
		return nil, err
	}

	return sig, nil
}

// Returns a map of all active nodes in the specified layer id
func (o *Oracle) actives(layer types.LayerID) (map[string]struct{}, error) {
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
		return val.(map[string]struct{}), nil
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
