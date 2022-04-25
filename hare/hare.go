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
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/system"
)

// LayerBuffer is the number of layer results we keep at a given time.
const LayerBuffer = 20

type consensusFactory func(cfg config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, terminationReport chan TerminationOutput, certificationReport chan CertificationOutput) Consensus

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

// CertificationOutput represents an certification output of a consensus process.
type CertificationOutput = types.LayerID

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
	config        config.Config
	publisher     pubsub.Publisher
	layerClock    LayerClock
	broker        *Broker
	sign          Signer
	blockGen      blockGenerator
	mesh          meshProvider
	pdb           proposalProvider
	beacons       system.BeaconGetter
	fetcher       system.ProposalFetcher
	rolacle       Rolacle
	patrol        layerPatrol
	factory       consensusFactory
	newRoundClock func(LayerID types.LayerID) RoundClock
	networkDelta  time.Duration
	nid           types.NodeID
	totalCPs      int32
	bufferSize    uint32
	outputChan    chan TerminationOutput
	mu            sync.RWMutex
	outputs       map[types.LayerID][]types.ProposalID
	certChan      chan types.LayerID
	certified     map[types.LayerID]struct{}
	layerLock     sync.RWMutex
	lastLayer     types.LayerID
	wg            sync.WaitGroup
}

// New returns a new Hare struct.
func New(
	conf config.Config,
	peer p2p.Peer,
	publisher pubsub.PublishSubsciber,
	sign Signer,
	nid types.NodeID,
	blockGen blockGenerator,
	syncState system.SyncStateProvider,
	mesh meshProvider,
	ppp proposalProvider,
	beacons system.BeaconGetter,
	fetch system.ProposalFetcher,
	rolacle Rolacle,
	patrol layerPatrol,
	layersPerEpoch uint16,
	idProvider identityProvider,
	stateQ stateQuerier,
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
	h.broker = newBroker(peer, ev, stateQ, syncState, layersPerEpoch, conf.LimitConcurrent, h.Closer, logger)
	h.sign = sign
	h.blockGen = blockGen

	h.mesh = mesh
	h.pdb = ppp
	h.beacons = beacons
	h.fetcher = fetch
	h.rolacle = rolacle
	h.patrol = patrol

	h.networkDelta = time.Duration(conf.WakeupDelta) * time.Second
	// todo: this should be loaded from global config
	h.bufferSize = LayerBuffer // XXX: must be at least the size of `hdist`
	h.outputChan = make(chan TerminationOutput, h.bufferSize)
	h.certChan = make(chan CertificationOutput, h.bufferSize)
	h.outputs = make(map[types.LayerID][]types.ProposalID, h.bufferSize) // we keep results about LayerBuffer past layers
	h.certified = make(map[types.LayerID]struct{}, h.bufferSize)
	h.factory = func(conf config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, terminationReport chan TerminationOutput, certificationReport chan CertificationOutput) Consensus {
		return newConsensusProcess(conf, instanceId, s, oracle, stateQ, layersPerEpoch, signing, nid, p2p, terminationReport, certificationReport, ev, clock, logger)
	}
	h.nid = nid

	return h
}

// GetHareMsgHandler returns the gossip handler for hare protocol message.
func (h *Hare) GetHareMsgHandler() pubsub.GossipHandler {
	return h.broker.HandleMessage
}

func (h *Hare) getLastLayer() types.LayerID {
	h.layerLock.RLock()
	defer h.layerLock.RUnlock()
	return h.lastLayer
}

