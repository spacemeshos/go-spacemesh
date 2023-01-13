package hare

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/codec"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/hare/config"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/p2p/pubsub"
	"github.com/spacemeshos/go-spacemesh/sql"
	"github.com/spacemeshos/go-spacemesh/sql/ballots"
	"github.com/spacemeshos/go-spacemesh/sql/identities"
	"github.com/spacemeshos/go-spacemesh/sql/layers"
	"github.com/spacemeshos/go-spacemesh/sql/proposals"
	"github.com/spacemeshos/go-spacemesh/system"
)

type consensusFactory func(context.Context, config.Config, types.LayerID, *Set, Rolacle, Signer, pubsub.Publisher, RoundClock, chan TerminationOutput, chan types.MalfeasanceGossip) Consensus

// Consensus represents an item that acts like a consensus process.
type Consensus interface {
	ID() types.LayerID
	Start() error
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

// LayerOutput is the output of each hare consensus process.
type LayerOutput struct {
	Ctx       context.Context
	Layer     types.LayerID
	Proposals []types.ProposalID
}

// Hare is the orchestrator that starts new consensus processes and collects their output.
type Hare struct {
	log.Log
	db            *sql.Database
	config        config.Config
	publisher     pubsub.Publisher
	layerClock    LayerClock
	broker        *Broker
	sign          Signer
	blockGenCh    chan LayerOutput
	malCh         chan types.MalfeasanceGossip
	beacons       system.BeaconGetter
	rolacle       Rolacle
	patrol        layerPatrol
	newRoundClock func(LayerID types.LayerID) RoundClock

	networkDelta time.Duration

	layerLock sync.RWMutex
	lastLayer types.LayerID

	outputChan chan TerminationOutput
	mu         sync.RWMutex
	outputs    map[types.LayerID][]types.ProposalID

	factory consensusFactory

	nid types.NodeID

	totalCPs int32

	ctx    context.Context
	cancel context.CancelFunc
	eg     errgroup.Group
}

// New returns a new Hare struct.
func New(
	db *sql.Database,
	conf config.Config,
	publisher pubsub.PublishSubsciber,
	sign Signer,
	nid types.NodeID,
	ch chan LayerOutput,
	syncState system.SyncStateProvider,
	beacons system.BeaconGetter,
	rolacle Rolacle,
	patrol layerPatrol,
	stateQ stateQuerier,
	layerClock LayerClock,
	logger log.Log,
) *Hare {
	h := new(Hare)
	h.db = db

	h.Log = logger
	h.config = conf
	h.publisher = publisher
	h.layerClock = layerClock
	h.newRoundClock = func(layerID types.LayerID) RoundClock {
		layerTime := layerClock.LayerToTime(layerID)
		wakeupDelta := time.Duration(conf.WakeupDelta) * time.Second
		roundDuration := time.Duration(h.config.RoundDuration) * time.Second
		h.With().Debug("creating hare round clock", layerID,
			log.String("layer_time", layerTime.String()),
			log.Duration("wakeup_delta", wakeupDelta),
			log.Duration("round_duration", roundDuration))
		return NewSimpleRoundClock(layerTime, wakeupDelta, roundDuration)
	}

	ev := newEligibilityValidator(rolacle, conf.N, conf.ExpectedLeaders, logger)
	h.broker = newBroker(ev, stateQ, syncState, conf.LimitConcurrent, logger)
	h.sign = sign
	h.blockGenCh = ch
	h.malCh = make(chan types.MalfeasanceGossip, conf.N)

	h.beacons = beacons
	h.rolacle = rolacle
	h.patrol = patrol

	h.networkDelta = time.Duration(conf.WakeupDelta) * time.Second
	h.outputChan = make(chan TerminationOutput, h.config.Hdist)
	h.outputs = make(map[types.LayerID][]types.ProposalID, h.config.Hdist) // we keep results about LayerBuffer past layers
	h.factory = func(ctx context.Context, conf config.Config, instanceId types.LayerID, s *Set, oracle Rolacle, signing Signer, p2p pubsub.Publisher, clock RoundClock, terminationReport chan TerminationOutput, malCh chan types.MalfeasanceGossip) Consensus {
		return newConsensusProcess(ctx, conf, instanceId, s, oracle, stateQ, signing, nid, p2p, terminationReport, ev, clock, malCh, logger)
	}

	h.nid = nid
	h.ctx, h.cancel = context.WithCancel(context.Background())
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
	if layerID == (types.LayerID{}) {
		// layers starts from 0. nothing to do here.
		return
	}
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
	if !last.After(types.NewLayerID(h.config.Hdist)) {
		return false
	}
	if id.Before(last.Sub(h.config.Hdist)) { // bufferSize>=0
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
		consensusOkCnt.Inc()
		h.WithContext(ctx).With().Info("hare terminated with success", layerID, log.Int("num_proposals", output.Set().Size()))
		set := output.Set()
		postNumProposals.Add(float64(set.Size()))
		pids = set.ToSlice()
		select {
		case h.blockGenCh <- LayerOutput{
			Ctx:       ctx,
			Layer:     layerID,
			Proposals: pids,
		}:
		case <-ctx.Done():
		}
	} else {
		consensusFailCnt.Inc()
		h.WithContext(ctx).With().Warning("hare terminated with failure", layerID)
	}

	if h.outOfBufferRange(layerID) {
		return ErrTooLate
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	if uint32(len(h.outputs)) >= h.config.Hdist {
		delete(h.outputs, h.oldestResultInBuffer())
	}
	h.outputs[layerID] = pids
	return nil
}

func (h *Hare) isClosed() bool {
	select {
	case <-h.ctx.Done():
		return true
	default:
		return false
	}
}

// the logic that happens when a new layer arrives.
// this function triggers the start of new consensus processes.
func (h *Hare) onTick(ctx context.Context, id types.LayerID) (bool, error) {
	logger := h.WithContext(ctx).WithFields(id)
	if h.isClosed() {
		logger.Info("hare exiting")
		return false, nil
	}

	h.setLastLayer(id)

	if id.GetEpoch().IsGenesis() {
		logger.Info("not starting hare: genesis")
		return false, nil
	}

	var err error
	beacon, err := h.beacons.GetBeacon(id.GetEpoch())
	if err != nil {
		logger.Info("not starting hare: beacon not retrieved")
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
	h.eg.Go(func() error {
		// this is called only for its side effects, but at least print the error if it returns one
		if isActive, err := h.rolacle.IsIdentityActiveOnConsensusView(ctx, h.nid, id); err != nil {
			logger.With().Error("error checking if identity is active",
				log.Bool("isActive", isActive), log.Err(err))
		}
		return nil
	})

	logger.With().Debug("hare got tick, sleeping", log.String("delta", fmt.Sprint(h.networkDelta)))

	clock := h.newRoundClock(id)
	select {
	case <-clock.AwaitWakeup():
		break // keep going
	case <-h.ctx.Done():
		return false, errors.New("closed while waiting for hare delta")
	}

	if !h.broker.Synced(ctx, id) {
		// if not currently synced don't start consensus process
		logger.Info("not starting hare: node not synced at this layer")
		return false, nil
	}

	h.layerLock.RLock()
	props := h.getGoodProposal(h.lastLayer, beacon, logger)
	h.layerLock.RUnlock()
	logger.With().Info("starting hare", log.Int("num_proposals", len(props)))
	preNumProposals.Add(float64(len(props)))
	set := NewSet(props)

	instID := id
	c, err := h.broker.Register(ctx, instID)
	if err != nil {
		logger.With().Error("could not register consensus process on broker", log.Err(err))
		return false, fmt.Errorf("broker register: %w", err)
	}
	cp := h.factory(h.ctx, h.config, instID, set, h.rolacle, h.sign, h.publisher, clock, h.outputChan, h.malCh)
	cp.SetInbox(c)
	if err = cp.Start(); err != nil {
		logger.With().Error("could not start consensus process", log.Err(err))
		h.broker.Unregister(ctx, cp.ID())
		return false, fmt.Errorf("start consensus: %w", err)
	}
	h.patrol.SetHareInCharge(instID)
	logger.With().Debug("number of consensus processes (after register)",
		log.Int32("count", atomic.AddInt32(&h.totalCPs, 1)))
	return true, nil
}

// getGoodProposal finds the "good proposals" for the specified layer. a proposal is good if
// it has the same beacon value as the node's beacon value.
// any error encountered will be ignored and an empty set is returned.
func (h *Hare) getGoodProposal(lyrID types.LayerID, epochBeacon types.Beacon, logger log.Log) []types.ProposalID {
	props, err := proposals.GetByLayer(h.db, lyrID)
	if err != nil {
		if errors.Is(err, sql.ErrNotFound) {
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
	for _, p := range props {
		if p.EpochData != nil {
			beacon = p.EpochData.Beacon
		} else if p.RefBallot == types.EmptyBallotID {
			logger.With().Error("proposal missing ref ballot", p.ID())
			return []types.ProposalID{}
		} else if refBallot, err := ballots.Get(h.db, p.RefBallot); err != nil {
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
	props, ok := h.outputs[lid]
	if !ok {
		return nil, errNoResult
	}

	return props, nil
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
			logger.With().Debug("recording weak coin result for layer",
				log.Bool("weak_coin", coin))
			if err := layers.SetWeakCoin(h.db, layerID, coin); err != nil {
				logger.With().Error("failed to set weak coin for layer", log.Err(err))
			}
			if err := h.collectOutput(ctx, out); err != nil {
				logger.With().Warning("error collecting output from hare", log.Err(err))
			}
			h.broker.Unregister(ctx, out.ID())
			logger.With().Debug("number of consensus processes (after unregister)",
				log.Int32("count", atomic.AddInt32(&h.totalCPs, -1)))
		case <-h.ctx.Done():
			return
		}
	}
}

// listens to new layers.
func (h *Hare) tickLoop(ctx context.Context) {
	for layer := h.layerClock.GetCurrentLayer(); ; layer = layer.Add(1) {
		select {
		case <-h.layerClock.AwaitLayer(layer):
			if time.Since(h.layerClock.LayerToTime(layer)) > (time.Duration(h.config.WakeupDelta) * time.Second) {
				h.With().Warning("missed hare window, skipping layer", layer)
				continue
			}
			_, _ = h.onTick(ctx, layer)
		case <-h.ctx.Done():
			return
		}
	}
}

// process MalfeasanceProof.
func (h *Hare) malfeasanceLoop(ctx context.Context) {
	h.WithContext(ctx).With().Info("starting malfeasance loop")
	for {
		select {
		case gossip := <-h.malCh:
			if gossip.Eligibility == nil {
				h.WithContext(ctx).Fatal("missing hare eligibility")
			}
			nodeID := types.BytesToNodeID(gossip.Eligibility.PubKey)
			logger := h.WithContext(ctx).WithFields(nodeID)
			if malicious, err := identities.IsMalicious(h.db, nodeID); err != nil {
				logger.With().Error("failed to check identity", log.Err(err))
				continue
			} else if malicious {
				logger.Debug("known malicious identity")
				continue
			}
			proofBytes, err := codec.Encode(&gossip.MalfeasanceProof)
			if err != nil {
				logger.With().Fatal("failed to encode MalfeasanceProof", log.Err(err))
			}
			if err = identities.SetMalicious(h.db, nodeID, proofBytes); err != nil {
				logger.With().Error("failed to save MalfeasanceProof", log.Err(err))
				continue
			}
			gossipBytes, err := codec.Encode(&gossip)
			if err != nil {
				logger.With().Fatal("failed to encode MalfeasanceGossip", log.Err(err))
			}
			if err = h.publisher.Publish(ctx, pubsub.MalfeasanceProof, gossipBytes); err != nil {
				logger.With().Error("failed to broadcast MalfeasanceProof")
			}
		case <-ctx.Done():
			return
		case <-h.ctx.Done():
			return
		}
	}
}

// Start starts listening for layers and outputs.
func (h *Hare) Start(ctx context.Context) error {
	{
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.cancel != nil {
			h.cancel()
		}
		h.ctx, h.cancel = context.WithCancel(ctx)
		ctx = h.ctx
	}
	h.WithContext(ctx).With().Info("starting protocol", log.String("protocol", pubsub.HareProtocol))

	// Create separate contexts for each subprocess. This allows us to better track the flow of messages.
	ctxBroker := log.WithNewSessionID(ctx, log.String("protocol", pubsub.HareProtocol+"_broker"))
	ctxTickLoop := log.WithNewSessionID(ctx, log.String("protocol", pubsub.HareProtocol+"_tickloop"))
	ctxOutputLoop := log.WithNewSessionID(ctx, log.String("protocol", pubsub.HareProtocol+"_outputloop"))
	ctxMalfLoop := log.WithNewSessionID(ctx, log.String("protocol", pubsub.HareProtocol+"_malfloop"))

	h.broker.Start(ctxBroker)

	h.eg.Go(func() error {
		h.tickLoop(ctxTickLoop)
		return nil
	})
	h.eg.Go(func() error {
		h.outputCollectionLoop(ctxOutputLoop)
		return nil
	})
	h.eg.Go(func() error {
		h.malfeasanceLoop(ctxMalfLoop)
		return nil
	})

	return nil
}

// Close sends a termination signal to hare goroutines and waits for their termination.
func (h *Hare) Close() {
	h.cancel()
	_ = h.eg.Wait()
}

func reportEquivocation(
	ctx context.Context,
	pubKey []byte,
	old, new *types.HareProofMsg,
	eligibility *types.HareEligibility,
	mch chan types.MalfeasanceGossip,
) error {
	gossip := types.MalfeasanceGossip{
		MalfeasanceProof: types.MalfeasanceProof{
			Layer: old.InnerMsg.Layer,
			Proof: types.Proof{
				Type: types.HareEquivocation,
				Data: &types.HareProof{
					Messages: [2]types.HareProofMsg{*old, *new},
				},
			},
		},
		Eligibility: &types.HareEligibilityGossip{
			Layer:       old.InnerMsg.Layer,
			Round:       old.InnerMsg.Round,
			PubKey:      pubKey,
			Eligibility: *eligibility,
		},
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case mch <- gossip:
	}
	return nil
}
