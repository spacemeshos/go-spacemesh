package eligibility

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"github.com/hashicorp/golang-lru"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"math"
)

const k = types.LayerID(25) // the confidence interval // TODO: read from config
const cacheSize = 5         // we don't expect to handle more than three layers concurrently

type valueProvider interface {
	Value(layer types.LayerID) (uint32, error)
}

// a func to retrieve the active set size for the provided layer
// this func is assumed to be cpu intensive and hence we cache its results
type activeSetFunc func(layer types.LayerID) (uint32, error)

type Signer interface {
	Sign(msg []byte) ([]byte, error)
}

type VerifierFunc = func(msg, sig, pub []byte) (bool, error)

// Oracle is the hare eligibility oracle
type Oracle struct {
	beacon         valueProvider
	getActiveSet   activeSetFunc
	vrfSigner      Signer
	vrfVerifier    VerifierFunc
	layersPerEpoch uint16
	cache          *lru.Cache
	log.Log
}

// Returns the relative Layer that w.h.p. we have agreement on its view
// safe is defined to be k layers prior to the provided Layer
func safeLayer(layer types.LayerID) types.LayerID {
	if layer > k { // assuming genesis is zero
		return layer - k
	}

	return config.Genesis
}

// New returns a new eligibility oracle instance
func New(beacon valueProvider, activeSetFunc activeSetFunc, vrfVerifier VerifierFunc, vrfSigner Signer, layersPerEpoch uint16, log log.Log) *Oracle {
	c, e := lru.New(cacheSize)
	if e != nil {
		log.Panic("Could not create lru cache err=%v", e)
	}

	return &Oracle{
		beacon:         beacon,
		getActiveSet:   activeSetFunc,
		vrfVerifier:    vrfVerifier,
		vrfSigner:      vrfSigner,
		layersPerEpoch: layersPerEpoch,
		cache:          c,
		Log:            log,
	}
}

type vrfMessage struct {
	Beacon uint32
	Id     types.NodeId
	Layer  types.LayerID
	Round  int32
}

// buildVRFMessage builds the VRF message used as input for the BLS (msg=Beacon##Id##Layer##Round)
func (o *Oracle) buildVRFMessage(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	v, err := o.beacon.Value(safeLayer(layer))
	if err != nil {
		o.Error("Could not get Beacon value: %v", err)
		return nil, err
	}

	var w bytes.Buffer
	msg := vrfMessage{v, id, layer, round}
	_, err = xdr.Marshal(&w, &msg)
	if err != nil {
		o.Error("Fatal: could not marshal xdr")
		return nil, err
	}

	return w.Bytes(), nil
}

func (o *Oracle) activeSetSize(layer types.LayerID) (uint32, error) {
	sl := safeLayer(layer) // calc safe layer

	// check genesis
	ep := sl.GetEpoch(o.layersPerEpoch)
	if ep == 0 {
		return 5, nil // TODO: agree on the inception problem
	}

	// check cache
	val, ok := o.cache.Get(layer)
	if ok {
		return val.(uint32), nil
	}

	setSize, err := o.getActiveSet(sl)
	if err != nil {
		o.Error("Could not retrieve active set size err=%v", err)
		return 0, err
	}

	// update
	o.cache.Add(sl, setSize)

	return setSize, nil
}

// Eligible checks if Id is eligible on the given Layer where msg is the VRF message, sig is the role proof and assuming commSize as the expected committee size
func (o *Oracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	msg, err := o.buildVRFMessage(id, layer, round)
	if err != nil {
		o.Error("Could not build VRF message")
		return false, err
	}

	// validate message
	res, err := o.vrfVerifier(msg, sig, id.VRFPublicKey)
	if err != nil {
		o.Error("VRF verification failed: %v", err)
		return false, err
	}
	if !res {
		o.With().Warning("A node did not pass VRF verification",
			log.String("id", id.ShortString()), log.Uint64("layer_id", uint64(layer)))
		return false, nil
	}

	// get active set size
	activeSetSize, err := o.activeSetSize(layer)
	if err != nil {
		return false, err
	}

	// require activeSetSize > 0
	if activeSetSize == 0 {
		o.Error("Active set size is zero")
		return false, errors.New("active set size is zero")
	}

	// calc hash & check threshold
	sha := sha256.Sum256(sig)
	shaUint32 := binary.LittleEndian.Uint32(sha[:4])
	// avoid division (no floating point) & do operations on uint64 to avoid overflow
	if uint64(activeSetSize)*uint64(shaUint32) > uint64(committeeSize)*uint64(math.MaxUint32) {
		o.With().Error("A node did not pass eligibility",
			log.String("id", id.ShortString()), log.Int("committee_size", committeeSize),
			log.Uint32("active_set_size", activeSetSize), log.Int32("round", round),
			log.Uint64("layer_id", uint64(layer)))
		return false, nil
	}

	// lower or equal
	return true, nil
}

// Proof returns the role proof for the current Layer & Round
func (o *Oracle) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	msg, err := o.buildVRFMessage(id, layer, round)
	if err != nil {
		o.Error("Proof: could not build VRF message err=%v", err)
		return nil, err
	}

	sig, err := o.vrfSigner.Sign(msg)
	if err != nil {
		o.Error("Proof: could not sign VRF message err=%v", err)
		return nil, err
	}

	return sig, nil
}
