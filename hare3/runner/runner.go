package runner

import (
	"context"
	"fmt"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/spacemeshos/go-spacemesh/beacon"
	"github.com/spacemeshos/go-spacemesh/common/types"
	"github.com/spacemeshos/go-spacemesh/datastore"
	"github.com/spacemeshos/go-spacemesh/hare"
	heligibility "github.com/spacemeshos/go-spacemesh/hare/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3"
	"github.com/spacemeshos/go-spacemesh/hare3/broker"
	"github.com/spacemeshos/go-spacemesh/hare3/eligibility"
	"github.com/spacemeshos/go-spacemesh/hare3/weakcoin"
	"github.com/spacemeshos/go-spacemesh/log"
	"github.com/spacemeshos/go-spacemesh/syncer"
)

// WeakCoinResult is used to communicate the result of the weak coin back to
// the application, via a channel.
type WeakCoinResult struct {
	Layer types.LayerID
	// A nil coin value means that no coins were received.
	Coin *bool
}

// Protocol runner runs an instance of the hare3 protocol.
type ProtocolRunner struct {
	clock        RoundClock
	protocol     hare3.Protocol
	coinChooser  *weakcoin.Chooser
	maxRound     hare3.AbsRound
	gossiper     NetworkGossiper
	layer        types.LayerID
	ac           *eligibility.ActiveCheck
	weakcoinChan chan<- WeakCoinResult
}

func NewProtocolRunner(
	clock RoundClock,
	protocol *hare3.Protocol,
	coinChooser *weakcoin.Chooser,
	iterationLimit int8,
	gossiper NetworkGossiper,
	ac *eligibility.ActiveCheck,
	weakcoinChan chan<- WeakCoinResult,
) *ProtocolRunner {
	return &ProtocolRunner{
		clock:        clock,
		protocol:     *protocol,
		coinChooser:  coinChooser,
		maxRound:     hare3.NewAbsRound(iterationLimit, 0),
		gossiper:     gossiper,
		ac:           ac,
		weakcoinChan: weakcoinChan,
	}
}

