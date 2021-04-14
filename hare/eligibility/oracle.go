package eligibility

import (
	"bytes"
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
	errGenesis            = errors.New("no data about active nodes for genesis")
)

type valueProvider interface {
	Value(layer types.LayerID) (uint32, error)
}

type signer interface {
	Sign(msg []byte) ([]byte, error)
}

type atxProvider interface {
	// GetEpochAtxs is used to get the active set for an epoch
	GetEpochAtxs(types.EpochID) []types.ATXID
	GetAtxHeader(types.ATXID) (*types.ActivationTxHeader, error)
}

// a function to verify the message with the signature and its public key.
type verifierFunc = func(msg, sig, pub []byte) (bool, error)

// Oracle is the hare eligibility oracle
type Oracle struct {
	lock                 sync.Mutex
	beacon               valueProvider
	atxdb                atxProvider
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

// Returns the relative layer id that w.h.p. we have agreement on its contextually valid blocks
// safe layer is defined to be the confidence param layers prior to the provided Layer
func safeLayer(layer types.LayerID, safetyParam types.LayerID) types.LayerID {
	if layer <= types.GetEffectiveGenesis()+safetyParam { // assuming genesis is zero
		return types.GetEffectiveGenesis()
	}

	return layer - safetyParam
}

// New returns a new eligibility oracle instance.
func New(
	beacon valueProvider,
	atxdb atxProvider,
	vrfVerifier verifierFunc,
	vrfSigner signer,
	layersPerEpoch uint16,
	genesisActiveSet int,
	hDist int,
	cfg eCfg.Config,
	log log.Log) *Oracle {
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
		atxdb:				  atxdb,
		vrfVerifier:          vrfVerifier,
		vrfSigner:            vrfSigner,
		layersPerEpoch:       layersPerEpoch,
		vrfMsgCache:          vmc,
		activesCache:         ac,
		genesisActiveSetSize: genesisActiveSet,
		hDist:                hDist,
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

		o.With().Error("error calling actives func", log.Err(err), layer)
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
	// lock until any return
	// note: no need to lock per safeEp - we do not expect many concurrent requests per safeEp (max two)
	o.lock.Lock()
	defer o.lock.Unlock()

	// we can't read blocks during genesis epochs as there are none
	if layer.GetEpoch().IsGenesis() {
		return nil, errGenesis
	}

	// check cache first
	if val, exist := o.activesCache.Get(layer.GetEpoch()); exist {
		return val.(map[string]struct{}), nil
	}

	// get the active set for the epoch
	atxs := o.atxdb.GetEpochAtxs(layer.GetEpoch())
	if len(atxs) == 0 {
		return nil, fmt.Errorf("empty active set for layer %v in non-genesis epoch %v",
			layer, layer.GetEpoch())
	}

	// extract the nodeIDs
	activeMap := make(map[string]struct{}, len(atxs))
	for _, atxid := range atxs {
		atx, err := o.atxdb.GetAtxHeader(atxid)
		if err != nil {
			o.With().Error("failed to fetch atx for layer in non-genesis epoch",
				layer,
				layer.GetEpoch(),
				log.Err(err))
			return nil, err
		}
		activeMap[atx.NodeID.Key] = struct{}{}
	}

	// update cache and return
	o.activesCache.Add(layer.GetEpoch(), activeMap)
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
