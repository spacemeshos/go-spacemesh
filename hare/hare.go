package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/types"
	"sync"
	"time"
)

// Delta is the time we wait before we start processing hare messages gor the round
const Delta = time.Second // todo: add to config

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
}

// New returns a new Hare struct.
func New(conf config.Config, p2p NetworkService, sign Signer, obp orphanBlockProvider, rolacle Rolacle, beginLayer chan types.LayerID, logger log.Log) *Hare {
	h := new(Hare)

	h.Closer = NewCloser()

	h.Log = logger

	h.config = conf

	h.network = p2p
	h.beginLayer = beginLayer

	h.broker = NewBroker(p2p, NewEligibilityValidator(NewHareOracle(rolacle, conf.N), logger), h.Closer)

	h.sign = sign

	h.obp = obp
	h.rolacle = rolacle

	h.networkDelta = Delta
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer

	h.lastLayer = 0

	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.outputs = make(map[types.LayerID][]types.BlockID, h.bufferSize) //  we keep results about LayerBuffer past layers

	h.factory = func(conf config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus {
		return NewConsensusProcess(conf, instanceId, s, oracle, signing, p2p, terminationReport, logger)
	}

	return h
}

func (h *Hare) isTooLate(id InstanceId) bool {
	h.layerLock.RLock()
	if int64(id) < int64(h.lastLayer)-int64(h.bufferSize) { // bufferSize>=0
		h.layerLock.RUnlock()
		return true
	}
	h.layerLock.RUnlock()
	return false
}

// ErrTooLate means that the consensus was terminated too late
var ErrTooLate = errors.New("consensus process %v finished too late")

func (h *Hare) collectOutput(output TerminationOutput) error {
	id := output.Id()

	if h.isTooLate(id) {
		return ErrTooLate
	}

	set := output.Set()
	blocks := make([]types.BlockID, len(set.values))
	i := 0
	for _, v := range set.values {
		blocks[i] = v.BlockID
		i++
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

	h.Info("received %v new blocks ", len(blocks))
	set := NewEmptySet(len(blocks))
	for _, b := range blocks {
		// todo: figure out real type of blockid
		set.Add(Value{b})
	}

	instid := InstanceId(id)

	cp := h.factory(h.config, instid, set, h.rolacle, h.sign, h.network, h.outputChan)
	cp.SetInbox(h.broker.Register(cp.Id()))
	e := cp.Start()
	if e != nil {
		h.Error("Could not start consensus process %v", e.Error())
	}
	metrics.TotalConsensusProcesses.Add(1)
}

var (
	// ErrTooOld is an error we return when we've been requested output about old consensus procs
	ErrTooOld = errors.New("results for that layer already deleted")
	// ErrTooEarly is what we return when the requested layer consensus is still in process
	ErrTooEarly = errors.New("results for that layer haven't arrived yet")
)

// GetResults returns the hare output for a given LayerID. returns error if we don't have results yet.
func (h *Hare) BlockingGetResult(id types.LayerID) ([]types.BlockID, error) {
	if h.isTooLate(InstanceId(id)) {
		return nil, ErrTooOld
	}

	h.mu.RLock()
	blks, ok := h.outputs[id]
	if !ok {
		h.mu.RUnlock()
		return nil, ErrTooEarly
	}
	h.mu.RUnlock()
	return blks, nil
}

// GetResults returns the hare output for a given LayerID. returns error if we don't have results yet.
func (h *Hare) GetResult(id types.LayerID) ([]types.BlockID, error) {
	if h.isTooLate(InstanceId(id)) {
		return nil, ErrTooOld
	}

	h.mu.RLock()
	blks, ok := h.outputs[id]
	if !ok {
		h.mu.RUnlock()
		return nil, ErrTooEarly
	}
	h.mu.RUnlock()
	return blks, nil
}

func (h *Hare) outputCollectionLoop() {
	for {
		select {
		case out := <-h.outputChan:
			metrics.TotalConsensusProcesses.Add(-1)
			err := h.collectOutput(out)
			if err != nil {
				h.Warning("Err collecting output from hare err: %v", err)
			}
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
	err := h.broker.Start()
	if err != nil {
		return err
	}

	go h.tickLoop()
	go h.outputCollectionLoop()

	return nil
}
