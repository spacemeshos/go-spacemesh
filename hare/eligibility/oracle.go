package eligibility

import (
	"bytes"
	"context"
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

const (
	vrfMsgCacheSize      = 20 // numRounds per layer is <= 2. numConcurrentLayers<=10 (typically <=2) so numRounds*numConcurrentLayers <= 2*10 = 20 is a good upper bound
	activesCacheSize     = 5  // we don't expect to handle more than two layers concurrently
	cacheWarmingDistance = 2  // the number of layers before a safe epoch boundary that we warm the active set cache
)

var (
	errGenesis = errors.New("no data about active nodes for genesis")
)

type valueProvider interface {
	Value(layer types.LayerID) (uint32, error)
}

// a func to retrieve the active set size for the provided layer
// this func is assumed to be cpu intensive and hence we cache its results
type activeSetFunc func(context.Context, types.EpochID, map[types.BlockID]struct{}) (map[string]struct{}, error)

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
	lastSafeEpoch        types.EpochID
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
	if _, err = xdr.Marshal(&w, &msg); err != nil {
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

		o.WithContext(ctx).With().Error("activeSetSize erred while calling actives func", log.Err(err), layer)
		return 0, err
	}

	return uint32(len(actives)), nil
}

// Eligible checks if ID is eligible on the given Layer where msg is the VRF message, sig is the role proof and assuming commSize as the expected committee size
func (o *Oracle) Eligible(ctx context.Context, layer types.LayerID, round int32, committeeSize int, id types.NodeID, sig []byte) (bool, error) {
	o.WithContext(ctx).With().Debug("hare oracle checking eligibility")
	defer func() {
		o.WithContext(ctx).With().Debug("hare oracle eligibility check complete")
	}()
	msg, err := o.buildVRFMessage(ctx, layer, round)
	if err != nil {
		o.Error("eligibility: could not build vrf message")
		return false, err
	}

	// validate message
	if !o.vrfVerifier(id.VRFPublicKey, msg, sig) {
		o.With().Info("eligibility: a node did not pass vrf signature verification",
			id,
			layer)
		return false, nil
	}

	// get active set size
	activeSetSize, err := o.activeSetSize(ctx, layer)
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

	return o.vrfSigner.Sign(msg), nil
}

// Returns a map of all active nodes in the specified layer id
func (o *Oracle) actives(ctx context.Context, layer types.LayerID) (map[string]struct{}, error) {
	sl := roundedSafeLayer(layer, types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	safeEp := sl.GetEpoch()
	logger := o.WithContext(ctx).WithFields(
		log.FieldNamed("safe_layer", sl),
		log.FieldNamed("safe_epoch", safeEp))
	logger.With().Info("safe layer and epoch")

	getBlocksAndActiveSet := func(ctx context.Context, safeLayer types.LayerID) (map[string]struct{}, error) {
		safeEpoch := safeLayer.GetEpoch()
		logger := o.WithContext(ctx).WithFields(
			log.FieldNamed("safe_layer", safeLayer),
			log.FieldNamed("safe_epoch", safeEpoch))

		// try a range of safe layers (in case there are no valid blocks for one layer)
		var mp map[types.BlockID]struct{}
		for candidateLayer := safeLayer; candidateLayer < safeLayer.Add(10); candidateLayer++ {
			logger := logger.WithFields(log.FieldNamed("candidate_safe_layer", candidateLayer))
			logger.Debug("trying candidate safe layer")

			// build a map of all blocks in the safe layer
			mpInner, err := o.blocksProvider.ContextuallyValidBlock(candidateLayer)
			if err != nil {
				return nil, err
			}
			mp = mpInner
			logger.With().Debug("contextually valid blocks in candidate safe layer",
				log.Int("count", len(mpInner)))
			if len(mp) == 0 {
				logger.Warning("no contextually valid blocks in candidate safe layer")
			} else {
				break
			}
		}

		// no contextually valid blocks: for now we just fall back on an empty active set. this will go away when we
		// upgrade hare eligibility to use the tortoise beacon.
		if len(mp) == 0 {
			logger.With().Error("no contextually valid blocks in any candidate safe layer, using active set of size zero",
				layer,
				layer.GetEpoch(),
				log.FieldNamed("safe_layer_id", sl),
				log.FieldNamed("safe_epoch_id", safeEpoch))
			return nil, nil
		}

		// the active set targeting epoch N is the active set as epoch N-1
		activeMap, err := o.getActiveSet(ctx, safeEpoch-1, mp)
		if err != nil {
			return nil, err
		}
		logger.With().Debug("active set for safe epoch", log.Int("active_set_size", len(activeMap)))
		return activeMap, nil
	}

	// lock until any return
	// note: no need to lock per safeEp - we do not expect many concurrent requests per safeEp (max two)
	o.lock.Lock()
	defer o.lock.Unlock()

	// check if we'll soon cross into a new safe epoch
	// TODO: this won't trigger during sync/while not synced, nor if hare doesn't run for another reason.
	slNext := roundedSafeLayer(layer.Add(cacheWarmingDistance), types.LayerID(o.cfg.ConfidenceParam), o.layersPerEpoch, types.LayerID(o.cfg.EpochOffset))
	if safeEp != slNext.GetEpoch() && o.lastSafeEpoch < slNext.GetEpoch() {
		logger := logger.WithFields(log.FieldNamed("safe_epoch_next", slNext.GetEpoch()))
		logger.Info("warming active set cache before new safe epoch boundary")

		// make sure we only run once for this epoch. this is thread safe since we are inside an exclusive lock.
		o.lastSafeEpoch = slNext.GetEpoch()

		go func(ctx context.Context) {
			logger := o.WithContext(ctx).WithFields(log.FieldNamed("safe_epoch_next", slNext.GetEpoch()))
			if activeMap, err := getBlocksAndActiveSet(ctx, slNext); err != nil {
				logger.With().Error("error warming active set cache for next safe epoch", log.Err(err))
			} else {
				// note: this may block on the outer method if that's still holding the lock, but there's no
				// possibility of deadlock since it does not block on this goroutine.
				o.lock.Lock()
				defer o.lock.Unlock()
				o.activesCache.Add(slNext.GetEpoch(), activeMap)

				logger.With().Info("successfully warmed active set cache for next safe epoch",
					log.Int("active_set_size", len(activeMap)))
			}
		}(log.WithNewSessionID(ctx))
	}

	// check genesis
	// genesis is for 3 epochs with hare since it can only count active identities found in blocks
	if safeEp < 3 {
		return nil, errGenesis
	}

	// check cache
	if val, exist := o.activesCache.Get(safeEp); exist {
		logger.With().Debug("read active map from cache for safe epoch",
			log.Int("active_set_size", len(val.(map[string]struct{}))))
		return val.(map[string]struct{}), nil
	}

	activeMap, err := getBlocksAndActiveSet(ctx, sl)
	if err != nil {
		return nil, err
	}
	logger.With().Debug("active map for safe epoch not found in cache, finished reading",
		log.Int("active_set_size", len(activeMap)))
	o.activesCache.Add(safeEp, activeMap)
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
