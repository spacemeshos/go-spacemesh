package eligibility

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"github.com/nullstyle/go-xdr/xdr3"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"math"
)

const k = types.LayerID(25) // the confidence interval // TODO: read from config

type valueProvider interface {
	Value(layer types.LayerID) (uint32, error)
}

type activeSetProvider interface {
	ActiveSetSize(ech types.EpochId) uint32
}

type Signer interface {
	Sign(msg []byte) ([]byte, error)
}

type VerifierFunc = func(msg, sig, pub []byte) (bool, error)

// Oracle is the hare eligibility oracle
type Oracle struct {
	beacon            valueProvider
	activeSetProvider activeSetProvider
	vrfSigner         Signer
	vrfVerifier       VerifierFunc
	layersPerEpoch    uint16
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
func New(beacon valueProvider, asProvider activeSetProvider, vrfVerifier VerifierFunc, vrfSigner Signer, layersPerEpoch uint16) *Oracle {
	return &Oracle{
		beacon:            beacon,
		activeSetProvider: asProvider,
		vrfVerifier:       vrfVerifier,
		vrfSigner:         vrfSigner,
		layersPerEpoch:    layersPerEpoch,
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
		log.Error("Could not get Beacon value: %v", err)
		return nil, err
	}

	var w bytes.Buffer
	msg := vrfMessage{v, id, layer, round}
	_, err = xdr.Marshal(&w, &msg)
	if err != nil {
		log.Error("Fatal: could not marshal xdr")
		return nil, err
	}

	return w.Bytes(), nil
}

func (o *Oracle) activeSetSize(layer types.LayerID) uint32 {
	ep := safeLayer(layer).GetEpoch(o.layersPerEpoch)
	if ep == 0 {
		return 5 // TODO: agree on the inception problem
		// return o.activeSetProvider.ActiveSetSize(0)
	}
	return o.activeSetProvider.ActiveSetSize(ep - 1)
}

// Eligible checks if Id is eligible on the given Layer where msg is the VRF message, sig is the role proof and assuming commSize as the expected committee size
func (o *Oracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	msg, err := o.buildVRFMessage(id, layer, round)
	if err != nil {
		log.Error("Could not build VRF message")
		return false, err
	}

	// validate message
	res, err := o.vrfVerifier(msg, sig, id.VRFPublicKey)
	if err != nil {
		log.Error("VRF verification failed: %v", err)
		return false, err
	}
	if !res {
		log.Warning("Id %v did not pass VRF verification", id.Key)
		return false, nil
	}

	// get active set size
	activeSetSize := o.activeSetSize(layer)

	// require activeSetSize > 0
	if activeSetSize == 0 {
		log.Error("Active set size is zero")
		return false, errors.New("active set size is zero")
	}

	// calc hash & check threshold
	sha := sha256.Sum256(sig)
	shaUint32 := binary.LittleEndian.Uint32(sha[:4])
	// avoid division (no floating point) & do operations on uint64 to avoid overflow
	if uint64(activeSetSize)*uint64(shaUint32) > uint64(committeeSize)*uint64(math.MaxUint32) {
		log.Error("identity %v did not pass eligibility committeeSize=%v activeSetSize=%v", id, committeeSize, activeSetSize)
		return false, nil
	}

	// lower or equal
	return true, nil
}

// Proof returns the role proof for the current Layer & Round
func (o *Oracle) Proof(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	msg, err := o.buildVRFMessage(id, layer, round)
	if err != nil {
		log.Error("Proof: could not build VRF message err=%v", err)
		return nil, err
	}

	sig, err := o.vrfSigner.Sign(msg)
	if err != nil {
		log.Error("Proof: could not sign VRF message err=%v", err)
		return nil, err
	}

	return sig, nil
}
