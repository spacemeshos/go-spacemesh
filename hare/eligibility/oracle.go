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
	Value(layer types.LayerID) (int, error)
}

type activeSetProvider interface {
	GetActiveSetSize(layer types.LayerID) (uint32, error)
}

type Verifier interface {
	Verify(msg, sig []byte) (bool, error)
}

// Oracle is the hare eligibility oracle
type Oracle struct {
	beacon            valueProvider
	activeSetProvider activeSetProvider
	vrf               Verifier
}

// Returns the relative layer that w.h.p. we have agreement on its view
// safe is defined to be k layers prior to the provided layer
func safeLayer(layer types.LayerID) types.LayerID {
	if layer > k { // assuming genesis is zero
		return layer - k
	}

	return config.Genesis
}

func New(beacon valueProvider, asProvider activeSetProvider, vrf Verifier) *Oracle {
	return &Oracle{
		beacon:            beacon,
		activeSetProvider: asProvider,
		vrf:               vrf,
	}
}

type vrfMessage struct {
	beacon int
	id     types.NodeId
	layer  types.LayerID
	round  int32
}

// BuildVRFMessage builds the VRF message used as input for the BLS (msg=beacon##id##layer##round)
func (o *Oracle) buildVRFMessage(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
	v, err := o.beacon.Value(safeLayer(layer))
	if err != nil {
		log.Error("Could not get beacon value: %v", err)
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

// Eligible checks if id is eligible on the given layer where msg is the VRF message, sig is the role proof and assuming commSize as the expected committee size
func (o *Oracle) Eligible(layer types.LayerID, round int32, committeeSize int, id types.NodeId, sig []byte) (bool, error) {
	msg, err := o.buildVRFMessage(id, layer, round)

	if err != nil {
		log.Error("Could not build VRF message")
		return false, err
	}

	// validate message
	if res, err := o.vrf.Verify(msg, sig); !res {
		log.Error("VRF verification failed: %v", err)
		return false, err
	}

	// get active set size
	activeSetSize, err := o.activeSetProvider.GetActiveSetSize(safeLayer(layer))
	if err != nil {
		log.Error("Could not get active set size: %v", err)
		return false, err
	}

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
