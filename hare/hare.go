package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
)

// LayerBuffer is the number of layer results we keep at a given time.
const LayerBuffer = 20

type consensusFactory func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus

// Consensus represents an item that acts like a consensus process.
type Consensus interface {
	ID() types.LayerID
	Close()
	CloseChannel() chan struct{}

	Start(ctx context.Context) error
	SetInbox(chan *Msg)
}

// TerminationOutput represents an output of a consensus process.
type TerminationOutput interface {
	ID() types.LayerID
	Set() *Set
	Coinflip() bool
	Completed() bool
}

type meshProvider interface {
	// LayerBlockIds returns the block IDs stored for a layer
	LayerBlockIds(layerID types.LayerID) ([]types.BlockID, error)
	// HandleValidatedLayer receives Hare output when it succeeds
	HandleValidatedLayer(ctx context.Context, validatedLayer types.LayerID, layer []types.BlockID)
	// InvalidateLayer receives the signal that Hare failed for a layer
	InvalidateLayer(ctx context.Context, layerID types.LayerID)
	// RecordCoinflip records the weak coinflip result for a layer
	RecordCoinflip(ctx context.Context, layerID types.LayerID, coinflip bool)
}

// Hare is the orchestrator that starts new consensus processes and collects their output.
type Hare struct {
	util.Closer
	log.Log
	config config.Config

	network    NetworkService
	beginLayer chan types.LayerID

	broker *Broker

	sign Signer

	mesh    meshProvider
	rolacle Rolacle

	networkDelta time.Duration

	layerLock sync.RWMutex
	lastLayer types.LayerID

	bufferSize uint32

	outputChan chan TerminationOutput
	mu         sync.RWMutex
	outputs    map[types.LayerID][]types.BlockID

	factory consensusFactory

	nid types.NodeID

	totalCPs int32
}

