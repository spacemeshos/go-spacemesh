package hare

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/spacemeshos/go-spacemesh/blocks"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/common/util"
	"github.com/spacemeshos/go-spacemesh/database"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/hare/metrics"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
)

// LayerBuffer is the number of layer results we keep at a given time.
const LayerBuffer = 20

type consensusFactory func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, terminationReport chan TerminationOutput) Consensus

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

// RoundClock is a timer interface.
type RoundClock interface {
	AwaitWakeup() <-chan struct{}
	AwaitEndOfRound(round uint32) <-chan struct{}
}

// LayerClock provides a timer for the start of a given layer, as well as the current layer and allows converting a
// layer number to a clock time.
type LayerClock interface {
	LayerToTime(id types.LayerID) time.Time
	AwaitLayer(layerID types.LayerID) chan struct{}
	GetCurrentLayer() types.LayerID
}

// Hare is the orchestrator that starts new consensus processes and collects their output.
type Hare struct {
	util.Closer
	log.Log
	config config.Config

	publisher  pubsub.Publisher
	layerClock LayerClock

	newRoundClock func(LayerID types.LayerID) RoundClock

	broker *Broker

	sign Signer

	mesh    meshProvider
	beacons blocks.BeaconGetter
	rolacle Rolacle
	patrol  layerPatrol

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
	pid peer.ID,
	publisher pubsub.PublishSubsciber,
	sign Signer,
	nid types.NodeID,
	syncState syncStateFunc,
	mesh meshProvider,
	beacons blocks.BeaconGetter,
	rolacle Rolacle,
	patrol layerPatrol,
	layersPerEpoch uint16,
	idProvider identityProvider,
	stateQ StateQuerier,
	layerClock LayerClock,
	logger log.Log,
) *Hare {
	h := new(Hare)

	h.Closer = util.NewCloser()

	h.Log = logger
	h.config = conf
	h.publisher = publisher
	h.layerClock = layerClock
	h.newRoundClock = func(layerID types.LayerID) RoundClock {
		layerTime := layerClock.LayerToTime(layerID)
		wakeupDelta := time.Duration(conf.WakeupDelta) * time.Second
		roundDuration := time.Duration(h.config.RoundDuration) * time.Second
		h.With().Info("creating hare round clock", layerID,
			log.String("layer_time", layerTime.String()),
			log.Duration("wakeup_delta", wakeupDelta),
			log.Duration("round_duration", roundDuration))
		return NewSimpleRoundClock(layerTime, wakeupDelta, roundDuration)
	}

	ev := newEligibilityValidator(rolacle, layersPerEpoch, idProvider, conf.N, conf.ExpectedLeaders, logger)
	h.broker = newBroker(pid, ev, stateQ, syncState, layersPerEpoch, conf.LimitConcurrent, h.Closer, logger)
	publisher.Register(protoName, h.broker.HandleMessage)
	h.sign = sign

	h.mesh = mesh
	h.beacons = beacons
	h.rolacle = rolacle
	h.patrol = patrol

	h.networkDelta = time.Duration(conf.WakeupDelta) * time.Second
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer // XXX: must be at least the size of `hdist`
	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.outputs = make(map[types.LayerID][]types.BlockID, h.bufferSize) // we keep results about LayerBuffer past layers
	h.factory = func(conf config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, terminationReport chan TerminationOutput) Consensus {
		return newConsensusProcess(conf, instanceId, s, oracle, stateQ, layersPerEpoch, signing, nid, p2p, terminationReport, ev, clock, logger)
	}

	h.nid = nid

	return h
}