func (h *Hare) setLastLayer(layerID types.LayerID) {
	h.layerLock.Lock()
	defer h.layerLock.Unlock()
	if layerID.After(h.lastLayer) {
		h.lastLayer = layerID
	} else {
		h.With().Error("received out of order layer tick", log.FieldNamed("last_layer", h.lastLayer))
	}
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
	defer h.patrol.CompleteHare(layerID)

	var pids []types.ProposalID
	if output.Completed() {
		h.WithContext(ctx).With().Info("hare terminated with success", layerID, log.Int("num_proposals", output.Set().Size()))
		set := output.Set()
		pids = make([]types.ProposalID, 0, set.len())
		for _, v := range set.elements() {
			pids = append(pids, v)
		}
	} else {
		h.WithContext(ctx).With().Info("hare terminated with failure", layerID)
	}

	hareOutput := types.EmptyBlockID
	if len(pids) > 0 {
		// fetch proposals from peers if not locally available
		if err := h.fetcher.GetProposals(ctx, pids); err != nil {
			h.WithContext(ctx).With().Warning("failed to fetch proposals", log.Err(err))
			return fmt.Errorf("hare fetch proposals: %w", err)
		}
		// now all proposals should be in local DB
		proposals, err := h.pdb.GetProposals(pids)
		if err != nil {
			h.WithContext(ctx).With().Warning("failed to get proposals locally", log.Err(err))
			return fmt.Errorf("hare get proposals: %w", err)
		}

		if block, err := h.blockGen.GenerateBlock(ctx, layerID, proposals); err != nil {
			return fmt.Errorf("hare gen block: %w", err)
		} else if err = h.mesh.AddBlockWithTXs(ctx, block); err != nil {
			return fmt.Errorf("hare save block: %w", err)
		} else {
			hareOutput = block.ID()
		}
	}
	if err := h.mesh.ProcessLayerPerHareOutput(ctx, layerID, hareOutput); err != nil {
		h.WithContext(ctx).With().Warning("mesh failed to process layer", layerID, log.Err(err))
	}

	if h.outOfBufferRange(layerID) {
		return ErrTooLate
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if uint32(len(h.outputs)) >= h.bufferSize {
		delete(h.outputs, h.oldestResultInBuffer())
	}
	h.outputs[layerID] = pids
	return nil
}

func (h *Hare) certify(ctx context.Context, id types.LayerID) {
	if h.outOfBufferRange(id) {
		// ignore
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if uint32(len(h.certified)) >= h.bufferSize {
		delete(h.certified, h.oldestResultInBuffer())
	}

	h.certified[id] = struct{}{}
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new consensus processes.
func (h *Hare) onTick(ctx context.Context, id types.LayerID) (bool, error) {
	logger := h.WithContext(ctx).WithFields(id)
	if h.IsClosed() {
		logger.Info("hare exiting")
		return false, nil
	}

	h.setLastLayer(id)

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
	proposals := h.getGoodProposal(h.lastLayer, beacon, logger)
	h.layerLock.RUnlock()
	logger.With().Info("starting hare consensus with proposals", log.Int("num_proposals", len(proposals)))
	set := NewSet(proposals)

	instID := id
	c, err := h.broker.Register(ctx, instID)
	if err != nil {
		logger.With().Error("could not register consensus process on broker", log.Err(err))
		return false, fmt.Errorf("broker register: %w", err)
	}
	cp := h.factory(h.config, instID, set, h.rolacle, h.sign, h.publisher, clock, h.outputChan, h.certChan)
	cp.SetInbox(c)
	if err = cp.Start(ctx); err != nil {
		logger.With().Error("could not start consensus process", log.Err(err))
		h.broker.Unregister(ctx, cp.ID())
		return false, fmt.Errorf("start consensus: %w", err)
	}
	h.patrol.SetHareInCharge(instID)
	logger.With().Info("number of consensus processes (after register)",
		log.Int32("count", atomic.AddInt32(&h.totalCPs, 1)))
	return true, nil
}

// getGoodProposal finds the "good proposals" for the specified layer. a proposal is good if
// it has the same beacon value as the node's beacon value.
// any error encountered will be ignored and an empty set is returned.
func (h *Hare) getGoodProposal(lyrID types.LayerID, epochBeacon types.Beacon, logger log.Log) []types.ProposalID {
	proposals, err := h.pdb.LayerProposals(lyrID)
	if err != nil {
		if !errors.Is(err, sql.ErrNotFound) {
			logger.With().Warning("no proposals found for hare, using empty set", log.Err(err))
		} else {
			logger.With().Error("failed to get proposals for hare", log.Err(err))
		}
		return []types.ProposalID{}
	}

	var (
		beacon        types.Beacon
		goodProposals []types.ProposalID
	)
	for _, p := range proposals {
		if p.EpochData != nil {
			beacon = p.EpochData.Beacon
		} else if p.RefBallot == types.EmptyBallotID {
			logger.With().Error("proposal missing ref ballot", p.ID())
			return []types.ProposalID{}
		} else if refBallot, err := h.mesh.GetBallot(p.RefBallot); err != nil {
			logger.With().Error("failed to get ref ballot", p.ID(), p.RefBallot, log.Err(err))
			return []types.ProposalID{}
		} else if refBallot.EpochData == nil {
			logger.With().Error("ref ballot missing epoch data", refBallot.ID())
			return []types.ProposalID{}
		} else {
			beacon = refBallot.EpochData.Beacon
		}

		if beacon == epochBeacon {
			goodProposals = append(goodProposals, p.ID())
		} else {
			logger.With().Warning("proposal has different beacon value",
				p.ID(),
				log.String("proposal_beacon", beacon.ShortString()),
				log.String("epoch_beacon", epochBeacon.ShortString()))
		}
	}
	return goodProposals
}

var (
	errTooOld   = errors.New("layer has already been evacuated from buffer")
	errNoResult = errors.New("no result for the requested layer")
)

func (h *Hare) getResult(lid types.LayerID) ([]types.ProposalID, error) {
	if h.outOfBufferRange(lid) {
		return nil, errTooOld
	}

	h.mu.RLock()
	defer h.mu.RUnlock()
	proposals, ok := h.outputs[lid]
	if !ok {
		return nil, errNoResult
	}

	return proposals, nil
}

// listens to outputs arriving from consensus processes.
func (h *Hare) outputCollectionLoop(ctx context.Context) {
	defer h.wg.Done()

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
		case <-h.CloseChannel():
			return
		}
	}
}

// listens to outputs arriving from consensus processes.
func (h *Hare) certificationLoop(ctx context.Context) {
	defer h.wg.Done()

	h.WithContext(ctx).With().Info("starting certification collection loop")
	for {
		select {
		case out := <-h.certChan:
			ctx := log.WithNewSessionID(ctx)
			h.certify(ctx, out)
		case <-h.CloseChannel():
			return
		}
	}
}

// listens to new layers.
func (h *Hare) tickLoop(ctx context.Context) {
	defer h.wg.Done()

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
	h.WithContext(ctx).With().Info("starting protocol", log.String("protocol", ProtoName))

	// Create separate contexts for each subprocess. This allows us to better track the flow of messages.
	ctxBroker := log.WithNewSessionID(ctx, log.String("protocol", ProtoName+"_broker"))
	ctxTickLoop := log.WithNewSessionID(ctx, log.String("protocol", ProtoName+"_tickloop"))
	ctxOutputLoop := log.WithNewSessionID(ctx, log.String("protocol", ProtoName+"_outputloop"))
	ctxCertLoop := log.WithNewSessionID(ctx, log.String("protocol", ProtoName+"_certloop"))

	if err := h.broker.Start(ctxBroker); err != nil {
		return fmt.Errorf("start broker: %w", err)
	}

	h.wg.Add(3)
	go h.tickLoop(ctxTickLoop)
	go h.outputCollectionLoop(ctxOutputLoop)
	go h.certificationLoop(ctxCertLoop)

	return nil
}

// Close sends a termination signal to hare goroutines and waits for their termination.
func (h *Hare) Close() {
	h.Closer.Close()
	h.wg.Wait()
}