// New returns a new Hare struct.
func New(
	conf config.Config,
	p2p NetworkService,
	sign Signer,
	nid types.NodeID,
	syncState syncStateFunc,
	mesh meshProvider,
	rolacle Rolacle,
	layersPerEpoch uint16,
	idProvider identityProvider,
	stateQ StateQuerier,
	beginLayer chan types.LayerID,
	logger log.Log,
) *Hare {
	h := new(Hare)

	h.Closer = util.NewCloser()

	h.Log = logger
	h.config = conf
	h.network = p2p
	h.beginLayer = beginLayer

	ev := newEligibilityValidator(rolacle, layersPerEpoch, idProvider, conf.N, conf.ExpectedLeaders, logger)
	h.broker = newBroker(p2p, ev, stateQ, syncState, layersPerEpoch, conf.LimitConcurrent, h.Closer, logger)
	h.sign = sign

	h.mesh = mesh
	h.rolacle = rolacle

	h.networkDelta = time.Duration(conf.WakeupDelta) * time.Second
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer // XXX: must be at least the size of `hdist`
	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.outputs = make(map[types.LayerID][]types.BlockID, h.bufferSize) // we keep results about LayerBuffer past layers
	h.factory = func(conf config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p NetworkService, terminationReport chan TerminationOutput) Consensus {
		return newConsensusProcess(conf, instanceId, s, oracle, stateQ, layersPerEpoch, signing, nid, p2p, terminationReport, ev, logger)
	}

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
func (h *Hare) outOfBufferRange(id types.LayerID) bool {
	last := h.getLastLayer()
	if !last.After(types.NewLayerID(h.bufferSize)) {
		return false
	}
	if id.Before(last.Sub(h.bufferSize)) { // bufferSize>=0
		return true
	}
	return false
}

func (h *Hare) oldestResultInBuffer() types.LayerID {
	// buffer is usually quite small so its cheap to iterate.
	// TODO: if it gets bigger change `outputs` to array.
	lyr := h.getLastLayer()
	for k := range h.outputs {
		if k.Before(lyr) {
			lyr = k
		}
	}
	return lyr
}

// ErrTooLate means that the consensus was terminated too late
var ErrTooLate = errors.New("consensus process finished too late")

// records the provided output
func (h *Hare) collectOutput(ctx context.Context, output TerminationOutput) error {
	set := output.Set()
	blocks := make([]types.BlockID, len(set.values))
	i := 0
	for v := range set.values {
		blocks[i] = v
		i++
	}

	id := output.ID()
	h.mesh.HandleValidatedLayer(ctx, id, blocks)

	if h.outOfBufferRange(id) {
		return ErrTooLate
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if uint32(len(h.outputs)) >= h.bufferSize {
		delete(h.outputs, h.oldestResultInBuffer())
	}
	h.outputs[id] = blocks

	return nil
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new consensus processes.
func (h *Hare) onTick(ctx context.Context, id types.LayerID) (err error) {
	logger := h.WithContext(ctx).WithFields(id)
	h.layerLock.Lock()
	if id.After(h.lastLayer) {
		h.lastLayer = id
	} else {
		logger.With().Error("received out of order layer tick", log.FieldNamed("last_layer", h.lastLayer))
	}

	h.layerLock.Unlock()

	if id.GetEpoch().IsGenesis() {
		logger.Info("not starting hare since we are in genesis epoch")
		return
	}

	if !h.rolacle.IsEpochBeaconReady(ctx, id.GetEpoch()) {
		logger.Info("not starting hare since beacon is not retrieved")
		return
	}

	defer func() {
		// it must not return without starting consensus process or mark result as fail
		// except if it's genesis layer
		if err != nil {
			h.outputChan <- procReport{id, &Set{}, false, notCompleted}
		}
	}()

	// call to start the calculation of active set size beforehand
	go func() {
		// this is called only for its side effects, but at least print the error if it returns one
		if isActive, err := h.rolacle.IsIdentityActiveOnConsensusView(ctx, h.nid.Key, id); err != nil {
			logger.With().Error("error checking if identity is active",
				log.Bool("is_active", isActive),
				log.Err(err))
		}
	}()

	logger.With().Debug("hare got tick, sleeping", log.String("delta", fmt.Sprint(h.networkDelta)))

	ti := time.NewTimer(h.networkDelta)
	select {
	case <-ti.C:
		break
	case <-h.CloseChannel():
		err = errors.New("closed while waiting for hare delta")
		return
	}

	if !h.broker.Synced(ctx, id) {
		// if not currently synced don't start consensus process
		logger.Info("not processing hare tick since node is not synced")
		return
	}

	// retrieve set from orphan blocks
	blocks, err := h.mesh.LayerBlockIds(h.lastLayer)
	if err != nil {
		logger.With().Error("no blocks found for hare, using empty set", log.Err(err))
		// just fail here, it will end hare with empty set result
		// return // ?
		// TODO: there can be a difference between just fail with empty set
		// TODO:   and achieve consensus on empty set
	}

	logger.With().Info("starting hare consensus with blocks", log.Int("num_blocks", len(blocks)))
	set := NewSet(blocks)

	instID := id
	c, err := h.broker.Register(ctx, instID)
	if err != nil {
		logger.With().Error("could not register consensus process on broker", log.Err(err))
		return
	}
	cp := h.factory(h.config, instID, set, h.rolacle, h.sign, h.network, h.outputChan)
	cp.SetInbox(c)
	if err = cp.Start(ctx); err != nil {
		logger.With().Error("could not start consensus process", log.Err(err))
		h.broker.Unregister(ctx, cp.ID())
		return
	}
	logger.With().Info("number of consensus processes (after register)",
		log.Int32("count", atomic.AddInt32(&h.totalCPs, 1)))
	metrics.TotalConsensusProcesses.With("layer", id.String()).Add(1)
	return
}

var (
	errTooOld   = errors.New("layer has already been evacuated from buffer")
	errNoResult = errors.New("no result for the requested layer")
)

// GetResult returns the hare output for the provided range.
// Returns error if the requested layer is too old.
func (h *Hare) GetResult(lid types.LayerID) ([]types.BlockID, error) {
	if h.outOfBufferRange(lid) {
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

// listens to outputs arriving from consensus processes.
func (h *Hare) outputCollectionLoop(ctx context.Context) {
	for {
		select {
		case out := <-h.outputChan:
			layerID := out.ID()
			coin := out.Coinflip()
			ctx := log.WithNewSessionID(ctx)
			logger := h.WithContext(ctx).WithFields(layerID)

			// collect coinflip, regardless of success
			logger.With().Info("recording weak coinflip result for layer",
				log.Bool("coinflip", coin))
			h.mesh.RecordCoinflip(ctx, layerID, coin)

			if out.Completed() { // CP completed, collect the output
				logger.With().Info("collecting results for completed hare instance", layerID)
				if err := h.collectOutput(ctx, out); err != nil {
					logger.With().Warning("error collecting output from hare", log.Err(err))
				}
			} else {
				// Notify the mesh that Hare failed
				h.WithContext(ctx).With().Info("recording hare instance failure", layerID)
				h.mesh.InvalidateLayer(ctx, layerID)
			}
			h.broker.Unregister(ctx, out.ID())
			logger.With().Info("number of consensus processes (after unregister)",
				log.Int32("count", atomic.AddInt32(&h.totalCPs, -1)))
			metrics.TotalConsensusProcesses.With("layer", out.ID().String()).Add(-1)
		case <-h.CloseChannel():
			return
		}
	}
}

// listens to new layers.
func (h *Hare) tickLoop(ctx context.Context) {
	for {
		select {
		case layer := <-h.beginLayer:
			go func() {
				if err := h.onTick(ctx, layer); err != nil {
					h.WithContext(ctx).With().Error("error processing hare tick", log.Err(err))
				}
			}()
		case <-h.CloseChannel():
			return
		}
	}
}

// Start starts listening for layers and outputs.
func (h *Hare) Start(ctx context.Context) error {
	h.WithContext(ctx).With().Info("starting protocol", log.String("protocol", protoName))

	// Create separate contexts for each subprocess. This allows us to better track the flow of messages.
	ctxBroker := log.WithNewSessionID(ctx, log.String("protocol", protoName+"_broker"))
	ctxTickLoop := log.WithNewSessionID(ctx, log.String("protocol", protoName+"_tickloop"))
	ctxOutputLoop := log.WithNewSessionID(ctx, log.String("protocol", protoName+"_outputloop"))

	if err := h.broker.Start(ctxBroker); err != nil {
		return err
	}

	go h.tickLoop(ctxTickLoop)
	go h.outputCollectionLoop(ctxOutputLoop)

	return nil
}