func (h *Hare) getLastLayer() types.LayerID {
	h.layerLock.RLock()
	defer h.layerLock.RUnlock()
	return h.lastLayer
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

// ErrTooLate means that the consensus was terminated too late.
var ErrTooLate = errors.New("consensus process finished too late")

// records the provided output.
func (h *Hare) collectOutput(ctx context.Context, output TerminationOutput) error {
	layerID := output.ID()
	var blocks []types.BlockID
	if output.Completed() {
		h.WithContext(ctx).With().Info("hare terminated with success", layerID)
		set := output.Set()
		blocks = make([]types.BlockID, 0, set.len())
		for _, v := range set.elements() {
			blocks = append(blocks, v)
		}
	} else {
		h.WithContext(ctx).With().Info("hare terminated with failure", layerID)
	}
	h.mesh.HandleValidatedLayer(ctx, layerID, blocks)

	if h.outOfBufferRange(layerID) {
		return ErrTooLate
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if uint32(len(h.outputs)) >= h.bufferSize {
		delete(h.outputs, h.oldestResultInBuffer())
	}
	h.outputs[layerID] = blocks
	return nil
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new consensus processes.
func (h *Hare) onTick(ctx context.Context, id types.LayerID) (bool, error) {
	logger := h.WithContext(ctx).WithFields(id)
	if h.IsClosed() {
		logger.Info("hare exiting")
		return false, nil
	}

	h.layerLock.Lock()
	if id.After(h.lastLayer) {
		h.lastLayer = id
	} else {
		logger.With().Error("received out of order layer tick", log.FieldNamed("last_layer", h.lastLayer))
	}
	h.layerLock.Unlock()

	if id.GetEpoch().IsGenesis() {
		logger.Info("not starting hare since we are in genesis epoch")
		return false, nil
	}

	var err error
	beacon, err := h.beacons.GetBeacon(id.GetEpoch())
	if err != nil {
		logger.Info("not starting hare since beacon is not retrieved")
		return false, nil
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
				log.Bool("isActive", isActive), log.Err(err))
		}
	}()

	logger.With().Debug("hare got tick, sleeping", log.String("delta", fmt.Sprint(h.networkDelta)))

	clock := h.newRoundClock(id)
	select {
	case <-clock.AwaitWakeup():
		break // keep going
	case <-h.CloseChannel():
		return false, errors.New("closed while waiting for hare delta")
	}

	if !h.broker.Synced(ctx, id) {
		// if not currently synced don't start consensus process
		logger.Info("not processing hare tick since node is not synced")
		return false, nil
	}

	h.layerLock.RLock()
	blocks := h.getGoodBlocks(h.lastLayer, beacon, logger)
	h.layerLock.RUnlock()
	logger.With().Info("starting hare consensus with blocks", log.Int("num_blocks", len(blocks)))
	set := NewSet(blocks)

	instID := id
	c, err := h.broker.Register(ctx, instID)
	if err != nil {
		logger.With().Error("could not register consensus process on broker", log.Err(err))
		return false, fmt.Errorf("broker register: %w", err)
	}
	cp := h.factory(h.config, instID, set, h.rolacle, h.sign, h.publisher, clock, h.outputChan)
	cp.SetInbox(c)
	if err = cp.Start(ctx); err != nil {
		logger.With().Error("could not start consensus process", log.Err(err))
		h.broker.Unregister(ctx, cp.ID())
		return false, fmt.Errorf("start consensus: %w", err)
	}
	h.patrol.SetHareInCharge(instID)
	logger.With().Info("number of consensus processes (after register)",
		log.Int32("count", atomic.AddInt32(&h.totalCPs, 1)))
	metrics.TotalConsensusProcesses.With(prometheus.Labels{"layer": id.String()}).Inc()
	return true, nil
}

// getGoodBlocks finds the "good blocks" for the specified layer. a block is considered a good block if
// it has the same beacon value as the node's beacon value.
// any error encountered will be ignored and an empty set is returned.
func (h *Hare) getGoodBlocks(lyrID types.LayerID, epochBeacon []byte, logger log.Log) []types.BlockID {
	blocks, err := h.mesh.LayerBlocks(lyrID)
	if err != nil {
		if err != database.ErrNotFound {
			logger.With().Error("no blocks found for hare, using empty set", log.Err(err))
		}
		return []types.BlockID{}
	}

	var goodBlocks []types.BlockID
	for _, b := range blocks {
		beacon := b.TortoiseBeacon
		if b.RefBlock != nil {
			refBlock, err := h.mesh.GetBlock(*b.RefBlock)
			if err != nil {
				logger.With().Error("failed to find ref block",
					b.ID(),
					log.String("ref_block_id", b.RefBlock.AsHash32().ShortString()))
				return []types.BlockID{}
			}
			beacon = refBlock.TortoiseBeacon
		}

		if bytes.Equal(beacon, epochBeacon) {
			goodBlocks = append(goodBlocks, b.ID())
		} else {
			logger.With().Warning("block has different beacon value",
				b.ID(),
				log.String("block_beacon", types.BytesToHash(beacon).ShortString()),
				log.String("epoch_beacon", types.BytesToHash(epochBeacon).ShortString()))
		}
	}
	return goodBlocks
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
	h.WithContext(ctx).With().Info("starting collection loop")
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

			if err := h.collectOutput(ctx, out); err != nil {
				logger.With().Warning("error collecting output from hare", log.Err(err))
			}
			h.broker.Unregister(ctx, out.ID())
			logger.With().Info("number of consensus processes (after unregister)",
				log.Int32("count", atomic.AddInt32(&h.totalCPs, -1)))
			metrics.TotalConsensusProcesses.With(prometheus.Labels{"layer": out.ID().String()}).Dec()
		case <-h.CloseChannel():
			return
		}
	}
}

// listens to new layers.
func (h *Hare) tickLoop(ctx context.Context) {
	for layer := h.layerClock.GetCurrentLayer(); ; layer = layer.Add(1) {
		select {
		case <-h.layerClock.AwaitLayer(layer):
			if h.layerClock.LayerToTime(layer).Sub(time.Now()) > (time.Duration(h.config.WakeupDelta) * time.Second) {
				h.With().Warning("missed hare window, skipping layer", layer)
				continue
			}
			go func(l types.LayerID) {
				started, err := h.onTick(ctx, l)
				if err != nil {
					h.With().Error("failed to handle tick", log.Err(err))
				} else if !started {
					h.WithContext(ctx).With().Warning("consensus not started for layer", l)
				}
			}(layer)
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
		return fmt.Errorf("start broker: %w", err)
	}

	go h.tickLoop(ctxTickLoop)
	go h.outputCollectionLoop(ctxOutputLoop)

	return nil
}