// Run runs the protocol until it terminates and returns the result. If the
// protocol exceeded the iteration limit or the context expires an error will be
// returned and the result will be nil.
func (r *ProtocolRunner) Run(ctx context.Context) ([]types.Hash20, error) {
	// ok want to actually use a lock here
	for {
		round := r.protocol.Round()
		if round == r.maxRound {
			return nil, fmt.Errorf("hare protocol runner exceeded iteration limit of %d", r.maxRound.Iteration())
		}
		// The weak coin is calculated from pre-round messages, we select the
		// coin after one round, we are not concerned about late messages
		// affecting the outcome since the coin is weak (see package doc for
		// hare3/weakcoin for an explanation of weak)
		if round == hare3.Preround.Round()+1 {
			r.weakcoinChan <- WeakCoinResult{
				Layer: r.layer,
				Coin:  r.coinChooser.Choose(),
			}
		}
		select {
		// We await the beginning of the round, which is achieved by calling AwaitEndOfRound with (round - 1).
		case <-r.clock.AwaitEndOfRound(uint32(round - 1)):
			toSend, output := r.protocol.NextRound(r.ac.Active(ctx, round))
			if toSend != nil {
				msg, err := buildEncodedOutputMessage(toSend)
				if err != nil {
					// This should never happen
					panic(err)
				}
				r.gossiper.Gossip(msg)
			}
			if output != nil {
				return output, nil
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

type NetworkGossiper interface {
	Gossip(msg []byte) error
}

func buildEncodedOutputMessage(m *hare3.OutputMessage) ([]byte, error) {
	return nil, nil
}

// RoundClock is a timer interface.
type RoundClock interface {
	AwaitWakeup() <-chan struct{}
	// RoundEnd returns the time at which round ends, passing round-1 will
	// return the time at which round starts.
	RoundEnd(round uint32) time.Time
	AwaitEndOfRound(round uint32) <-chan struct{}
}

type LayerClock interface {
	LayerToTime(types.LayerID) time.Time
	AwaitLayer(types.LayerID) <-chan struct{}
	CurrentLayer() types.LayerID
}

type HareRunner struct {
	clock           LayerClock
	gossiper        NetworkGossiper
	oracle          *heligibility.Oracle
	syncer          *syncer.Syncer
	beaconRetriever *beacon.ProtocolDriver
	lc              hare3.LeaderChecker
	b               *broker.Broker
	l               log.Log
	db              *datastore.CachedDB
	output          chan hare.LayerOutput
	weakcoinChan    chan<- WeakCoinResult
	eg              errgroup.Group

	effectiveGenesis types.LayerID
	maxIterations    int8
	wakeupDelta      time.Duration
	roundDuration    time.Duration
	committeeSize    int
	nodeID           types.NodeID
}

func NewHareRunner(clock LayerClock,
	gossiper NetworkGossiper,
	oracle *heligibility.Oracle,
	syncer *syncer.Syncer,
	beaconRetriever *beacon.ProtocolDriver,
	lc hare3.LeaderChecker,
	b *broker.Broker,
	l log.Log,
	db *datastore.CachedDB,
	output chan hare.LayerOutput,
	weakcoinChan chan<- WeakCoinResult,
	effectiveGenesis types.LayerID,
	maxIterations int8,
	wakeupDelta time.Duration,
	roundDuration time.Duration,
	committeeSize int,
	nodeID types.NodeID,
) *HareRunner {
	return &HareRunner{
		clock:            clock,
		gossiper:         gossiper,
		oracle:           oracle,
		syncer:           syncer,
		beaconRetriever:  beaconRetriever,
		lc:               lc,
		b:                b,
		l:                l,
		db:               db,
		output:           output,
		weakcoinChan:     weakcoinChan,
		effectiveGenesis: effectiveGenesis,
		maxIterations:    maxIterations,
		wakeupDelta:      wakeupDelta,
		roundDuration:    roundDuration,
		committeeSize:    committeeSize,
		nodeID:           nodeID,
	}
}

func (r *HareRunner) Run(ctx context.Context) {
	// Ensure that we wait for any child routines to complete before returning.
	defer r.eg.Wait()
	currentLayer := r.clock.CurrentLayer()
	var startLayer types.LayerID
	if currentLayer < r.effectiveGenesis {
		startLayer = r.effectiveGenesis + 1
		r.l.WithContext(ctx).Info(
			"hare runner awaiting start layer",
			log.Stringer("current_layer", currentLayer),
			log.Stringer("effective_genesis", r.effectiveGenesis),
			log.Stringer("start_layer", r.effectiveGenesis))
	} else {
		// We wait for the subsequent layer so that we can be sure of being on time.
		startLayer = currentLayer + 1
		r.l.WithContext(ctx).Info(
			"hare runner awaiting start layer",
			log.Stringer("current_layer", currentLayer),
			log.Stringer("start_layer", startLayer))
	}

	for l := startLayer; ; l += 1 {
		// copy the loop variable, it's used in a go-routine below.
		layer := l
		select {
		case <-r.clock.AwaitLayer(layer):
			if !r.syncer.IsSynced(ctx) {
				// if not currently synced don't start consensus process.
				r.l.With().Info("not starting hare: node not synced",
					log.Context(ctx),
					layer)
				continue
			}

			// We construct a proof to see if we are eligible, for each round.
			// we will need the eligibility oracle at least.

			// It seems like it could be possible for the beacon to be synced
			// slowly as it can be reconstructed piecemeal from smeshers
			// ballots. Well in fact ReportBeaconFromBallot takes ballots from
			// any layer in the epoch and then adds up the weight for that
			// ballot value, and when the threshold is crossed we consider the
			// beacon decided. But the actual value of the beacon comes from
			// the first ballot (the reference ballot) and so we can end up
			// with multiple ballots from the same smesher pushing their
			// beacon value over the threshold. It seems what we really want is
			// to get all the ref ballots and take the majority there (by
			// storing them), oh we can't get all the ref ballots because they
			// don't actually exist at the beginning of the epoch, you only get
			// 50 per layer, but we could store all the final messages from
			// beacon.
			// TODO ask in chat.
			//
			// Also I thought that the beacon protocol did not take into
			// account weight, so it seems strange to base the syncing around
			// weight. Actually I think this is wrong, votes in beacon are
			// weighted.
			//
			// So we will just try every time, this could be improved at least
			// in logging to change the message if this is really taking too
			// long, with the default settings it shouldn't take longer than 16
			// Layers to sync. Really we want to be notified of the beacon
			// arriving.
			beacon, err := r.beaconRetriever.GetBeacon(layer.GetEpoch())
			if err != nil {
				r.l.With().Info("no beacon for epoch",
					log.Context(ctx),
					layer,
				)
				continue
			}
			props := goodProposals(ctx, r.l, r.db, r.nodeID, layer, beacon)

			// Execute layer
			r.eg.Go(func() error {
				result, err := r.runLayer(log.WithNewSessionID(ctx), layer, props)
				// We log the error, which is either ctx timeout or iteration exceeded
				if err != nil {
					r.l.With().Info("hare terminated without agreement", layer, log.Err(err))
				} else {
					select {
					// Send the result
					case r.output <- hare.LayerOutput{
						Ctx:       ctx,
						Layer:     layer,
						Proposals: result,
					}:
					case <-ctx.Done():
						return nil
					}
				}
				return nil
			})
		case <-ctx.Done():
			return
		}
	}
}

// runLayer constructs a ProtocolRunner and returns the output of its Run method.
func (r *HareRunner) runLayer(ctx context.Context, layer types.LayerID, props []types.ProposalID) ([]types.ProposalID, error) {
	// The broker may have already created a handler and coinChooser in
	// response to early messages, that's why we get them from the broker.
	handler, coinChooser := r.b.Register(ctx, layer)
	roundClock := hare.NewSimpleRoundClock(r.clock.LayerToTime(layer), r.wakeupDelta, r.roundDuration)

	initialSet := make([]types.Hash20, len(props))
	for i := 0; i < len(props); i++ {
		initialSet[i] = types.Hash20(props[i])
	}
	ac := eligibility.NewActiveCheck(layer, r.nodeID, r.committeeSize, r.oracle, r.l)
	protocolRunner := NewProtocolRunner(roundClock, handler.Protocol(r.lc, initialSet), coinChooser, r.maxIterations, r.gossiper, ac, r.weakcoinChan)
	v, err := protocolRunner.Run(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]types.ProposalID, len(v))
	for i := 0; i < len(v); i++ {
		result[i] = types.ProposalID(v[i])
	}
	return result, nil
}
