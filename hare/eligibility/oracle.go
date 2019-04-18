package eligibility

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"hash/fnv"
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

type Oracle struct {
	committeeSize uint32
	beacon        valueProvider
	asProvider    activeSetProvider
	vrf           Verifier
}

func safeLayer(layer types.LayerID) types.LayerID {
	if layer > k {
		return layer - k
	}

	return types.LayerID(config.Genesis)
}

func New(committeeSize uint32, beacon valueProvider, asProvider activeSetProvider, vrf Verifier) *Oracle {
	return &Oracle{
		committeeSize: committeeSize,
		beacon:        beacon,
		asProvider:    asProvider,
		vrf:           vrf,
	}
}

type vrfMessage struct {
	beacon int
	id     types.NodeId
	layer  types.LayerID
	round  int32
}

func (o *Oracle) BuildVRFMessage(id types.NodeId, layer types.LayerID, round int32) ([]byte, error) {
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

func (o *Oracle) IsEligible(id types.NodeId, layer types.LayerID, msg, sig []byte) (bool, error) {
	// validate message
	if res, err := o.vrf.Verify(msg, sig); !res {
		log.Error("VRF verification failed: %v", err)
		return false, err
	}

	// calc threshold
	activeSetSize, err := o.asProvider.GetActiveSetSize(safeLayer(layer))
	if err != nil {
		log.Error("Could not get active set size: %v", err)
		return false, err
	}

	if activeSetSize == 0 { // require activeSetSize > 0
		log.Error("Active set size is zero")
		return false, errors.New("active set size is zero")
	}

	// calc hash & check threshold
	h := fnv.New32()
	h.Write(sig)
	// avoid division (no floating point) & do operations on uint64 to avoid flow
	if uint64(activeSetSize)*uint64(h.Sum32()) > uint64(o.committeeSize)*uint64(math.MaxUint32) {
		log.Error("identity %v did not pass eligibility committeeSize=%v activeSetSize=%v", id, o.committeeSize, activeSetSize)
		return false, errors.New("did not pass eligibility threshold")
	}

	return true, nil
}
