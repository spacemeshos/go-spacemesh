package hare

import (
	"errors"
	"fmt"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"time"
)

// LayerBuffer is the number of layer results we keep at a given time.
const LayerBuffer = 20

type consensusFactory func(cfg config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus

// Consensus represents a consensus
type Consensus interface {
	Id() InstanceId
	Close()
	CloseChannel() chan struct{}

	Start() error
	SetInbox(chan *Msg)
}

// TerminationOutput is a result of a process terminated with output.
type TerminationOutput interface {
	Id() InstanceId
	Set() *Set
}

type orphanBlockProvider interface {
	GetUnverifiedLayerBlocks(layerId types.LayerID) ([]types.BlockID, error)
}

// checks if the collected output is valid
type outputValidationFunc func(blocks []types.BlockID) bool

// Hare is an orchestrator that shoots consensus processes and collects their termination output
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
}

// New returns a new Hare struct.
func New(conf config.Config, p2p NetworkService, sign Signer, nid types.NodeId, validate outputValidationFunc,
	syncState syncStateFunc, obp orphanBlockProvider, rolacle Rolacle,
	layersPerEpoch uint16, idProvider IdentityProvider, stateQ StateQuerier,
	beginLayer chan types.LayerID, logger log.Log) *Hare {
	h := new(Hare)

	h.Closer = NewCloser()

	h.Log = logger

	h.config = conf

	h.network = p2p
	h.beginLayer = beginLayer

	h.broker = NewBroker(p2p, NewEligibilityValidator(rolacle, layersPerEpoch, idProvider, conf.N, conf.ExpectedLeaders, logger), stateQ, syncState, layersPerEpoch, h.Closer, logger)

	h.sign = sign

	h.obp = obp
	h.rolacle = rolacle

	h.networkDelta = time.Duration(conf.WakeupDelta) * time.Second
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer

	h.lastLayer = 0

	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.outputs = make(map[types.LayerID][]types.BlockID, h.bufferSize) //  we keep results about LayerBuffer past layers

	h.factory = func(conf config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus {
		return NewConsensusProcess(conf, instanceId, s, oracle, stateQ, layersPerEpoch, signing, nid, p2p, terminationReport, logger)
	}

	h.validate = validate

	return h
}

func (h *Hare) isTooLate(id InstanceId) bool {
	if id < InstanceId(h.oldestResultInBuffer()) { // bufferSize>=0
		return true
	}
	return false
}

func (h *Hare) oldestResultInBuffer() types.LayerID {

	h.layerLock.RLock()

	if h.lastLayer <= types.LayerID(h.bufferSize) {
		h.layerLock.RUnlock()
		return 0
	}

	lyr := h.lastLayer - types.LayerID(h.bufferSize)
	h.layerLock.RUnlock()
	return lyr
}

// ErrTooLate means that the consensus was terminated too late
var ErrTooLate = errors.New("consensus process %v finished too late")

func (h *Hare) collectOutput(output TerminationOutput) error {
	set := output.Set()
	blocks := make([]types.BlockID, len(set.values))
	i := 0
	for _, v := range set.values {
		blocks[i] = v.BlockID
		i++
	}

	// check validity of the collected output
	if !h.validate(blocks) {
		h.Panic("Failed to validate the collected output set")
	}

	id := output.Id()

	if h.isTooLate(id) {
		return ErrTooLate
	}

	h.mu.Lock()
	if len(h.outputs) == h.bufferSize {
		for k := range h.outputs {
			if h.isTooLate(InstanceId(k)) {
				delete(h.outputs, k)
			}
		}
	}
	h.outputs[types.LayerID(id)] = blocks
	h.mu.Unlock()

	return nil
}

func (h *Hare) onTick(id types.LayerID) {
	h.layerLock.Lock()
	if id > h.lastLayer {
		h.lastLayer = id
	}
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
		set.Add(Value{b})
	}

	instId := InstanceId(id)
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
	// todo: PROMETHEUS
	//metrics.TotalConsensusProcesses.Add(1)
}

var (
	// ErrTooOld is an error we return when we've been requested output about old consensus procs
	ErrTooOld = errors.New("results for that layer already deleted")
	// ErrTooEarly is what we return when the requested layer consensus is still in process
	ErrTooEarly = errors.New("results for that layer haven't arrived yet")
)

// GetResults returns the hare output for a given LayerID. returns error if we don't have results yet.
func (h *Hare) GetResult(lower types.LayerID, upper types.LayerID) ([]types.BlockID, error) {

	if h.isTooLate(InstanceId(upper)) {
		return nil, ErrTooOld
	}

	var results []types.BlockID
	h.mu.RLock()
	for id := lower; id <= upper; id++ {
		blks, ok := h.outputs[id]
		if !ok {
			continue
		}
		results = append(results, blks...)
	}
	h.mu.RUnlock()

	if len(results) == 0 {
		return nil, errors.New(fmt.Sprintf("could not get any hare results between %v %v ", lower, upper))
	}

	return results, nil
}

func (h *Hare) outputCollectionLoop() {
	for {
		select {
		case out := <-h.outputChan:
			err := h.collectOutput(out)
			if err != nil {
				h.Warning("Err collecting output from hare err: %v", err)
			}
			h.broker.Unregister(out.Id()) // unregister from broker after termination
			// todo: PROMETHEUS
			//metrics.TotalConsensusProcesses.Add(-1)
		case <-h.CloseChannel():
			return
		}
	}
}

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

// Start starts listening on layers to participate in.
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
