package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"time"
)

// LayerBuffer is the number of layer results we keep at a given time.
const LayerBuffer = 20

type consensusFactory func(cfg config.Config, instanceId instanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus

// Consensus represents an item that acts like a consensus process.
type Consensus interface {
	Id() instanceId
	Close()
	CloseChannel() chan struct{}

	Start() error
	SetInbox(chan *Msg)
}

// TerminationOutput represents an output of a consensus process.
type TerminationOutput interface {
	Id() instanceId
	Set() *Set
	Completed() bool
}

type orphanBlockProvider interface {
	GetUnverifiedLayerBlocks(layerId types.LayerID) ([]types.BlockID, error)
}

// checks if the collected output is valid
type outputValidationFunc func(blocks []types.BlockID) bool

// Hare is the orchestrator that starts new consensus processes and collects their output.
type Hare struct {
	Closer
	log.Log
	config config.Config

	network    NetworkService
	beginLayer chan types.LayerID

	broker *Broker

	sign Signer

	obp     orphanBlockProvider
	rolacle Rolacle

	networkDelta time.Duration

	layerLock sync.RWMutex
	lastLayer types.LayerID

	bufferSize int

	outputChan chan TerminationOutput
	mu         sync.RWMutex
	outputs    map[types.LayerID][]types.BlockID

	factory consensusFactory

	validate outputValidationFunc

	nid types.NodeId
}

// New returns a new Hare struct.
func New(conf config.Config, p2p NetworkService, sign Signer, nid types.NodeId, validate outputValidationFunc,
	syncState syncStateFunc, obp orphanBlockProvider, rolacle Rolacle,
	layersPerEpoch uint16, idProvider identityProvider, stateQ StateQuerier,
	beginLayer chan types.LayerID, logger log.Log) *Hare {
	h := new(Hare)

	h.Closer = NewCloser()

	h.Log = logger

	h.config = conf

	h.network = p2p
	h.beginLayer = beginLayer

	ev := newEligibilityValidator(rolacle, layersPerEpoch, idProvider, conf.N, conf.ExpectedLeaders, logger)
	h.broker = newBroker(p2p, ev, stateQ, syncState, layersPerEpoch, conf.LimitConcurrent, h.Closer, logger)

	h.sign = sign

	h.obp = obp
	h.rolacle = rolacle

	h.networkDelta = time.Duration(conf.WakeupDelta) * time.Second
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer // XXX: must be at least the size of `hdist`

	h.lastLayer = 0

	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.outputs = make(map[types.LayerID][]types.BlockID, h.bufferSize) //  we keep results about LayerBuffer past layers

	h.factory = func(conf config.Config, instanceId instanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus {
		return NewConsensusProcess(conf, instanceId, s, oracle, stateQ, layersPerEpoch, signing, nid, p2p, terminationReport, ev, logger)
	}

	h.validate = validate

	h.nid = nid

	return h
}

func (h *Hare) getLastLayer() types.LayerID {
	h.layerLock.RLock()
	lyr := h.lastLayer
	h.layerLock.RUnlock()
	return lyr
}

// checks if the provided id is too late/old to be requested.
func (h *Hare) outOfBufferRange(id instanceId) bool {
	lyr := h.getLastLayer()

	if lyr <= types.LayerID(h.bufferSize) {
		return false
	}

	if id < instanceId(lyr-types.LayerID(h.bufferSize)) { // bufferSize>=0
		return true
	}
	return false
}

func (h *Hare) oldestResultInBuffer() types.LayerID {
	// buffer is usually quite small so its cheap to iterate.
	// TODO: if it gets bigger change `outputs` to array.
	lyr := h.getLastLayer()
	for k := range h.outputs {
		if k < lyr {
			lyr = k
		}
	}
	return lyr
}

// ErrTooLate means that the consensus was terminated too late
var ErrTooLate = errors.New("consensus process %v finished too late")

// records the provided output.
func (h *Hare) collectOutput(output TerminationOutput) error {
	set := output.Set()
	blocks := make([]types.BlockID, len(set.values))
	i := 0
	for v := range set.values {
		blocks[i] = v
		i++
	}

	// check validity of the collected output
	if !h.validate(blocks) {
		h.Panic("Failed to validate the collected output set")
	}

	id := output.Id()

	if h.outOfBufferRange(id) {
		return ErrTooLate
	}

	h.mu.Lock()
	if len(h.outputs) >= h.bufferSize {
		delete(h.outputs, h.oldestResultInBuffer())
	}
	h.outputs[types.LayerID(id)] = blocks
	h.mu.Unlock()

	return nil
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new CPs.
func (h *Hare) onTick(id types.LayerID) {
	h.layerLock.Lock()
	if id > h.lastLayer {
		h.lastLayer = id
	}

	// call to start the calculation of active set size beforehand
	go h.rolacle.IsIdentityActiveOnConsensusView(h.nid.Key, id)

	h.layerLock.Unlock()
	h.Debug("hare got tick, sleeping for %v", h.networkDelta)
	ti := time.NewTimer(h.networkDelta)
	select {
	case <-ti.C:
		break // keep going
	case <-h.CloseChannel():
		// closed while waiting the delta
		return
	}

	h.Debug("get hare results")
	// retrieve set form orphan blocks
	blocks, err := h.obp.GetUnverifiedLayerBlocks(h.lastLayer)
	if err != nil {
		h.Error("No blocks for consensus on layer %v %v", id, err)
		return
	}

	h.Debug("received %v new blocks ", len(blocks))
	set := NewEmptySet(len(blocks))
	for _, b := range blocks {
		set.Add(b)
	}

	instId := instanceId(id)
	c, err := h.broker.Register(instId)
	if err != nil {
		h.Warning("Could not register CP for layer %v on broker err=%v", id, err)
		return
	}
	cp := h.factory(h.config, instId, set, h.rolacle, h.sign, h.network, h.outputChan)
	cp.SetInbox(c)
	e := cp.Start()
	if e != nil {
		h.Error("Could not start consensus process %v", e.Error())
		h.broker.Unregister(cp.Id())
		return
	}
	metrics.TotalConsensusProcesses.Add(1)
}

var (
	// ErrTooOld means the requested result has already been evacuated from the buffer because the layer is too old
	ErrTooOld   = errors.New("layer has already been evacuated from buffer")
	errNoResult = errors.New("no result for the requested layer")
)

// GetResult returns the hare output for the provided range.
// Returns error iff the request for the upper is too old.
func (h *Hare) GetResult(lid types.LayerID) ([]types.BlockID, error) {

	if h.outOfBufferRange(instanceId(lid)) {
		return nil, ErrTooOld
	}

	h.mu.RLock()
	blks, ok := h.outputs[lid]
	if !ok {
		h.mu.RUnlock()
		return nil, errNoResult
	}

	h.mu.RUnlock()
	return blks, nil
}

// listens to outputs arriving from consensus processes.
func (h *Hare) outputCollectionLoop() {
	for {
		select {
		case out := <-h.outputChan:
			if out.Completed() { // CP completed, collect the output
				err := h.collectOutput(out)
				if err != nil {
					h.Warning("Err collecting output from hare err: %v", err)
				}
			}

			// anyway, unregister from broker
			h.broker.Unregister(out.Id()) // unregister from broker after termination
			metrics.TotalConsensusProcesses.Add(-1)
		case <-h.CloseChannel():
			return
		}
	}
}

// listens to new layers.
func (h *Hare) tickLoop() {
	for {
		select {
		case layer := <-h.beginLayer:
			go h.onTick(layer)
		case <-h.CloseChannel():
			return
		}
	}
}

// Start starts listening for layers and outputs.
func (h *Hare) Start() error {
	h.Log.Info("Starting %v", protoName)
	err := h.broker.Start()
	if err != nil {
		return err
	}

	go h.tickLoop()
	go h.outputCollectionLoop()

	return nil
}
