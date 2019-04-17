package eligibility

import (
	"bytes"
	"errors"
	"github.com/davecgh/go-xdr/xdr2"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"hash/fnv"
)

const genesisLayer = 0 // TODO: read from conf
const k = 25           // the confidence interval // TODO: read from config

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
	if layer >= k {
		return layer - k
	}

	return types.LayerID(genesisLayer)
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
	id types.NodeId
	layer types.LayerID
	round int32
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
	threshold := o.committeeSize / activeSetSize

	// check threshold
	h := fnv.New32()
	h.Write(sig)
	if h.Sum32() < threshold {
		log.Error("identity %v did not pass eligibility threshold %v", id, threshold)
		return false, errors.New("did not pass threshold")
	}

	return true, nil
}
