package hare

import (
	"errors"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"sync"
	"sync/atomic"
	"time"
)

// LayerBuffer is the number of layer results we keep at a given time.
const LayerBuffer = 20

type consensusFactory func(cfg config.Config, instanceId instanceID, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus

// Consensus represents an item that acts like a consensus process.
type Consensus interface {
	ID() instanceID
	Close()
	CloseChannel() chan struct{}

	Start() error
	SetInbox(chan *Msg)
}

// TerminationOutput represents an output of a consensus process.
type TerminationOutput interface {
	ID() instanceID
	Set() *Set
	Coinflip() bool
	Completed() bool
}

type layers interface {
	LayerBlockIds(layerID types.LayerID) ([]types.BlockID, error)
	HandleValidatedLayer(validatedLayer types.LayerID, layer []types.BlockID)
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

	msh     layers
	rolacle Rolacle

	networkDelta time.Duration

	layerLock sync.RWMutex
	lastLayer types.LayerID

	bufferSize int

	outputChan chan TerminationOutput
	mu         sync.RWMutex
	outputs    map[types.LayerID][]types.BlockID
	coinflips  map[types.LayerID]bool

	factory consensusFactory

	validate outputValidationFunc

	nid types.NodeID

	totalCPs int32
}

// New returns a new Hare struct.
func New(conf config.Config, p2p NetworkService, sign Signer, nid types.NodeID, validate outputValidationFunc,
	syncState syncStateFunc, obp layers, rolacle Rolacle,
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
	h.msh = obp
	h.rolacle = rolacle

	h.networkDelta = time.Duration(conf.WakeupDelta) * time.Second
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer // XXX: must be at least the size of `hdist`
	h.lastLayer = 0
	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.outputs = make(map[types.LayerID][]types.BlockID, h.bufferSize) // we keep results about LayerBuffer past layers
	h.coinflips = make(map[types.LayerID]bool) // no buffer size for now, TODO: do we need one?
	h.factory = func(conf config.Config, instanceId instanceID, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus {
		return newConsensusProcess(conf, instanceId, s, oracle, stateQ, layersPerEpoch, signing, nid, p2p, terminationReport, ev, logger)
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
func (h *Hare) outOfBufferRange(id instanceID) bool {
	lyr := h.getLastLayer()

	if lyr <= types.LayerID(h.bufferSize) {
		return false
	}

	if id < instanceID(lyr-types.LayerID(h.bufferSize)) { // bufferSize>=0
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
var ErrTooLate = errors.New("consensus process finished too late")

// records the provided output
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
		h.Error("failed to validate the collected output set")
	}

	id := output.ID()
	h.msh.HandleValidatedLayer(types.LayerID(id), blocks)
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

// record weak coin flip results
// this probably does not need to be stored in the database since it's only used when building blocks, and that only
// happens when the node is fully synced and Hare is working.
func (h *Hare) collectCoinflip(id instanceID, coinflip bool) {
	// TODO: do we need to check buffer range?
	// bytearray is stored little endian, so leftmost byte is least significant
	h.mu.Lock()
	h.coinflips[types.LayerID(id)] = coinflip
	h.mu.Unlock()
	h.With().Info("weak coin flip value for layer", types.LayerID(id), log.Bool("coinflip", coinflip))
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new consensus processes.
func (h *Hare) onTick(id types.LayerID) {
	h.layerLock.Lock()
	if id > h.lastLayer {
		h.lastLayer = id
	} else {
		h.With().Error("received out of order layer tick",
			log.FieldNamed("last_layer", h.lastLayer),
			log.FieldNamed("this_layer", id))
	}

	h.layerLock.Unlock()

	if !h.broker.Synced(instanceID(id)) { // if not synced don't start consensus
		h.With().Info("not starting hare since node is not synced", id)
		return
	}

	if id.GetEpoch().IsGenesis() {
		h.With().Info("not starting hare since we are in genesis epoch", id)
		return
	}

	// call to start the calculation of active set size beforehand
	go func() {
		// this is called only for its side effects, but at least print the error if it returns one
		if isActive, err := h.rolacle.IsIdentityActiveOnConsensusView(h.nid.Key, id); err != nil {
			h.With().Error("error checking if identity is active",
				log.Bool("isActive", isActive), log.Err(err))
		}
	}()

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

	// retrieve set from orphan blocks
	blocks, err := h.msh.LayerBlockIds(h.lastLayer)
	if err != nil {
		h.With().Error("no blocks for consensus", id, log.Err(err))
		return
	}

	h.With().Debug("received new blocks", log.Int("count", len(blocks)))
	set := NewEmptySet(len(blocks))
	for _, b := range blocks {
		set.Add(b)
	}

	instID := instanceID(id)
	c, err := h.broker.Register(instID)
	if err != nil {
		h.With().Warning("could not register consensus process on broker", id, log.Err(err))
		return
	}
	cp := h.factory(h.config, instID, set, h.rolacle, h.sign, h.network, h.outputChan)
	cp.SetInbox(c)
	if err := cp.Start(); err != nil {
		h.With().Error("could not start consensus process", log.Err(err))
		h.broker.Unregister(cp.ID())
		return
	}
	h.With().Info("number of consensus processes (after +1)",
		log.Int32("count", atomic.AddInt32(&h.totalCPs, 1)))
	// TODO: fix metrics
	//metrics.TotalConsensusProcesses.With("layer", strconv.FormatUint(uint64(id), 10)).Add(1)
}

var (
	errTooOld   = errors.New("layer has already been evacuated from buffer")
	errNoResult = errors.New("no result for the requested layer")
)

// GetResult returns the hare output for the provided range.
// Returns error iff the request for the upper is too old.
func (h *Hare) GetResult(lid types.LayerID) ([]types.BlockID, error) {
	if h.outOfBufferRange(instanceID(lid)) {
		return nil, errTooOld
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	blks, ok := h.outputs[lid]
	if !ok {
		return nil, errNoResult
	}

	return blks, nil
}

// GetWeakCoinForLayer returns the weak coin flip value for the layer.
// It returns an error if no value has been recorded for the layer.
func (h *Hare) GetWeakCoinForLayer(lid types.LayerID) (coinflip bool, err error) {
	coinflip, ok := h.coinflips[lid]
	if !ok {
		err = errNoResult
	}
	return
}

// listens to outputs arriving from consensus processes.
func (h *Hare) outputCollectionLoop() {
	for {
		select {
		case out := <-h.outputChan:
			// collect coinflip data regardless of completion
			h.collectCoinflip(out.ID(), out.Coinflip())
			if out.Completed() { // CP completed, collect the output
				if err := h.collectOutput(out); err != nil {
					h.With().Warning("error collecting output from hare", log.Err(err))
				}
			}

			// either way, unregister from broker
			h.broker.Unregister(out.ID())
			h.With().Info("number of consensus processes (after -1)",
				log.Int32("count", atomic.AddInt32(&h.totalCPs, -1)))
			// TODO: fix metrics
			//metrics.TotalConsensusProcesses.With("layer", strconv.FormatUint(uint64(out.ID()), 10)).Add(-1)
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
	h.With().Info("starting protocol", log.String("protocol", protoName))
	err := h.broker.Start()
	if err != nil {
		return err
	}

	go h.tickLoop()
	go h.outputCollectionLoop()

	return nil
}
