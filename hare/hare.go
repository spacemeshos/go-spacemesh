package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/mesh"
	"sync"
	"time"
)

// Delta is the time we wait before we start processing hare messages gor the round
const Delta = time.Second // todo: add to config

// LayerBuffer is the number of layer results we keep at a given time.
const LayerBuffer = 20

type consensusFactory func(cfg config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService, terminationReport chan TerminationOutput) Consensus

// Consensus represents a consensus
type Consensus interface {
	IdentifiableInboxer

	Close()
	CloseChannel() chan struct{}

	Start() error
}

// TerminationOutput is a result of a process terminated with output.
type TerminationOutput interface {
	Id() []byte
	Set() *Set
}

type orphanBlockProvider interface {
	GetOrphanBlocks() []mesh.BlockID
}

// Hare is an orchestrator that shoots consensus processes and collects their termination output
type Hare struct {
	Closer

	config config.Config

	network    NetworkService
	beginLayer chan mesh.LayerID

	broker *Broker

	sign   Signing

	obp     orphanBlockProvider
	rolacle Rolacle

	networkDelta time.Duration

	layerLock sync.RWMutex
	lastLayer mesh.LayerID

	bufferSize int

	outputChan chan TerminationOutput
	mu         sync.RWMutex
	outputs    map[mesh.LayerID][]mesh.BlockID

	factory consensusFactory
}

// New returns a new Hare struct.
func New(conf config.Config, p2p NetworkService, sign Signing, obp orphanBlockProvider, rolacle Rolacle, beginLayer chan mesh.LayerID) *Hare {
	h := new(Hare)
	h.Closer = NewCloser()

	h.config = conf

	h.network = p2p
	h.beginLayer = beginLayer

	h.broker = NewBroker(p2p)

	h.sign = sign

	h.obp = obp
	h.rolacle = rolacle

	h.networkDelta = Delta
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer

	h.lastLayer = 0

	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.outputs = make(map[mesh.LayerID][]mesh.BlockID, h.bufferSize) //  we keep results about LayerBuffer past layers

	h.factory = func(conf config.Config, instanceId InstanceId, s *Set, oracle Rolacle, signing Signing, p2p NetworkService, terminationReport chan TerminationOutput) Consensus {
		return NewConsensusProcess(conf, instanceId, s, oracle, signing, p2p, terminationReport)
	}

	return h
}

func (h *Hare) isTooLate(id mesh.LayerID) bool {
	h.layerLock.RLock()
	if int(id) < int(h.lastLayer)-h.bufferSize {
		h.layerLock.RUnlock()
		return true
	}
	h.layerLock.RUnlock()
	return false
}

// ErrTooLate means that the consensus was terminated too late
var ErrTooLate = errors.New("consensus process %v finished too late")

func (h *Hare) collectOutput(output TerminationOutput) error {
	id := common.BytesToUint32(output.Id())

	if h.isTooLate(mesh.LayerID(id)) {
		return ErrTooLate
	}

	set := output.Set()
	blocks := make([]mesh.BlockID, len(set.values))
	i := 0
	for _, v := range set.values {
		blocks[i] = mesh.BlockID(common.BytesToUint32(v.Bytes()))
		i++
	}
	h.mu.Lock()
	if len(h.outputs) == h.bufferSize {
		for k := range h.outputs {
			if h.isTooLate(k) {
				delete(h.outputs, k)
			}
		}
	}
	h.outputs[mesh.LayerID(id)] = blocks
	h.mu.Unlock()

	return nil
}

func (h *Hare) onTick(id mesh.LayerID) {
	h.layerLock.Lock()
	if id > h.lastLayer {
		h.lastLayer = id
	}
	h.layerLock.Unlock()

	ti := time.NewTimer(h.networkDelta)
	select {
	case <-ti.C:
		break // keep going
	case <-h.CloseChannel():
		// closed while waiting the delta
		return
	}

	// retrieve set form orphan blocks
	blocks := h.obp.GetOrphanBlocks()

	set := NewEmptySet(len(blocks))

	for _, b := range blocks {
		// todo: figure out real type of blockid
		set.Add(Value{NewBytes32(b.ToBytes())})
	}

	instid := InstanceId{NewBytes32(id.ToBytes())}

	cp := h.factory(h.config, instid, set, h.rolacle, h.sign, h.network, h.outputChan)
	cp.Start()
	h.broker.Register(cp)
}

var (
	// ErrTooOld is an error we return when we've been requested output about old consensus procs
	ErrTooOld = errors.New("results for that layer already deleted")
	// ErrTooEarly is what we return when the requested layer consensus is still in process
	ErrTooEarly = errors.New("results for that layer haven't arrived yet")
)

// GetResults returns the hare output for a given LayerID. returns error if we don't have results yet.
func (h *Hare) GetResult(id mesh.LayerID) ([]mesh.BlockID, error) {
	if h.isTooLate(id) {
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
			err := h.collectOutput(out)
			if err != nil {
				log.Warning("Err collecting output from hare err: %v", err)
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
